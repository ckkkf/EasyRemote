//! 采集 + 编码的组合器：**优先 GPU 零拷贝，失败自动回退 CPU**。
//!
//! 这里是产品路径（`er-client` 的会话与 `er-ffi`）唯一该用的入口。
//!
//! 三种结果必须分清（之前的 bug 就是把 NoFrame 和 Fallback 混在一起，
//! 导致静止桌面时 backend 被误标成 cpu、且没有回退日志）：
//!
//! | GPU 返回 | 含义 | 行为 |
//! |---|---|---|
//! | `Ok(Some)` | 真的用 GPU 编出来了 | backend=gpu-zero-copy |
//! | `Ok(None)` | **桌面没有新帧**（不是失败） | 直接返回 None，不编码、不落 CPU、不改 backend |
//! | `Err` | GPU 路径失效（无硬件/尺寸不符/注册失败/编码失败） | 停用 + 打一次日志 + 本帧交 CPU |

use er_capture::{Capture, ScreenCapture};
use er_codec::{create_encoder, EncodeConfig, Encoder};

use crate::gpu_policy::{GpuFallback, GpuPath};

/// GPU 路径的决策结果。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum GpuDecision {
    /// 用 GPU 编出来了
    Encoded,
    /// 桌面无新帧：不编码，也不改 backend
    NoFrame,
    /// GPU 路径失效：本帧交 CPU
    Fallback,
}

/// 决策点（独立函数，便于无 GPU 单测）。
pub(crate) fn classify_gpu_result(
    result: Result<Option<Vec<u8>>, GpuFallback>,
    policy: &mut GpuPath,
    backend: &mut &'static str,
) -> Option<GpuDecision> {
    match result {
        Ok(Some(_)) => {
            *backend = "gpu-zero-copy";
            Some(GpuDecision::Encoded)
        }
        // 注意：这里**不能**动 backend，也不能把 GPU 路径标成失效
        Ok(None) => Some(GpuDecision::NoFrame),
        Err(reason) => {
            policy.mark_failed(reason);
            if let Some(msg) = policy.take_log() {
                eprintln!("[er] 零拷贝路径不可用，回退 CPU: {msg}");
            }
            *backend = "cpu";
            Some(GpuDecision::Fallback)
        }
    }
}

/// 一帧的完整决策 + 执行（GPU 结果由调用方先算好，方便注入测试）。
///
/// 这里是「NoFrame / Fallback / Encoded」的唯一判定点：
///   - `Some(Ok(Some(b)))` → 用 GPU 的结果，backend=gpu-zero-copy
///   - `Some(Ok(None))`    → 桌面无新帧：返回 Ok(None)，**不动 backend、不调 CPU**
///   - `Some(Err(e))`      → GPU 失效：停用 + 打一次日志 + 本帧交 CPU
///   - `None`              → 没有 GPU 路径（非 Windows / 已停用）：直接 CPU
pub(crate) fn encode_frame_with_gpu<C>(
    policy: &mut GpuPath,
    backend: &mut &'static str,
    gpu_result: Option<Result<Option<Vec<u8>>, GpuFallback>>,
    mut cpu: C,
) -> Result<Option<Vec<u8>>, String>
where
    C: FnMut() -> Result<Option<Vec<u8>>, String>,
{
    match gpu_result {
        Some(Ok(Some(bytes))) => {
            *backend = "gpu-zero-copy";
            Ok(Some(bytes))
        }
        Some(Ok(None)) => {
            // 无新帧 ≠ 失败：策略不变、backend 不变、不落 CPU
            policy.on_no_frame();
            Ok(None)
        }
        Some(Err(reason)) => {
            policy.mark_failed(reason);
            if let Some(msg) = policy.take_log() {
                eprintln!("[er] 零拷贝路径不可用，回退 CPU: {msg}");
            }
            *backend = "cpu";
            cpu()
        }
        None => cpu(),
    }
}

