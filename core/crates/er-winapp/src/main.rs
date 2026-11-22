//! Windows 主控端 GUI（egui）：用 Windows 远控 macOS。
//!
//! 这是给 Windows 用户双击运行的 app：填设备码 + 验证码 → 连接 → 看远端画面，
//! 鼠标/键盘实时转发到被控端（mac_host）。
//!
//! 为什么用 egui 而不是 Flutter：验证机上只有 Rust + MSVC，没有 Flutter/CMake；
//! egui 是纯 Rust，cargo build 直接出 exe，不需要额外 SDK。
//!
//! 网络线程与 UI 线程分离：解码 1080p 一帧约 90ms，放 UI 线程会卡死交互。

use std::net::SocketAddr;
use std::sync::mpsc::{channel, Receiver, Sender, TryRecvError};
use std::time::{Duration, Instant};

use er_client::agent::ControllerAgent;
use er_client::link::VideoHeader;
use er_codec::H264Decoder;
use er_input::{InputEvent, MouseButton};

const MSG_VIDEO: u8 = er_client::agent::MSG_VIDEO;

/// UI → 网络线程
enum NetCmd {
    Input(InputEvent),
    Disconnect,
}

/// 网络线程 → UI
enum NetEvent {
    Status(String),
    Ready,
    Frame {
        width: usize,
        height: usize,
        rgb: Vec<u8>,
    },
    Stats {
        frames: u64,
        kb: u64,
        fps: f32,
    },
    Failed(String),
}

fn main() -> eframe::Result<()> {
    let opts = eframe::NativeOptions {
        // 注意 DPI：验证机是 175% 缩放，逻辑 1280x860 = 物理 2240x1505，远超 1646x926 屏幕。
        // 窗口尺寸超出显示区域时 winit 在 Windows 上会得到一个 14x14 的畸形窗口（实测），
        // 所以这里用「最大化」交给系统决定，不硬编码像素尺寸。
        viewport: egui::ViewportBuilder::default()
            .with_maximized(true)
            .with_title("EasyRemote · Windows 主控端"),
        ..Default::default()
    };
    eframe::run_native(
        "EasyRemote",
        opts,
        Box::new(|cc| Ok(Box::new(App::new(cc)))),
    )
}

struct App {
    gateway: String,
    relay: String,
    phone: String,
    device_code: String,
    captcha: String,
    status: String,
    connected: bool,
    connecting: bool,
    texture: Option<egui::TextureHandle>,
    frame_size: (usize, usize),
    stats: String,
    cmd_tx: Option<Sender<NetCmd>>,
    evt_rx: Option<Receiver<NetEvent>>,
    last_pointer: Option<(i32, i32)>,
    last_pointer_at: Instant,
    last_mods: (bool, bool, bool, bool),
    want_repaint: bool,
    /// er.conf 里 auto_connect=1：第一帧自动发起连接（无人值守/演示用）
    auto_connect: bool,
}

impl App {
    fn new(cc: &eframe::CreationContext<'_>) -> Self {
        cc.egui_ctx.set_visuals(egui::Visuals::dark());
        install_cjk_font(&cc.egui_ctx);
        let conf = read_conf();
        let mut app = Self {
            gateway: "http://127.0.0.1:18090".to_string(),
            relay: "127.0.0.1:19000".to_string(),
            phone: "13700008102".to_string(),
            device_code: String::new(),
            captcha: String::new(),
            status: "未连接".to_string(),
            connected: false,
            connecting: false,
            texture: None,
            frame_size: (0, 0),
            stats: String::new(),
            cmd_tx: None,
            evt_rx: None,
            last_pointer: None,
            last_pointer_at: Instant::now(),
            last_mods: (false, false, false, false),
            want_repaint: false,
            auto_connect: false,
        };
        if let Some(v) = conf.gateway {
            app.gateway = v;
        }
        if let Some(v) = conf.relay {
            app.relay = v;
        }
        if let Some(v) = conf.phone {
            app.phone = v;
        }
        if let Some(v) = conf.device_code {
            app.device_code = v;
        }
        if let Some(v) = conf.captcha {
            app.captcha = v;
        }
        if conf.auto_connect && !app.device_code.is_empty() {
            app.auto_connect = true;
            app.status = "自动连接中…".to_string();
        }
        app
    }

