//! Windows DXGI Desktop Duplication 采集后端。
//!
//! 目标：把采集从「xcap 全帧 CPU 拷贝」换成 GPU 侧的 Desktop Duplication，
//! 并且让取到的 ID3D11Texture2D 能**不经过 host 内存**直接交给 NVENC。
//!
//! 关键事实（决定了这里的结构）：
//! * AcquireNextFrame 拿到的桌面纹理归 DXGI 所有，必须 ReleaseFrame 才能再取下一帧，
//!   所以同一时刻只持有一帧；GpuFrame 借用 &mut self 保证不会同时持有两帧。
//! * 该纹理格式由输出模式决定（通常 DXGI_FORMAT_B8G8R8A8_UNORM），
//!   与 NVENC 的 NV_ENC_BUFFER_FORMAT_ARGB 一致，可直接注册。
//! * 桌面无变化时 AcquireNextFrame 返回 DXGI_ERROR_WAIT_TIMEOUT，按契约返回 None。
//! * 上层要 CPU 像素时走 staging 纹理 Map（一次显存->内存拷贝）；要零拷贝时走
//!   ensure_encoder_input + copy_held_to_encoder_input（纯 GPU CopyResource，
//!   NVENC 可直接注册这块纹理）。

use std::ffi::c_void;

use windows::core::{Interface, Result as WinResult};
use windows::Win32::Foundation::HMODULE;
use windows::Win32::Graphics::Direct3D::{
    D3D_DRIVER_TYPE_UNKNOWN, D3D_FEATURE_LEVEL, D3D_FEATURE_LEVEL_10_0, D3D_FEATURE_LEVEL_10_1,
    D3D_FEATURE_LEVEL_11_0,
};
use windows::Win32::Graphics::Direct3D11::{
    D3D11CreateDevice, ID3D11Device, ID3D11DeviceContext, ID3D11Texture2D,
    D3D11_BIND_RENDER_TARGET, D3D11_CPU_ACCESS_READ, D3D11_CREATE_DEVICE_BGRA_SUPPORT,
    D3D11_MAPPED_SUBRESOURCE, D3D11_MAP_READ, D3D11_SDK_VERSION, D3D11_SUBRESOURCE_DATA,
    D3D11_TEXTURE2D_DESC, D3D11_USAGE_DEFAULT, D3D11_USAGE_STAGING,
};
use windows::Win32::Graphics::Dxgi::Common::{DXGI_FORMAT_B8G8R8A8_UNORM, DXGI_SAMPLE_DESC};
use windows::Win32::Graphics::Dxgi::{
    CreateDXGIFactory1, IDXGIAdapter1, IDXGIFactory1, IDXGIOutput1, IDXGIOutputDuplication,
    IDXGIResource, DXGI_ERROR_ACCESS_LOST, DXGI_ERROR_INVALID_CALL, DXGI_ERROR_WAIT_TIMEOUT,
    DXGI_OUTDUPL_FRAME_INFO,
};
use windows::Win32::System::Power::{
    SetThreadExecutionState, ES_CONTINUOUS, ES_DISPLAY_REQUIRED, ES_SYSTEM_REQUIRED,
};
use windows::Win32::UI::Input::KeyboardAndMouse::{
    SendInput, INPUT, INPUT_MOUSE, MOUSEEVENTF_MOVE, MOUSEINPUT,
};

use crate::{CaptureError, CaptureInfo, RawFrame};

/// 一帧 GPU 侧采集结果。
///
/// texture 是 DXGI Desktop Duplication 持有的 ID3D11Texture2D*，
/// 有效期到下一次 grab_gpu/release 为止。
#[derive(Debug, Clone, Copy)]
pub struct GpuFrame {
    pub texture: *mut c_void,
    pub width: u32,
    pub height: u32,
    /// DXGI_FORMAT 原始数值
    pub format: i32,
    pub ts_ms: u64,
}

/// 候选输出：适配器 + 该适配器上的第 n 个输出。
struct Candidate {
    adapter: IDXGIAdapter1,
    output_index: u32,
    name: String,
    primary: bool,
}

fn wide_to_string(w: &[u16]) -> String {
    let end = w.iter().position(|c| *c == 0).unwrap_or(w.len());
    String::from_utf16_lossy(&w[..end])
}

/// 当前输入桌面是不是锁屏（LogonUI）。
///
/// 判定方式：`OpenInputDesktop` 拿输入桌面句柄 → 读它的名字 →
/// 交互正常时是 "Default"，锁屏时是 "Winlogon"（不同 Windows 版本可能叫别的）。
/// 因此判据是「不是 Default 就算锁屏」，而不是硬编码某个名字。
///
/// 查不出来（非交互会话、无权限）时返回 false：宁可照常尝试采集，
/// 也不要把正常错误当锁屏吞掉。
fn input_desktop_is_lock_screen() -> bool {
    use windows::Win32::Foundation::HANDLE;
    use windows::Win32::System::StationsAndDesktops::{
        CloseDesktop, GetUserObjectInformationW, OpenInputDesktop, DESKTOP_CONTROL_FLAGS,
        DESKTOP_READOBJECTS, UOI_NAME,
    };
    unsafe {
        let Ok(desk) = OpenInputDesktop(DESKTOP_CONTROL_FLAGS(0), false, DESKTOP_READOBJECTS)
        else {
            return false;
        };
        let mut buf = [0u16; 64];
        let mut needed = 0u32;
        let res = GetUserObjectInformationW(
            HANDLE(desk.0),
            UOI_NAME,
            Some(buf.as_mut_ptr() as *mut c_void),
            (buf.len() * std::mem::size_of::<u16>()) as u32,
            Some(&mut needed),
        );
        let _ = CloseDesktop(desk);
        if res.is_err() {
            return false;
        }
        let end = buf.iter().position(|c| *c == 0).unwrap_or(buf.len());
        let name = String::from_utf16_lossy(&buf[..end]);
        !name.eq_ignore_ascii_case("Default")
    }
}

