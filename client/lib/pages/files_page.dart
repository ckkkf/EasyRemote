import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../file_io.dart' if (dart.library.js_interop) '../file_stub.dart';
import '../file_pick_stub.dart' if (dart.library.js_interop) '../file_pick_web.dart';
import '../models.dart';
import '../shell.dart';
import '../theme.dart';

/// 文件传输（原型 s07 复刻）。
///
/// 版式：顶栏（队列数 / 暂停全部）→ 传输队列（真实上传任务）→ 本机 / 远端双栏。
/// 数据全部来自真实接口，平台能力如实标注：
/// - 队列：`POST /v1/files`（multipart，按字节流回传进度，完成后固定在 100%），
///   「暂停」是真停：请求体是可停顿流，暂停后不再向连接写字节，恢复后从断点续写；
/// - 远端栏：`GET /v1/files`；下载走 `POST /v1/files/{id}/ticket` 一次性票据；
///   删除走 `DELETE /v1/files/{id}`；
/// - 本机栏：Web 只有「选择文件」（浏览器不允许枚举目录，所以不画目录树），
///   文本内容入口是同一栏的降级路径；桌面端用本机路径读取。
class FilesPage extends StatefulWidget {
  const FilesPage({super.key, required this.client, this.config});

  final GatewayClient client;

  /// 运行配置（`file.max_bytes` 用于上传前置校验）。
  final AppConfig? config;

  @override
  State<FilesPage> createState() => _FilesPageState();
}

/// 队列里一条真实上传任务的状态。
enum _TaskState { queued, uploading, paused, done, failed }

class _UploadTask {
  _UploadTask({required this.name, required this.bytes, required this.size});

  final String name;
  final int size;
  final Uint8List bytes;

  _TaskState state = _TaskState.queued;
  int sent = 0;
  int total = 0;
  DateTime? startedAt;
  Duration? elapsed;
  String? error;
  String fileId = '';
  UploadControl control = UploadControl();

  double get progress {
    if (total <= 0) return 0;
    return (sent / total).clamp(0.0, 1.0);
  }

  int get percent => (progress * 100).round();

  /// 平均速度（字节/秒）：已发送字节 / 已用时，暂停时间也计入，展示用途足够。
  double get bytesPerSecond {
    final done = elapsed;
    if (done != null) {
      return done.inMilliseconds <= 0 ? 0 : size / (done.inMilliseconds / 1000);
    }
    final start = startedAt;
    if (start == null) return 0;
    final ms = DateTime.now().difference(start).inMilliseconds;
    if (ms <= 0) return 0;
    return sent / (ms / 1000);
  }
}

class _FilesPageState extends State<FilesPage> {
  List<FileInfo>? _files;
  bool _loading = false;
  String? _error;
  String? _notice;

  /// 本机栏：用户主动选中的文件（Web 无法枚举目录，所以只列选择结果）。
  final List<PickedLocalFile> _local = <PickedLocalFile>[];
  final List<_UploadTask> _tasks = <_UploadTask>[];

  bool _pausedAll = false;
  bool _pumping = false;
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  int get _maxBytes => widget.config?.fileMaxBytes ?? 32 * 1024 * 1024;

  int get _doneCount => _tasks.where((t) => t.state == _TaskState.done).length;
  int get _activeCount => _tasks.where((t) => t.state == _TaskState.uploading).length;
  int get _queuedCount => _tasks.where((t) => t.state == _TaskState.queued).length;

