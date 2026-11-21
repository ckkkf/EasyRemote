//! 隐私屏端到端：真实采集 → 打隐私屏 → 校验像素全平。
fn main() {
    use er_capture::privacy::{apply_privacy, is_uniform, PrivacyColor};
    use er_capture::{Capture, ScreenCapture};

    let mut cap = match ScreenCapture::open_primary() {
        Ok(c) => c,
        Err(e) => {
            println!("NO_DISPLAY: {e}");
            return;
        }
    };
    let mut frame = match cap.grab() {
        Ok(f) => f,
        Err(e) => {
            println!("CAPTURE_FAIL: {e}");
            return;
        }
    };
    if is_uniform(&frame) {
        println!("SKIP: desktop is already uniform");
        return;
    }
    let before = frame.data.iter().filter(|b| **b != 0).count();
    apply_privacy(&mut frame, PrivacyColor::default());
    let after = frame.data.iter().filter(|b| **b != 0).count();

    println!("non-zero bytes before={before} after={after}");
    if !is_uniform(&frame) {
        println!("FAIL: frame not uniform after privacy");
        std::process::exit(1);
    }
    if after != (frame.width * frame.height) as usize {
        // 只有 alpha=255 是非零，数量应等于像素数
        println!("FAIL: unexpected residual bytes {after}");
        std::process::exit(1);
    }
    println!("PRIVACY SCREEN PASSED");
}
