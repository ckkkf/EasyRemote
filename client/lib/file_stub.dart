// Web 替身：浏览器沙箱拿不到本机文件路径，也不允许写盘。
// 页面据 [localFileAccessSupported] 切换为「文本内容上传 + 票据链接下载」。
import 'dart:typed_data';

const bool localFileAccessSupported = false;

Future<Uint8List> readLocalFile(String path) {
  return Future<Uint8List>.error(StateError('网页预览不支持读取本机文件，请使用文本内容上传'));
}

/// Web 下不落盘，返回空串；调用方改为展示一次性下载票据链接。
Future<String> saveDownload(Uint8List bytes, String name) async => '';