    fn connect(&mut self, ctx: &egui::Context) {
        if self.connecting || self.connected {
            return;
        }
        let (cmd_tx, cmd_rx) = channel::<NetCmd>();
        let (evt_tx, evt_rx) = channel::<NetEvent>();
        self.cmd_tx = Some(cmd_tx);
        self.evt_rx = Some(evt_rx);
        self.connecting = true;
        self.status = "连接中…".to_string();
        self.texture = None;
        self.frame_size = (0, 0);
        self.stats.clear();

        let gateway = self.gateway.trim().to_string();
        let relay = self.relay.trim().to_string();
        let phone = self.phone.trim().to_string();
        let code = self.device_code.trim().to_string();
        let captcha = self.captcha.trim().to_string();
        let ctx2 = ctx.clone();
        std::thread::spawn(move || {
            net_thread(gateway, relay, phone, code, captcha, cmd_rx, evt_tx, ctx2);
        });
    }

    fn disconnect(&mut self) {
        if let Some(tx) = &self.cmd_tx {
            let _ = tx.send(NetCmd::Disconnect);
        }
    }

    fn pump_events(&mut self, ctx: &egui::Context) {
        let Some(rx) = &self.evt_rx else { return };
        loop {
            match rx.try_recv() {
                Ok(NetEvent::Status(s)) => self.status = s,
                Ok(NetEvent::Ready) => {
                    self.connected = true;
                    self.connecting = false;
                    self.status = "已连接 · 等待画面…".to_string();
                }
                Ok(NetEvent::Frame { width, height, rgb }) => {
                    if width == 0 || height == 0 {
                        continue;
                    }
                    let img = egui::ColorImage::from_rgb([width, height], &rgb);
                    self.texture =
                        Some(ctx.load_texture("remote", img, egui::TextureOptions::LINEAR));
                    self.frame_size = (width, height);
                    if self.connected {
                        self.status = format!("已连接 · {width}x{height}");
                    }
                }
                Ok(NetEvent::Stats { frames, kb, fps }) => {
                    self.stats = format!("帧 {frames} · {kb} KB · {fps:.1} fps");
                }
                Ok(NetEvent::Failed(e)) => {
                    self.status = format!("连接失败：{e}");
                    self.connected = false;
                    self.connecting = false;
                    self.texture = None;
                    self.cmd_tx = None;
                    self.evt_rx = None;
                    break;
                }
                Err(TryRecvError::Empty) => break,
                Err(TryRecvError::Disconnected) => {
                    if self.connecting || self.connected {
                        self.status = "连接已结束".to_string();
                    }
                    self.connected = false;
                    self.connecting = false;
                    self.cmd_tx = None;
                    self.evt_rx = None;
                    break;
                }
            }
        }
        if self.want_repaint {
            self.want_repaint = false;
            ctx.request_repaint();
        }
    }

    fn send_input(&self, ev: InputEvent) {
        static SENT: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
        if let Some(tx) = &self.cmd_tx {
            if tx.send(NetCmd::Input(ev)).is_ok() {
                let n = SENT.fetch_add(1, std::sync::atomic::Ordering::Relaxed) + 1;
                if n % 20 == 1 {
                    eprintln!("UI: input queued #{n}");
                }
            } else {
                eprintln!("UI: input channel closed");
            }
        } else {
            eprintln!("UI: input dropped (not connected)");
        }
    }

