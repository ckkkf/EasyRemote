//! NVENC 硬件编码后端（Windows / NVIDIA GeForce）。
//!
//! 绑定方式：**运行时动态加载** `nvEncodeAPI64.dll`（随 NVIDIA 驱动安装），
//! 不依赖 CUDA SDK / nv-codec-headers 的链接期产物，也不需要在构建机装任何 SDK。
//! `bindings.rs` 由 `bindgen` 从仓库内 vendored 的 `third_party/ffnvcodec/nvEncodeAPI.h`
//! （NVENC API 13.1）生成，生成命令见 `third_party/ffnvcodec/README.md`。
//!
//! 两条输入路径：
//!
//! 1. **CUDA（copy）** —— 老路径。`cuMemAllocPitch` 分配 → `cuMemcpy2D` 把
//!    host BGRA 整帧上传 → `nvEncRegisterResource(NV_ENC_INPUT_RESOURCE_TYPE_CUDADEVICEPTR)`。
//!    每帧都有一次 16.6MB 级别的 host→device 上传（2880x1620）。
//! 2. **D3D11 / DirectX（zero-copy）** —— 新路径。会话用
//!    `NV_ENC_DEVICE_TYPE_DIRECTX` 开到调用方的 `ID3D11Device` 上，
//!    `nvEncRegisterResource(NV_ENC_INPUT_RESOURCE_TYPE_DIRECTX)` 直接注册
//!    `ID3D11Texture2D`，NVENC 在显存里取样，**没有 cuMemcpy2D、没有 host 读回**。
//!    与 `er-capture` 的 DXGI Desktop Duplication 后端配套。

// bindgen 产物的命名与位域风格和本 crate 规范不同，这里统一放开。
#[allow(
    non_upper_case_globals,
    non_camel_case_types,
    non_snake_case,
    dead_code,
    clippy::all
)]
pub mod bindings;

use std::ffi::c_void;
use std::ptr;

use bindings as nv;

use crate::{CodecError, EncodeConfig, Encoder, EncoderKind, Packet};
use er_common::protocol::MediaHeader;

#[link(name = "kernel32")]
extern "system" {
    fn LoadLibraryA(name: *const u8) -> *mut c_void;
    fn GetProcAddress(module: *mut c_void, name: *const u8) -> *mut c_void;
}

// ---------------------------------------------------------------------------
// 版本号与 GUID（头文件里是宏 / static const，bindgen 不生成常量，这里照抄数值）
// ---------------------------------------------------------------------------

/// NVENCAPI_MAJOR_VERSION 13 / NVENCAPI_MINOR_VERSION 1
const NVENCAPI_VERSION: u32 = 0x0100_000D;

const fn struct_version(ver: u32) -> u32 {
    NVENCAPI_VERSION | (ver << 16) | (0x7 << 28)
}

const VER_OPEN_SESSION: u32 = struct_version(1);
const VER_INIT: u32 = struct_version(7) | (1 << 31);
const VER_PRESET: u32 = struct_version(5) | (1 << 31);
const VER_CONFIG: u32 = struct_version(9) | (1 << 31);
const VER_PIC: u32 = struct_version(7) | (1 << 31);
const VER_LOCK_BITSTREAM: u32 = struct_version(2) | (1 << 31);
const VER_MAP: u32 = struct_version(4);
const VER_REGISTER: u32 = struct_version(5);
const VER_CREATE_BITSTREAM: u32 = struct_version(1);

const fn guid(d1: u32, d2: u16, d3: u16, d4: [u8; 8]) -> nv::GUID {
    nv::GUID {
        Data1: d1,
        Data2: d2,
        Data3: d3,
        Data4: d4,
    }
}

/// {6BC82762-4E63-4CA4-AA85-1E50F321F6BF}
const GUID_CODEC_H264: nv::GUID = guid(
    0x6bc82762,
    0x4e63,
    0x4ca4,
    [0xaa, 0x85, 0x1e, 0x50, 0xf3, 0x21, 0xf6, 0xbf],
);
/// {E7CBC309-4F7A-4B89-AF2A-D537C92BE310}
const GUID_PROFILE_H264_HIGH: nv::GUID = guid(
    0xe7cbc309,
    0x4f7a,
    0x4b89,
    [0xaf, 0x2a, 0xd5, 0x37, 0xc9, 0x2b, 0xe3, 0x10],
);
/// {36850110-3A07-441F-94D5-3670631F91F6}
const GUID_PRESET_P3: nv::GUID = guid(
    0x36850110,
    0x3a07,
    0x441f,
    [0x94, 0xd5, 0x36, 0x70, 0x63, 0x1f, 0x91, 0xf6],
);

