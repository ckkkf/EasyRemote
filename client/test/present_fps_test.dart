import 'package:easyremote_client/present_fps.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('TestPresentFpsCountsWindow', () {
    var now = DateTime(2026);
    final p = PresentFps(clock: () => now);
    for (var i = 0; i < 32; i++) {
      now = now.add(const Duration(milliseconds: 33));
      p.tick();
    }
    expect(p.fps, inInclusiveRange(27, 33));
  });
}
