/// 呈现侧 FPS 窗口计数（ER-601）：只统计真正替换到 UI 的帧。
class PresentFps {
  PresentFps({DateTime Function()? clock}) : _clock = clock ?? DateTime.now;

  final DateTime Function() _clock;
  DateTime? _start;
  int _frames = 0;
  int _last = 0;

  int get fps => _last;

  /// 每呈现一帧调用；窗口满 1 秒更新一次。
  void tick() {
    final now = _clock();
    _start ??= now;
    _frames++;
    final elapsed = now.difference(_start!).inMilliseconds;
    if (elapsed >= 1000) {
      _last = (_frames * 1000 / elapsed).round();
      _frames = 0;
      _start = now;
    }
  }
}