fn now_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

fn create_device(adapter: &IDXGIAdapter1) -> WinResult<(ID3D11Device, ID3D11DeviceContext)> {
    // 给了 adapter 就必须用 D3D_DRIVER_TYPE_UNKNOWN（D3D11 文档硬要求）
    let levels = [
        D3D_FEATURE_LEVEL_11_0,
        D3D_FEATURE_LEVEL_10_1,
        D3D_FEATURE_LEVEL_10_0,
    ];
    let mut device: Option<ID3D11Device> = None;
    let mut context: Option<ID3D11DeviceContext> = None;
    let mut level = D3D_FEATURE_LEVEL(0);
    unsafe {
        D3D11CreateDevice(
            adapter,
            D3D_DRIVER_TYPE_UNKNOWN,
            HMODULE(std::ptr::null_mut()),
            D3D11_CREATE_DEVICE_BGRA_SUPPORT,
            Some(&levels),
            D3D11_SDK_VERSION,
            Some(&mut device),
            Some(&mut level),
            Some(&mut context),
        )?;
    }
    match (device, context) {
        (Some(d), Some(c)) => Ok((d, c)),
        _ => Err(windows::core::Error::from(
            windows::Win32::Foundation::E_FAIL,
        )),
    }
}

/// 列出所有 (适配器, 输出)，主输出（桌面坐标覆盖 (0,0)）排在前面。
fn candidates() -> Vec<Candidate> {
    let mut out = Vec::new();
    unsafe {
        let Ok(factory) = CreateDXGIFactory1::<IDXGIFactory1>() else {
            return out;
        };
        let mut ai = 0u32;
        while let Ok(adapter) = factory.EnumAdapters1(ai) {
            ai += 1;
            let name = adapter
                .GetDesc1()
                .map(|d| wide_to_string(&d.Description))
                .unwrap_or_else(|_| format!("adapter#{ai}"));
            let mut oi = 0u32;
            while let Ok(output) = adapter.EnumOutputs(oi) {
                oi += 1;
                let desc = match output.GetDesc() {
                    Ok(d) => d,
                    Err(_) => continue,
                };
                let r = desc.DesktopCoordinates;
                let on_desktop = desc.AttachedToDesktop.as_bool();
                let covers_origin =
                    on_desktop && r.left <= 0 && r.top <= 0 && r.right > 0 && r.bottom > 0;
                out.push(Candidate {
                    adapter: adapter.clone(),
                    output_index: oi - 1,
                    name: name.clone(),
                    primary: covers_origin,
                });
            }
        }
    }
    out.sort_by_key(|c| if c.primary { 0 } else { 1 });
    out
}

/// 采集器。
pub struct DxgiCapture {
    device: ID3D11Device,
    context: ID3D11DeviceContext,
    /// ACCESS_LOST 后要重新 DuplicateOutput，必须记住是哪块适配器的第几个输出。
    /// 注意：**只重建 duplication，绝不重建 device**——NVENC 会话与纹理都绑在它上面。
    adapter: IDXGIAdapter1,
    output_index: u32,
    duplication: IDXGIOutputDuplication,
    staging: ID3D11Texture2D,
    /// 当前被 Acquire 持有、尚未 ReleaseFrame 的桌面纹理
    held: Option<ID3D11Texture2D>,
    /// 给 NVENC 注册的常驻输入纹理（D3D11_USAGE_DEFAULT）
    encoder_input: Option<ID3D11Texture2D>,
    info: CaptureInfo,
    adapter_name: String,
    seq: u64,
    /// 连续 AcquireNextFrame 超时次数（休眠/静置判定用）
    consecutive_timeouts: u32,
    /// 锁屏判定缓存：(检查时刻, 是否锁屏)，1s TTL，避免每帧枚举进程
    locked_cache: Option<(std::time::Instant, bool)>,
    /// duplication 重建次数（自愈证据：ACCESS_LOST 后应该 +1）
    rebuild_count: u32,
    /// 设备级重建次数（显示模式切换/驱动重置会把 D3D11 device 移除，只能整条重建）
    device_rebuilds: u32,
    /// 最近一次重建失败的原因（诊断/证据用，不影响控制流）
    last_rebuild_error: Option<String>,
}

impl DxgiCapture {
    /// 打开主输出（覆盖桌面坐标 (0,0) 的输出优先）。
    pub fn open_primary() -> Result<Self, CaptureError> {
        Self::open_primary_detailed().map_err(|_| CaptureError::Unavailable)
    }