  double get _totalRate =>
      _tasks.where((t) => t.state == _TaskState.uploading).fold<double>(0, (sum, t) => sum + t.bytesPerSecond);

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final list = await widget.client.listFiles();
      if (!mounted) return;
      setState(() => _files = list);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _error = e.message);
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  // ---------- 本机栏 ----------

  Future<void> _pick() async {
    if (!localFilePickerSupported) {
      await _openTextEntry();
      return;
    }
    final picked = await pickLocalFile();
    if (picked == null || !mounted) return;
    setState(() {
      _local.insert(0, picked);
      _notice = '已选择 ${picked.name}（${_pretty(picked.size)}），点「上传」进入队列';
      _error = null;
    });
  }

  Future<void> _openTextEntry() async {
    final draft = await showDialog<PickedLocalFile>(
      context: context,
      builder: (_) => const _TextFileDialog(),
    );
    if (draft == null || !mounted) return;
    setState(() {
      _local.insert(0, draft);
      _notice = '已加入本机栏：${draft.name}（${_pretty(draft.size)}）';
      _error = null;
    });
  }

  void _removeLocal(PickedLocalFile f) {
    setState(() => _local.remove(f));
  }

  // ---------- 队列 ----------

  void _enqueue(PickedLocalFile f) {
    if (f.size == 0) {
      setState(() => _error = '文件内容为空，服务端会拒绝空文件');
      return;
    }
    if (f.size > _maxBytes) {
      setState(() => _error = '文件超过服务端上限（${_pretty(_maxBytes)}）');
      return;
    }
    final task = _UploadTask(name: f.name, bytes: f.bytes, size: f.size);
    setState(() {
      _tasks.insert(0, task);
      _error = null;
      _notice = '已加入队列：${f.name}';
    });
    _ensureTicker();
    unawaited(_pump());
  }

  void _ensureTicker() {
    if (_ticker != null) return;
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      if (_tasks.any((t) => t.state == _TaskState.uploading)) {
        setState(() {});
      } else {
        _ticker?.cancel();
        _ticker = null;
      }
    });
  }

  /// 串行发送：一次只跑一个任务，避免并行上传把网关打满（也贴近原型的队列语义）。
  Future<void> _pump() async {
    if (_pumping) return;
    _pumping = true;
    try {
      while (mounted && !_pausedAll) {
        final pending = _tasks.where((t) => t.state == _TaskState.queued).toList();
        if (pending.isEmpty) break;
        await _runTask(pending.last);
      }
    } finally {
      _pumping = false;
    }
  }

  Future<void> _runTask(_UploadTask task) async {
    setState(() {
      task.state = _TaskState.uploading;
      task.startedAt = DateTime.now();
      task.sent = 0;
      task.total = task.bytes.length;
      task.error = null;
    });
    task.control = UploadControl(onProgress: (sent, total) {
      task.sent = sent;
      task.total = total;
      if (mounted) setState(() {});
    });
    if (_pausedAll) task.control.pause();
    try {
      final info = await widget.client.uploadFile(task.name, task.bytes, control: task.control);
      if (!mounted) return;
      setState(() {
        task.state = _TaskState.done;
        task.fileId = info.id;
        task.sent = task.total;
        task.elapsed = DateTime.now().difference(task.startedAt ?? DateTime.now());
        _notice = '已上传 ${info.name}（${info.prettySize}）';
      });
      await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        task.state = _TaskState.failed;
        task.error = e.message;
        _error = '上传失败：${e.message}';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        task.state = _TaskState.failed;
        task.error = e.toString();
        _error = '上传失败：${e.toString()}';
      });
    }
  }

  void _pauseTask(_UploadTask task) {
    task.control.pause();
    setState(() {
      task.state = _TaskState.paused;
      _notice = '已暂停：${task.name}';
    });
  }

  void _resumeTask(_UploadTask task) {
    task.control.resume();
    setState(() {
      task.state = _TaskState.uploading;
      _pausedAll = false;
    });
    _ensureTicker();
  }

  void _togglePauseAll() {
    final pause = !_pausedAll;
    setState(() {
      _pausedAll = pause;
      for (final t in _tasks) {
        if (pause) {
          if (t.state == _TaskState.uploading) {
            t.control.pause();
            t.state = _TaskState.paused;
          }
        } else if (t.state == _TaskState.paused) {
          t.control.resume();
          t.state = _TaskState.uploading;
        }
      }
      _notice = pause ? '已暂停全部传输' : '已恢复传输';
    });
    if (!pause) {
      _ensureTicker();
      unawaited(_pump());
    }
  }

  void _retryTask(_UploadTask task) {
    setState(() {
      task.state = _TaskState.queued;
      task.sent = 0;
      task.error = null;
    });
    _ensureTicker();
    unawaited(_pump());
  }

  void _removeTask(_UploadTask task) {
    if (task.state == _TaskState.uploading) task.control.pause();
    setState(() => _tasks.remove(task));
  }

  void _clearDone() {
    setState(() {
      _tasks.removeWhere((t) => t.state == _TaskState.done);
      _notice = '已清空已完成的任务';
    });
  }

  // ---------- 远端栏 ----------

  Future<void> _download(FileInfo f) async {
    setState(() {
      _error = null;
      _notice = null;
    });
    try {
      if (localFileAccessSupported) {
        final bytes = await widget.client.downloadFile(f.id);
        final path = await saveDownload(bytes, f.name);
        if (!mounted) return;
        setState(() => _notice = path.isEmpty ? '已下载 ${f.name}' : '已保存到 $path');
      } else {
        // 浏览器不能带 Authorization 打开链接，换一张 60 秒单次票据。
        final url = await widget.client.downloadTicket(f.id);
        if (!mounted) return;
        if (url.isEmpty) {
          setState(() => _error = '服务端未返回下载票据');
          return;
        }
        await Clipboard.setData(ClipboardData(text: url));
        if (!mounted) return;
        setState(() => _notice = '下载票据（60 秒内单次有效）已复制，粘贴到新标签即可下载');
      }
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _error = e.message);
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
    }
  }

  Future<void> _delete(FileInfo f) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除文件'),
        content: Text('确定删除「${f.name}」？删除后无法恢复。'),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('取消')),
          FilledButton(onPressed: () => Navigator.of(ctx).pop(true), child: const Text('删除')),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    setState(() {
      _error = null;
      _notice = null;
    });
    try {
      await widget.client.deleteFile(f.id);
      if (!mounted) return;
      setState(() => _notice = '已删除 ${f.name}');
      await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _error = e.message);
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
    }
  }

  // ---------- 视图 ----------

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;

    final header = PageHeader(
      title: '文件传输',
      subtitle: '本机 ⇄ 远端 · 网关中转（P2P 直连见桌面端）',
      actions: [
        _CountChip(text: '队列 ${_tasks.length}'),
        OutlinedButton.icon(
          key: const Key('pause-all'),
          onPressed: _tasks.isEmpty ? null : _togglePauseAll,
          icon: Icon(_pausedAll ? Icons.play_arrow_rounded : Icons.pause_rounded, size: 18),
          label: Text(_pausedAll ? '继续全部' : '暂停全部'),
        ),
      ],
    );

    final queue = _QueuePanel(
      tasks: _tasks,
      activeCount: _activeCount,
      queuedCount: _queuedCount,
      doneCount: _doneCount,
      rate: _totalRate,
      onPause: _pauseTask,
      onResume: _resumeTask,
      onRetry: _retryTask,
      onRemove: _removeTask,
      onClearDone: _clearDone,
    );

    final localPane = _LocalPane(
      files: _local,
      pickerSupported: localFilePickerSupported,
      onPick: _pick,
      onTextEntry: _openTextEntry,
      onUpload: _enqueue,
      onRemove: _removeLocal,
      notice: '单文件上限 ${_pretty(_maxBytes)}',
    );

    final remotePane = _RemotePane(
      files: _files,
      loading: _loading,
      error: _error,
      onRefresh: _load,
      onDownload: _download,
      onDelete: _delete,
      onUploadEntry: _openTextEntry,
    );

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        header,
        Expanded(
          child: Padding(
            padding: EdgeInsets.fromLTRB(wide ? 32 : 16, 0, wide ? 32 : 16, wide ? 24 : 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (_notice != null)
                  _Banner(
                    icon: Icons.check_circle_outline_rounded,
                    color: const Color(0xFF2A6239),
                    background: scheme.surfaceContainerLow,
                    text: _notice!,
                    onClose: () => setState(() => _notice = null),
                  ),
                if (_error != null)
                  _Banner(
                    icon: Icons.error_outline_rounded,
                    color: scheme.error,
                    background: scheme.errorContainer,
                    text: _error!,
                    onClose: () => setState(() => _error = null),
                  ),
                if (_notice != null || _error != null) const SizedBox(height: 12),
                queue,
                const SizedBox(height: 16),
                Expanded(
                  child: wide
                      ? Row(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            Expanded(child: localPane),
                            const SizedBox(width: 12),
                            Expanded(child: remotePane),
                          ],
                        )
                      : ListView(
                          children: [
                            SizedBox(height: 320, child: localPane),
                            const SizedBox(height: 12),
                            SizedBox(height: 360, child: remotePane),
                          ],
                        ),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// 队列区：标题 + 进行中计数 + 总速率 + 清除已完成，下面是真实任务行。
class _QueuePanel extends StatelessWidget {
  const _QueuePanel({
    required this.tasks,
    required this.activeCount,
    required this.queuedCount,
    required this.doneCount,
    required this.rate,
    required this.onPause,
    required this.onResume,
    required this.onRetry,
    required this.onRemove,
    required this.onClearDone,
  });

  final List<_UploadTask> tasks;
  final int activeCount;
  final int queuedCount;
  final int doneCount;
  final double rate;
  final ValueChanged<_UploadTask> onPause;
  final ValueChanged<_UploadTask> onResume;
  final ValueChanged<_UploadTask> onRetry;
  final ValueChanged<_UploadTask> onRemove;
  final VoidCallback onClearDone;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    return Container(
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 12, 12),
            child: Row(
              children: [
                Text('传输队列', style: text.titleSmall),
                const SizedBox(width: 10),
                _CountChip(text: '$activeCount 个进行中'),
                const Spacer(),
                if (rate > 0)
                  Text(
                    '总速率 ${_rate(rate)}',
                    style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant, fontFamily: 'monospace'),
                  ),
                const SizedBox(width: 10),
                TextButton(
                  key: const Key('clear-done'),
                  onPressed: doneCount == 0 ? null : onClearDone,
                  child: const Text('清除已完成'),
                ),
              ],
            ),
          ),
          Divider(height: 1, color: scheme.outlineVariant.withValues(alpha: 0.5)),
          if (tasks.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 22),
              child: Row(
                children: [
                  Icon(Icons.swap_vert_rounded, size: 18, color: scheme.onSurfaceVariant),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      '还没有传输任务：在左侧「本机」栏选择文件后点上传，这里会显示真实进度。',
                      style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                    ),
                  ),
                ],
              ),
            )
          else
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 214),
              child: ListView.separated(
                shrinkWrap: true,
                padding: EdgeInsets.zero,
                itemCount: tasks.length,
                separatorBuilder: (_, _) => Divider(height: 1, color: scheme.outlineVariant.withValues(alpha: 0.4)),
                itemBuilder: (context, i) => _QueueRow(
                  task: tasks[i],
                  onPause: () => onPause(tasks[i]),
                  onResume: () => onResume(tasks[i]),
                  onRetry: () => onRetry(tasks[i]),
                  onRemove: () => onRemove(tasks[i]),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _QueueRow extends StatelessWidget {
  const _QueueRow({
    required this.task,
    required this.onPause,
    required this.onResume,
    required this.onRetry,
    required this.onRemove,
  });

  final _UploadTask task;
  final VoidCallback onPause;
  final VoidCallback onResume;
  final VoidCallback onRetry;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    final (Color barColor, Color stateColor, String stateText) = switch (task.state) {
      _TaskState.done => (const Color(0xFF1E7B45), const Color(0xFF1E7B45), '已完成'),
      _TaskState.failed => (scheme.error, scheme.error, '失败'),
      _TaskState.paused => (const Color(0xFFB4BCC8), scheme.onSurfaceVariant, '已暂停'),
      _TaskState.queued => (scheme.outlineVariant, scheme.onSurfaceVariant, '排队中'),
      _TaskState.uploading => (scheme.primary, scheme.primary, '上传中'),
    };

    final sentText = task.state == _TaskState.done
        ? '${_pretty(task.size)} · 平均 ${_rate(task.bytesPerSecond)}'
        : '${_pretty(task.sent)} / ${_pretty(task.total == 0 ? task.size : task.total)}'
            '${task.state == _TaskState.uploading ? ' · ${_rate(task.bytesPerSecond)}' : ''}';

    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 10, 10, 10),
      child: Row(
        children: [
          Container(
            width: 30,
            height: 30,
            decoration: BoxDecoration(
              color: task.state == _TaskState.done ? const Color(0xFFDDF1E4) : scheme.primaryContainer,
              borderRadius: BorderRadius.circular(ErTokens.radiusSm),
            ),
            child: Icon(
              task.state == _TaskState.done ? Icons.check_rounded : Icons.upload_rounded,
              size: 16,
              color: task.state == _TaskState.done ? const Color(0xFF1E7B45) : scheme.onPrimaryContainer,
            ),
          ),
          const SizedBox(width: 10),
          SizedBox(
            width: 210,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  task.name,
                  style: text.bodySmall?.copyWith(fontWeight: FontWeight.w600),
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: 2),
                Text(
                  '本机 → 远端（网关存储）',
                  style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                ClipRRect(
                  borderRadius: BorderRadius.circular(ErTokens.radiusPill),
                  child: LinearProgressIndicator(
                    value: task.state == _TaskState.done ? 1 : task.progress,
                    minHeight: 6,
                    backgroundColor: scheme.surfaceContainerHighest,
                    valueColor: AlwaysStoppedAnimation<Color>(barColor),
                  ),
                ),
                const SizedBox(height: 4),
                Row(
                  children: [
                    Text('${task.percent}%', style: text.labelSmall?.copyWith(color: scheme.onSurface)),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        task.error ?? sentText,
                        style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          SizedBox(
            width: 74,
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (task.state == _TaskState.done) Icon(Icons.check_circle_rounded, size: 14, color: stateColor),
                const SizedBox(width: 4),
                Flexible(
                  child: Text(
                    stateText,
                    style: text.labelSmall?.copyWith(color: stateColor, fontWeight: FontWeight.w600),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
          ),
          _taskAction(),
        ],
      ),
    );
  }

  Widget _taskAction() {
    return switch (task.state) {
      _TaskState.uploading => _iconButton(Icons.pause_rounded, '暂停传输', onPause),
      _TaskState.paused => _iconButton(Icons.play_arrow_rounded, '继续传输', onResume),
      _TaskState.queued => _iconButton(Icons.close_rounded, '移出队列', onRemove),
      _TaskState.done => _iconButton(Icons.close_rounded, '从队列移除', onRemove),
      _TaskState.failed => _iconButton(Icons.refresh_rounded, '重试', onRetry),
    };
  }

  Widget _iconButton(IconData icon, String tip, VoidCallback tap) {
    return IconButton(tooltip: tip, onPressed: tap, icon: Icon(icon, size: 18));
  }
}

/// 本机栏：Web 下是「选择文件」入口（浏览器不能枚举目录，所以不画目录树）。
class _LocalPane extends StatelessWidget {
  const _LocalPane({
    required this.files,
    required this.pickerSupported,
    required this.onPick,
    required this.onTextEntry,
    required this.onUpload,
    required this.onRemove,
    required this.notice,
  });

  final List<PickedLocalFile> files;
  final bool pickerSupported;
  final VoidCallback onPick;
  final VoidCallback onTextEntry;
  final ValueChanged<PickedLocalFile> onUpload;
  final ValueChanged<PickedLocalFile> onRemove;
  final String notice;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    return _Pane(
      icon: Icons.monitor_rounded,
      title: '本机',
      path: pickerSupported ? '浏览器沙箱 · 只能选择文件' : '本机文件 · 按路径读取',
      action: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextButton.icon(
            key: const Key('pick-file'),
            onPressed: onPick,
            icon: Icon(pickerSupported ? Icons.upload_rounded : Icons.drive_file_rename_outline_rounded, size: 16),
            label: Text(pickerSupported ? '选择文件' : '按路径选择'),
          ),
          TextButton(
            key: const Key('text-file-entry'),
            onPressed: onTextEntry,
            child: const Text('文本内容'),
          ),
        ],
      ),
      crumbs: const ['待上传', '全部文件'],
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const _FTableHeader(columns: ['名称', '大小', '状态']),
          Expanded(
            child: files.isEmpty
                ? Center(
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 20),
                      child: Text(
                        pickerSupported
                            ? '点「选择文件」挑一个本机文件；浏览器不允许枚举目录，所以这里只列你选中的文件。文本文件也可以走「文本内容」入口。'
                            : '点「按路径选择」填写本机路径；桌面端才能读本机文件。',
                        textAlign: TextAlign.center,
                        style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                      ),
                    ),
                  )
                : ListView.separated(
                    padding: EdgeInsets.zero,
                    itemCount: files.length,
                    separatorBuilder: (_, _) => Divider(height: 1, color: scheme.outlineVariant.withValues(alpha: 0.4)),
                    itemBuilder: (context, i) {
                      final f = files[i];
                      return _FRow(
                        leading: Icons.insert_drive_file_outlined,
                        name: f.name,
                        size: _pretty(f.size),
                        meta: '待上传',
                        actions: [
                          _rowIconButton(
                            tooltip: '上传',
                            onPressed: () => onUpload(f),
                            icon: Icon(Icons.upload_rounded, size: 18, color: scheme.primary),
                          ),
                          _rowIconButton(
                            tooltip: '移除',
                            onPressed: () => onRemove(f),
                            icon: const Icon(Icons.close_rounded, size: 18),
                          ),
                        ],
                      );
                    },
                  ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 6, 12, 8),
            child: Text(notice, style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
          ),
        ],
      ),
    );
  }
}

