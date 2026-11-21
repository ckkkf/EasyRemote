//! DXGI Desktop Duplication 取帧诊断（Windows 交互会话）。
//!
//! 回答两个问题：
//!   1) AcquireNextFrame 拿不到帧时，到底是「锁屏」还是「显示器休眠 / DWM 不出帧」？
//!   2) 产品路径（只 wake_display、不 SendInput 扰动光标）能不能自己恢复？
//!
//! 用法：dxgi_probe.exe [nonudge|nudge]
//!   nonudge（默认）：只 wake_display，不制造输入 —— 模拟 er-client 的产品路径
//!   nudge          ：每轮补一次 SendInput 鼠标位移 —— 基准脚本用的强制唤醒

fn main() {
    #[cfg(target_os = "windows")]
    {
        use er_capture::dxgi::DxgiCapture;
        let mode = std::env::args()
            .nth(1)
            .unwrap_or_else(|| "nonudge".to_string());
        println!("=== DXGI 拓扑 ===");
        for l in er_capture::dxgi::topology() {
            println!("{l}");
        }
        let mut cap = match DxgiCapture::open_primary_detailed() {
            Ok(c) => c,
            Err(e) => {
                println!("OPEN_FAIL: {e}");
                std::process::exit(1);
            }
        };
        println!(
            "adapter={:?} size={}x{} format={}",
            cap.adapter_name(),
            cap.info().width,
            cap.info().height,
            cap.format()
        );
        println!("mode = {mode}");
        println!("is_locked = {}", cap.is_locked());
        println!("wake_display = {}", DxgiCapture::wake_display());
        for i in 0..6 {
            let line = cap.acquire_debug(1000);
            println!("acquire #{i}: {line}");
            cap.release();
            if mode == "nudge" {
                println!("nudge(sendinput) #{i}: ok={}", cap.nudge_desktop());
            } else if mode == "zeromove" {
                // 关键实验：dx=dy=0 的 MOUSEEVENTF_MOVE 算不算「输入事件」？
                // 算的话就能唤醒显示器/DWM 而**不移动光标**，产品路径可以放心用。
                println!(
                    "zeromove(sendinput, dx=0) #{i}: ok={}",
                    cap.nudge_desktop_zero()
                );
            }
            std::thread::sleep(std::time::Duration::from_millis(150));
        }
        // 再用 grab_gpu（带锁屏门控 + 自愈）试一次，看产品路径能不能拿到帧
        let gated = match cap.grab_gpu(1000) {
            Ok(Some(f)) => format!("Some({}x{})", f.width, f.height),
            Ok(None) => "None".to_string(),
            Err(e) => format!("Err({e})"),
        };
        println!("grab_gpu_after = {gated}");
        cap.release();
        println!("is_locked_after = {}", cap.is_locked());
        println!("PROBE_DONE");
    }
    #[cfg(not(target_os = "windows"))]
    {
        println!("SKIP: windows only");
    }
}
