import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../capabilities.dart';
import '../capability_view.dart';
import '../error_view.dart';
import '../prefs.dart';
import '../shell.dart';
import '../theme.dart';

/// 连接页：3-3-3 分段设备码 + 验证码（复刻原型「输入设备码」）。
///
/// 输入实现：一个透明的 TextField 承载真实文本，外面画 9 个格子。
/// 这样焦点只有一处，输入法/退格/粘贴都按系统行为走，不会出现逐格丢字。
class ConnectPage extends StatefulWidget {
  const ConnectPage({
    super.key,
    required this.client,
    required this.initialCode,
    required this.recents,
    required this.onCodeUsed,
    required this.onSessionStarted,
  });

  final GatewayClient client;
  final String initialCode;
  final List<String> recents;
  final ValueChanged<String> onCodeUsed;

  /// 会话建立回调：(会话 ID, 目标设备码)。协助码接入时设备码为空串，
  /// 会话页据此在「连接信息 · 连接方式」里如实写是哪种接入方式。
  final void Function(String sessionId, String targetCode) onSessionStarted;

  @override
  State<ConnectPage> createState() => _ConnectPageState();
}

/// 连接方式：设备码 + 临时密码（被控端窗口显示，长期有效）/ 协助码（被控端临时授权）。
enum _ConnectMode { device, assist }

class _ConnectPageState extends State<ConnectPage> {
  final _codeCtl = TextEditingController();

  /// 9 格输入的句柄：粘贴后把焦点放到第一个空格。
  final _codeEntryKey = GlobalKey<_CodeEntryState>();
  final _captcha = TextEditingController();
  final _assistCtl = TextEditingController();
  final _assistFocus = FocusNode();
  _ConnectMode _mode = _ConnectMode.device;
  bool _busy = false;
  String? _error;
  ApiException? _apiError;

  /// 请求期间每秒 +1：把「卡住」变成「看得见的等待」。
  int _elapsed = 0;
  Timer? _busyTick;

  /// 每次发起请求自增；取消时也自增，用来丢弃迟到的响应。
  int _attempt = 0;

  @override
  void initState() {
    super.initState();
    _applyCode(widget.initialCode);
  }