/// 采集 + 编码。GPU 零拷贝可用时走 GPU，否则 CPU。
pub struct HybridEncoder {
    cfg: EncodeConfig,
    cpu: Box<dyn Encoder>,
    /// 当前实际生效的后端名（日志/证据用）："gpu-zero-copy" | "cpu"
    backend: &'static str,
    #[cfg(target_os = "windows")]
    gpu: crate::gpu_glue::GpuGlue,
    #[cfg(target_os = "windows")]
    policy: GpuPath,
    #[cfg(not(target_os = "windows"))]
    policy: GpuPath,
}

impl HybridEncoder {
    pub fn new(cfg: EncodeConfig) -> Result<Self, String> {
        let cpu = create_encoder(cfg).map_err(|e| e.to_string())?;
        Ok(Self {
            cfg,
            cpu,
            backend: "cpu",
            #[cfg(target_os = "windows")]
            gpu: crate::gpu_glue::GpuGlue::new(),
            policy: GpuPath::new(3),
        })
    }

    pub fn config(&self) -> EncodeConfig {
        self.cfg
    }

    /// 当前后端名。只有真的用 GPU 编出来过才会变成 "gpu-zero-copy"；
    /// 静止桌面不会把它翻回 "cpu"。
    pub fn backend(&self) -> &'static str {
        self.backend
    }

    pub fn cpu_kind(&self) -> er_codec::EncoderKind {
        self.cpu.kind()
    }

    #[cfg(target_os = "windows")]
    pub fn gpu_disabled(&self) -> Option<&GpuFallback> {
        self.policy.failure()
    }

    /// 抓一帧并编码。`Ok(None)` = 本轮桌面没有新帧（静止），上游可重复上一帧。
    pub fn capture_and_encode(
        &mut self,
        cap: &mut ScreenCapture,
    ) -> Result<Option<Vec<u8>>, String> {
        // ABR 降分辨率时 cfg 与采集尺寸不一致：零拷贝路径要求「编码尺寸 == 纹理尺寸」，
        // 这里必须直接跳过 GPU 分支。否则 check_dims 每次都失败，会把 GPU 判成失效
        // 并永久回退（这是接入 ABR 最容易踩的坑）。
        let info = cap.info();
        let dims_match = self.cfg.width == info.width && self.cfg.height == info.height;

        if !dims_match {
            return self.capture_scale_encode(cap, info.width, info.height);
        }

        #[cfg(target_os = "windows")]
        let gpu_result: Option<Result<Option<Vec<u8>>, GpuFallback>> = if self.policy.should_try() {
            Some(self.gpu.try_encode(cap, &self.cfg, &mut self.policy))
        } else {
            None
        };
        #[cfg(not(target_os = "windows"))]
        let gpu_result: Option<Result<Option<Vec<u8>>, GpuFallback>> = None;

        let cpu = &mut self.cpu;
        let policy = &mut self.policy;
        let backend = &mut self.backend;
        encode_frame_with_gpu(policy, backend, gpu_result, || {
            let frame = cap.grab().map_err(|e| format!("capture: {e}"))?;
            let pkt = cpu
                .encode(frame.ts_ms, &frame.data)
                .map_err(|e| e.to_string())?;
            Ok(Some(pkt.data))
        })
    }

    /// ABR 改档：换 `EncodeConfig`（码率/分辨率/帧率）。
    ///
    /// 两件事必须同时做，否则会出现「日志说降到 720p，实际还在 2880x1620 编」：
    ///   1. 重建 CPU 编码器（OpenH264 的尺寸在 init 时固定）
    ///   2. 丢掉 NVENC 会话（D3D11 输入纹理与编码器同尺寸，换档只能重开）
    ///
    /// 不重置 `policy`：降分辨率期间 GPU 路径根本没被尝试过（尺寸守卫直接跳过），
    /// 不该因此把 GPU 判成失效。
    pub fn reconfigure(&mut self, cfg: EncodeConfig) -> Result<(), String> {
        let cpu = create_encoder(cfg).map_err(|e| e.to_string())?;
        self.cpu = cpu;
        self.cfg = cfg;
        #[cfg(target_os = "windows")]
        self.gpu.reset();
        Ok(())
    }

    /// 当前是否只能在 CPU 上缩放（`cfg` 尺寸与采集尺寸不一致）。
    pub fn is_downscaled(&self, src_w: u32, src_h: u32) -> bool {
        self.cfg.width != src_w || self.cfg.height != src_h
    }

    /// 降分辨率路径：CPU 取一帧 → 箱式下采样 → 软件编码。
    ///
    /// 为什么不在 GPU 上缩放：D3D11 的 `CopyResource` 不做缩放（源/目标尺寸必须一致），
    /// 要缩放就得引一条 shader 管线。当前阶段先把「ABR 真的改配置」跑通并留证，
    /// 零拷贝仅在同分辨率档位成立——这一点在日志里用 backend=cpu-scaled 明确标出，
    /// 绝不允许出现「以为还在零拷贝」的假象。
    fn capture_scale_encode(
        &mut self,
        cap: &mut ScreenCapture,
        src_w: u32,
        src_h: u32,
    ) -> Result<Option<Vec<u8>>, String> {
        let frame = cap.grab().map_err(|e| format!("capture: {e}"))?;
        let scaled = downscale_bgra(&frame.data, src_w, src_h, self.cfg.width, self.cfg.height)?;
        let pkt = self
            .cpu
            .encode(frame.ts_ms, &scaled)
            .map_err(|e| e.to_string())?;
        self.backend = "cpu-scaled";
        Ok(Some(pkt.data))
    }

    pub fn encode_bgra(&mut self, ts_ms: u64, data: &[u8]) -> Result<Vec<u8>, String> {
        self.cpu
            .encode(ts_ms, data)
            .map(|p| p.data)
            .map_err(|e| e.to_string())
    }

    pub fn request_keyframe(&mut self) {
        self.cpu.request_keyframe();
    }
}

