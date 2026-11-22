//! QUIC 实现：真正的端到端传输，可跨进程/跨机。
//!
//! 与 MemoryTransport 实现同一个 `Transport` trait，上层无需感知差异。
//! 通道映射：视频/音频走 QUIC DATAGRAM（不可靠），控制/数据走单向 Stream（可靠）。

use std::net::SocketAddr;
use std::sync::Arc;

use bytes::Bytes;
use er_common::protocol::MediaHeader;
use quinn::{ClientConfig, Connection, Endpoint, ServerConfig};
use tokio::io::AsyncReadExt;

use crate::{Channel, Frame, Stats, Transport, TransportError};

/// QUIC 传输的错误。
#[derive(Debug)]
pub enum QuicError {
    Io(std::io::Error),
    Connect(quinn::ConnectError),
    Connection(quinn::ConnectionError),
    Tls(String),
}

impl std::fmt::Display for QuicError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            QuicError::Io(e) => write!(f, "io: {e}"),
            QuicError::Connect(e) => write!(f, "connect: {e}"),
            QuicError::Connection(e) => write!(f, "connection: {e}"),
            QuicError::Tls(e) => write!(f, "tls: {e}"),
        }
    }
}

impl std::error::Error for QuicError {}

/// 生成本地自签证书（返回服务端配置与证书 DER）。
fn make_server_config() -> Result<(ServerConfig, Vec<u8>), QuicError> {
    let cert = rcgen::generate_simple_self_signed(vec!["easyremote.local".to_string()])
        .map_err(|e| QuicError::Tls(e.to_string()))?;
    let cert_der = cert.cert.der().to_vec();
    let key_der = cert.key_pair.serialize_der();

    let cfg = ServerConfig::with_single_cert(
        vec![rustls::pki_types::CertificateDer::from(cert_der.clone())],
        rustls::pki_types::PrivateKeyDer::Pkcs8(key_der.into()),
    )
    .map_err(|e| QuicError::Tls(e.to_string()))?;
    Ok((cfg, cert_der))
}

/// 接受指定的自签证书。
///
/// 用于自托管/本地联调：证书由对端在带外提供（例如扫码或配置）。
/// 生产环境应改为连接真实 CA 签发的证书。
#[derive(Debug)]
struct PinnedCertVerifier {
    cert: rustls::pki_types::CertificateDer<'static>,
}

impl rustls::client::danger::ServerCertVerifier for PinnedCertVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &rustls::pki_types::CertificateDer<'_>,
        _intermediates: &[rustls::pki_types::CertificateDer<'_>],
        _server_name: &rustls::pki_types::ServerName<'_>,
        _ocsp: &[u8],
        _now: rustls::pki_types::UnixTime,
    ) -> Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        if end_entity.as_ref() == self.cert.as_ref() {
            Ok(rustls::client::danger::ServerCertVerified::assertion())
        } else {
            Err(rustls::Error::General("certificate mismatch".into()))
        }
    }

    fn verify_tls12_signature(
        &self,
        _message: &[u8],
        _cert: &rustls::pki_types::CertificateDer<'_>,
        _dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        Ok(rustls::client::danger::HandshakeSignatureValid::assertion())
    }

    fn verify_tls13_signature(
        &self,
        _message: &[u8],
        _cert: &rustls::pki_types::CertificateDer<'_>,
        _dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        Ok(rustls::client::danger::HandshakeSignatureValid::assertion())
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        vec![
            rustls::SignatureScheme::ECDSA_NISTP256_SHA256,
            rustls::SignatureScheme::ED25519,
            rustls::SignatureScheme::RSA_PSS_SHA256,
        ]
    }
}

fn make_client_config(cert_der: Vec<u8>) -> Result<ClientConfig, QuicError> {
    let cert = rustls::pki_types::CertificateDer::from(cert_der);
    let crypto = rustls::ClientConfig::builder()
        .dangerous()
        .with_custom_certificate_verifier(Arc::new(PinnedCertVerifier { cert }))
        .with_no_client_auth();
    Ok(ClientConfig::new(Arc::new(
        quinn::crypto::rustls::QuicClientConfig::try_from(crypto)
            .map_err(|e| QuicError::Tls(e.to_string()))?,
    )))
}

