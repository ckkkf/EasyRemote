// EasyRemote 被控端（macOS 应用）
//
// 产品形态：Mac 用户双击打开 → 自动拉起服务/隧道/被控逻辑 → 窗口显示
// 「设备码 + 临时密码」，密码默认遮成圆点，可一键查看明文、一键复制。
// 主控端（Windows）输入这两个就能连，不需要每十分钟找新验证码。
import Cocoa

let repo = "/Users/ckkk/dev/code/Go/EasyRemote"
let binDir = "/tmp/erbin"
let panelFile = "/tmp/er_captcha.txt"
let helloFile = "/tmp/er_captcha_hello.txt"

final class HostApp: NSObject, NSApplicationDelegate {
    var procs: [Process] = []
    var window: NSWindow!

    let dot = NSTextField(labelWithString: "●")
    let stateLabel = NSTextField(labelWithString: "正在启动…")
    let codeLabel = NSTextField(labelWithString: "—")
    let passwordLabel = NSTextField(labelWithString: "……")
    let hintLabel = NSTextField(labelWithString: "让对方在 Windows 的 EasyRemote 里输入上面的设备码和临时密码")
    let eyeButton = NSButton(title: "显示", target: nil, action: nil)
    let copyButton = NSButton(title: "复制", target: nil, action: nil)

    var passwordValue = ""
    var showPassword = false

    func applicationDidFinishLaunching(_ n: Notification) {
        buildWindow()
        startAll()
        Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { _ in self.refresh() }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func buildWindow() {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 480, height: 330),
                          styleMask: [.titled, .closable, .miniaturizable],
                          backing: .buffered, defer: false)
        window.title = "EasyRemote · 被控端"
        window.center()
        window.level = .floating
        window.isReleasedWhenClosed = false

        let bg = NSView(frame: window.contentView!.bounds)
        bg.wantsLayer = true
        bg.layer?.backgroundColor = NSColor(calibratedWhite: 0.08, alpha: 1).cgColor
        window.contentView = bg

        func add(_ v: NSView, _ y: CGFloat, _ h: CGFloat, _ x: CGFloat = 28, _ w: CGFloat = 424) {
            v.frame = NSRect(x: x, y: y, width: w, height: h)
            bg.addSubview(v)
        }

        dot.font = .systemFont(ofSize: 14)
        dot.textColor = .systemOrange
        dot.frame = NSRect(x: 28, y: 286, width: 16, height: 18)
        bg.addSubview(dot)

        stateLabel.font = .systemFont(ofSize: 13, weight: .semibold)
        stateLabel.textColor = .white
        add(stateLabel, 286, 18, 48, 380)

        let c1 = NSTextField(labelWithString: "设备码")
        c1.font = .systemFont(ofSize: 11)
        c1.textColor = NSColor(calibratedWhite: 0.62, alpha: 1)
        add(c1, 254, 14)

        codeLabel.font = .monospacedDigitSystemFont(ofSize: 34, weight: .semibold)
        codeLabel.textColor = .white
        add(codeLabel, 206, 42)

        let c2 = NSTextField(labelWithString: "临时密码（可查看明文 / 复制）")
        c2.font = .systemFont(ofSize: 11)
        c2.textColor = NSColor(calibratedWhite: 0.62, alpha: 1)
        add(c2, 180, 14)

        passwordLabel.font = .monospacedDigitSystemFont(ofSize: 40, weight: .bold)
        passwordLabel.textColor = .systemGreen
        add(passwordLabel, 126, 46, 28, 300)

        eyeButton.bezelStyle = .rounded
        eyeButton.target = self
        eyeButton.action = #selector(toggleShow)
        add(eyeButton, 132, 30, 334, 50)

        copyButton.bezelStyle = .rounded
        copyButton.target = self
        copyButton.action = #selector(copyPassword)
        add(copyButton, 132, 30, 388, 50)