const NV_ENC_SUCCESS: u32 = 0;
const NV_ENC_ERR_NEED_MORE_INPUT: u32 = 11;
const NV_ENC_TUNING_ULTRA_LOW_LATENCY: u32 = 3;
const NV_ENC_DEVICE_TYPE_CUDA: u32 = 1;
const NV_ENC_BUFFER_FORMAT_ARGB: u32 = 0x0100_0000;
const NV_ENC_INPUT_RESOURCE_TYPE_CUDADEVICEPTR: u32 = 1;
/// ID3D11Texture2D 直通：NVENC 自己在显存里取纹理，无 host 拷贝
const NV_ENC_INPUT_RESOURCE_TYPE_DIRECTX: u32 = 0;
const NV_ENC_DEVICE_TYPE_DIRECTX: u32 = 0;
const NV_ENC_INPUT_IMAGE: u32 = 0;
const NV_ENC_PARAMS_RC_CBR: u32 = 2;
const NV_ENC_H264_ENTROPY_CABAC: u32 = 1;
const NV_ENC_PIC_STRUCT_FRAME: u32 = 1;
const NV_ENC_PIC_FLAG_FORCEIDR: u32 = 2;

// ---------------------------------------------------------------------------
// 动态加载
// ---------------------------------------------------------------------------

struct Module(*mut c_void);

impl Module {
    fn load(name: &str) -> Option<Self> {
        let mut c = name.as_bytes().to_vec();
        c.push(0);
        let h = unsafe { LoadLibraryA(c.as_ptr()) };
        if h.is_null() {
            None
        } else {
            Some(Module(h))
        }
    }

    fn sym(&self, name: &str) -> Option<*mut c_void> {
        let mut c = name.as_bytes().to_vec();
        c.push(0);
        let p = unsafe { GetProcAddress(self.0, c.as_ptr()) };
        if p.is_null() {
            None
        } else {
            Some(p)
        }
    }
}

// CUDA 驱动 API（nvcuda.dll，随 NVIDIA 驱动安装）
type CuMemAllocPitch = unsafe extern "system" fn(*mut u64, *mut usize, usize, usize, u32) -> i32;
type CuMemFree = unsafe extern "system" fn(u64) -> i32;
type CuMemcpy2D = unsafe extern "system" fn(*const CudaMemcpy2D) -> i32;
type CuCtxCreate = unsafe extern "system" fn(*mut *mut c_void, u32, i32) -> i32;

#[repr(C)]
struct CudaMemcpy2D {
    src_x_in_bytes: usize,
    src_y: usize,
    src_memory_type: u32,
    src_host: *const c_void,
    src_device: u64,
    src_array: *mut c_void,
    src_pitch: usize,
    dst_x_in_bytes: usize,
    dst_y: usize,
    dst_memory_type: u32,
    dst_host: *mut c_void,
    dst_device: u64,
    dst_array: *mut c_void,
    dst_pitch: usize,
    width_in_bytes: usize,
    height: usize,
}

const CU_MEMORYTYPE_HOST: u32 = 1;
const CU_MEMORYTYPE_DEVICE: u32 = 2;

struct Cuda {
    mem_alloc_pitch: CuMemAllocPitch,
    mem_free: CuMemFree,
    memcpy2d: CuMemcpy2D,
    ctx: *mut c_void,
}

impl Cuda {
    fn open() -> Result<Self, CodecError> {
        let m = Module::load("nvcuda.dll").ok_or(CodecError::Unavailable)?;
        unsafe {
            let init: unsafe extern "system" fn(u32) -> i32 =
                std::mem::transmute(m.sym("cuInit").ok_or(CodecError::Unavailable)?);
            if init(0) != 0 {
                return Err(CodecError::Unavailable);
            }
            let ctx_create: CuCtxCreate =
                std::mem::transmute(m.sym("cuCtxCreate_v2").ok_or(CodecError::Unavailable)?);
            let mut ctx: *mut c_void = ptr::null_mut();
            if ctx_create(&mut ctx, 0, 0) != 0 || ctx.is_null() {
                return Err(CodecError::Unavailable);
            }
            let alloc: CuMemAllocPitch =
                std::mem::transmute(m.sym("cuMemAllocPitch_v2").ok_or(CodecError::Unavailable)?);
            let free: CuMemFree =
                std::mem::transmute(m.sym("cuMemFree_v2").ok_or(CodecError::Unavailable)?);
            let copy: CuMemcpy2D =
                std::mem::transmute(m.sym("cuMemcpy2D_v2").ok_or(CodecError::Unavailable)?);
            Ok(Self {
                mem_alloc_pitch: alloc,
                mem_free: free,
                memcpy2d: copy,
                ctx,
            })
        }
    }
}

// ---------------------------------------------------------------------------
// 能力探测
// ---------------------------------------------------------------------------

/// 运行时探测结果。**只有真正能开出 NVENC 会话才算可用**，
/// 光有显卡名不算——驱动版本过旧、显卡被独占、会话数用尽都会失败。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NvencProbe {
    pub available: bool,
    /// 驱动支持的最大 NVENC API 版本（高 16 位主版本 / 低 16 位次版本由驱动给出）
    pub max_api_version: u32,
    /// 本机 NVIDIA 显示适配器名字（NVML 读取；读不到就是 None）
    pub gpu_name: Option<String>,
    /// NVML 报告的驱动版本
    pub driver_version: Option<String>,
    /// 不可用时的原因
    pub detail: String,
}