    fn remote_view(&mut self, ui: &mut egui::Ui) {
        let avail = ui.available_size();
        if self.frame_size.0 == 0 || self.frame_size.1 == 0 {
            ui.centered_and_justified(|ui| {
                ui.label(if self.connecting {
                    "正在连接…"
                } else {
                    "连接后这里显示远端屏幕"
                });
            });
            return;
        }
        let (fw, fh) = (self.frame_size.0 as f32, self.frame_size.1 as f32);
        let scale = (avail.x / fw).min(avail.y / fh).min(1.0);
        let size = egui::vec2(fw * scale, fh * scale);
        let (rect, resp) = ui.allocate_exact_size(size, egui::Sense::click_and_drag());

        if let Some(tex) = &self.texture {
            ui.painter().image(
                tex.id(),
                rect,
                egui::Rect::from_min_max(egui::pos2(0.0, 0.0), egui::pos2(1.0, 1.0)),
                egui::Color32::WHITE,
            );
        }

        // 鼠标移动：映射到远端像素坐标，节流（>=2px 或 >=30ms 才发）
        if let Some(pos) = resp.hover_pos() {
            let nx = ((pos.x - rect.min.x) / rect.width()).clamp(0.0, 1.0);
            let ny = ((pos.y - rect.min.y) / rect.height()).clamp(0.0, 1.0);
            let x = (nx * (self.frame_size.0 as f32 - 1.0)).round() as i32;
            let y = (ny * (self.frame_size.1 as f32 - 1.0)).round() as i32;
            let moved = match self.last_pointer {
                Some((px, py)) => (px - x).abs() >= 2 || (py - y).abs() >= 2,
                None => true,
            };
            if moved || self.last_pointer_at.elapsed() > Duration::from_millis(30) {
                self.last_pointer = Some((x, y));
                self.last_pointer_at = Instant::now();
                self.send_input(InputEvent::MouseMove { x, y });
            }
        }

        if resp.clicked_by(egui::PointerButton::Primary) {
            self.send_input(InputEvent::MouseButton {
                button: MouseButton::Left,
                pressed: true,
            });
            self.send_input(InputEvent::MouseButton {
                button: MouseButton::Left,
                pressed: false,
            });
        }
        if resp.clicked_by(egui::PointerButton::Secondary) {
            self.send_input(InputEvent::MouseButton {
                button: MouseButton::Right,
                pressed: true,
            });
            self.send_input(InputEvent::MouseButton {
                button: MouseButton::Right,
                pressed: false,
            });
        }
        if resp.clicked_by(egui::PointerButton::Middle) {
            self.send_input(InputEvent::MouseButton {
                button: MouseButton::Middle,
                pressed: true,
            });
            self.send_input(InputEvent::MouseButton {
                button: MouseButton::Middle,
                pressed: false,
            });
        }
    }

    fn forward_keyboard(&mut self, ctx: &egui::Context) {
        if !self.connected {
            return;
        }
        let events = ctx.input(|i| i.events.clone());
        for ev in events {
            if let egui::Event::Key {
                key,
                pressed,
                repeat,
                ..
            } = ev
            {
                if repeat {
                    continue;
                }
                if let Some(sc) = key_to_scancode(key) {
                    self.send_input(InputEvent::Key {
                        scancode: sc,
                        pressed,
                    });
                }
            }
        }
        // 修饰键：egui 不把它们当 Key 事件，单独比较状态
        let (ctrl, shift, alt, cmd) = ctx.input(|i| {
            (
                i.modifiers.ctrl,
                i.modifiers.shift,
                i.modifiers.alt,
                i.modifiers.mac_cmd || i.modifiers.command,
            )
        });
        let now = (ctrl, shift, alt, cmd);
        let names = ["ctrl", "shift", "alt", "cmd"];
        let before = self.last_mods;
        for (idx, changed) in [
            now.0 != before.0,
            now.1 != before.1,
            now.2 != before.2,
            now.3 != before.3,
        ]
        .into_iter()
        .enumerate()
        {
            if changed {
                if let Some(sc) = er_input::scancode_for(names[idx]) {
                    let pressed = match idx {
                        0 => now.0,
                        1 => now.1,
                        2 => now.2,
                        _ => now.3,
                    };
                    self.send_input(InputEvent::Key {
                        scancode: sc,
                        pressed,
                    });
                }
            }
        }
        self.last_mods = now;

        // 滚轮
        let scroll = ctx.input(|i| i.raw_scroll_delta);
        if scroll.y.abs() > 0.5 {
            self.send_input(InputEvent::Wheel {
                dx: 0,
                dy: scroll.y.signum() as i32 * 3,
            });
        }
    }
}

impl eframe::App for App {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        if self.auto_connect && !self.connecting && !self.connected {
            self.auto_connect = false;
            self.connect(ctx);
        }
        self.pump_events(ctx);
        if self.connected || self.connecting {
            ctx.request_repaint_after(Duration::from_millis(16));
        }