    /// 同 open_primary，但失败时带上逐输出原因（诊断/证据用）。
    pub fn open_primary_detailed() -> Result<Self, String> {
        let cands = candidates();
        if cands.is_empty() {
            return Err("no DXGI adapter/output found".into());
        }
        let mut last = String::from("no candidate output");
        for cand in &cands {
            match Self::open_candidate(cand) {
                Ok(c) => return Ok(c),
                Err(detail) => {
                    last = format!("{} output#{} -> {detail}", cand.name, cand.output_index)
                }
            }
        }
        Err(last)
    }

    /// 打开指定适配器/输出。
    fn open_candidate(cand: &Candidate) -> Result<Self, String> {
        let (device, context) =
            create_device(&cand.adapter).map_err(|e| format!("D3D11CreateDevice: {e}"))?;
        let dup = unsafe {
            let output = cand
                .adapter
                .EnumOutputs(cand.output_index)
                .map_err(|e| format!("EnumOutputs: {e}"))?;
            let output1: IDXGIOutput1 = output.cast().map_err(|e| format!("IDXGIOutput1: {e}"))?;
            output1
                .DuplicateOutput(&device)
                .map_err(|e| format!("DuplicateOutput: {e}"))?
        };
        let desc = unsafe { dup.GetDesc() };
        let width = desc.ModeDesc.Width;
        let height = desc.ModeDesc.Height;
        if width == 0 || height == 0 {
            return Err("duplication desc has zero size".into());
        }
        let staging = create_staging(&device, width, height)?;
        Ok(Self {
            device,
            context,
            adapter: cand.adapter.clone(),
            output_index: cand.output_index,
            duplication: dup,
            staging,
            held: None,
            encoder_input: None,
            info: CaptureInfo {
                width,
                height,
                refresh_hz: 60,
                display_count: 1,
            },
            adapter_name: cand.name.clone(),
            seq: 0,
            consecutive_timeouts: 0,
            locked_cache: None,
            rebuild_count: 0,
            device_rebuilds: 0,
            last_rebuild_error: None,
        })
    }

    pub fn adapter_name(&self) -> &str {
        &self.adapter_name
    }

    pub fn info(&self) -> CaptureInfo {
        self.info
    }

    /// 输出的 DXGI_FORMAT 数值（确认是 B8G8R8A8_UNORM 才能直接进 NVENC）。
    pub fn format(&self) -> i32 {
        unsafe { self.duplication.GetDesc().ModeDesc.Format.0 }
    }

    /// ID3D11Device*（NVENC 的 DIRECTX 会话必须用同一块设备）。
    pub fn device_raw(&self) -> *mut c_void {
        self.device.as_raw()
    }

    /// 取一帧 GPU 纹理（不做任何 CPU 拷贝）。
    ///
    /// 返回 Ok(None) 表示：timeout_ms 内桌面无变化，**或当前处于锁屏**（锁屏下
    /// AcquireNextFrame 恒失败，把它当错误会对上层刷屏，所以按契约返回 None）。
    ///
    /// ACCESS_LOST（切换分辨率/显示器、UAC、RDP 重连、驱动重置）会**重建一次**
    /// IDXGIOutputDuplication 并重试一次；重建只换 duplication，device/context 不动
    /// ——NVENC 会话与已注册纹理都绑在那块 device 上。
    pub fn grab_gpu(&mut self, timeout_ms: u32) -> Result<Option<GpuFrame>, CaptureError> {
        // 锁屏时直接返回「无新帧」，不制造错误日志
        if self.is_locked() {
            self.release();
            return Ok(None);
        }

        match self.acquire_once(timeout_ms) {
            Err(CaptureError::Lost) => {
                // 两级自愈，且必须**逐级升级**：
                //   1) 只换 duplication（ACCESS_LOST 类：UAC/RDP/热插拔，device 还活着）
                //   2) 换完还是拿到 Lost/INVALID_CALL → device 已被驱动移除（实测：切刷新率后
                //      DuplicateOutput 仍能成功，但新 duplication 的 AcquireNextFrame 恒返
                //      0x887A0001，只有换一块 device 才恢复），整条重建 device/context/staging
                let mut recovered = false;
                if self.rebuild_duplication().is_ok() {
                    match self.acquire_once(timeout_ms) {
                        Ok(v) => return Ok(v),
                        Err(CaptureError::Lost) => { /* 升级到设备级重建 */ }
                        other => return other,
                    }
                }
                if self.reopen_after_device_lost().is_ok() {
                    match self.acquire_once(timeout_ms) {
                        Ok(v) => {
                            recovered = true;
                            return Ok(v);
                        }
                        Err(CaptureError::Lost) if self.is_locked() => return Ok(None),
                        other => return other,
                    }
                }
                let _ = recovered;
                return Err(CaptureError::Lost);
            }
            Ok(None) => {
                self.consecutive_timeouts = self.consecutive_timeouts.saturating_add(1);
                // 静置/休眠：连续多次拿不到帧时
                //   - wake_display()：申请保持显示器，避免真休眠（对"静止桌面不出帧"无效）
                //   - nudge_desktop_zero()：零位移鼠标输入，不移动光标，但能让 DWM 重新出帧
                //     （实测：只 wake_display 时 #1~#5 全 WAIT_TIMEOUT；补一次零位移输入后立刻出帧）
                // 注意：这里**不**判定为锁屏，也不吞掉后续帧。
                if self.consecutive_timeouts >= 5 {
                    let _ = Self::wake_display();
                    let _ = self.nudge_desktop_zero();
                    self.consecutive_timeouts = 0;
                }
                Ok(None)
            }
            other => {
                self.consecutive_timeouts = 0;
                other
            }
        }
    }