/// 读 NVML：显卡名 + 驱动版本。缺失返回 (None, None)。
fn nvml_info() -> (Option<String>, Option<String>) {
    let Some(m) = Module::load("nvml.dll") else {
        return (None, None);
    };
    unsafe {
        let init: Option<unsafe extern "system" fn() -> i32> =
            m.sym("nvmlInit_v2").map(|p| std::mem::transmute(p));
        let handle_by_index: Option<unsafe extern "system" fn(u32, *mut *mut c_void) -> i32> = m
            .sym("nvmlDeviceGetHandleByIndex_v2")
            .map(|p| std::mem::transmute(p));
        let get_name: Option<unsafe extern "system" fn(*mut c_void, *mut u8, u32) -> i32> =
            m.sym("nvmlDeviceGetName").map(|p| std::mem::transmute(p));
        let get_driver: Option<unsafe extern "system" fn(*mut u8, u32) -> i32> = m
            .sym("nvmlSystemGetDriverVersion")
            .map(|p| std::mem::transmute(p));
        let (Some(init), Some(handle_by_index), Some(get_name)) = (init, handle_by_index, get_name)
        else {
            return (None, None);
        };
        if init() != 0 {
            return (None, None);
        }
        let mut handle: *mut c_void = ptr::null_mut();
        if handle_by_index(0, &mut handle) != 0 || handle.is_null() {
            return (None, None);
        }
        let mut buf = [0u8; 96];
        let name = if get_name(handle, buf.as_mut_ptr(), 96) == 0 {
            Some(cstr(&buf))
        } else {
            None
        };
        let mut dbuf = [0u8; 96];
        let driver = match get_driver {
            Some(f) if f(dbuf.as_mut_ptr(), 96) == 0 => Some(cstr(&dbuf)),
            _ => None,
        };
        (name, driver)
    }
}

fn cstr(buf: &[u8]) -> String {
    let end = buf.iter().position(|b| *b == 0).unwrap_or(buf.len());
    String::from_utf8_lossy(&buf[..end]).to_string()
}

/// 探测本机 NVENC 是否可用。
pub fn probe() -> NvencProbe {
    let (gpu_name, driver_version) = nvml_info();
    let Some(mod_) = Module::load("nvEncodeAPI64.dll") else {
        return NvencProbe {
            available: false,
            max_api_version: 0,
            gpu_name,
            driver_version,
            detail: "nvEncodeAPI64.dll not found (no NVIDIA driver)".into(),
        };
    };
    let Some(create_ptr) = mod_.sym("NvEncodeAPICreateInstance") else {
        return NvencProbe {
            available: false,
            max_api_version: 0,
            gpu_name,
            driver_version,
            detail: "nvEncodeAPI64.dll missing NvEncodeAPICreateInstance".into(),
        };
    };
    unsafe {
        let get_max: Option<unsafe extern "system" fn(*mut u32) -> u32> = mod_
            .sym("NvEncodeAPIGetMaxSupportedVersion")
            .map(|p| std::mem::transmute(p));
        let mut max_api = 0u32;
        let _ = get_max.map(|f| f(&mut max_api));

        let mut list: nv::NV_ENCODE_API_FUNCTION_LIST = std::mem::zeroed();
        list.version = VER_OPEN_SESSION;
        let create: unsafe extern "system" fn(*mut nv::NV_ENCODE_API_FUNCTION_LIST) -> u32 =
            std::mem::transmute(create_ptr);
        if create(&mut list) != NV_ENC_SUCCESS {
            return NvencProbe {
                available: false,
                max_api_version: max_api,
                gpu_name,
                driver_version,
                detail: "NvEncodeAPICreateInstance failed".into(),
            };
        }

        // 真正开一次 CUDA 会话，能开才算可用
        let cuda = match Cuda::open() {
            Ok(c) => c,
            Err(_) => {
                return NvencProbe {
                    available: false,
                    max_api_version: max_api,
                    gpu_name,
                    driver_version,
                    detail: "cannot create CUDA context".into(),
                }
            }
        };
        let mut params: nv::NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS = std::mem::zeroed();
        params.version = VER_OPEN_SESSION;
        params.deviceType = NV_ENC_DEVICE_TYPE_CUDA;
        params.device = cuda.ctx;
        params.apiVersion = NVENCAPI_VERSION;
        let mut enc: *mut c_void = ptr::null_mut();
        let status = match list.nvEncOpenEncodeSessionEx {
            Some(f) => f(&mut params, &mut enc),
            None => {
                return NvencProbe {
                    available: false,
                    max_api_version: max_api,
                    gpu_name,
                    driver_version,
                    detail: "nvEncOpenEncodeSessionEx missing".into(),
                }
            }
        };
        if status != NV_ENC_SUCCESS || enc.is_null() {
            return NvencProbe {
                available: false,
                max_api_version: max_api,
                gpu_name,
                driver_version,
                detail: format!("nvEncOpenEncodeSessionEx failed: status={status}"),
            };
        }
        // 检查 H.264 编码 GUID 是否受支持
        let mut count = 0u32;
        let ok = match list.nvEncGetEncodeGUIDCount {
            Some(f) => f(enc, &mut count) == NV_ENC_SUCCESS && count > 0,
            None => false,
        };
        let mut has_h264 = false;
        if ok {
            let mut guids: Vec<nv::GUID> = vec![std::mem::zeroed(); count as usize];
            let mut got = 0u32;
            if let Some(f) = list.nvEncGetEncodeGUIDs {
                if f(enc, guids.as_mut_ptr(), count, &mut got) == NV_ENC_SUCCESS {
                    has_h264 = guids[..got as usize]
                        .iter()
                        .any(|g| g.Data1 == GUID_CODEC_H264.Data1);
                }
            }
        }
        if let Some(f) = list.nvEncDestroyEncoder {
            f(enc);
        }
        NvencProbe {
            available: has_h264,
            max_api_version: max_api,
            gpu_name,
            driver_version,
            detail: if has_h264 {
                "nvEnc session opened, H.264 encode GUID supported".into()
            } else {
                "H.264 encode GUID not advertised".into()
            },
        }
    }
}