/// 监听端：先 bind 拿到地址与证书，再 accept。
///
/// 拆开的原因：旧版 `accept_one` 阻塞等待连接才返回地址，
/// 而客户端又要先拿到地址才能连接 —— 测试里直接死锁。
pub struct QuicListener {
    endpoint: Endpoint,
    local_addr: SocketAddr,
    cert_der: Vec<u8>,
}

impl QuicListener {
    /// 绑定端口并生成自签证书。
    pub fn bind(bind: SocketAddr) -> Result<Self, QuicError> {
        let (server_cfg, cert_der) = make_server_config()?;
        let endpoint = Endpoint::server(server_cfg, bind).map_err(QuicError::Io)?;
        let local_addr = endpoint.local_addr().map_err(QuicError::Io)?;
        Ok(Self {
            endpoint,
            local_addr,
            cert_der,
        })
    }

    pub fn local_addr(&self) -> SocketAddr {
        self.local_addr
    }

    /// 证书 DER，带外交给客户端做 pin。
    pub fn cert_der(&self) -> &[u8] {
        &self.cert_der
    }

    /// 接受一个连接。
    pub async fn accept(&self) -> Result<QuicTransport, QuicError> {
        let incoming = self
            .endpoint
            .accept()
            .await
            .ok_or_else(|| QuicError::Tls("endpoint closed".into()))?;
        let conn = incoming.await.map_err(QuicError::Connection)?;
        Ok(QuicTransport::from_connection(conn))
    }
}

/// 可靠发送队列容量。满了返回 Backpressure，绝不无限增长。
const RELIABLE_QUEUE_CAP: usize = 256;

/// QUIC 传输。
///
/// 可靠性说明：quinn 的 `open_uni` 是异步 API，而 `Transport::send_reliable`
/// 是同步签名，因此这里用「有序队列 + 后台写任务」保持顺序且不阻塞调用方。
pub struct QuicTransport {
    /// 必须持有 Endpoint：quinn 的连接依赖 endpoint 存活，
    /// 一旦 endpoint 被 drop，连接会立即关闭（曾导致端到端测试挂死）。
    /// 由 QuicListener 接受而来的连接，endpoint 由 listener 持有。
    _endpoint: Option<Endpoint>,
    conn: Connection,
    stats: std::sync::Arc<std::sync::Mutex<Stats>>,
    max_datagram: usize,
    control_tx: Option<tokio::sync::mpsc::Sender<Vec<u8>>>,
    data_tx: Option<tokio::sync::mpsc::Sender<Vec<u8>>>,
    reliable_rx: Option<tokio::sync::mpsc::Receiver<Frame>>,
    /// 已入队但尚未写完的可靠帧数，供 flush 等待。
    pending_reliable: std::sync::Arc<std::sync::atomic::AtomicUsize>,
}

impl QuicTransport {
    /// 启动服务端并等待一个连接，返回本地地址、传输实例、证书 DER。
    pub async fn accept_one(bind: SocketAddr) -> Result<(SocketAddr, Self, Vec<u8>), QuicError> {
        let listener = QuicListener::bind(bind)?;
        let addr = listener.local_addr();
        let cert = listener.cert_der().to_vec();
        let t = listener.accept().await?;
        Ok((addr, t, cert))
    }