    /// 单次 AcquireNextFrame（不做自愈），供 grab_gpu 复用。
    fn acquire_once(&mut self, timeout_ms: u32) -> Result<Option<GpuFrame>, CaptureError> {
        // 上一帧必须先 ReleaseFrame，DXGI 不允许同时持有两帧
        self.release();
        let mut info = DXGI_OUTDUPL_FRAME_INFO::default();
        let mut resource: Option<IDXGIResource> = None;
        let res = unsafe {
            self.duplication
                .AcquireNextFrame(timeout_ms, &mut info, &mut resource)
        };
        if let Err(e) = res {
            if e.code() == DXGI_ERROR_WAIT_TIMEOUT {
                return Ok(None);
            }
            // 两种错误都要走自愈：
            //   ACCESS_LOST   —— 切分辨率/换显示器/UAC/RDP 重连/驱动重置
            //   INVALID_CALL  —— 实测「切刷新率」后拿到的是 0x887A0001（不是 ACCESS_LOST），
            //                    duplication 已处于无效状态，同样只能重建
            if e.code() == DXGI_ERROR_ACCESS_LOST || e.code() == DXGI_ERROR_INVALID_CALL {
                return Err(CaptureError::Lost);
            }
            return Err(CaptureError::Unavailable);
        }
        let Some(resource) = resource else {
            self.release();
            return Err(CaptureError::Unavailable);
        };
        // 桌面图像变化、指针移动、指针形状变化都算「有变化」：
        // 指针变化时返回的纹理仍是当前桌面内容，可以直接编码。
        let changed = info.LastPresentTime != 0
            || info.AccumulatedFrames != 0
            || info.LastMouseUpdateTime != 0
            || info.PointerShapeBufferSize != 0;
        if !changed {
            self.release();
            return Ok(None);
        }
        let texture: ID3D11Texture2D = match resource.cast() {
            Ok(t) => t,
            Err(_) => {
                self.release();
                return Err(CaptureError::Unavailable);
            }
        };
        let raw = texture.as_raw();
        self.held = Some(texture);
        self.seq += 1;
        Ok(Some(GpuFrame {
            texture: raw,
            width: self.info.width,
            height: self.info.height,
            format: self.format(),
            ts_ms: now_ms(),
        }))
    }

    /// 当前会话是否锁屏（LogonUI 在前台时输入桌面不是 "Default"）。
    ///
    /// 结果缓存 1s：锁屏检测要走一次 user32，每帧都查代价不划算。
    /// 查不出来（无权限/非交互会话）时保守地认为**没有锁屏**，
    /// 免得把正常的采集错误当成锁屏吞掉。
    pub fn is_locked(&mut self) -> bool {
        const TTL: std::time::Duration = std::time::Duration::from_millis(1000);
        if let Some((at, locked)) = self.locked_cache {
            if at.elapsed() < TTL {
                return locked;
            }
        }
        let locked = input_desktop_is_lock_screen();
        self.locked_cache = Some((std::time::Instant::now(), locked));
        locked
    }

    /// ACCESS_LOST 自愈：对**同一个 output、同一块 device** 重新建 duplication。
    /// 尺寸变了就顺带重建 staging 与 NVENC 输入纹理（尺寸相关的资源全部作废）。
    pub fn rebuild_duplication(&mut self) -> Result<(), CaptureError> {
        self.release();
        let output = unsafe { self.adapter.EnumOutputs(self.output_index) }.map_err(|e| {
            self.last_rebuild_error = Some(format!("EnumOutputs: {e}"));
            CaptureError::Lost
        })?;
        let output1: IDXGIOutput1 = output.cast().map_err(|e| {
            self.last_rebuild_error = Some(format!("cast IDXGIOutput1: {e}"));
            CaptureError::Lost
        })?;
        let dup = unsafe { output1.DuplicateOutput(&self.device) }.map_err(|e| {
            self.last_rebuild_error = Some(format!("DuplicateOutput(same device): {e}"));
            CaptureError::Lost
        })?;
        let desc = unsafe { dup.GetDesc() };
        let (w, h) = (desc.ModeDesc.Width, desc.ModeDesc.Height);
        if w == 0 || h == 0 {
            return Err(CaptureError::Lost);
        }
        self.duplication = dup;
        if (w, h) != (self.info.width, self.info.height) {
            self.staging = create_staging(&self.device, w, h).map_err(|_| CaptureError::Lost)?;
            self.encoder_input = None;
            self.info.width = w;
            self.info.height = h;
        }
        self.consecutive_timeouts = 0;
        self.locked_cache = None;
        self.rebuild_count = self.rebuild_count.saturating_add(1);
        Ok(())
    }

    /// duplication 重建次数（自愈验证用：ACCESS_LOST 后应 +1）。
    pub fn rebuild_count(&self) -> u32 {
        self.rebuild_count
    }

