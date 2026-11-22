import Cocoa
import FlutterMacOS

/// ER-601b：把 native 解码帧直接交给 Flutter 外部纹理。
///
/// 仍有一次 Rust→CVPixelBuffer 拷贝（BGRA），但不再经过 Dart
/// decodeImageFromPixels / 每帧 ui.Image。
final class VideoTexture: NSObject, FlutterTexture {
  var pixelBuffer: CVPixelBuffer?

  func copyPixelBuffer() -> Unmanaged<CVPixelBuffer>? {
    guard let buffer = pixelBuffer else { return nil }
    return Unmanaged.passRetained(buffer)
  }
}

final class VideoTexturePlugin: NSObject, FlutterPlugin {
  static let channelName = "easyremote/video_texture"

  private let registrar: FlutterPluginRegistrar
  private var textures: [Int64: VideoTexture] = [:]
  private var timers: [Int64: Timer] = [:]
  private var sessions: [Int64: String] = [:]
  private var frameFn: (@convention(c) (UnsafePointer<CChar>?, UnsafeMutablePointer<UInt8>?, Int) -> Int32)?
  private var ffiHandle: UnsafeMutableRawPointer?

  init(registrar: FlutterPluginRegistrar) {
    self.registrar = registrar
    super.init()
    loadFFI()
  }

  static func register(with registrar: FlutterPluginRegistrar) {
    let channel = FlutterMethodChannel(
      name: channelName, binaryMessenger: registrar.messenger)
    let instance = VideoTexturePlugin(registrar: registrar)
    registrar.addMethodCallDelegate(instance, channel: channel)
  }

  private func loadFFI() {
    let candidates: [String] = [
      Bundle.main.privateFrameworksPath.map { $0 + "/liber_ffi.dylib" } ?? "",
      Bundle.main.bundlePath + "/Contents/Frameworks/liber_ffi.dylib",
      Bundle.main.bundlePath + "/Contents/MacOS/liber_ffi.dylib",
    ].filter { !$0.isEmpty }
    for path in candidates {
      guard FileManager.default.fileExists(atPath: path) else { continue }
      guard let handle = dlopen(path, RTLD_NOW) else { continue }
      guard let sym = dlsym(handle, "er_ctrl_frame_bgra") else { continue }
      ffiHandle = handle
      frameFn = unsafeBitCast(
        sym,
        to: (@convention(c) (UnsafePointer<CChar>?, UnsafeMutablePointer<UInt8>?, Int) -> Int32).self)
      return
    }
  }

  func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
    switch call.method {
    case "create":
      guard let args = call.arguments as? [String: Any],
            let session = args["session_id"] as? String,
            let width = args["width"] as? Int,
            let height = args["height"] as? Int,
            frameFn != nil else {
        result(nil)
        return
      }
      var buffer: CVPixelBuffer?
      let attrs: [CFString: Any] = [
        kCVPixelBufferIOSurfacePropertiesKey: [:] as CFDictionary
      ]
      let status = CVPixelBufferCreate(
        kCFAllocatorDefault, width, height, kCVPixelFormatType_32BGRA,
        attrs as CFDictionary, &buffer)
      guard status == kCVReturnSuccess, let pixelBuffer = buffer else {
        result(nil)
        return
      }
      let texture = VideoTexture()
      texture.pixelBuffer = pixelBuffer
      let textureId = registrar.textures.register(texture)
      textures[textureId] = texture
      sessions[textureId] = session
      startTimer(textureId: textureId, texture: texture)
      result(textureId)
    case "dispose":
      if let args = call.arguments as? [String: Any],
         let id = args["texture_id"] as? Int64 {
        timers[id]?.invalidate()
        timers[id] = nil
        textures[id] = nil
        sessions[id] = nil
        registrar.textures.unregisterTexture(id)
      }
      result(nil)
    default:
      result(FlutterMethodNotImplemented)
    }
  }

  private func startTimer(textureId: Int64, texture: VideoTexture) {
    let timer = Timer(timeInterval: 1.0 / 60.0, repeats: true) { [weak self, weak texture] _ in
      guard let self, let texture, let buffer = texture.pixelBuffer else { return }
      guard let session = self.sessions[textureId], let fn = self.frameFn else { return }
      CVPixelBufferLockBaseAddress(buffer, [])
      defer { CVPixelBufferUnlockBaseAddress(buffer, []) }
      guard let base = CVPixelBufferGetBaseAddress(buffer)?
        .assumingMemoryBound(to: UInt8.self) else { return }
      let cap = CVPixelBufferGetDataSize(buffer)
      let n = session.withCString { fn($0, base, cap) }
      if n > 0 {
        self.registrar.textures.textureFrameAvailable(textureId)
      }
    }
    RunLoop.main.add(timer, forMode: .common)
    timers[textureId] = timer
  }
}