// ---------------------------------------------------------------------------
// 编码器
// ---------------------------------------------------------------------------

/// 输入设备后端。
///
/// * CUDA：老的输入路径。BGRA 主机内存 → cuMemcpy2D → CUDA 设备指针 → NVENC。
///   有整帧 host→device 上传（2880x1620 一帧约 16.6MB）。
/// * D3d11：新输入路径。直接 nvEncRegisterResource(NV_ENC_INPUT_RESOURCE_TYPE_DIRECTX)
///   注册 ID3D11Texture2D，NVENC 自己在显存里取样，完全没有 cuMemcpy2D。
enum InputBackend {
    Cuda {
        cuda: Cuda,
        dev_ptr: u64,
        pitch: usize,
    },
    D3d11,
}

/// NVENC H.264 硬件编码器。
pub struct NvencEncoder {
    cfg: EncodeConfig,
    api: nv::NV_ENCODE_API_FUNCTION_LIST,
    backend: InputBackend,
    /// 已注册的输入资源（CUDA 设备指针 或 ID3D11Texture2D*）
    registered: *mut c_void,
    /// nvEncMapInputResource 得到的 NVENC 输入句柄
    mapped: *mut c_void,
    /// NV_ENC_PIC_PARAMS::inputPitch；D3D11 路径为 0（pitch 由资源自身决定）
    input_pitch: u32,
    bitstream: *mut c_void,
    encoder: *mut c_void,
    seq: u16,
    frame_idx: u32,
    next_keyframe: bool,
    encoded_since_intra: u32,
    /// 硬件名（构造时探测，便于日志）
    pub gpu_name: Option<String>,
}