  @override
  void didUpdateWidget(covariant ConnectPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.initialCode != oldWidget.initialCode)
      _applyCode(widget.initialCode);
  }

  void _applyCode(String code) {
    _codeCtl.text = code.replaceAll(RegExp(r'[^0-9]'), '');
    // 从「我的设备」点连接会带设备码进来，此时必须切回设备码模式，否则看起来像没反应。
    if (code.isNotEmpty) _mode = _ConnectMode.device;
    setState(() {});
  }

  String get _code => _codeCtl.text;

  @override
  void dispose() {
    _busyTick?.cancel();
    _codeCtl.dispose();
    _captcha.dispose();
    _assistCtl.dispose();
    _assistFocus.dispose();
    super.dispose();
  }

  /// 从剪贴板取 9 位设备码填进格子（剪贴板里混着空格/横线也能用）。
  Future<void> _pasteCodeFromClipboard() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final digits = (data?.text ?? "").replaceAll(RegExp(r"[^0-9]"), "");
    if (digits.isEmpty) return;
    _codeCtl.text = digits.length > 9 ? digits.substring(0, 9) : digits;
    _codeEntryKey.currentState?.focusNextEmpty();
    setState(() => _error = null);
  }

  /// 进入「等待中」：立刻给出进度、已等待秒数和一个可点的取消。
  void _startBusy() {
    _busyTick?.cancel();
    setState(() {
      _busy = true;
      _error = null;
      _apiError = null;
      _apiError = null;
      _elapsed = 0;
    });
    _busyTick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted && _busy) setState(() => _elapsed++);
    });
  }

  /// 收尾：只有「还是同一次尝试」才改界面，避免取消后又被迟到的响应改回去。
  void _stopBusy(int attempt) {
    if (attempt != _attempt) return;
    _busyTick?.cancel();
    _busyTick = null;
    if (mounted) setState(() => _busy = false);
  }

  /// 取消等待。
  ///
  /// 在途 HTTP 无法真正中断，但界面必须立刻回到可操作状态；_attempt 自增让
  /// 迟到的响应被丢弃（不会突然弹出一个会话页）。
  void _cancel() {
    _attempt++;
    _busyTick?.cancel();
    _busyTick = null;
    setState(() {
      _busy = false;
      _elapsed = 0;
      _error = null;
      _apiError = null;
    });
  }

  /// 9 位码按 3-3-3 展示（长度不对就原样）。
  String get _codePretty {
    final d = _code;
    if (d.length != 9) return d;
    return d.substring(0, 3) +
        ' ' +
        d.substring(3, 6) +
        ' ' +
        d.substring(6, 9);
  }

  Future<void> _connect() async {
    final attempt = ++_attempt;
    _startBusy();
    try {
      final code = _code;
      final sid = await widget.client.requestConnect(
        code,
        captcha: _captcha.text.trim(),
        // 只对「同一账号」的目标生效：跨账号服务端仍会要求临时密码。
        direct: appPrefs.value.sameAccountDirect,
      );
      if (!mounted || attempt != _attempt) return;
      widget.onCodeUsed(code);
      widget.onSessionStarted(sid, code.replaceAll(RegExp(r'[^0-9]'), ''));
    } on ApiException catch (e) {
      if (mounted && attempt == _attempt) {
        setState(() {
          _apiError = e;
          _error = _friendlyConnectError(e);
        });
      }
    } catch (e) {
      if (mounted && attempt == _attempt) {
        setState(() {
          _apiError = null;
          _error = e.toString();
        });
      }
    } finally {
      _stopBusy(attempt);
    }
  }

  /// 协助码兑换：协助码本身就是被控端的授权意图，服务端不做验证码校验。
  Future<void> _redeemAssist() async {
    final code = _assistCtl.text.trim();
    if (code.length != 6) {
      setState(() {
        _apiError = null;
        _error = '协助码是 6 位数字';
      });
      return;
    }
    final attempt = ++_attempt;
    _startBusy();
    try {
      final sid = await widget.client.redeemAssistCode(code);
      if (!mounted || attempt != _attempt) return;
      _assistCtl.clear();
      widget.onSessionStarted(sid, '');
    } on ApiException catch (e) {
      if (mounted && attempt == _attempt) {
        setState(() {
          _apiError = e;
          _error = e.message;
        });
      }
    } catch (e) {
      if (mounted && attempt == _attempt) {
        setState(() {
          _apiError = null;
          _error = e.toString();
        });
      }
    } finally {
      _stopBusy(attempt);
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;
    final ready = _code.length == 9;
    final assistReady = _assistCtl.text.trim().length == 6;

    // 能力门：发布构建里实验能力直接给空态，不渲染可点的假入口。
    final controlGate = CapabilityGate.instance.stateOf('remote_control');
    if (!controlGate.available) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PageHeader(title: '连接设备', subtitle: '远程控制能力未开放'),
          Expanded(
            child: CapabilityUnavailableView(
              title: '远程控制尚未开放',
              state: controlGate,
            ),
          ),
        ],
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const PageHeader(title: '连接设备', subtitle: '用设备码请求连接，或用对方给的协助码临时接入'),
        Expanded(
          child: LayoutBuilder(
            builder: (context, constraints) => SingleChildScrollView(
              padding: EdgeInsets.fromLTRB(
                wide ? 32 : 16,
                0,
                wide ? 32 : 16,
                32,
              ),
              child: ConstrainedBox(
                constraints: BoxConstraints(
                  minHeight: constraints.maxHeight - 8,
                ),
                child: Center(
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 560),
                    child: Container(
                      padding: EdgeInsets.all(wide ? 28 : 20),
                      decoration: BoxDecoration(
                        color: scheme.surfaceContainerLow,
                        borderRadius: BorderRadius.circular(ErTokens.radiusXl),
                        border: Border.all(
                          color: scheme.outlineVariant.withValues(alpha: 0.7),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          Row(
                            children: [
                              Container(
                                width: 40,
                                height: 40,
                                decoration: BoxDecoration(
                                  color: scheme.primaryContainer,
                                  borderRadius: BorderRadius.circular(11),
                                ),
                                child: Icon(
                                  Icons.pin_rounded,
                                  size: 21,
                                  color: scheme.onPrimaryContainer,
                                ),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(
                                      _mode == _ConnectMode.device
                                          ? '输入设备码'
                                          : '输入协助码',
                                      style: text.titleLarge,
                                    ),
                                    Text(
                                      _mode == _ConnectMode.device
                                          ? '9 位数字，输满后即可请求连接'
                                          : '6 位数字，一次一码，10 分钟内有效',
                                      style: text.bodySmall?.copyWith(
                                        color: scheme.onSurfaceVariant,
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 18),
                          _ModeSwitch(
                            mode: _mode,
                            onChanged: (m) => setState(() {
                              _mode = m;
                              _error = null;
                              _apiError = null;
                              _apiError = null;
                            }),
                          ),
                          const SizedBox(height: 18),
                          if (_mode == _ConnectMode.device) ...[
                            _CodeEntry(
                              key: _codeEntryKey,
                              controller: _codeCtl,
                              onChanged: () => setState(() {}),
                            ),
                            // 粘贴入口：透明输入层的光标与格子对不上，
                            // 与其纠结光标位置，不如给一个显式的「粘贴设备码」。
                            Align(
                              alignment: Alignment.centerRight,
                              child: TextButton.icon(
                                onPressed: _pasteCodeFromClipboard,
                                icon: const Icon(
                                  Icons.content_paste_rounded,
                                  size: 16,
                                ),
                                label: const Text("粘贴设备码"),
                              ),
                            ),
                            const SizedBox(height: 6),
                            TextField(
                              key: const Key('captcha-field'),
                              controller: _captcha,
                              keyboardType: TextInputType.number,
                              decoration: InputDecoration(
                                labelText: '临时密码',
                                hintText: appPrefs.value.sameAccountDirect
                                    ? '同账号设备可留空（免密直连已开启）'
                                    : '被控端窗口里显示的 8 位数字（可长期复用）',
                                prefixIcon: const Icon(
                                  Icons.verified_user_outlined,
                                  size: 18,
                                ),
                              ),
                            ),
                          ] else ...[
                            TextField(
                              key: const Key('assist-code-field'),
                              controller: _assistCtl,
                              focusNode: _assistFocus,
                              keyboardType: TextInputType.number,
                              maxLength: 6,
                              onChanged: (_) => setState(() {
                                _error = null;
                                _apiError = null;
                              }),
                              inputFormatters: [
                                FilteringTextInputFormatter.digitsOnly,
                                LengthLimitingTextInputFormatter(6),
                              ],
                              decoration: const InputDecoration(
                                labelText: '协助码',
                                hintText: '对方「我的设备」页生成的 6 位数字',
                                counterText: '',
                                prefixIcon: Icon(Icons.key_rounded, size: 18),
                              ),
                            ),
                            const SizedBox(height: 8),
                            Text(
                              '协助码 10 分钟内有效、只能用一次；兑换成功即建立会话，无需验证码。',
                              style: text.labelSmall?.copyWith(
                                color: scheme.onSurfaceVariant,
                              ),
                            ),
                          ],
                          if (_apiError != null) ...[
                            const SizedBox(height: 12),
                            ApiErrorView(
                              error: _apiError!,
                              onRetry: _mode == _ConnectMode.device
                                  ? _connect
                                  : _redeemAssist,
                            ),
                          ],
                          if (_error != null && _apiError == null) ...[
                            const SizedBox(height: 12),
                            Container(
                              padding: const EdgeInsets.symmetric(
                                horizontal: 12,
                                vertical: 10,
                              ),
                              decoration: BoxDecoration(
                                color: scheme.errorContainer,
                                borderRadius: BorderRadius.circular(
                                  ErTokens.radiusMd,
                                ),
                              ),
                              child: Row(
                                children: [
                                  Icon(
                                    Icons.error_outline_rounded,
                                    size: 16,
                                    color: scheme.onErrorContainer,
                                  ),
                                  const SizedBox(width: 8),
                                  Expanded(
                                    child: Text(
                                      _error!,
                                      style: text.bodySmall?.copyWith(
                                        color: scheme.onErrorContainer,
                                      ),
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ],
                          const SizedBox(height: 18),
                          // 请求中不再是「按钮里塞个小转圈」——那样一卡住就只剩一个
                          // 转不动的界面。这里给出显式的等待条：进度 + 已等待秒数 + 取消。
                          if (_busy)
                            _PendingBar(
                              label: _mode == _ConnectMode.device
                                  ? '正在请求连接 ' + _codePretty
                                  : '正在兑换协助码 ' + _assistCtl.text.trim(),
                              seconds: _elapsed,
                              onCancel: _cancel,
                            )
                          else
                            FilledButton(
                              key: const Key('connect-button'),
                              onPressed:
                                  (_mode == _ConnectMode.device
                                      ? !ready
                                      : !assistReady)
                                  ? null
                                  : (_mode == _ConnectMode.device
                                        ? _connect
                                        : _redeemAssist),
                              child: Text(
                                _mode == _ConnectMode.device ? '请求连接' : '兑换协助码',
                              ),
                            ),
                          const SizedBox(height: 10),
                          Text(
                            _mode == _ConnectMode.device
                                ? '首次连接需要对方在弹出的授权框里同意；建立信任后可直接连接。'
                                : '协助码是被控端给出的临时授权，兑换即建立请求，不需对方再点同意。',
                            style: text.labelSmall?.copyWith(
                              color: scheme.onSurfaceVariant,
                            ),
                            textAlign: TextAlign.center,
                          ),
                          if (_mode == _ConnectMode.device &&
                              widget.recents.isNotEmpty) ...[
                            const SizedBox(height: 20),
                            Text(
                              '最近连接',
                              style: text.labelSmall?.copyWith(
                                color: scheme.onSurfaceVariant,
                              ),
                            ),
                            const SizedBox(height: 8),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              children: [
                                for (final r in widget.recents)
                                  ActionChip(
                                    label: Text(r),
                                    onPressed: () => _applyCode(r),
                                  ),
                              ],
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}

/// 两种接入方式的切换：设备码（9 位）/ 协助码（6 位）。
class _ModeSwitch extends StatelessWidget {
  const _ModeSwitch({required this.mode, required this.onChanged});

  final _ConnectMode mode;
  final ValueChanged<_ConnectMode> onChanged;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    Widget chip(_ConnectMode value, IconData icon, String label) {
      final on = mode == value;
      return Expanded(
        child: InkWell(
          key: Key(
            value == _ConnectMode.device ? 'mode-device' : 'mode-assist',
          ),
          onTap: () => onChanged(value),
          borderRadius: BorderRadius.circular(ErTokens.radiusMd),
          child: AnimatedContainer(
            duration: const Duration(milliseconds: 120),
            height: 40,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: on ? scheme.primaryContainer : Colors.transparent,
              borderRadius: BorderRadius.circular(ErTokens.radiusMd),
              border: Border.all(
                color: on
                    ? scheme.primary.withValues(alpha: 0.5)
                    : Colors.transparent,
              ),
            ),
            child: Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(
                  icon,
                  size: 16,
                  color: on
                      ? scheme.onPrimaryContainer
                      : scheme.onSurfaceVariant,
                ),
                const SizedBox(width: 6),
                Text(
                  label,
                  style: text.labelMedium?.copyWith(
                    color: on
                        ? scheme.onPrimaryContainer
                        : scheme.onSurfaceVariant,
                    fontWeight: on ? FontWeight.w600 : FontWeight.w400,
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    }

    return Container(
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHigh.withValues(alpha: 0.7),
        borderRadius: BorderRadius.circular(ErTokens.radiusMd + 2),
      ),
      child: Row(
        children: [
          chip(_ConnectMode.device, Icons.pin_rounded, '设备码'),
          const SizedBox(width: 4),
          chip(_ConnectMode.assist, Icons.key_rounded, '协助码'),
        ],
      ),
    );
  }
}

/// 9 格设备码输入：每一格都是一个**真的** TextField。
///
/// 旧实现是「一个透明 TextField 盖在 9 个画出来的格子上」——文字和光标都透明，
/// 光标位置永远和格子对不上，选区也看不见，结果就是「没光标、选不中数字」。
/// 这里换成 9 个真输入框：光标、选区、输入法、退格全部走系统原生行为。
class _CodeEntry extends StatefulWidget {
  const _CodeEntry({
    super.key,
    required this.controller,
    required this.onChanged,
  });

  /// 聚合值：恒为 0–9 位纯数字。页面其它入口（粘贴按钮、最近连接）只改这里。
  final TextEditingController controller;
  final VoidCallback onChanged;

  @override
  State<_CodeEntry> createState() => _CodeEntryState();
}

class _CodeEntryState extends State<_CodeEntry> {
  static const _len = 9;

  final List<String> _chars = List<String>.filled(_len, '');
  late final List<TextEditingController> _ctls = List.generate(
    _len,
    (_) => TextEditingController(),
  );
  late final List<FocusNode> _focus = List.generate(_len, (_) => FocusNode());

  /// 防回环：我们自己写 widget.controller.text 时不要再触发一次分发。
  bool _publishing = false;

  @override
  void initState() {
    super.initState();
    for (var i = 0; i < _len; i++) {
      _focus[i].addListener(_onFocusChanged);
    }
    _distribute(widget.controller.text);
    widget.controller.addListener(_onExternalChange);
  }

  @override
  void dispose() {
    widget.controller.removeListener(_onExternalChange);
    for (final c in _ctls) {
      c.dispose();
    }
    for (final f in _focus) {
      f.removeListener(_onFocusChanged);
      f.dispose();
    }
    super.dispose();
  }

  void _onFocusChanged() {
    if (mounted) setState(() {});
  }

  void _onExternalChange() {
    if (_publishing) return;
    if (widget.controller.text == _chars.join()) return;
    setState(() => _distribute(widget.controller.text));
  }

  /// 把一串数字铺进 9 格（外部粘贴 / 从「最近连接」带入）。
  void _distribute(String raw) {
    final digits = raw.replaceAll(RegExp(r'[^0-9]'), '');
    for (var i = 0; i < _len; i++) {
      _chars[i] = i < digits.length ? digits[i] : '';
      if (_ctls[i].text != _chars[i]) _ctls[i].text = _chars[i];
    }
  }

  /// 把格子内容写回聚合控制器（页面的 _code 取的就是这里）。
  void _publish() {
    _publishing = true;
    widget.controller.text = _chars.join();
    _publishing = false;
    widget.onChanged();
  }

  void _onCellChanged(int i, String value) {
    final digits = value.replaceAll(RegExp(r'[^0-9]'), '');
    if (digits.length > 1) {
      // 粘贴 / 自动填充：从当前格开始往后铺，焦点落到下一个空格。
      var j = i;
      for (final d in digits.split('')) {
        if (j >= _len) break;
        _chars[j] = d;
        _ctls[j].text = d;
        j++;
      }
      _focus[j < _len ? j : _len - 1].requestFocus();
    } else {
      _chars[i] = digits;
      if (_ctls[i].text != digits) _ctls[i].text = digits;
      if (digits.isNotEmpty && i < _len - 1) _focus[i + 1].requestFocus();
    }
    setState(() {});
    _publish();
  }

  /// 空格子上的退格：回到上一格并删掉它；左右方向键在格子间移动。
  KeyEventResult _onKey(int i, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    if (key == LogicalKeyboardKey.backspace && _chars[i].isEmpty && i > 0) {
      setState(() {
        _chars[i - 1] = '';
        _ctls[i - 1].text = '';
      });
      _focus[i - 1].requestFocus();
      _publish();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.arrowLeft && i > 0) {
      _focus[i - 1].requestFocus();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.arrowRight && i < _len - 1) {
      _focus[i + 1].requestFocus();
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  /// 给页面「粘贴设备码」用：把焦点放到第一个空格。
  void focusNextEmpty() {
    final i = _chars.indexWhere((c) => c.isEmpty);
    _focus[i < 0 ? _len - 1 : i].requestFocus();
  }

  Widget _cell(int i) {
    final scheme = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    final active = _focus[i].hasFocus;

    return Focus(
      skipTraversal: true,
      onKeyEvent: (_, event) => _onKey(i, event),
      child: GestureDetector(
        onTap: () => _focus[i].requestFocus(),
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 120),
          width: 46,
          height: 56,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: scheme.surfaceContainerLowest,
            borderRadius: BorderRadius.circular(ErTokens.radiusMd),
            border: Border.all(
              color: active ? scheme.primary : scheme.outlineVariant,
              width: active ? 1.8 : 1.2,
            ),
          ),
          child: TextField(
            key: Key('device-code-digit-$i'),
            controller: _ctls[i],
            focusNode: _focus[i],
            textAlign: TextAlign.center,
            keyboardType: TextInputType.number,
            textInputAction: TextInputAction.next,
            cursorColor: scheme.primary,
            cursorWidth: 1.6,
            style: textTheme.headlineSmall?.copyWith(
              fontFeatures: const [FontFeature.tabularFigures()],
            ),
            strutStyle: const StrutStyle(forceStrutHeight: true, height: 1.1),
            // 不做长度截断：粘贴 9 位时先让它进来，再分发到后面几格。
            inputFormatters: [FilteringTextInputFormatter.digitsOnly],
            onChanged: (v) => _onCellChanged(i, v),
            decoration: const InputDecoration(
              isDense: true,
              counterText: '',
              border: InputBorder.none,
              enabledBorder: InputBorder.none,
              focusedBorder: InputBorder.none,
              contentPadding: EdgeInsets.zero,
              // Material 3 的 InputDecorator 会在悬停时自己画一块灰色高亮，
              // 而格子的底色/边框是外层 AnimatedContainer 画的——两层叠在一起就是
              // 鼠标划过时格子中间多出来的那个灰色小方块。这里关掉它。
              hoverColor: Colors.transparent,
            ),
          ),
        ),
      ),
    );
  }

  Widget _group(int start) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      for (var i = start; i < start + 3; i++) ...[
        _cell(i),
        if (i != start + 2) const SizedBox(width: 6),
      ],
    ],
  );

  @override
  Widget build(BuildContext context) {
    return FittedBox(
      fit: BoxFit.scaleDown,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _group(0),
          const SizedBox(width: 14),
          _group(3),
          const SizedBox(width: 14),
          _group(6),
        ],
      ),
    );
  }
}

/// 请求中的等待条：进度 + 已等待秒数 + 取消。
///
/// 以前这里只有按钮里一个 18px 的转圈，请求一卡住（网关不回、反代挂住）界面
/// 就等于「死了」：既没有明确状态，也没有任何退路。取消只让界面回到可操作，
/// 在途请求由页面丢弃，不会弹出一个迟到的会话页。
class _PendingBar extends StatelessWidget {
  const _PendingBar({
    required this.label,
    required this.seconds,
    required this.onCancel,
  });

  final String label;
  final int seconds;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Container(
      key: const Key('connect-pending'),
      padding: const EdgeInsets.fromLTRB(14, 8, 8, 8),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHigh,
        borderRadius: BorderRadius.circular(ErTokens.radiusMd),
      ),
      child: Row(
        children: [
          const SizedBox(
            width: 18,
            height: 18,
            child: CircularProgressIndicator(strokeWidth: 2),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  label,
                  style: text.bodySmall,
                  overflow: TextOverflow.ellipsis,
                ),
                Text(
                  '已等待 ' + seconds.toString() + ' 秒',
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
          TextButton(
            key: const Key('connect-cancel'),
            onPressed: onCancel,
            child: const Text('取消'),
          ),
        ],
      ),
    );
  }
}

/// 连接阶段的服务端错误 → 用户能照做的提示。
///
/// 连接错误 → 用户能照做的提示。
///
/// 现在的主流程是「设备码 + 临时密码」：密码长期有效、可重复使用，被控端窗口里
/// 点「显示」就能看到明文、点「复制」可直接粘。仍保留对旧「一次性验证码」错误的兼容文案。
String _friendlyConnectError(ApiException e) {
  if (e.code.contains("CAPTCHA_EXPIRED") || e.message.contains("验证码已过期")) {
    return "密码/验证码已失效：请让对方在被控端窗口重新查看密码后再试（9 位设备码不用换）";
  }
  if (e.code.contains("CAPTCHA") || e.message.contains("验证码不正确")) {
    return "密码不正确：请核对被控端窗口显示的 8 位数字（可点「显示」查看明文）";
  }
  if (e.code.contains("DEVICE")) {
    return "设备码不存在或对方未开启被控：请确认 9 位设备码，并让对方在该设备上允许被控";
  }
  return e.message;
}
