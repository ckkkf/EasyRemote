// Web 端实现：用浏览器 <input type="file"> 真正选择本机文件。
//
// 只被 pages/files_page.dart 以条件导入的方式引入（dart.library.js_interop 为真时）。
// 这里刻意不引第三方包：Flutter 自带的 dart:html 就够用，且 web 产物只在浏览器里跑。
// ignore_for_file: avoid_web_libraries_in_flutter, deprecated_member_use
import 'dart:async';
import 'dart:html' as html;
import 'dart:typed_data';

import '../models.dart';

/// 浏览器可以弹系统文件选择器。
const bool localFilePickerSupported = true;

/// 弹出选择器并读取所选文件；用户取消时返回 null。
///
/// 浏览器不允许页面枚举本机目录，所以「本机栏」只列出用户主动选中的文件，
/// 不伪造目录树（原型里的桌面目录树只有桌面端能做到）。
Future<PickedLocalFile?> pickLocalFile() async {
  final input = html.FileUploadInputElement();
  input.click();
  final selected = input.onChange.first;
  // 用户取消不会触发 change；用 60 秒上限避免挂住整页。
  final event = await selected.timeout(
    const Duration(seconds: 60),
    onTimeout: () => html.Event('change'),
  );
  final files = input.files;
  if (event.type != 'change' || files == null || files.isEmpty) return null;
  final file = files.first;
  final reader = html.FileReader();
  final loaded = reader.onLoadEnd.first;
  reader.readAsArrayBuffer(file);
  await loaded;
  final buffer = reader.result;
  if (buffer is! ByteBuffer) return null;
  return PickedLocalFile(file.name, Uint8List.view(buffer));
}