impl NvencEncoder {
    /// 开 NVENC 会话 + 初始化 H.264 配置。device_type/device 决定输入后端。
    fn open_session(
        cfg: EncodeConfig,
        device_type: u32,
        device: *mut c_void,
    ) -> Result<(nv::NV_ENCODE_API_FUNCTION_LIST, *mut c_void), CodecError> {
        let mod_ = Module::load("nvEncodeAPI64.dll").ok_or(CodecError::Unavailable)?;
        let create_ptr = mod_
            .sym("NvEncodeAPICreateInstance")
            .ok_or(CodecError::Unavailable)?;
        let mut api: nv::NV_ENCODE_API_FUNCTION_LIST = unsafe { std::mem::zeroed() };
        api.version = VER_OPEN_SESSION;
        unsafe {
            let create: unsafe extern "system" fn(*mut nv::NV_ENCODE_API_FUNCTION_LIST) -> u32 =
                std::mem::transmute(create_ptr);
            if create(&mut api) != NV_ENC_SUCCESS {
                return Err(CodecError::Unavailable);
            }
        }

        // 1. 开会话
        let mut open: nv::NV_ENC_OPEN_ENCODE_SESSION_EX_PARAMS = unsafe { std::mem::zeroed() };
        open.version = VER_OPEN_SESSION;
        open.deviceType = device_type;
        open.device = device;
        open.apiVersion = NVENCAPI_VERSION;
        let mut encoder: *mut c_void = ptr::null_mut();
        let st = unsafe {
            api.nvEncOpenEncodeSessionEx
                .ok_or(CodecError::Unavailable)?(&mut open, &mut encoder)
        };
        if st != NV_ENC_SUCCESS {
            return Err(CodecError::Internal(format!(
                "open session(type={device_type}): {st}"
            )));
        }

        // 2. 取低延迟预设配置，再按我们的参数覆盖
        let mut preset: nv::NV_ENC_PRESET_CONFIG = unsafe { std::mem::zeroed() };
        preset.version = VER_PRESET;
        preset.presetCfg.version = VER_CONFIG;
        let st = unsafe {
            api.nvEncGetEncodePresetConfigEx
                .ok_or(CodecError::Unavailable)?(
                encoder,
                GUID_CODEC_H264,
                GUID_PRESET_P3,
                NV_ENC_TUNING_ULTRA_LOW_LATENCY,
                &mut preset,
            )
        };
        if st != NV_ENC_SUCCESS {
            unsafe { api.nvEncDestroyEncoder.map(|f| f(encoder)) };
            return Err(CodecError::Internal(format!("preset config: {st}")));
        }
        let mut config = preset.presetCfg;
        config.version = VER_CONFIG;
        config.profileGUID = GUID_PROFILE_H264_HIGH;
        config.gopLength = cfg.gop.max(1);
        // 无 B 帧 = frameIntervalP 1（低延迟预设的硬约束）
        config.frameIntervalP = 1;
        config.rcParams.rateControlMode = NV_ENC_PARAMS_RC_CBR;
        config.rcParams.averageBitRate = cfg.bitrate_kbps.saturating_mul(1000);
        config.rcParams.maxBitRate = cfg.bitrate_kbps.saturating_mul(1000);
        config.rcParams.vbvBufferSize = cfg.bitrate_kbps.saturating_mul(1000) / cfg.fps.max(1);
        config.rcParams.vbvInitialDelay = config.rcParams.vbvBufferSize;
        let h264 = unsafe { &mut config.encodeCodecConfig.h264Config };
        h264.idrPeriod = cfg.gop.max(1);
        h264.sliceMode = 0;
        h264.entropyCodingMode = NV_ENC_H264_ENTROPY_CABAC;
        // 每个 IDR 都带 SPS/PPS：中途加入的解码器也能起解
        h264.set_repeatSPSPPS(1);

        let mut init: nv::NV_ENC_INITIALIZE_PARAMS = unsafe { std::mem::zeroed() };
        init.version = VER_INIT;
        init.encodeGUID = GUID_CODEC_H264;
        init.presetGUID = GUID_PRESET_P3;
        init.encodeWidth = cfg.width;
        init.encodeHeight = cfg.height;
        init.darWidth = cfg.width;
        init.darHeight = cfg.height;
        init.frameRateNum = cfg.fps;
        init.frameRateDen = 1;
        init.enableEncodeAsync = 0; // 同步模式：单帧即取即走，延迟最低
        init.enablePTD = 1;
        init.encodeConfig = &mut config;
        init.tuningInfo = NV_ENC_TUNING_ULTRA_LOW_LATENCY;
        init.bufferFormat = NV_ENC_BUFFER_FORMAT_ARGB;
        let st = unsafe {
            api.nvEncInitializeEncoder.ok_or(CodecError::Unavailable)?(encoder, &mut init)
        };
        if st != NV_ENC_SUCCESS {
            unsafe { api.nvEncDestroyEncoder.map(|f| f(encoder)) };
            return Err(CodecError::Internal(format!("initialize: {st}")));
        }
        Ok((api, encoder))
    }

    /// 输出码流缓冲。
    fn create_bitstream(
        api: &nv::NV_ENCODE_API_FUNCTION_LIST,
        encoder: *mut c_void,
    ) -> Result<*mut c_void, CodecError> {
        let mut bs: nv::NV_ENC_CREATE_BITSTREAM_BUFFER = unsafe { std::mem::zeroed() };
        bs.version = VER_CREATE_BITSTREAM;
        let st = unsafe {
            api.nvEncCreateBitstreamBuffer
                .ok_or(CodecError::Unavailable)?(encoder, &mut bs)
        };
        if st != NV_ENC_SUCCESS {
            return Err(CodecError::Internal(format!("create bitstream: {st}")));
        }
        Ok(bs.bitstreamBuffer)
    }

    /// CUDA 路径（原始实现）：CUDA 设备内存 + cuMemcpy2D 上传。
    pub fn new(cfg: EncodeConfig) -> Result<Self, CodecError> {
        if cfg.width == 0 || cfg.height == 0 || cfg.fps == 0 || !cfg.is_low_latency() {
            return Err(CodecError::UnsupportedConfig);
        }
        let (gpu_name, _driver) = nvml_info();
        let cuda = Cuda::open()?;
        let (api, encoder) = Self::open_session(cfg, NV_ENC_DEVICE_TYPE_CUDA, cuda.ctx)?;

        // 输入缓冲：CUDA 设备内存，pitch 由 cuMemAllocPitch 给出
        let row_bytes = cfg.width as usize * 4;
        let mut dev_ptr = 0u64;
        let mut pitch = 0usize;
        let rc = unsafe {
            (cuda.mem_alloc_pitch)(&mut dev_ptr, &mut pitch, row_bytes, cfg.height as usize, 16)
        };
        if rc != 0 || dev_ptr == 0 {
            unsafe { api.nvEncDestroyEncoder.map(|f| f(encoder)) };
            return Err(CodecError::Internal(format!("cuMemAllocPitch: {rc}")));
        }
        let mut this = Self {
            cfg,
            api,
            backend: InputBackend::Cuda {
                cuda,
                dev_ptr,
                pitch,
            },
            registered: ptr::null_mut(),
            mapped: ptr::null_mut(),
            input_pitch: pitch as u32,
            bitstream: ptr::null_mut(),
            encoder,
            seq: 0,
            frame_idx: 0,
            next_keyframe: true,
            encoded_since_intra: 0,
            gpu_name,
        };
        let pitch_u32 = this.input_pitch;
        match this.register_input(
            dev_ptr as *mut c_void,
            NV_ENC_INPUT_RESOURCE_TYPE_CUDADEVICEPTR,
            pitch_u32,
        ) {
            Ok(()) => {}
            Err(e) => {
                unsafe { this.api.nvEncDestroyEncoder.map(|f| f(this.encoder)) };
                unsafe { (this.backend_cuda().unwrap().0.mem_free)(dev_ptr) };
                return Err(e);
            }
        }
        match Self::create_bitstream(&this.api, this.encoder) {
            Ok(bs) => this.bitstream = bs,
            Err(e) => {
                this.unregister_input();
                unsafe { this.api.nvEncDestroyEncoder.map(|f| f(this.encoder)) };
                return Err(e);
            }
        }
        Ok(this)
    }