    /// 作为客户端连接服务端。
    pub async fn connect(
        bind: SocketAddr,
        server: SocketAddr,
        cert_der: Vec<u8>,
    ) -> Result<Self, QuicError> {
        let mut endpoint = Endpoint::client(bind).map_err(QuicError::Io)?;
        endpoint.set_default_client_config(make_client_config(cert_der)?);
        let conn = endpoint
            .connect(server, "easyremote.local")
            .map_err(QuicError::Connect)?
            .await
            .map_err(QuicError::Connection)?;
        let max = conn.max_datagram_size().unwrap_or(1200);
        let stats = std::sync::Arc::new(std::sync::Mutex::new(Stats::default()));
        let rx = spawn_reader(conn.clone(), stats.clone());
        Ok(Self {
            _endpoint: Some(endpoint),
            conn,
            stats,
            max_datagram: max,
            control_tx: None,
            data_tx: None,
            reliable_rx: Some(rx),
            pending_reliable: std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0)),
        })
    }

    /// 单帧上限（受 MTU 约束）。
    pub fn max_datagram_size(&self) -> usize {
        self.max_datagram
    }

    /// 当前 QUIC 连接的平滑 RTT（诊断/ADR 用）。
    pub fn rtt(&self) -> std::time::Duration {
        self.conn.rtt()
    }

    /// 对端地址。
    pub fn remote_address(&self) -> SocketAddr {
        self.conn.remote_address()
    }

    /// 由已建立的连接构造（listener 侧）。
    pub fn from_connection(conn: Connection) -> Self {
        let max = conn.max_datagram_size().unwrap_or(1200);
        let stats = std::sync::Arc::new(std::sync::Mutex::new(Stats::default()));
        let rx = spawn_reader(conn.clone(), stats.clone());
        Self {
            _endpoint: None,
            conn,
            stats,
            max_datagram: max,
            control_tx: None,
            data_tx: None,
            reliable_rx: Some(rx),
            pending_reliable: std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0)),
        }
    }

    /// 控制/数据各自使用一条长驻流，保证可靠有序且互不阻塞。
    fn ensure_writer(&mut self, channel: Channel) {
        let target = if channel == Channel::Data {
            &mut self.data_tx
        } else {
            &mut self.control_tx
        };
        if target.is_some() {
            return;
        }
        let (tx, mut rx) = tokio::sync::mpsc::channel::<Vec<u8>>(RELIABLE_QUEUE_CAP);
        let conn = self.conn.clone();
        let stats = self.stats.clone();
        let pending = self.pending_reliable.clone();
        tokio::spawn(async move {
            let result = if channel == Channel::Data {
                match conn.open_bi().await {
                    Ok((mut send, _recv)) => {
                        let _ = send.write_all(&[channel as u8]).await;
                        Some(send)
                    }
                    Err(_) => None,
                }
            } else {
                match conn.open_uni().await {
                    Ok(mut send) => {
                        let _ = send.write_all(&[channel as u8]).await;
                        Some(send)
                    }
                    Err(_) => None,
                }
            };
            let Some(mut send) = result else {
                return;
            };
            while let Some(buf) = rx.recv().await {
                let len = (buf.len() as u32).to_be_bytes();
                let ok = async {
                    send.write_all(&len).await.ok()?;
                    send.write_all(&buf).await.ok()?;
                    Some(())
                }
                .await
                .is_some();
                if let Ok(mut st) = stats.lock() {
                    if ok {
                        st.sent_reliable += 1;
                        st.bytes_sent += buf.len() as u64;
                    } else {
                        st.dropped_datagrams += 1;
                    }
                }
                pending.fetch_sub(1, std::sync::atomic::Ordering::SeqCst);
            }
        });
        *target = Some(tx);
    }

    /// 等待已入队的可靠帧全部写完（最多等待 timeout）。
    ///
    /// 关闭连接前必须调用，否则排队中的控制帧会随连接销毁而丢失。
    pub async fn flush(&self, timeout: std::time::Duration) -> bool {
        let deadline = tokio::time::Instant::now() + timeout;
        while self
            .pending_reliable
            .load(std::sync::atomic::Ordering::SeqCst)
            > 0
        {
            if tokio::time::Instant::now() >= deadline {
                return false;
            }
            tokio::time::sleep(std::time::Duration::from_millis(5)).await;
        }
        true
    }

    /// 主动关闭。
    pub fn shutdown(&self) {
        self.conn.close(0u32.into(), b"bye");
    }

    /// 异步接收一帧不可靠数据（视频/音频）。
    pub async fn recv_datagram(&mut self) -> Option<Frame> {
        let datagram = self.conn.read_datagram().await.ok()?;
        if datagram.len() < 8 {
            return None;
        }
        let header = MediaHeader::decode(&datagram[..8])?;
        let payload = datagram[8..].to_vec();
        if let Ok(mut s) = self.stats.lock() {
            s.recv_frames += 1;
            s.bytes_recv += datagram.len() as u64;
        }
        let channel = if header.frame_type == 2 {
            Channel::Audio
        } else {
            Channel::Video
        };
        Some(Frame {
            channel,
            header: Some(header),
            payload,
        })
    }

    /// 异步接收一条可靠流（控制/数据）。
    pub async fn recv_reliable(&mut self) -> Option<Frame> {
        self.reliable_rx.as_mut()?.recv().await
    }
}

