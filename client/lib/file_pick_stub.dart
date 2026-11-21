// 非 Web 端的替身实现：桌面端没有浏览器文件选择器。
//
// 存在意义是让 pages/files_page.dart 用同一份代码编到 Web / 桌面两端，
// 由条件导入决定是否把真正的 <input type="file"> 换进来。
import '../models.dart';

const bool localFilePickerSupported = false;

Future<PickedLocalFile?> pickLocalFile() async => null;