/// 远端栏：`GET /v1/files` 的真实列表，行内下载（票据）与删除。
class _RemotePane extends StatelessWidget {
  const _RemotePane({
    required this.files,
    required this.loading,
    required this.error,
    required this.onRefresh,
    required this.onDownload,
    required this.onDelete,
    required this.onUploadEntry,
  });

  final List<FileInfo>? files;
  final bool loading;
  final String? error;
  final VoidCallback onRefresh;
  final ValueChanged<FileInfo> onDownload;
  final ValueChanged<FileInfo> onDelete;
  final VoidCallback onUploadEntry;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final list = files;

    return _Pane(
      icon: Icons.dns_rounded,
      title: '远端',
      path: '网关存储 · /v1/files',
      action: TextButton.icon(
        key: const Key('refresh-files'),
        onPressed: loading ? null : onRefresh,
        icon: const Icon(Icons.refresh_rounded, size: 16),
        label: const Text('刷新'),
      ),
      crumbs: const ['工作区', '全部文件'],
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const _FTableHeader(columns: ['名称', '大小', '时间']),
          Expanded(
            child: loading && list == null
                ? const Center(child: SizedBox(height: 22, width: 22, child: CircularProgressIndicator(strokeWidth: 2)))
                : (list == null || list.isEmpty)
                    ? Center(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 20),
                          child: Column(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Icon(Icons.folder_open_rounded, size: 26, color: scheme.onSurfaceVariant),
                              const SizedBox(height: 8),
                              Text(
                                error != null ? '加载失败：$error' : '远端还没有文件',
                                textAlign: TextAlign.center,
                                style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                              ),
                              const SizedBox(height: 10),
                              Wrap(
                                spacing: 8,
                                children: [
                                  OutlinedButton(onPressed: onRefresh, child: const Text('刷新')),
                                  TextButton(onPressed: onUploadEntry, child: const Text('上传文本文件')),
                                ],
                              ),
                            ],
                          ),
                        ),
                      )
                    : ListView.separated(
                        padding: EdgeInsets.zero,
                        itemCount: list.length,
                        separatorBuilder: (_, _) => Divider(height: 1, color: scheme.outlineVariant.withValues(alpha: 0.4)),
                        itemBuilder: (context, i) {
                          final f = list[i];
                          return _FRow(
                            leading: Icons.description_outlined,
                            name: f.name,
                            size: f.prettySize,
                            meta: _ago(f.createdAt),
                            actions: [
                              _rowIconButton(
                                tooltip: localFileAccessSupported ? '下载' : '复制下载票据',
                                onPressed: () => onDownload(f),
                                icon: Icon(
                                  localFileAccessSupported ? Icons.download_rounded : Icons.link_rounded,
                                  size: 18,
                                  color: scheme.primary,
                                ),
                              ),
                              _rowIconButton(
                                tooltip: '删除',
                                onPressed: () => onDelete(f),
                                icon: Icon(Icons.delete_outline_rounded, size: 18, color: scheme.error),
                              ),
                            ],
                          );
                        },
                      ),
          ),
        ],
      ),
    );
  }
}