fn spawn_reader(
    conn: Connection,
    stats: std::sync::Arc<std::sync::Mutex<Stats>>,
) -> tokio::sync::mpsc::Receiver<Frame> {
    let (tx, rx) = tokio::sync::mpsc::channel::<Frame>(256);
    tokio::spawn(async move {
        loop {
            tokio::select! {
                uni = conn.accept_uni() => match uni {
                    Ok(stream) => {
                        let tx = tx.clone();
                        let stats = stats.clone();
                        tokio::spawn(read_stream(stream, tx, stats));
                    }
                    Err(_) => break,
                },
                bi = conn.accept_bi() => match bi {
                    Ok((_send, stream)) => {
                        let tx = tx.clone();
                        let stats = stats.clone();
                        tokio::spawn(read_stream(stream, tx, stats));
                    }
                    Err(_) => break,
                },
                _ = conn.closed() => break,
            }
        }
    });
    rx
}

async fn read_stream<S>(
    mut stream: S,
    tx: tokio::sync::mpsc::Sender<Frame>,
    stats: std::sync::Arc<std::sync::Mutex<Stats>>,
) where
    S: tokio::io::AsyncRead + Unpin + Send + 'static,
{
    let mut ch = [0u8; 1];
    if stream.read_exact(&mut ch).await.is_err() {
        return;
    }
    let channel = match ch[0] {
        2 => Channel::Control,
        3 => Channel::Data,
        _ => return,
    };
    loop {
        let mut len = [0u8; 4];
        if stream.read_exact(&mut len).await.is_err() {
            break;
        }
        let n = u32::from_be_bytes(len) as usize;
        if n > 16 * 1024 * 1024 {
            break;
        }
        let mut buf = vec![0u8; n];
        if stream.read_exact(&mut buf).await.is_err() {
            break;
        }
        if let Ok(mut s) = stats.lock() {
            s.recv_frames += 1;
            s.bytes_recv += n as u64;
        }
        if tx
            .send(Frame {
                channel,
                header: None,
                payload: buf,
            })
            .await
            .is_err()
        {
            break;
        }
    }
}

impl Transport for QuicTransport {
    fn send_datagram(
        &mut self,
        channel: Channel,
        header: MediaHeader,
        payload: Vec<u8>,
    ) -> Result<(), TransportError> {
        if channel.is_reliable() {
            return Err(TransportError::Backpressure);
        }
        let mut buf = Vec::with_capacity(8 + payload.len());
        buf.extend_from_slice(&header.encode());
        buf.extend_from_slice(&payload);
        // 超过一个 datagram 就丢弃：绝不排队、不分片（PRD「帧不排队」）
        if buf.len() > self.max_datagram {
            if let Ok(mut s) = self.stats.lock() {
                s.dropped_datagrams += 1;
            }
            return Err(TransportError::TooLarge);
        }
        self.conn
            .send_datagram(Bytes::from(buf))
            .map_err(|_| TransportError::Closed)?;
        if let Ok(mut s) = self.stats.lock() {
            s.sent_datagrams += 1;
            s.bytes_sent += (8 + payload.len()) as u64;
        }
        Ok(())
    }