    /// 设备级重建次数（显示模式切换/驱动重置会命中）。
    pub fn device_rebuilds(&self) -> u32 {
        self.device_rebuilds
    }

    /// 最近一次重建失败的原因（诊断用）。
    pub fn last_rebuild_error(&self) -> Option<&str> {
        self.last_rebuild_error.as_deref()
    }

    /// 整条重建：device + context + duplication + staging。
    ///
    /// 何时需要：显示模式/刷新率切换、驱动重置、TDR —— 这些会**移除 D3D11 device**，
    /// 此时对旧 device 调 DuplicateOutput 直接失败，只换 duplication 救不回来。
    /// 重建后 device_raw() 会变，**绑在旧 device 上的 NVENC 会话必须由调用方重建**。
    pub fn reopen_after_device_lost(&mut self) -> Result<(), CaptureError> {
        self.release();
        // 重新枚举适配器/输出：模式切换后缓存的 IDXGIAdapter1/IDXGIOutput 句柄也会失效，
        // 复用它们建出来的 duplication 会一直返回 INVALID_CALL（实测）。
        let cands = candidates();
        if cands.is_empty() {
            self.last_rebuild_error = Some("no DXGI candidate after reopen".into());
            return Err(CaptureError::Lost);
        }
        let chosen = cands
            .iter()
            .find(|c| c.name == self.adapter_name && c.output_index == self.output_index)
            .or_else(|| cands.iter().find(|c| c.primary))
            .ok_or(CaptureError::Lost)?;
        let (device, context) = create_device(&chosen.adapter).map_err(|e| {
            self.last_rebuild_error = Some(format!("CreateDevice: {e}"));
            CaptureError::Lost
        })?;
        let output = unsafe { chosen.adapter.EnumOutputs(chosen.output_index) }.map_err(|e| {
            self.last_rebuild_error = Some(format!("EnumOutputs: {e}"));
            CaptureError::Lost
        })?;
        let output1: IDXGIOutput1 = output.cast().map_err(|e| {
            self.last_rebuild_error = Some(format!("cast IDXGIOutput1: {e}"));
            CaptureError::Lost
        })?;
        let dup = unsafe { output1.DuplicateOutput(&device) }.map_err(|e| {
            self.last_rebuild_error = Some(format!("DuplicateOutput: {e}"));
            CaptureError::Lost
        })?;
        let desc = unsafe { dup.GetDesc() };
        let (w, h) = (desc.ModeDesc.Width, desc.ModeDesc.Height);
        if w == 0 || h == 0 {
            return Err(CaptureError::Lost);
        }
        let staging = create_staging(&device, w, h).map_err(|_| CaptureError::Lost)?;
        self.adapter = chosen.adapter.clone();
        self.output_index = chosen.output_index;
        self.adapter_name = chosen.name.clone();
        self.device = device;
        self.context = context;
        self.duplication = dup;
        self.staging = staging;
        self.encoder_input = None;
        self.info.width = w;
        self.info.height = h;
        self.device_rebuilds = self.device_rebuilds.saturating_add(1);
        self.rebuild_count = self.rebuild_count.saturating_add(1);
        self.consecutive_timeouts = 0;
        self.locked_cache = None;
        Ok(())
    }

    /// 轻推鼠标制造桌面变化（基准/保活用）。
    ///
    /// Desktop Duplication 只在桌面变化时出帧；静止桌面下取不到新帧，
    /// 基准脚本用它保证每轮都有真实帧可比。
    pub fn nudge_desktop(&mut self) -> bool {
        use std::sync::atomic::{AtomicI32, Ordering};
        static TOGGLE: AtomicI32 = AtomicI32::new(0);
        let dir = if TOGGLE.fetch_xor(1, Ordering::Relaxed) == 0 {
            2
        } else {
            -2
        };
        // 必须走 SendInput（真实输入事件）：SetCursorPos 不算输入，
        // 既不会唤醒显示器，也不会让 DWM 重新出帧。
        let mut input = INPUT::default();
        input.r#type = INPUT_MOUSE;
        input.Anonymous.mi = MOUSEINPUT {
            dx: dir,
            dy: 0,
            mouseData: 0,
            dwFlags: MOUSEEVENTF_MOVE,
            time: 0,
            dwExtraInfo: 0,
        };
        unsafe { SendInput(&[input], std::mem::size_of::<INPUT>() as i32) > 0 }
    }

    /// 零位移的 MOUSEEVENTF_MOVE（dx=dy=0）：**不移动光标**，只产生一次输入事件。
    ///
    /// 用途：显示器休眠后 DWM 停止出帧，需要一次真实输入才能恢复；产品路径
    /// 不能去挪用户光标，所以用零位移输入做唤醒。
    pub fn nudge_desktop_zero(&mut self) -> bool {
        let mut input = INPUT::default();
        input.r#type = INPUT_MOUSE;
        input.Anonymous.mi = MOUSEINPUT {
            dx: 0,
            dy: 0,
            mouseData: 0,
            dwFlags: MOUSEEVENTF_MOVE,
            time: 0,
            dwExtraInfo: 0,
        };
        unsafe { SendInput(&[input], std::mem::size_of::<INPUT>() as i32) > 0 }
    }