/// 双栏容器：头（图标 + 标题 + 路径 + 操作）+ 面包屑 + 内容。
class _Pane extends StatelessWidget {
  const _Pane({
    required this.icon,
    required this.title,
    required this.path,
    required this.action,
    required this.crumbs,
    required this.child,
  });

  final IconData icon;
  final String title;
  final String path;
  final Widget action;
  final List<String> crumbs;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    return Container(
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 8, 8, 8),
            child: Row(
              children: [
                Icon(icon, size: 17, color: scheme.onSurfaceVariant),
                const SizedBox(width: 7),
                Text(title, style: text.titleSmall),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    path,
                    style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant, fontFamily: 'monospace'),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                action,
              ],
            ),
          ),
          Divider(height: 1, color: scheme.outlineVariant.withValues(alpha: 0.5)),
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 8, 14, 8),
            child: Row(
              children: [
                Text(crumbs.first, style: text.labelSmall?.copyWith(fontWeight: FontWeight.w600)),
                Icon(Icons.chevron_right_rounded, size: 14, color: scheme.onSurfaceVariant),
                Text(
                  crumbs.length > 1 ? crumbs[1] : '',
                  style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
                ),
              ],
            ),
          ),
          Divider(height: 1, color: scheme.outlineVariant.withValues(alpha: 0.45)),
          Expanded(child: child),
        ],
      ),
    );
  }
}