    /// D3D11 路径：整个会话挂在调用方给的 ID3D11Device 上，
    /// 后续用 register_d3d11_input 注册同一块设备上的纹理，零 host 拷贝。
    pub fn new_d3d11(cfg: EncodeConfig, device: *mut c_void) -> Result<Self, CodecError> {
        if cfg.width == 0 || cfg.height == 0 || cfg.fps == 0 || !cfg.is_low_latency() {
            return Err(CodecError::UnsupportedConfig);
        }
        if device.is_null() {
            return Err(CodecError::Unavailable);
        }
        let (gpu_name, _driver) = nvml_info();
        let (api, encoder) = Self::open_session(cfg, NV_ENC_DEVICE_TYPE_DIRECTX, device)?;
        let bitstream = match Self::create_bitstream(&api, encoder) {
            Ok(b) => b,
            Err(e) => {
                unsafe { api.nvEncDestroyEncoder.map(|f| f(encoder)) };
                return Err(e);
            }
        };
        Ok(Self {
            cfg,
            api,
            backend: InputBackend::D3d11,
            registered: ptr::null_mut(),
            mapped: ptr::null_mut(),
            input_pitch: 0,
            bitstream,
            encoder,
            seq: 0,
            frame_idx: 0,
            next_keyframe: true,
            encoded_since_intra: 0,
            gpu_name,
        })
    }

    fn backend_cuda(&mut self) -> Option<(&Cuda, u64)> {
        match &self.backend {
            InputBackend::Cuda { cuda, dev_ptr, .. } => Some((cuda, *dev_ptr)),
            InputBackend::D3d11 => None,
        }
    }

