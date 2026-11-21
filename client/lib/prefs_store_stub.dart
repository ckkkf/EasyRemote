// 兜底实现：没有持久化能力的平台（测试 / 未知宿主）只放进程内存。
// 刷新即丢，行为等同「没改过」，不会假装持久化。
import 'prefs_store_types.dart';

Prefs? _memory;

Future<Prefs> loadPrefs() async => _memory ?? const Prefs();

Future<void> savePrefs(Prefs prefs) async => _memory = prefs;