class _FTableHeader extends StatelessWidget {
  const _FTableHeader({required this.columns});

  final List<String> columns;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 7, 14, 7),
      color: scheme.surfaceContainerLow,
      child: Row(
        children: [
          const SizedBox(width: 25),
          Expanded(
            child: Text(columns[0], style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
          ),
          SizedBox(
            width: 78,
            child: Text(columns[1], style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
          ),
          SizedBox(
            width: 92,
            child: Text(columns[2], style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
          ),
          const SizedBox(width: 72),
        ],
      ),
    );
  }
}

class _FRow extends StatelessWidget {
  const _FRow({
    required this.leading,
    required this.name,
    required this.size,
    required this.meta,
    required this.actions,
  });

  final IconData leading;
  final String name;
  final String size;
  final String meta;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.fromLTRB(14, 2, 6, 2),
      child: Row(
        children: [
          Icon(leading, size: 17, color: scheme.onSurfaceVariant),
          const SizedBox(width: 8),
          Expanded(child: Text(name, style: text.bodySmall, overflow: TextOverflow.ellipsis)),
          SizedBox(
            width: 78,
            child: Text(
              size,
              style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant, fontFamily: 'monospace'),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          SizedBox(
            width: 92,
            child: Text(
              meta,
              style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant, fontFamily: 'monospace'),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          SizedBox(
            width: 72,
            child: Row(mainAxisAlignment: MainAxisAlignment.end, children: actions),
          ),
        ],
      ),
    );
  }
}

