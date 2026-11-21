// EasyRemote 被控端状态面板（macOS 原生，零依赖）
//
// 为什么要有它：验证码只存在于被控端（服务端只存哈希），而被控端逻辑跑在 CLI 里，
// 普通人根本看不到终端。这里读 mac_host 写的 /tmp/er_captcha.txt，
// 把「设备码 + 当前验证码 + 剩余有效秒数」放大显示在桌面上。
import Cocoa

let panelFile = "/tmp/er_captcha.txt"
let ttlSeconds = 600.0

final class Panel: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    let codeLabel = NSTextField(labelWithString: "—")
    let captchaLabel = NSTextField(labelWithString: "—")
    let statusLabel = NSTextField(labelWithString: "等待被控端启动…")
    let hintLabel = NSTextField(labelWithString: "让对方在 EasyRemote 里输入上面的设备码和验证码")

    func applicationDidFinishLaunching(_ n: Notification) {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 460, height: 280),
                          styleMask: [.titled, .closable, .miniaturizable],
                          backing: .buffered, defer: false)
        window.title = "EasyRemote · 被控端"
        window.center()
        window.level = .floating
        window.isReleasedWhenClosed = false

        let bg = NSView(frame: window.contentView!.bounds)
        bg.wantsLayer = true
        bg.layer?.backgroundColor = NSColor(calibratedWhite: 0.09, alpha: 1).cgColor
        window.contentView = bg

        func add(_ v: NSView, _ y: CGFloat, _ h: CGFloat, _ x: CGFloat = 28, _ w: CGFloat = 404) {
            v.frame = NSRect(x: x, y: y, width: w, height: h)
            bg.addSubview(v)
        }

        let title = NSTextField(labelWithString: "被控端已就绪")
        title.font = .systemFont(ofSize: 13, weight: .semibold)
        title.textColor = .white
        add(title, 236, 18)

        let codeCaption = NSTextField(labelWithString: "设备码")
        codeCaption.font = .systemFont(ofSize: 11)
        codeCaption.textColor = NSColor(calibratedWhite: 0.65, alpha: 1)
        add(codeCaption, 208, 14)

        codeLabel.font = .monospacedDigitSystemFont(ofSize: 34, weight: .semibold)
        codeLabel.textColor = .white
        add(codeLabel, 166, 40)

        let captchaCaption = NSTextField(labelWithString: "验证码（让对方输入）")
        captchaCaption.font = .systemFont(ofSize: 11)
        captchaCaption.textColor = NSColor(calibratedWhite: 0.65, alpha: 1)
        add(captchaCaption, 140, 14)

        captchaLabel.font = .monospacedDigitSystemFont(ofSize: 44, weight: .bold)
        captchaLabel.textColor = NSColor(calibratedRed: 0.35, green: 0.85, blue: 0.55, alpha: 1)
        add(captchaLabel, 92, 46)

        statusLabel.font = .monospacedDigitSystemFont(ofSize: 12, weight: .regular)
        statusLabel.textColor = NSColor(calibratedWhite: 0.75, alpha: 1)
        add(statusLabel, 66, 16)

        hintLabel.font = .systemFont(ofSize: 11)
        hintLabel.textColor = NSColor(calibratedWhite: 0.55, alpha: 1)
        hintLabel.lineBreakMode = .byWordWrapping
        hintLabel.maximumNumberOfLines = 2
        add(hintLabel, 22, 34)

        refresh()
        Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { _ in self.refresh() }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func refresh() {
        guard let text = try? String(contentsOfFile: panelFile, encoding: .utf8) else {
            statusLabel.stringValue = "等待被控端启动…（/tmp/er_captcha.txt 不存在）"
            return
        }
        let lines = text.split(separator: "\n").map(String.init)
        guard lines.count >= 3, let ts = Double(lines[2]) else { return }
        codeLabel.stringValue = Self.pretty(lines[0])
        captchaLabel.stringValue = lines[1]
        let elapsed = Date().timeIntervalSince1970 - ts
        let left = ttlSeconds - elapsed
        if left <= 0 {
            statusLabel.stringValue = "验证码已过期，等待下一次自动轮换（每 5 分钟）"
            captchaLabel.textColor = NSColor(calibratedWhite: 0.45, alpha: 1)
        } else {
            statusLabel.stringValue = String(format: "剩余有效 %.0f 秒 · 每 5 分钟自动轮换", left)
            captchaLabel.textColor = NSColor(calibratedRed: 0.35, green: 0.85, blue: 0.55, alpha: 1)
        }
    }

    static func pretty(_ raw: String) -> String {
        let d = raw.filter { $0.isNumber }
        guard d.count == 9 else { return raw }
        let a = d.prefix(3)
        let b = d.dropFirst(3).prefix(3)
        let c = d.suffix(3)
        return "\(a) \(b) \(c)"
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let panel = Panel()
app.delegate = panel
app.run()