    /// 请求显示器保持点亮（避免「屏幕休眠 -> DWM 停止出帧 -> 复制永远超时」）。
    pub fn wake_display() -> bool {
        unsafe {
            !SetThreadExecutionState(ES_CONTINUOUS | ES_DISPLAY_REQUIRED | ES_SYSTEM_REQUIRED)
                .0
                .eq(&0)
        }
    }

    /// 诊断用：一次 AcquireNextFrame 的原始结果（不做过早过滤，便于定位「取不到帧」）。
    pub fn acquire_debug(&mut self, timeout_ms: u32) -> String {
        self.release();
        let mut info = DXGI_OUTDUPL_FRAME_INFO::default();
        let mut resource: Option<IDXGIResource> = None;
        let res = unsafe {
            self.duplication
                .AcquireNextFrame(timeout_ms, &mut info, &mut resource)
        };
        match res {
            Ok(()) => {
                let has = resource.is_some();
                if let Some(r) = resource {
                    if let Ok(t) = r.cast::<ID3D11Texture2D>() {
                        self.held = Some(t);
                    }
                }
                format!(
                    "OK texture={} last_present={} accum={} last_mouse={} ptr_shape_B={} ptr_pos=({},{}) visible={}",
                    has,
                    info.LastPresentTime,
                    info.AccumulatedFrames,
                    info.LastMouseUpdateTime,
                    info.PointerShapeBufferSize,
                    info.PointerPosition.Position.x,
                    info.PointerPosition.Position.y,
                    info.PointerPosition.Visible.as_bool()
                )
            }
            Err(e) => format!("ERR hresult=0x{:08x} msg={e}", e.code().0),
        }
    }

    /// 释放当前持有的桌面帧（幂等）。
    pub fn release(&mut self) {
        if self.held.take().is_some() {
            unsafe {
                let _ = self.duplication.ReleaseFrame();
            }
        }
    }

    /// 把当前持有的桌面帧 GPU 侧拷进常驻输入纹理（NVENC 可注册的那块）。
    pub fn copy_held_to_encoder_input(&mut self) -> Result<(), CaptureError> {
        let (Some(src), Some(dst)) = (self.held.as_ref(), self.encoder_input.as_ref()) else {
            return Err(CaptureError::Unavailable);
        };
        unsafe {
            self.context.CopyResource(dst, src);
        }
        Ok(())
    }

    /// 创建/复用 NVENC 输入纹理，返回 ID3D11Texture2D*。
    ///
    /// 尺寸必须与编码器配置一致；格式 B8G8R8A8_UNORM = NVENC 的 ARGB。
    pub fn ensure_encoder_input(
        &mut self,
        width: u32,
        height: u32,
    ) -> Result<*mut c_void, CaptureError> {
        if let Some(t) = &self.encoder_input {
            let mut d = D3D11_TEXTURE2D_DESC::default();
            unsafe { t.GetDesc(&mut d) };
            if d.Width == width && d.Height == height {
                return Ok(t.as_raw());
            }
            self.encoder_input = None;
        }
        let mut desc = D3D11_TEXTURE2D_DESC::default();
        desc.Width = width;
        desc.Height = height;
        desc.MipLevels = 1;
        desc.ArraySize = 1;
        desc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
        desc.SampleDesc = DXGI_SAMPLE_DESC {
            Count: 1,
            Quality: 0,
        };
        desc.Usage = D3D11_USAGE_DEFAULT;
        desc.BindFlags = D3D11_BIND_RENDER_TARGET.0 as u32;
        desc.CPUAccessFlags = 0;
        desc.MiscFlags = 0;
        let mut tex: Option<ID3D11Texture2D> = None;
        unsafe {
            self.device
                .CreateTexture2D(&desc, None, Some(&mut tex))
                .map_err(|_| CaptureError::Unavailable)?;
        }
        let tex = tex.ok_or(CaptureError::Unavailable)?;
        let raw = tex.as_raw();
        self.encoder_input = Some(tex);
        Ok(raw)
    }

    /// CPU 路径：取帧 -> staging -> Map 读回 BGRA（紧排）。
    pub fn grab_cpu(&mut self, timeout_ms: u32) -> Result<Option<RawFrame>, CaptureError> {
        if self.grab_gpu(timeout_ms)?.is_none() {
            return Ok(None);
        }
        let raw = self.read_held()?;
        Ok(Some(raw))
    }

    fn read_held(&mut self) -> Result<RawFrame, CaptureError> {
        let Some(src) = self.held.as_ref() else {
            return Err(CaptureError::Unavailable);
        };
        unsafe {
            self.context.CopyResource(&self.staging, src);
        }
        let mut mapped = D3D11_MAPPED_SUBRESOURCE::default();
        unsafe {
            self.context
                .Map(&self.staging, 0, D3D11_MAP_READ, 0, Some(&mut mapped))
                .map_err(|_| CaptureError::Unavailable)?;
        }
        let (w, h) = (self.info.width as usize, self.info.height as usize);
        let row_bytes = w * 4;
        let mut data = vec![0u8; row_bytes * h];
        unsafe {
            let base = mapped.pData as *const u8;
            let pitch = mapped.RowPitch as usize;
            for y in 0..h {
                std::ptr::copy_nonoverlapping(
                    base.add(y * pitch),
                    data.as_mut_ptr().add(y * row_bytes),
                    row_bytes,
                );
            }
            self.context.Unmap(&self.staging, 0);
        }
        let frame = RawFrame {
            width: self.info.width,
            height: self.info.height,
            ts_ms: now_ms(),
            data,
        };
        self.release();
        Ok(frame)
    }