/// 表格行里的小图标按钮：默认 48×48 会把双栏行撑爆，这里收紧到 30×30。
Widget _rowIconButton({
  required String tooltip,
  required VoidCallback onPressed,
  required Widget icon,
}) {
  return IconButton(
    tooltip: tooltip,
    onPressed: onPressed,
    icon: icon,
    padding: EdgeInsets.zero,
    constraints: const BoxConstraints(minWidth: 30, minHeight: 30),
    visualDensity: VisualDensity.compact,
    style: IconButton.styleFrom(tapTargetSize: MaterialTapTargetSize.shrinkWrap),
  );
}

class _CountChip extends StatelessWidget {
  const _CountChip({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: scheme.surfaceContainer,
        borderRadius: BorderRadius.circular(ErTokens.radiusPill),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.6)),
      ),
      child: Text(text, style: Theme.of(context).textTheme.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
    );
  }
}

class _Banner extends StatelessWidget {
  const _Banner({
    required this.icon,
    required this.color,
    required this.background,
    required this.text,
    required this.onClose,
  });

  final IconData icon;
  final Color color;
  final Color background;
  final String text;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.fromLTRB(12, 8, 4, 8),
      decoration: BoxDecoration(color: background, borderRadius: BorderRadius.circular(ErTokens.radiusMd)),
      child: Row(
        children: [
          Icon(icon, size: 16, color: color),
          const SizedBox(width: 8),
          Expanded(child: Text(text, style: Theme.of(context).textTheme.bodySmall?.copyWith(color: color))),
          IconButton(
            onPressed: onClose,
            icon: Icon(Icons.close_rounded, size: 16, color: color),
            visualDensity: VisualDensity.compact,
          ),
        ],
      ),
    );
  }
}