    /// 输入后端名字（证据/日志用）。
    pub fn input_backend_name(&self) -> &'static str {
        match self.backend {
            InputBackend::Cuda { .. } => "cuda(copy)",
            InputBackend::D3d11 => "directx(zero-copy)",
        }
    }

    pub fn gpu_name(&self) -> Option<&str> {
        self.gpu_name.as_deref()
    }

    /// 注册 + 映射一块输入资源。
    fn register_input(
        &mut self,
        resource: *mut c_void,
        resource_type: u32,
        pitch: u32,
    ) -> Result<(), CodecError> {
        self.unregister_input();
        let mut reg: nv::NV_ENC_REGISTER_RESOURCE = unsafe { std::mem::zeroed() };
        reg.version = VER_REGISTER;
        reg.resourceType = resource_type;
        reg.width = self.cfg.width;
        reg.height = self.cfg.height;
        // DIRECTX 资源必须填 0（NVENC 从纹理自身取 pitch）
        reg.pitch = pitch;
        reg.resourceToRegister = resource;
        reg.bufferFormat = NV_ENC_BUFFER_FORMAT_ARGB;
        reg.bufferUsage = NV_ENC_INPUT_IMAGE;
        let st = unsafe {
            self.api
                .nvEncRegisterResource
                .ok_or(CodecError::Unavailable)?(self.encoder, &mut reg)
        };
        if st != NV_ENC_SUCCESS {
            return Err(CodecError::Internal(format!(
                "register resource(type={resource_type}): {st}"
            )));
        }
        let mut map: nv::NV_ENC_MAP_INPUT_RESOURCE = unsafe { std::mem::zeroed() };
        map.version = VER_MAP;
        map.registeredResource = reg.registeredResource;
        let st = unsafe {
            self.api
                .nvEncMapInputResource
                .ok_or(CodecError::Unavailable)?(self.encoder, &mut map)
        };
        if st != NV_ENC_SUCCESS {
            unsafe {
                self.api
                    .nvEncUnregisterResource
                    .map(|f| f(self.encoder, reg.registeredResource))
            };
            return Err(CodecError::Internal(format!("map input: {st}")));
        }
        self.registered = reg.registeredResource;
        self.mapped = map.mappedResource;
        Ok(())
    }

    /// 解注册当前输入资源（幂等）。
    fn unregister_input(&mut self) {
        unsafe {
            if !self.mapped.is_null() {
                self.api
                    .nvEncUnmapInputResource
                    .map(|f| f(self.encoder, self.mapped));
                self.mapped = ptr::null_mut();
            }
            if !self.registered.is_null() {
                self.api
                    .nvEncUnregisterResource
                    .map(|f| f(self.encoder, self.registered));
                self.registered = ptr::null_mut();
            }
        }
    }

    /// 注册一块常驻的 ID3D11Texture2D（一次注册、多帧复用）。
    ///
    /// texture 必须与采集/NVENC 会话同一块 D3D11 设备，尺寸等于 cfg.width/height，
    /// 格式 B8G8R8A8_UNORM（= NV_ENC_BUFFER_FORMAT_ARGB）。
    pub fn register_d3d11_input(&mut self, texture: *mut c_void) -> Result<(), CodecError> {
        if matches!(self.backend, InputBackend::Cuda { .. }) {
            return Err(CodecError::UnsupportedConfig);
        }
        if texture.is_null() {
            return Err(CodecError::Unavailable);
        }
        self.register_input(texture, NV_ENC_INPUT_RESOURCE_TYPE_DIRECTX, 0)
    }

    /// 编码「已注册」的 D3D11 纹理（零拷贝：没有 cuMemcpy2D，也没有 host 读回）。
    pub fn encode_d3d11_registered(&mut self, ts_ms: u64) -> Result<Vec<u8>, CodecError> {
        if self.mapped.is_null() {
            return Err(CodecError::Internal("no registered d3d11 input".into()));
        }
        self.submit(ts_ms)
    }

    /// 注册 → 编码 → 解注册，直接吃一块外来 ID3D11Texture2D。
    ///
    /// 这是最纯粹的零拷贝（连 GPU CopyResource 都省了），代价是每帧一次
    /// register/map/unmap/unregister。
    pub fn encode_d3d11_texture(
        &mut self,
        texture: *mut c_void,
        ts_ms: u64,
    ) -> Result<Vec<u8>, CodecError> {
        if matches!(self.backend, InputBackend::Cuda { .. }) {
            return Err(CodecError::UnsupportedConfig);
        }
        if texture.is_null() {
            return Err(CodecError::Unavailable);
        }
        self.register_input(texture, NV_ENC_INPUT_RESOURCE_TYPE_DIRECTX, 0)?;
        let out = self.submit(ts_ms);
        self.unregister_input();
        out
    }

    /// 编码一帧 BGRA（必须 width*height*4）→ Annex-B H.264（CUDA 上传路径）。
    pub fn encode_bgra(&mut self, frame: &[u8], ts_ms: u64) -> Result<Vec<u8>, CodecError> {
        let expected = self.cfg.width as usize * self.cfg.height as usize * 4;
        if frame.len() != expected {
            return Err(CodecError::DimensionMismatch);
        }
        match self.backend {
            InputBackend::Cuda { dev_ptr, pitch, .. } => {
                // 上传：host → device
                let copy = CudaMemcpy2D {
                    src_x_in_bytes: 0,
                    src_y: 0,
                    src_memory_type: CU_MEMORYTYPE_HOST,
                    src_host: frame.as_ptr() as *const c_void,
                    src_device: 0,
                    src_array: ptr::null_mut(),
                    src_pitch: self.cfg.width as usize * 4,
                    dst_x_in_bytes: 0,
                    dst_y: 0,
                    dst_memory_type: CU_MEMORYTYPE_DEVICE,
                    dst_host: ptr::null_mut(),
                    dst_device: dev_ptr,
                    dst_array: ptr::null_mut(),
                    dst_pitch: pitch,
                    width_in_bytes: self.cfg.width as usize * 4,
                    height: self.cfg.height as usize,
                };
                let rc = unsafe {
                    match &self.backend {
                        InputBackend::Cuda { cuda, .. } => (cuda.memcpy2d)(&copy),
                        InputBackend::D3d11 => return Err(CodecError::UnsupportedConfig),
                    }
                };
                if rc != 0 {
                    return Err(CodecError::Internal(format!("cuMemcpy2D: {rc}")));
                }
                self.submit(ts_ms)
            }
            InputBackend::D3d11 => Err(CodecError::UnsupportedConfig),
        }
    }

    /// 送一帧进 NVENC（三条路径共用：CUDA 上传后 / 常驻注册 / 临时注册）。
    fn submit(&mut self, ts_ms: u64) -> Result<Vec<u8>, CodecError> {
        let force_idr = self.next_keyframe || self.encoded_since_intra >= self.cfg.gop.max(1);
        let mut pic: nv::NV_ENC_PIC_PARAMS = unsafe { std::mem::zeroed() };
        pic.version = VER_PIC;
        pic.inputWidth = self.cfg.width;
        pic.inputHeight = self.cfg.height;
        pic.inputPitch = self.input_pitch;
        pic.inputBuffer = self.mapped;
        pic.outputBitstream = self.bitstream;
        pic.bufferFmt = NV_ENC_BUFFER_FORMAT_ARGB;
        pic.pictureStruct = NV_ENC_PIC_STRUCT_FRAME;
        pic.frameIdx = self.frame_idx;
        pic.inputTimeStamp = ts_ms;
        pic.encodePicFlags = if force_idr {
            NV_ENC_PIC_FLAG_FORCEIDR
        } else {
            0
        };

        let mut data = Vec::new();
        for attempt in 0..4 {
            let st = unsafe {
                self.api.nvEncEncodePicture.ok_or(CodecError::Unavailable)?(self.encoder, &mut pic)
            };
            if st == NV_ENC_SUCCESS {
                data = self.lock_bitstream()?;
                break;
            }
            if st == NV_ENC_ERR_NEED_MORE_INPUT && attempt < 3 {
                continue;
            }
            return Err(CodecError::Internal(format!("encode picture: {st}")));
        }
        if force_idr {
            self.next_keyframe = false;
            self.encoded_since_intra = 0;
        } else {
            self.encoded_since_intra += 1;
        }
        self.frame_idx = self.frame_idx.wrapping_add(1);
        Ok(data)
    }

    fn lock_bitstream(&mut self) -> Result<Vec<u8>, CodecError> {
        let mut lock: nv::NV_ENC_LOCK_BITSTREAM = unsafe { std::mem::zeroed() };
        lock.version = VER_LOCK_BITSTREAM;
        lock.outputBitstream = self.bitstream;
        // doNotWait 是位域，默认 0（= 阻塞等待），不用改
        let st = unsafe {
            self.api.nvEncLockBitstream.ok_or(CodecError::Unavailable)?(self.encoder, &mut lock)
        };
        if st != NV_ENC_SUCCESS {
            return Err(CodecError::Internal(format!("lock bitstream: {st}")));
        }
        let n = lock.bitstreamSizeInBytes as usize;
        let mut out = Vec::with_capacity(n);
        unsafe {
            let p = lock.bitstreamBufferPtr as *const u8;
            out.extend_from_slice(std::slice::from_raw_parts(p, n));
        }
        unsafe {
            self.api
                .nvEncUnlockBitstream
                .map(|f| f(self.encoder, self.bitstream));
        }
        Ok(out)
    }
}

