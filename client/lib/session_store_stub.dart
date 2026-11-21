// 兜底实现：没有持久化能力的平台（测试 / 未知宿主）只放进程内存。
// 刷新即丢，行为等同「没登录过」，不会假装持久化。
import 'session_store_types.dart';

StoredSession? _memory;

Future<StoredSession?> loadSession() async => _memory;

Future<void> saveSession(StoredSession session) async => _memory = session;

Future<void> clearSession() async => _memory = null;