/// 文本文件入口：Web 端没有本机路径时的等价路径，字节真的按 UTF-8 上传。
class _TextFileDialog extends StatefulWidget {
  const _TextFileDialog();

  @override
  State<_TextFileDialog> createState() => _TextFileDialogState();
}

class _TextFileDialogState extends State<_TextFileDialog> {
  final _name = TextEditingController(text: 'note.txt');
  final _body = TextEditingController(text: 'EasyRemote 文件传输测试');
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _body.dispose();
    super.dispose();
  }

  void _submit() {
    final name = _name.text.trim().isEmpty ? 'note.txt' : _name.text.trim();
    final bytes = Uint8List.fromList(utf8.encode(_body.text));
    if (bytes.isEmpty) {
      setState(() => _error = '内容为空，服务端会拒绝空文件');
      return;
    }
    Navigator.of(context).pop(PickedLocalFile(name, bytes));
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return AlertDialog(
      title: const Text('文本文件'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              '浏览器拿不到本机磁盘路径时的等价入口：按 UTF-8 编码后走同一个上传接口。',
              style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('text-file-name'),
              controller: _name,
              decoration: const InputDecoration(
                labelText: '文件名',
                prefixIcon: Icon(Icons.insert_drive_file_outlined, size: 18),
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('text-file-body'),
              controller: _body,
              minLines: 4,
              maxLines: 8,
              decoration: const InputDecoration(labelText: '文本内容（UTF-8）', alignLabelWithHint: true),
            ),
            if (_error != null) ...[
              const SizedBox(height: 10),
              Text(_error!, style: text.bodySmall?.copyWith(color: scheme.error)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('取消')),
        FilledButton(
          key: const Key('text-file-submit'),
          onPressed: _submit,
          child: const Text('加入本机栏'),
        ),
      ],
    );
  }
}