        hintLabel.font = .systemFont(ofSize: 11)
        hintLabel.textColor = NSColor(calibratedWhite: 0.55, alpha: 1)
        hintLabel.lineBreakMode = .byWordWrapping
        hintLabel.maximumNumberOfLines = 2
        add(hintLabel, 62, 44)
    }

    @objc func toggleShow() {
        showPassword.toggle()
        eyeButton.title = showPassword ? "隐藏" : "显示"
        renderPassword()
    }

    @objc func copyPassword() {
        guard !passwordValue.isEmpty else { return }
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(passwordValue, forType: .string)
    }

    func renderPassword() {
        passwordLabel.stringValue = showPassword
            ? passwordValue
            : String(repeating: "●", count: max(passwordValue.count, 8))
        eyeButton.isEnabled = !passwordValue.isEmpty
        copyButton.isEnabled = !passwordValue.isEmpty
    }

    func spawn(_ path: String, _ args: [String]) -> Process? {
        guard FileManager.default.isExecutableFile(atPath: path) else { return nil }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: path)
        p.arguments = args
        p.standardOutput = FileHandle.nullDevice
        p.standardError = FileHandle.nullDevice
        do {
            try p.run()
            procs.append(p)
            return p
        } catch {
            return nil
        }
    }

    func startAll() {
        let fm = FileManager.default
        for name in ["stack", "relay", "udptun", "mac_host"] {
            let p = Process()
            p.executableURL = URL(fileURLWithPath: "/usr/bin/pkill")
            p.arguments = ["-f", name == "mac_host" ? "examples/mac_host" : "/tmp/erbin/\(name)"]
            try? p.run()
            p.waitUntilExit()
        }
        try? fm.removeItem(atPath: panelFile)
        try? fm.removeItem(atPath: helloFile)

        _ = spawn("\(binDir)/stack", ["-addr", "127.0.0.1:18090"])
        _ = spawn("\(binDir)/relay", ["-addr", "127.0.0.1:19000"])
        _ = spawn("\(binDir)/udptun", ["-mode", "server", "-listen", "127.0.0.1:19001", "-target", "127.0.0.1:19000"])
        _ = spawn("/usr/bin/ssh", ["-N", "-o", "BatchMode=yes", "-o", "ExitOnForwardFailure=yes",
                                   "-o", "ServerAliveInterval=20",
                                   "-R", "18090:127.0.0.1:18090", "-R", "19001:127.0.0.1:19001", "win"])
        Thread.sleep(forTimeInterval: 2.0)
        _ = spawn("\(repo)/core/target/debug/examples/mac_host",
                  ["http://127.0.0.1:18090", "127.0.0.1:19000", "13700009101", "123456",
                   "http://127.0.0.1:18090", helloFile, "30", "0"])
        stateLabel.stringValue = "正在等待设备码…"
    }

    func refresh() {
        if let text = try? String(contentsOfFile: panelFile, encoding: .utf8) {
            let lines = text.split(separator: "\n").map(String.init)
            if lines.count >= 2 {
                let raw = lines[0].filter { $0.isNumber }
                codeLabel.stringValue = raw.count == 9
                    ? "\(raw.prefix(3)) \(raw.dropFirst(3).prefix(3)) \(raw.suffix(3))"
                    : lines[0]
                if passwordValue != lines[1] {
                    passwordValue = lines[1]
                    renderPassword()
                }
            }
        }
        let alive = procs.contains { $0.isRunning }
        if alive && !codeLabel.stringValue.isEmpty {
            stateLabel.stringValue = "被控端已就绪 · 正在共享这台 Mac"
            dot.textColor = .systemGreen
        } else if alive {
            stateLabel.stringValue = "正在启动被控端…"
            dot.textColor = .systemOrange
        } else {
            stateLabel.stringValue = "被控端已停止"
            dot.textColor = .systemRed
        }
    }

    func applicationWillTerminate(_ n: Notification) {
        for p in procs where p.isRunning { p.terminate() }
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ s: NSApplication) -> Bool { true }
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let hostApp = HostApp()
app.delegate = hostApp
app.run()