impl Encoder for NvencEncoder {
    fn kind(&self) -> EncoderKind {
        EncoderKind::Nvenc
    }

    fn encode(&mut self, frame_ts_ms: u64, frame: &[u8]) -> Result<Packet, CodecError> {
        let force_idr = self.next_keyframe || self.encoded_since_intra >= self.cfg.gop.max(1);
        let data = self.encode_bgra(frame, frame_ts_ms)?;
        if data.is_empty() {
            // 同步模式下偶发空包不是错误，交给上层丢弃
            return Err(CodecError::Internal(
                "nvEnc produced empty bitstream".into(),
            ));
        }
        let mut flags = MediaHeader::FLAG_FRAME_END;
        if force_idr {
            flags |= MediaHeader::FLAG_KEYFRAME;
        }
        let header = MediaHeader {
            frame_type: 1,
            stream_id: 0,
            seq: self.seq,
            flags,
            ts_ms: (frame_ts_ms & 0xFFFF_FFFF) as u32,
        };
        self.seq = self.seq.wrapping_add(1);
        Ok(Packet { header, data })
    }

    fn request_keyframe(&mut self) {
        self.next_keyframe = true;
        self.encoded_since_intra = 0;
    }
}

impl Drop for NvencEncoder {
    fn drop(&mut self) {
        self.unregister_input();
        unsafe {
            self.api
                .nvEncDestroyBitstreamBuffer
                .map(|f| f(self.encoder, self.bitstream));
            self.api.nvEncDestroyEncoder.map(|f| f(self.encoder));
            if let InputBackend::Cuda { cuda, dev_ptr, .. } = &self.backend {
                (cuda.mem_free)(*dev_ptr);
            }
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;

    /// 探测本身不能 panic：没有 NVIDIA 卡 / 没装驱动 / 被独占都要给出结构体。
    #[test]
    fn probe_never_panics() {
        let p = probe();
        eprintln!("probe: {p:?}");
        assert!(!p.detail.is_empty(), "probe must explain itself");
    }

    /// 非低延迟配置必须在碰驱动之前就被拒绝。
    #[test]
    fn rejects_non_low_latency_config() {
        let cfg = EncodeConfig {
            bframes: 2,
            ..Default::default()
        };
        assert!(matches!(
            NvencEncoder::new(cfg),
            Err(CodecError::UnsupportedConfig)
        ));
        let cfg2 = EncodeConfig {
            width: 0,
            ..Default::default()
        };
        assert!(matches!(
            NvencEncoder::new(cfg2),
            Err(CodecError::UnsupportedConfig)
        ));
    }
}