/// 人类可读大小（B / KiB / MiB）与速率（B/s / KiB/s / MiB/s）。
String _pretty(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KiB';
  return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MiB';
}

String _rate(double bytesPerSecond) {
  if (bytesPerSecond <= 0) return '—';
  if (bytesPerSecond < 1024) return '${bytesPerSecond.toStringAsFixed(0)} B/s';
  if (bytesPerSecond < 1024 * 1024) return '${(bytesPerSecond / 1024).toStringAsFixed(1)} KiB/s';
  return '${(bytesPerSecond / (1024 * 1024)).toStringAsFixed(1)} MiB/s';
}

/// 相对时间：刚刚 / N 分钟前 / N 小时前 / N 天前 / 日期。
String _ago(DateTime? at) {
  if (at == null) return '时间未知';
  final diff = DateTime.now().difference(at);
  if (diff.inSeconds < 60) return '刚刚';
  if (diff.inMinutes < 60) return '${diff.inMinutes} 分钟前';
  if (diff.inHours < 24) return '${diff.inHours} 小时前';
  if (diff.inDays < 8) return '${diff.inDays} 天前';
  String two(int n) => n.toString().padLeft(2, '0');
  return '${at.year}-${two(at.month)}-${two(at.day)} ${two(at.hour)}:${two(at.minute)}';
}