    /// 把一帧 BGRA（host 内存）上传成一块全新的 D3D11 纹理，返回 ID3D11Texture2D*。
    ///
    /// 用途：在没有 Desktop Duplication 可用的环境（锁屏 / 无扫描输出）下，
    /// 仍然可以验证 NVENC 的 DIRECTX 输入路径本身（注册 -> 编码 -> 解码校验）。
    /// 注意这不是零拷贝：这一步本身就是一次 host -> GPU 上传。
    pub fn upload_bgra_texture(
        &mut self,
        data: &[u8],
        width: u32,
        height: u32,
    ) -> Result<*mut c_void, CaptureError> {
        if data.len() != (width as usize) * (height as usize) * 4 {
            return Err(CaptureError::Unavailable);
        }
        let mut desc = D3D11_TEXTURE2D_DESC::default();
        desc.Width = width;
        desc.Height = height;
        desc.MipLevels = 1;
        desc.ArraySize = 1;
        desc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
        desc.SampleDesc = DXGI_SAMPLE_DESC {
            Count: 1,
            Quality: 0,
        };
        desc.Usage = D3D11_USAGE_DEFAULT;
        desc.BindFlags = D3D11_BIND_RENDER_TARGET.0 as u32;
        desc.CPUAccessFlags = 0;
        desc.MiscFlags = 0;
        let init = D3D11_SUBRESOURCE_DATA {
            pSysMem: data.as_ptr() as *const c_void,
            SysMemPitch: width * 4,
            SysMemSlicePitch: 0,
        };
        let mut tex: Option<ID3D11Texture2D> = None;
        unsafe {
            self.device
                .CreateTexture2D(&desc, Some(&init), Some(&mut tex))
                .map_err(|_| CaptureError::Unavailable)?;
        }
        let tex = tex.ok_or(CaptureError::Unavailable)?;
        let raw = tex.as_raw();
        self.encoder_input = Some(tex);
        Ok(raw)
    }
}

fn create_staging(
    device: &ID3D11Device,
    width: u32,
    height: u32,
) -> Result<ID3D11Texture2D, String> {
    let mut desc = D3D11_TEXTURE2D_DESC::default();
    desc.Width = width;
    desc.Height = height;
    desc.MipLevels = 1;
    desc.ArraySize = 1;
    desc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
    desc.SampleDesc = DXGI_SAMPLE_DESC {
        Count: 1,
        Quality: 0,
    };
    desc.Usage = D3D11_USAGE_STAGING;
    desc.BindFlags = 0;
    desc.CPUAccessFlags = D3D11_CPU_ACCESS_READ.0 as u32;
    desc.MiscFlags = 0;
    let mut tex: Option<ID3D11Texture2D> = None;
    unsafe {
        device
            .CreateTexture2D(&desc, None, Some(&mut tex))
            .map_err(|e| format!("CreateTexture2D(staging): {e}"))?;
    }
    tex.ok_or_else(|| "staging texture is null".to_string())
}

/// 诊断：逐个 adapters/output 尝试 DuplicateOutput，返回可读报告。
///
/// 这条信息决定零拷贝是否可行——NVENC 的 DIRECTX 会话必须和采集用同一块
/// ID3D11Device，所以「桌面挂在哪个适配器上」是关键。
pub fn probe_report() -> Vec<String> {
    let mut lines = Vec::new();
    for cand in candidates() {
        let mut line = format!(
            "adapter={:?} output#{} primary={}",
            cand.name, cand.output_index, cand.primary
        );
        match DxgiCapture::open_candidate(&cand) {
            Ok(cap) => line.push_str(&format!(
                " -> OK {}x{} format={} current={}x{}",
                cap.info.width,
                cap.info.height,
                cap.format(),
                cap.info.width,
                cap.info.height
            )),
            Err(e) => line.push_str(&format!(" -> FAIL {e}")),
        }
        lines.push(line);
    }
    if lines.is_empty() {
        lines.push("no DXGI adapter/output found".into());
    }
    lines
}

