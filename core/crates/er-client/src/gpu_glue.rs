//! Windows：DXGI 采集纹理 → NVENC 零拷贝编码的胶水层。
//!
//! 决策（要不要走 GPU、什么情况下放弃）在 `gpu_policy::GpuPath` 里，
//! 这里只负责「按策略调用真实 API」，并把失败翻译成 `GpuFallback`。

use crate::gpu_policy::{GpuFallback, GpuPath};
use er_capture::ScreenCapture;
use er_codec::nvenc::NvencEncoder;
use er_codec::EncodeConfig;

pub(crate) struct GpuGlue {
    enc: Option<NvencEncoder>,
    /// 「无新帧」日志只打一次
    warned_idle: bool,
}

impl GpuGlue {
    pub(crate) fn new() -> Self {
        Self { enc: None, warned_idle: false }
    }

    /// ABR 改档后必须丢掉旧会话：NVENC 会话的输入纹理尺寸在初始化时就固定了，
    /// 换分辨率只能重建（D3D11 那边的输入纹理由 `ensure_encoder_input` 按新尺寸重开）。
    pub(crate) fn reset(&mut self) {
        self.enc = None;
        self.warned_idle = false;
    }

    /// 尝试走一次零拷贝：
    /// - `Ok(Some(bytes))` 编码成功
    /// - `Ok(None)` 桌面没有新帧（不改变策略状态，交给 CPU 路径兜底抓一帧）
    /// - `Err(reason)` 这条路径当前不可用，调用方应回退并把 reason 交给策略
    pub(crate) fn try_encode(
        &mut self,
        cap: &mut ScreenCapture,
        cfg: &EncodeConfig,
        policy: &mut GpuPath,
    ) -> Result<Option<Vec<u8>>, GpuFallback> {
        if !policy.should_try() {
            return Ok(None);
        }
        let Some(dxgi) = cap.dxgi_mut() else {
            return Err(GpuFallback::NotAvailable("no dxgi backend".into()));
        };

        // 1) 取一帧（不做 CPU 拷贝）
        //    静态桌面按设计不出帧：先补一次零位移输入（不移动光标）再取，
        //    仍没有就返回 Ok(None) 交给上层，**不**因此判定 GPU 路径失效。
        let first = dxgi
            .grab_gpu(0)
            .map_err(|e| GpuFallback::NotAvailable(format!("grab_gpu: {e}")))?;
        let frame = match first {
            Some(f) => f,
            None => {
                let _ = dxgi.nudge_desktop_zero();
                std::thread::sleep(std::time::Duration::from_millis(4));
                match dxgi.grab_gpu(30) {
                    Ok(Some(f)) => f,
                    Ok(None) => {
                        // 只在第一次打，避免每帧刷屏；否则会出现「日志里 backend=cpu
                        // 却不知道为什么」这种最难查的情况
                        if !self.warned_idle {
                            self.warned_idle = true;
                            eprintln!(
                                "[er] GPU 路径：补零位移输入后仍无新帧（静止桌面？），本帧走 CPU"
                            );
                        }
                        return Ok(None);
                    }
                    Err(e) => return Err(GpuFallback::NotAvailable(format!("grab_gpu: {e}"))),
                }
            }
        };

        // 2) 尺寸必须与编码配置一致，否则 NVENC 会拒帧/出花屏
        policy.check_dims((cfg.width, cfg.height), (frame.width, frame.height))?;

        // 3) 首次：在同一块 D3D11 device 上开 NVENC 会话，注册常驻输入纹理
        if self.enc.is_none() {
            let device = dxgi.device_raw();
            let mut enc = NvencEncoder::new_d3d11(*cfg, device)
                .map_err(|e| GpuFallback::NotAvailable(format!("nvenc d3d11: {e}")))?;
            let tex = dxgi
                .ensure_encoder_input(cfg.width, cfg.height)
                .map_err(|e| GpuFallback::RegisterFailed(format!("encoder input texture: {e}")))?;
            enc.register_d3d11_input(tex)
                .map_err(|e| GpuFallback::RegisterFailed(e.to_string()))?;
            self.enc = Some(enc);
            policy.mark_registered();
        }

        // 4) 纯 GPU CopyResource：桌面纹理 → NVENC 输入纹理
        dxgi.copy_held_to_encoder_input()
            .map_err(|e| GpuFallback::EncodeFailed(format!("copy resource: {e}")))?;

        // 5) 编码（take/放回是为了绕开 &mut self 与 dxgi 借用冲突）
        let mut enc = self.enc.take().expect("just ensured");
        let out = enc.encode_d3d11_registered(now_ms());
        self.enc = Some(enc);
        out.map(Some).map_err(|e| GpuFallback::EncodeFailed(e.to_string()))
    }
}

fn now_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}