        egui::TopBottomPanel::top("top").show(ctx, |ui| {
            ui.add_space(4.0);
            ui.horizontal(|ui| {
                ui.heading("EasyRemote");
                ui.label(egui::RichText::new("Windows → macOS").weak());
                ui.separator();
                ui.label("网关");
                ui.add_sized([190.0, 22.0], egui::TextEdit::singleline(&mut self.gateway));
                ui.label("中继");
                ui.add_sized([130.0, 22.0], egui::TextEdit::singleline(&mut self.relay));
                ui.label("手机号");
                ui.add_sized([120.0, 22.0], egui::TextEdit::singleline(&mut self.phone));
            });
            ui.add_space(2.0);
            ui.horizontal(|ui| {
                ui.label("设备码");
                ui.add_sized(
                    [120.0, 22.0],
                    egui::TextEdit::singleline(&mut self.device_code),
                );
                ui.label("验证码");
                ui.add_sized([90.0, 22.0], egui::TextEdit::singleline(&mut self.captcha));
                let busy = self.connecting || self.connected;
                if ui.add_enabled(!busy, egui::Button::new("连接")).clicked() {
                    self.connect(ctx);
                }
                if ui.add_enabled(busy, egui::Button::new("断开")).clicked() {
                    self.disconnect();
                }
                ui.separator();
                ui.label(&self.status);
            });
            ui.add_space(4.0);
        });

        egui::TopBottomPanel::bottom("bottom").show(ctx, |ui| {
            ui.horizontal(|ui| {
                ui.label(&self.stats);
                if self.connected {
                    ui.separator();
                    ui.label(egui::RichText::new("鼠标/键盘直接转发到被控端").weak());
                }
            });
        });

        egui::CentralPanel::default().show(ctx, |ui| {
            self.remote_view(ui);
        });

        self.forward_keyboard(ctx);
    }
}

/// 加载系统中文字体（egui 自带字体不含 CJK，不装就是这个界面全是「□」）。
fn install_cjk_font(ctx: &egui::Context) {
    let candidates = [
        "C:\\Windows\\Fonts\\msyh.ttc",
        "C:\\Windows\\Fonts\\simhei.ttf",
        "C:\\Windows\\Fonts\\simsun.ttc",
        "C:\\Windows\\Fonts\\Deng.ttf",
    ];
    for path in candidates {
        let Ok(bytes) = std::fs::read(path) else {
            continue;
        };
        let mut fonts = egui::FontDefinitions::default();
        fonts
            .font_data
            .insert("cjk".to_owned(), egui::FontData::from_owned(bytes));
        for family in [egui::FontFamily::Proportional, egui::FontFamily::Monospace] {
            fonts
                .families
                .entry(family)
                .or_default()
                .insert(0, "cjk".to_owned());
        }
        ctx.set_fonts(fonts);
        println!("FONT: loaded {path}");
        return;
    }
    println!("FONT: no CJK font found, UI will show boxes");
}

#[derive(Default)]
struct Conf {
    gateway: Option<String>,
    relay: Option<String>,
    phone: Option<String>,
    device_code: Option<String>,
    captcha: Option<String>,
    auto_connect: bool,
}

/// 读 exe 同目录的 er.conf（key=value，一行一条）；没有就用默认值。
fn read_conf() -> Conf {
    let Ok(exe) = std::env::current_exe() else {
        return Conf::default();
    };
    let dir = exe.parent().map(|p| p.to_path_buf()).unwrap_or_default();
    let path = dir.join("er.conf");
    let Ok(text) = std::fs::read_to_string(&path) else {
        return Conf::default();
    };
    let mut c = Conf::default();
    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let Some((k, v)) = line.split_once('=') else {
            continue;
        };
        let v = v.trim().to_string();
        match k.trim() {
            "gateway" => c.gateway = Some(v),
            "relay" => c.relay = Some(v),
            "phone" => c.phone = Some(v),
            "device_code" => c.device_code = Some(v),
            "captcha" => c.captcha = Some(v),
            "auto_connect" => c.auto_connect = v == "1" || v.eq_ignore_ascii_case("true"),
            _ => {}
        }
    }
    println!("CONF: loaded {}", path.display());
    c
}