/// 全量 DXGI 拓扑（诊断用）：每个适配器、每个输出的桌面坐标与刷新率。
///
/// 「AcquireNextFrame 恒超时」时先看这里：如果桌面根本没有落在可枚举的输出上
/// （虚拟显示 / 无物理扫描输出 / 会话锁屏），Desktop Duplication 就永远取不到帧。
pub fn topology() -> Vec<String> {
    let mut lines = Vec::new();
    unsafe {
        let Ok(factory) = CreateDXGIFactory1::<IDXGIFactory1>() else {
            return vec!["CreateDXGIFactory1 failed".into()];
        };
        let mut ai = 0u32;
        while let Ok(adapter) = factory.EnumAdapters1(ai) {
            let idx = ai;
            ai += 1;
            let desc = adapter.GetDesc1();
            let (name, vendor, luid) = match &desc {
                Ok(d) => (
                    wide_to_string(&d.Description),
                    d.VendorId,
                    format!(
                        "{:08x}:{:08x}",
                        d.AdapterLuid.HighPart, d.AdapterLuid.LowPart
                    ),
                ),
                Err(_) => (format!("adapter#{idx}"), 0u32, "?".to_string()),
            };
            let mut line = format!("adapter#{idx} name={name:?} vendor=0x{vendor:04x} luid={luid}");
            let mut oi = 0u32;
            let mut n_out = 0u32;
            while let Ok(output) = adapter.EnumOutputs(oi) {
                let oidx = oi;
                oi += 1;
                n_out += 1;
                match output.GetDesc() {
                    Ok(od) => {
                        let r = od.DesktopCoordinates;
                        line.push_str(&format!(
                            "\n    output#{oidx} attached={} rect=({},{})-({},{}) device={:?}",
                            od.AttachedToDesktop.as_bool(),
                            r.left,
                            r.top,
                            r.right,
                            r.bottom,
                            wide_to_string(&od.DeviceName)
                        ));
                    }
                    Err(e) => line.push_str(&format!("\n    output#{oidx} GetDesc failed: {e}")),
                }
            }
            if n_out == 0 {
                line.push_str("\n    (no DXGI outputs)");
            }
            lines.push(line);
        }
    }
    if lines.is_empty() {
        lines.push("no DXGI adapters".into());
    }
    lines
}

/// B8G8R8A8_UNORM 的 DXGI_FORMAT 数值，供上层断言采集格式可直接进 NVENC。
pub const FORMAT_BGRA8_UNORM: i32 = DXGI_FORMAT_B8G8R8A8_UNORM.0;

/// 显示模式操作（step3 自愈验证用：改分辨率触发 DXGI_ERROR_ACCESS_LOST）。
pub mod display {
    use windows::core::PCWSTR;
    use windows::Win32::Graphics::Gdi::{
        ChangeDisplaySettingsW, EnumDisplaySettingsW, CDS_UPDATEREGISTRY, DEVMODEW,
        DISP_CHANGE_SUCCESSFUL, DM_DISPLAYFREQUENCY, DM_PELSHEIGHT, DM_PELSWIDTH,
        ENUM_CURRENT_SETTINGS,
    };

    /// 当前模式 (宽, 高, 刷新率)。
    pub fn current_mode() -> Option<(u32, u32, u32)> {
        let mut dm = DEVMODEW::default();
        dm.dmSize = std::mem::size_of::<DEVMODEW>() as u16;
        let ok = unsafe { EnumDisplaySettingsW(PCWSTR::null(), ENUM_CURRENT_SETTINGS, &mut dm) };
        if !ok.as_bool() {
            return None;
        }
        Some((dm.dmPelsWidth, dm.dmPelsHeight, dm.dmDisplayFrequency))
    }

    /// 切到指定分辨率/刷新率。返回是否成功（DISP_CHANGE_SUCCESSFUL）。
    pub fn switch_mode(width: u32, height: u32, freq: u32) -> Result<(), String> {
        let mut dm = DEVMODEW::default();
        dm.dmSize = std::mem::size_of::<DEVMODEW>() as u16;
        // 以当前模式为基础改，避免把色彩位深等字段写成 0
        if !unsafe { EnumDisplaySettingsW(PCWSTR::null(), ENUM_CURRENT_SETTINGS, &mut dm) }
            .as_bool()
        {
            return Err("EnumDisplaySettingsW(current) failed".into());
        }
        dm.dmPelsWidth = width;
        dm.dmPelsHeight = height;
        dm.dmDisplayFrequency = freq;
        dm.dmFields = DM_PELSWIDTH | DM_PELSHEIGHT | DM_DISPLAYFREQUENCY;
        let rc = unsafe { ChangeDisplaySettingsW(Some(&dm), CDS_UPDATEREGISTRY) };
        if rc == DISP_CHANGE_SUCCESSFUL {
            Ok(())
        } else {
            Err(format!("ChangeDisplaySettingsW rc={}", rc.0))
        }
    }

    /// 在「同分辨率不同刷新率」里挑一个可切换的目标；挑不到再退到 1280x720 / 1024x768。
    ///
    /// 为什么要优先同分辨率：尺寸不变时 NVENC 的已注册纹理仍然有效，
    /// 这样「重建 duplication」与「重建编码器会话」才能被分开验证。
    pub fn alternate_mode() -> (u32, u32, u32) {
        let cur = current_mode();
        if let Some((w, h, freq)) = cur {
            for cand in [60u32, 50, 75, 30] {
                if cand != freq {
                    // 先验证这个模式存在
                    let mut dm = DEVMODEW::default();
                    dm.dmSize = std::mem::size_of::<DEVMODEW>() as u16;
                    dm.dmPelsWidth = w;
                    dm.dmPelsHeight = h;
                    dm.dmDisplayFrequency = cand;
                    dm.dmFields = DM_PELSWIDTH | DM_PELSHEIGHT | DM_DISPLAYFREQUENCY;
                    if unsafe {
                        windows::Win32::Graphics::Gdi::EnumDisplaySettingsW(
                            PCWSTR::null(),
                            windows::Win32::Graphics::Gdi::ENUM_CURRENT_SETTINGS,
                            &mut dm,
                        )
                    }
                    .as_bool()
                    {
                        return (w, h, cand);
                    }
                }
            }
        }
        (1280, 720, 60)
    }
}