    fn send_reliable(&mut self, channel: Channel, payload: Vec<u8>) -> Result<(), TransportError> {
        if !channel.is_reliable() {
            return Err(TransportError::Backpressure);
        }
        self.ensure_writer(channel);
        let tx = if channel == Channel::Data {
            self.data_tx.as_ref()
        } else {
            self.control_tx.as_ref()
        };
        let Some(tx) = tx else {
            return Err(TransportError::Closed);
        };
        match tx.try_send(payload) {
            Ok(()) => {
                self.pending_reliable
                    .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                Ok(())
            }
            // 队列满：明确返回背压，而不是静默丢弃或无限堆内存
            Err(tokio::sync::mpsc::error::TrySendError::Full(_)) => {
                Err(TransportError::Backpressure)
            }
            Err(tokio::sync::mpsc::error::TrySendError::Closed(_)) => Err(TransportError::Closed),
        }
    }

    fn recv(&mut self) -> Option<Frame> {
        // QUIC 必须异步接收；同步接口见 recv_datagram / recv_reliable
        None
    }

    fn stats(&self) -> Stats {
        self.stats.lock().map(|s| *s).unwrap_or_default()
    }

    fn close(&mut self) {
        self.shutdown();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    fn hdr(seq: u16) -> MediaHeader {
        MediaHeader {
            frame_type: 1,
            stream_id: 0,
            seq,
            flags: 0,
            ts_ms: 0,
        }
    }

    /// 端到端：真实 QUIC 连接，双向往返数据报与可靠流。
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn quic_end_to_end() {
        // 服务端先起，通过 oneshot 把地址与证书交给客户端
        let (tx, rx) = tokio::sync::oneshot::channel::<(SocketAddr, Vec<u8>)>();
        let server = tokio::spawn(async move {
            let bind: SocketAddr = "127.0.0.1:0".parse().unwrap();
            let listener = QuicListener::bind(bind).unwrap();
            let addr = listener.local_addr();
            let cert = listener.cert_der().to_vec();
            // 先交出地址与证书，再等待连接（否则客户端无从连接 → 死锁）
            tx.send((addr, cert)).unwrap();
            let mut t = listener.accept().await.unwrap();

            // 收数据报
            let d = tokio::time::timeout(Duration::from_secs(5), t.recv_datagram())
                .await
                .expect("datagram timeout")
                .expect("datagram");
            // 收可靠流
            let r = tokio::time::timeout(Duration::from_secs(5), t.recv_reliable())
                .await
                .expect("reliable timeout")
                .expect("reliable");

            // 回一条可靠流，并等待真正写完（否则 transport 析构会丢掉它）
            t.send_reliable(Channel::Control, b"pong".to_vec()).unwrap();
            assert!(t.flush(Duration::from_secs(3)).await, "flush timeout");
            // 等客户端读走再关闭
            tokio::time::sleep(Duration::from_millis(100)).await;
            (d, r)
        });

        let (addr, cert) = rx.await.unwrap();
        let bind: SocketAddr = "127.0.0.1:0".parse().unwrap();
        let mut client = QuicTransport::connect(bind, addr, cert).await.unwrap();

        client
            .send_datagram(Channel::Video, hdr(7), vec![1, 2, 3])
            .unwrap();
        client
            .send_reliable(Channel::Control, b"ping".to_vec())
            .unwrap();

        let pong = tokio::time::timeout(Duration::from_secs(5), client.recv_reliable())
            .await
            .expect("pong timeout")
            .expect("pong");
        assert_eq!(pong.payload, b"pong");
        assert_eq!(pong.channel, Channel::Control);

        let (d, r) = server.await.unwrap();
        assert_eq!(d.payload, vec![1, 2, 3]);
        assert_eq!(d.header.unwrap().seq, 7);
        assert_eq!(r.payload, b"ping");
    }

    #[test]
    fn rejects_oversized_datagram() {
        // 用 MemoryTransport 的边界语义校验同一约束
        let mut t = crate::MemoryTransport::new(4, 16);
        assert_eq!(
            t.send_datagram(Channel::Video, hdr(1), vec![0u8; 32]),
            Err(TransportError::TooLarge)
        );
    }
    /// 控制必须可靠有序，且不被视频丢包/拥塞阻塞。
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn control_ordered_under_video_flood() {
        let (tx, rx) = tokio::sync::oneshot::channel::<(SocketAddr, Vec<u8>)>();
        let server = tokio::spawn(async move {
            let listener = QuicListener::bind("127.0.0.1:0".parse().unwrap()).unwrap();
            let addr = listener.local_addr();
            let cert = listener.cert_der().to_vec();
            tx.send((addr, cert)).unwrap();
            let mut t = listener.accept().await.unwrap();
            let mut got = Vec::new();
            while got.len() < 10 {
                if let Ok(Some(f)) =
                    tokio::time::timeout(Duration::from_secs(5), t.recv_reliable()).await
                {
                    if f.channel == Channel::Control {
                        got.push(String::from_utf8(f.payload).unwrap());
                    }
                }
            }
            got
        });
        let (addr, cert) = rx.await.unwrap();
        let mut client = QuicTransport::connect("127.0.0.1:0".parse().unwrap(), addr, cert)
            .await
            .unwrap();
        for i in 0..300u16 {
            let _ = client.send_datagram(Channel::Video, hdr(i), vec![0u8; 900]);
        }
        for i in 0..10 {
            client
                .send_reliable(Channel::Control, format!("c{i}").into_bytes())
                .unwrap();
        }
        let got = tokio::time::timeout(Duration::from_secs(5), server)
            .await
            .expect("timeout")
            .unwrap();
        let want: Vec<String> = (0..10).map(|i| format!("c{i}")).collect();
        assert_eq!(got, want, "control stream must stay ordered");
    }

    /// 数据走独立 bidi stream：有背压，但不阻塞控制。
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn data_backpressure_does_not_block_control() {
        let (tx, rx) = tokio::sync::oneshot::channel::<(SocketAddr, Vec<u8>)>();
        let server = tokio::spawn(async move {
            let listener = QuicListener::bind("127.0.0.1:0".parse().unwrap()).unwrap();
            let addr = listener.local_addr();
            let cert = listener.cert_der().to_vec();
            tx.send((addr, cert)).unwrap();
            let mut t = listener.accept().await.unwrap();
            let mut data = Vec::new();
            let mut ctrl = Vec::new();
            while data.is_empty() || ctrl.is_empty() {
                if let Ok(Some(f)) =
                    tokio::time::timeout(Duration::from_secs(5), t.recv_reliable()).await
                {
                    match f.channel {
                        Channel::Data => data.push(f.payload),
                        Channel::Control => ctrl.push(f.payload),
                        _ => {}
                    }
                }
            }
            (data, ctrl)
        });
        let (addr, cert) = rx.await.unwrap();
        let mut client = QuicTransport::connect("127.0.0.1:0".parse().unwrap(), addr, cert)
            .await
            .unwrap();
        client
            .send_reliable(Channel::Data, b"hello".to_vec())
            .unwrap();
        let mut backpressure = 0;
        for _ in 0..1000 {
            if client
                .send_reliable(Channel::Data, vec![7u8; 1024])
                .is_err()
            {
                backpressure += 1;
            }
        }
        assert!(backpressure > 0, "data queue must apply backpressure");
        client
            .send_reliable(Channel::Control, b"ping".to_vec())
            .unwrap();
        let (data, ctrl) = tokio::time::timeout(Duration::from_secs(5), server)
            .await
            .expect("timeout")
            .unwrap();
        assert!(data.iter().any(|p| p == b"hello"));
        assert_eq!(ctrl, vec![b"ping".to_vec()]);
    }
}
