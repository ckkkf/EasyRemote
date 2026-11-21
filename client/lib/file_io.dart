// 桌面端实现：dart:io 可直接读写本机文件系统。
// 页面通过条件导入选择本文件或 file_stub.dart（Web），不要直接 import dart:io。
import 'dart:io';
import 'dart:typed_data';

/// 当前平台是否支持「按路径读取本机文件」与「把下载字节写盘」。
const bool localFileAccessSupported = true;

/// 读取本机文件字节；路径非法或文件不存在时抛 [StateError]（由页面转成错误文案）。
Future<Uint8List> readLocalFile(String path) async {
  final trimmed = path.trim();
  if (trimmed.isEmpty) throw StateError('请填写本机文件路径');
  final file = File(trimmed);
  if (!await file.exists()) throw StateError('文件不存在：${file.path}');
  return file.readAsBytes();
}

/// 把下载字节写到 ~/Downloads（不存在则落临时目录），返回落盘路径。
/// 同名文件不覆盖：追加序号，避免把用户已有文件冲掉。
Future<String> saveDownload(Uint8List bytes, String name) async {
  final home = Platform.environment['HOME'] ?? Platform.environment['USERPROFILE'];
  final downloads = home == null ? null : Directory('$home/Downloads');
  final dir = (downloads != null && downloads.existsSync())
      ? downloads
      : Directory.systemTemp;
  var target = File('${dir.path}/${_safeName(name)}');
  var i = 1;
  while (await target.exists()) {
    target = File('${dir.path}/${i}_${_safeName(name)}');
    i++;
  }
  await target.writeAsBytes(bytes, flush: true);
  return target.path;
}

/// 去掉路径分隔符与控制字符，防止写到目标目录之外。
String _safeName(String name) {
  final base = name.replaceAll('\\', '/').split('/').last;
  final cleaned = base.replaceAll(RegExp(r'[\x00-\x1f\x7f]'), '').trim();
  return cleaned.isEmpty ? 'download.bin' : cleaned;
}
