import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:ffi/ffi.dart';

import 'package:easyremote_client/native.dart';

/// 真实 FFI 端到端：Dart → C ABI → Rust 核心（采集/编码/隐私屏/ABR）。
/// 动态库不存在时跳过，不算失败。
void main() {
  late ErNative native;
  var available = true;

  setUpAll(() {
    try {
      native = ErNative.load();
    } catch (e) {
      available = false;
      // ignore: avoid_print
      print('skip native tests: ' + e.toString());
    }
  });

  test('版本号可读', () {
    if (!available) return;
    expect(native.version(), isNotEmpty);
  });

  test('创建会话并读取屏幕尺寸', () {
    if (!available) return;
    final s = native.open();
    final size = s.screenSize();
    if (size == null) {
      // ignore: avoid_print
      print('skip: no display');
      s.close();
      return;
    }
    expect(size.width, greaterThan(0));
    expect(size.height, greaterThan(0));
    s.close();
  });

  test('抓帧字节数与尺寸一致', () {
    if (!available) return;
    final s = native.open();
    final size = s.screenSize();
    if (size == null) { s.close(); return; }
    final frame = s.grabFrame();
    expect(frame.length, size.width * size.height * 4);
    expect(frame.any((b) => b != 0), isTrue, reason: 'captured frame is all zero');
    s.close();
  });

  test('隐私屏开启后整帧被抹平', () {
    if (!available) return;
    final s = native.open();
    final size = s.screenSize();
    if (size == null) { s.close(); return; }
    s.setPrivacy(true);
    s.grabFrame();
    expect(s.lastFrameUniform(), isTrue);
    s.setPrivacy(false);
    s.grabFrame();
    expect(s.lastFrameUniform(), isFalse);
    s.close();
  });

  test('编码产出的 H.264 以 Annex-B 起始码开头', () {
    if (!available) return;
    final s = native.open();
    final size = s.screenSize();
    if (size == null) { s.close(); return; }
    s.grabFrame();
    final encoded = s.encodeLastFrame();
    expect(encoded.length, greaterThan(0));
    final isAnnexB = encoded.length >= 4 &&
        encoded[0] == 0 && encoded[1] == 0 &&
        (encoded[2] == 1 || (encoded[2] == 0 && encoded[3] == 1));
    expect(isAnnexB, isTrue, reason: 'not Annex-B: ' + encoded.take(8).toList().toString());
    s.close();
  });

  test('ABR 决策可跨边界返回', () {
    if (!available) return;
    final s = native.open();
    final good = s.updateAbr(lossPct: 0.2, rttMs: 50, queueMs: 10);
    expect(good['name'], isNotEmpty);
    final bad = s.updateAbr(lossPct: 20.0, rttMs: 60, queueMs: 300);
    expect(bad['changed'], isTrue);
    expect(bad['bitrate_kbps'], lessThan(good['bitrate_kbps'] as int));
    s.close();
  });

  test('重复 close 安全', () {
    if (!available) return;
    final s = native.open();
    s.close();
    s.close();
  });
}