fn key_to_scancode(key: egui::Key) -> Option<u16> {
    use egui::Key as K;
    let name = match key {
        K::A => "a",
        K::B => "b",
        K::C => "c",
        K::D => "d",
        K::E => "e",
        K::F => "f",
        K::G => "g",
        K::H => "h",
        K::I => "i",
        K::J => "j",
        K::K => "k",
        K::L => "l",
        K::M => "m",
        K::N => "n",
        K::O => "o",
        K::P => "p",
        K::Q => "q",
        K::R => "r",
        K::S => "s",
        K::T => "t",
        K::U => "u",
        K::V => "v",
        K::W => "w",
        K::X => "x",
        K::Y => "y",
        K::Z => "z",
        K::Num0 => "0",
        K::Num1 => "1",
        K::Num2 => "2",
        K::Num3 => "3",
        K::Num4 => "4",
        K::Num5 => "5",
        K::Num6 => "6",
        K::Num7 => "7",
        K::Num8 => "8",
        K::Num9 => "9",
        K::Enter => "enter",
        K::Escape => "esc",
        K::Backspace => "backspace",
        K::Tab => "tab",
        K::Space => "space",
        K::Minus => "-",
        K::Plus => "=",
        K::OpenBracket => "[",
        K::CloseBracket => "]",
        K::Backslash => "\\",
        K::Semicolon => ";",
        K::Quote => "'",
        K::Backtick => "`",
        K::Comma => ",",
        K::Period => ".",
        K::Slash => "/",
        K::ArrowLeft => "left",
        K::ArrowRight => "right",
        K::ArrowUp => "up",
        K::ArrowDown => "down",
        K::Delete => "delete",
        K::Home => "home",
        K::End => "end",
        K::PageUp => "pageup",
        K::PageDown => "pagedown",
        K::Insert => "insert",
        K::F1 => "f1",
        K::F2 => "f2",
        K::F3 => "f3",
        K::F4 => "f4",
        K::F5 => "f5",
        K::F6 => "f6",
        K::F7 => "f7",
        K::F8 => "f8",
        K::F9 => "f9",
        K::F10 => "f10",
        K::F11 => "f11",
        K::F12 => "f12",
        _ => return None,
    };
    er_input::scancode_for(name)
}