/// BGRA 箱式下采样（区域平均，非最近邻——ABR 降档后画面不能出锯齿）。
///
/// `src_w x src_h` 必须与 `src.len()` 自洽；目标尺寸为 4 的倍数更好，但不是硬要求。
pub fn downscale_bgra(
    src: &[u8],
    src_w: u32,
    src_h: u32,
    dst_w: u32,
    dst_h: u32,
) -> Result<Vec<u8>, String> {
    if src_w == 0 || src_h == 0 || dst_w == 0 || dst_h == 0 {
        return Err("downscale: zero dimension".into());
    }
    let need = (src_w as usize) * (src_h as usize) * 4;
    if src.len() < need {
        return Err(format!("downscale: src too small {} < {need}", src.len()));
    }
    let mut out = vec![0u8; (dst_w as usize) * (dst_h as usize) * 4];
    for dy in 0..dst_h {
        let y0 = (dy as u64 * src_h as u64 / dst_h as u64) as usize;
        let y1 = (((dy + 1) as u64 * src_h as u64) / dst_h as u64).max((y0 + 1) as u64) as usize;
        for dx in 0..dst_w {
            let x0 = (dx as u64 * src_w as u64 / dst_w as u64) as usize;
            let x1 =
                (((dx + 1) as u64 * src_w as u64) / dst_w as u64).max((x0 + 1) as u64) as usize;
            let (mut b, mut g, mut r, mut a, mut n) = (0u32, 0u32, 0u32, 0u32, 0u32);
            for y in y0..y1.min(src_h as usize) {
                let row = y * src_w as usize * 4;
                for x in x0..x1.min(src_w as usize) {
                    let i = row + x * 4;
                    b += src[i] as u32;
                    g += src[i + 1] as u32;
                    r += src[i + 2] as u32;
                    a += src[i + 3] as u32;
                    n += 1;
                }
            }
            let d = ((dy * dst_w + dx) * 4) as usize;
            let n = n.max(1);
            out[d] = (b / n) as u8;
            out[d + 1] = (g / n) as u8;
            out[d + 2] = (r / n) as u8;
            out[d + 3] = (a / n) as u8;
        }
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use er_codec::{CodecError, EncoderKind, Packet};
    use er_common::protocol::MediaHeader;
    use std::cell::Cell;
    use std::rc::Rc;

    /// 计数用 CPU 编码器：只关心「有没有被调用」。
    struct CountingEncoder {
        calls: Rc<Cell<usize>>,
    }

    impl Encoder for CountingEncoder {
        fn kind(&self) -> EncoderKind {
            EncoderKind::Software
        }
        fn encode(&mut self, ts: u64, _frame: &[u8]) -> Result<Packet, CodecError> {
            self.calls.set(self.calls.get() + 1);
            Ok(Packet {
                header: MediaHeader {
                    frame_type: 1,
                    stream_id: 0,
                    seq: 0,
                    flags: MediaHeader::FLAG_FRAME_END | MediaHeader::FLAG_KEYFRAME,
                    ts_ms: ts as u32,
                },
                data: vec![0u8; 8],
            })
        }
        fn request_keyframe(&mut self) {}
    }

    fn policy() -> GpuPath {
        GpuPath::new(3)
    }

    /// 核心回归：GPU 返回 Ok(None)（静止桌面）时
    ///   - 不编码、不落 CPU（CPU 调用次数 0）
    ///   - backend 仍是 gpu-zero-copy（不能被误标成 cpu）
    ///   - GPU 路径不被停用
    #[test]
    fn no_frame_keeps_gpu_backend_and_skips_cpu() {
        let calls = Rc::new(Cell::new(0));
        let calls2 = calls.clone();
        let mut p = policy();
        p.mark_registered();
        let mut backend: &'static str = "gpu-zero-copy";

        let out = encode_frame_with_gpu(&mut p, &mut backend, Some(Ok(None)), || {
            calls2.set(calls2.get() + 1);
            Ok(Some(vec![9u8]))
        })
        .expect("no frame is not an error");

        assert!(out.is_none(), "静止帧不应产出编码数据");
        assert_eq!(backend, "gpu-zero-copy", "backend 不能被 NoFrame 翻成 cpu");
        assert_eq!(calls.get(), 0, "NoFrame 不得调用 CPU 编码器");
        assert!(p.should_try(), "NoFrame 不得停用 GPU 路径");
        assert!(p.failure().is_none());
    }

    /// 真正的 Err 才回退：backend=cpu、GPU 停用、本帧走 CPU（调用 1 次）
    #[test]
    fn real_error_falls_back_to_cpu() {
        let calls = Rc::new(Cell::new(0));
        let calls2 = calls.clone();
        let mut p = policy();
        p.mark_registered();
        let mut backend: &'static str = "gpu-zero-copy";

        let out = encode_frame_with_gpu(
            &mut p,
            &mut backend,
            Some(Err(GpuFallback::RegisterFailed("boom".into()))),
            || {
                calls2.set(calls2.get() + 1);
                Ok(Some(vec![7u8]))
            },
        )
        .expect("fallback still produces a frame");

        assert_eq!(out, Some(vec![7u8]));
        assert_eq!(backend, "cpu");
        assert_eq!(calls.get(), 1, "回退时必须走一次 CPU");
        // RegisterFailed 是「可重试」类别：policy 上限是 3 次，
        // 单次失败后仍应保持可用，到上限才停用。
        assert!(p.should_try(), "单次注册失败后仍可重试");
        for _ in 0..2 {
            let _ = encode_frame_with_gpu(
                &mut p,
                &mut backend,
                Some(Err(GpuFallback::RegisterFailed("boom".into()))),
                || Ok(Some(vec![7u8])),
            );
        }
        assert!(!p.should_try(), "连续失败到上限后应停用 GPU 路径");
    }

    /// GPU 成功：backend 必须是 gpu-zero-copy，且不碰 CPU
    #[test]
    fn gpu_success_skips_cpu() {
        let calls = Rc::new(Cell::new(0));
        let calls2 = calls.clone();
        let mut p = policy();
        p.mark_registered();
        let mut backend: &'static str = "cpu";

        let out = encode_frame_with_gpu(&mut p, &mut backend, Some(Ok(Some(vec![1u8, 2]))), || {
            calls2.set(calls2.get() + 1);
            Ok(None)
        })
        .expect("ok");

        assert_eq!(out, Some(vec![1u8, 2]));
        assert_eq!(backend, "gpu-zero-copy");
        assert_eq!(calls.get(), 0);
    }

    /// 没有 GPU 结果（非 Windows / 已停用）时直接走 CPU
    #[test]
    fn absent_gpu_result_uses_cpu() {
        let calls = Rc::new(Cell::new(0));
        let calls2 = calls.clone();
        let mut p = policy();
        let mut backend: &'static str = "cpu";

        let out = encode_frame_with_gpu(&mut p, &mut backend, None, || {
            calls2.set(calls2.get() + 1);
            Ok(Some(vec![3u8]))
        })
        .expect("ok");

        assert_eq!(out, Some(vec![3u8]));
        assert_eq!(calls.get(), 1);
        assert_eq!(backend, "cpu");
    }

    /// 降采样必须真的变小、通道顺序不能被换掉（BGRA 进 → BGRA 出）
    #[test]
    fn downscale_shrinks_and_keeps_channels() {
        // 4x4 纯色 BGRA → 2x2，颜色必须还是同一个纯色
        let src: Vec<u8> = std::iter::repeat([10u8, 20, 30, 255])
            .take(16)
            .flatten()
            .collect();
        let dst = downscale_bgra(&src, 4, 4, 2, 2).expect("downscale");
        assert_eq!(dst.len(), 2 * 2 * 4);
        assert_eq!(&dst[0..4], &[10, 20, 30, 255]);
        assert_eq!(&dst[12..16], &[10, 20, 30, 255]);
    }

    #[test]
    fn downscale_rejects_short_input() {
        assert!(downscale_bgra(&[0u8; 8], 4, 4, 2, 2).is_err());
    }

    /// ABR 改档：reconfigure 之后 config() 必须反映新参数（码率/分辨率/帧率）
    #[test]
    fn reconfigure_applies_new_config() {
        let src = EncodeConfig {
            width: 64,
            height: 48,
            fps: 60,
            bitrate_kbps: 8000,
            gop: 60,
            bframes: 0,
        };
        let mut enc = HybridEncoder::new(src).expect("encoder");
        assert_eq!(enc.config().bitrate_kbps, 8000);
        let next = EncodeConfig {
            width: 32,
            height: 24,
            fps: 30,
            bitrate_kbps: 1500,
            gop: 30,
            bframes: 0,
        };
        enc.reconfigure(next).expect("reconfigure");
        assert_eq!(enc.config(), next);
        assert!(enc.is_downscaled(64, 48));
        let pkt = enc
            .encode_bgra(0, &vec![0u8; 32 * 24 * 4])
            .expect("encode at new size");
        assert!(!pkt.is_empty());
    }

    /// 序列：NoFrame → NoFrame → Error → 停用；backend 只在真正失败时翻 cpu
    #[test]
    fn no_frame_then_error_transition() {
        let calls = Rc::new(Cell::new(0));
        let mut p = policy();
        p.mark_registered();
        let mut backend: &'static str = "gpu-zero-copy";

        for _ in 0..2 {
            let c = calls.clone();
            let _ = encode_frame_with_gpu(&mut p, &mut backend, Some(Ok(None)), || {
                c.set(c.get() + 1);
                Ok(None)
            });
            assert_eq!(backend, "gpu-zero-copy");
        }
        let c = calls.clone();
        let _ = encode_frame_with_gpu(
            &mut p,
            &mut backend,
            Some(Err(GpuFallback::NoHardwareEncoder)),
            || {
                c.set(c.get() + 1);
                Ok(Some(vec![1u8]))
            },
        );
        assert_eq!(backend, "cpu");
        assert!(!p.should_try());
        assert_eq!(calls.get(), 1, "只有真正回退那一帧走 CPU");
    }
}
