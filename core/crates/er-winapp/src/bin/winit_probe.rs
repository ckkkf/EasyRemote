//! 窗口环境自检：只开一个 800x600 的空窗口，用来隔离「窗口系统问题」和「egui/GL 问题」。
use winit::application::ApplicationHandler;
use winit::event::WindowEvent;
use winit::event_loop::{ActiveEventLoop, EventLoop};
use winit::window::{Window, WindowId};

struct Probe {
    window: Option<Window>,
}

impl ApplicationHandler for Probe {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        match el.create_window(
            Window::default_attributes()
                .with_title("ER probe")
                .with_inner_size(winit::dpi::LogicalSize::new(800.0, 600.0)),
        ) {
            Ok(w) => {
                println!(
                    "PROBE_WINDOW_OK size={:?} scale={:?}",
                    w.inner_size(),
                    w.scale_factor()
                );
                self.window = Some(w);
            }
            Err(e) => {
                println!("PROBE_WINDOW_ERR {e}");
                el.exit();
            }
        }
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _id: WindowId, event: WindowEvent) {
        if matches!(event, WindowEvent::CloseRequested) {
            el.exit();
        }
    }
}

fn main() {
    let el = EventLoop::new().unwrap();
    let mut app = Probe { window: None };
    println!("PROBE_START");
    let _ = el.run_app(&mut app);
    println!("PROBE_END");
}