/// 网络线程：登录 → 建会话 → 握手 → 收发循环。
// 参数多但都是一次性线程入口的显式依赖；拆 struct 只会让调用点更难读。
#[allow(clippy::too_many_arguments)]
fn net_thread(
    gateway: String,
    relay: String,
    phone: String,
    device_code: String,
    captcha: String,
    cmd_rx: Receiver<NetCmd>,
    evt_tx: Sender<NetEvent>,
    ctx: egui::Context,
) {
    let send = |e: NetEvent| {
        match &e {
            NetEvent::Status(s) => eprintln!("NET: status={s}"),
            NetEvent::Failed(msg) => eprintln!("NET: FAILED {msg}"),
            NetEvent::Ready => eprintln!("NET: handshake_ok"),
            _ => {}
        }
        let _ = evt_tx.send(e);
        ctx.request_repaint();
    };

    let relay_addr: SocketAddr = match relay.parse() {
        Ok(a) => a,
        Err(e) => {
            send(NetEvent::Failed(format!("中继地址无效: {e}")));
            return;
        }
    };

    send(NetEvent::Status("登录中…".to_string()));
    let ctrl = match ControllerAgent::start(&gateway, &phone, "123456", relay_addr) {
        Ok(c) => c,
        Err(e) => {
            send(NetEvent::Failed(format!("登录失败: {e}")));
            return;
        }
    };

    send(NetEvent::Status("请求会话…".to_string()));
    let session_id = match ctrl.request_session(&device_code, &captcha) {
        Ok(s) => s,
        Err(e) => {
            send(NetEvent::Failed(format!("设备码/验证码不对: {e}")));
            return;
        }
    };

    // 等被控端 consent 落地（真实产品里是用户点「接受」）
    std::thread::sleep(Duration::from_millis(1500));
    let (mut relay_cli, mut crypto, keys, msg) = match ctrl.begin(&session_id) {
        Ok(v) => v,
        Err(e) => {
            send(NetEvent::Failed(format!("中继注册失败: {e}")));
            return;
        }
    };
    std::thread::sleep(Duration::from_millis(2500));
    if let Err(e) = ctrl.announce(&mut relay_cli, &msg) {
        send(NetEvent::Failed(format!("握手发送失败: {e}")));
        return;
    }
    if let Err(e) = ctrl.finish(&mut relay_cli, &mut crypto, &keys, &session_id) {
        send(NetEvent::Failed(format!("握手校验失败: {e}")));
        return;
    }
    send(NetEvent::Ready);

    let mut dec = match H264Decoder::new() {
        Ok(d) => d,
        Err(e) => {
            send(NetEvent::Failed(format!("解码器初始化失败: {e}")));
            return;
        }
    };

    let mut frames: u64 = 0;
    let mut kb: u64 = 0;
    let mut last_decode = Instant::now() - Duration::from_secs(1);
    let mut last_stat = Instant::now();
    let mut stat_frames = 0u64;

    loop {
        // ① 处理 UI 命令
        loop {
            match cmd_rx.try_recv() {
                Ok(NetCmd::Input(ev)) => {
                    static IN: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
                    let n = IN.fetch_add(1, std::sync::atomic::Ordering::Relaxed) + 1;
                    match ctrl.send_input(&mut relay_cli, &mut crypto, &[ev]) {
                        Ok(_) => {
                            if n % 20 == 1 {
                                eprintln!("NET: input sent #{n}");
                            }
                        }
                        Err(e) => eprintln!("NET: input send failed: {e}"),
                    }
                }
                Ok(NetCmd::Disconnect) => return,
                Err(TryRecvError::Empty) => break,
                Err(TryRecvError::Disconnected) => return,
            }
        }

        // ② 收消息（短超时，让循环保持响应）
        match relay_cli.recv_timeout(Duration::from_millis(10)) {
            Ok(Some(sealed)) => {
                let Ok(plain) = crypto.open(&sealed) else {
                    continue;
                };
                if plain.is_empty() || plain[0] != MSG_VIDEO {
                    continue;
                }
                if plain.len() < 1 + er_client::link::VIDEO_HEADER_LEN {
                    continue;
                }
                let Some(_hdr) = VideoHeader::decode(&plain[1..]) else {
                    continue;
                };
                let payload = &plain[1 + er_client::link::VIDEO_HEADER_LEN..];
                // 软解 1080p 很慢：限到 ~12fps，否则解码队列会越拖越长
                if last_decode.elapsed() < Duration::from_millis(80) {
                    continue;
                }
                match dec.decode(payload) {
                    Ok(Some((w, h, rgb))) => {
                        last_decode = Instant::now();
                        frames += 1;
                        stat_frames += 1;
                        kb += (payload.len() as u64) / 1024;
                        let (w2, h2, small) = downsample(w, h, &rgb, 1280);
                        let _ = evt_tx.send(NetEvent::Frame {
                            width: w2,
                            height: h2,
                            rgb: small,
                        });
                        ctx.request_repaint();
                    }
                    Ok(None) => {}
                    Err(e) => {
                        send(NetEvent::Status(format!("解码错误: {e}")));
                    }
                }
            }
            Ok(None) => {}
            Err(_) => {}
        }

        if last_stat.elapsed() >= Duration::from_secs(1) {
            let fps = stat_frames as f32 / last_stat.elapsed().as_secs_f32();
            let _ = evt_tx.send(NetEvent::Stats { frames, kb, fps });
            ctx.request_repaint();
            stat_frames = 0;
            last_stat = Instant::now();
        }
    }
}

/// 超过 max_w 时按整数倍最近邻降采样（UI 端纹理与内存带宽都省一半以上）。
fn downsample(w: usize, h: usize, rgb: &[u8], max_w: usize) -> (usize, usize, Vec<u8>) {
    if w <= max_w || w == 0 || h == 0 {
        return (w, h, rgb.to_vec());
    }
    let factor = w.div_ceil(max_w);
    let nw = w / factor;
    let nh = h / factor;
    let mut out = Vec::with_capacity(nw * nh * 3);
    for y in 0..nh {
        let row = y * factor;
        for x in 0..nw {
            let idx = (row * w + x * factor) * 3;
            out.extend_from_slice(&rgb[idx..idx + 3]);
        }
    }
    (nw, nh, out)
}
