import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
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

/// 连接方式：设备码（首次连接要验证码）/ 协助码（被控端临时授权，免验证码）。
enum _ConnectMode { device, assist }

class _ConnectPageState extends State<ConnectPage> {
  final _codeCtl = TextEditingController();
  final _codeFocus = FocusNode();
  final _captcha = TextEditingController();
  final _assistCtl = TextEditingController();
  final _assistFocus = FocusNode();
  _ConnectMode _mode = _ConnectMode.device;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _applyCode(widget.initialCode);
  }

  @override
  void didUpdateWidget(covariant ConnectPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.initialCode != oldWidget.initialCode) _applyCode(widget.initialCode);
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
    _codeCtl.dispose();
    _codeFocus.dispose();
    _captcha.dispose();
    _assistCtl.dispose();
    _assistFocus.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final code = _code;
      final sid = await widget.client.requestConnect(code, captcha: _captcha.text.trim());
      widget.onCodeUsed(code);
      widget.onSessionStarted(sid, code.replaceAll(RegExp(r'[^0-9]'), ''));
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// 协助码兑换：协助码本身就是被控端的授权意图，服务端不做验证码校验。
  Future<void> _redeemAssist() async {
    final code = _assistCtl.text.trim();
    if (code.length != 6) {
      setState(() => _error = '协助码是 6 位数字');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final sid = await widget.client.redeemAssistCode(code);
      _assistCtl.clear();
      widget.onSessionStarted(sid, '');
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;
    final ready = _code.length == 9;
    final assistReady = _assistCtl.text.trim().length == 6;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const PageHeader(title: '连接设备', subtitle: '用设备码请求连接，或用对方给的协助码临时接入'),
        Expanded(
          child: LayoutBuilder(
            builder: (context, constraints) => SingleChildScrollView(
              padding: EdgeInsets.fromLTRB(wide ? 32 : 16, 0, wide ? 32 : 16, 32),
              child: ConstrainedBox(
                constraints: BoxConstraints(minHeight: constraints.maxHeight - 8),
                child: Center(
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 560),
                    child: Container(
                      padding: EdgeInsets.all(wide ? 28 : 20),
                      decoration: BoxDecoration(
                        color: scheme.surfaceContainerLow,
                        borderRadius: BorderRadius.circular(ErTokens.radiusXl),
                        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
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
                                    color: scheme.primaryContainer, borderRadius: BorderRadius.circular(11)),
                                child: Icon(Icons.pin_rounded, size: 21, color: scheme.onPrimaryContainer),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(_mode == _ConnectMode.device ? '输入设备码' : '输入协助码',
                                        style: text.titleLarge),
                                    Text(
                                      _mode == _ConnectMode.device
                                          ? '9 位数字，输满后即可请求连接'
                                          : '6 位数字，一次一码，10 分钟内有效',
                                      style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
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
                            }),
                          ),
                          const SizedBox(height: 18),
                          if (_mode == _ConnectMode.device) ...[
                          _CodeEntry(
                            controller: _codeCtl,
                            focusNode: _codeFocus,
                            onChanged: () => setState(() {}),
                          ),
                          const SizedBox(height: 14),
                          TextField(
                            key: const Key('captcha-field'),
                            controller: _captcha,
                            keyboardType: TextInputType.number,
                            decoration: const InputDecoration(
                              labelText: '验证码（首次连接必填）',
                              hintText: '对方屏幕上显示的数字',
                              prefixIcon: Icon(Icons.verified_user_outlined, size: 18),
                            ),
                          ),
                          ] else ...[
                            TextField(
                              key: const Key('assist-code-field'),
                              controller: _assistCtl,
                              focusNode: _assistFocus,
                              keyboardType: TextInputType.number,
                              maxLength: 6,
                              onChanged: (_) => setState(() => _error = null),
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
                            Text('协助码 10 分钟内有效、只能用一次；兑换成功即建立会话，无需验证码。',
                                style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
                          ],
                          if (_error != null) ...[
                            const SizedBox(height: 12),
                            Container(
                              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                              decoration: BoxDecoration(
                                color: scheme.errorContainer,
                                borderRadius: BorderRadius.circular(ErTokens.radiusMd),
                              ),
                              child: Row(
                                children: [
                                  Icon(Icons.error_outline_rounded, size: 16, color: scheme.onErrorContainer),
                                  const SizedBox(width: 8),
                                  Expanded(
                                    child: Text(_error!,
                                        style: text.bodySmall?.copyWith(color: scheme.onErrorContainer)),
                                  ),
                                ],
                              ),
                            ),
                          ],
                          const SizedBox(height: 18),
                          FilledButton(
                            key: const Key('connect-button'),
                            onPressed: _busy || (_mode == _ConnectMode.device ? !ready : !assistReady)
                                ? null
                                : (_mode == _ConnectMode.device ? _connect : _redeemAssist),
                            child: _busy
                                ? const SizedBox(height: 18, width: 18, child: CircularProgressIndicator(strokeWidth: 2))
                                : Text(_mode == _ConnectMode.device ? '请求连接' : '兑换协助码'),
                          ),
                          const SizedBox(height: 10),
                          Text(
                            _mode == _ConnectMode.device
                                ? '首次连接需要对方在弹出的授权框里同意；建立信任后可直接连接。'
                                : '协助码是被控端给出的临时授权，兑换即建立请求，不需对方再点同意。',
                            style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
                            textAlign: TextAlign.center,
                          ),
                          if (_mode == _ConnectMode.device && widget.recents.isNotEmpty) ...[
                            const SizedBox(height: 20),
                            Text('最近连接', style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
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
          key: Key(value == _ConnectMode.device ? 'mode-device' : 'mode-assist'),
          onTap: () => onChanged(value),
          borderRadius: BorderRadius.circular(ErTokens.radiusMd),
          child: AnimatedContainer(
            duration: const Duration(milliseconds: 120),
            height: 40,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: on ? scheme.primaryContainer : Colors.transparent,
              borderRadius: BorderRadius.circular(ErTokens.radiusMd),
              border: Border.all(color: on ? scheme.primary.withValues(alpha: 0.5) : Colors.transparent),
            ),
            child: Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(icon, size: 16, color: on ? scheme.onPrimaryContainer : scheme.onSurfaceVariant),
                const SizedBox(width: 6),
                Text(
                  label,
                  style: text.labelMedium?.copyWith(
                    color: on ? scheme.onPrimaryContainer : scheme.onSurfaceVariant,
                    fontWeight: on ? FontWeight.w600 : FontWeight.w500,
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

class _CodeEntry extends StatelessWidget {
  const _CodeEntry({required this.controller, required this.focusNode, required this.onChanged});

  final TextEditingController controller;
  final FocusNode focusNode;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final value = controller.text;

    Widget box(int i) {
      final filled = i < value.length;
      final active = focusNode.hasFocus && i == value.length.clamp(0, 8);
      return AnimatedContainer(
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
        child: Text(
          filled ? value[i] : '',
          style: text.headlineSmall?.copyWith(fontFeatures: const [FontFeature.tabularFigures()]),
        ),
      );
    }

    Widget group(int start) => Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (var i = start; i < start + 3; i++) ...[
              box(i),
              if (i != start + 2) const SizedBox(width: 6),
            ],
          ],
        );

    return Stack(
      alignment: Alignment.center,
      children: [
        FittedBox(
          fit: BoxFit.scaleDown,
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [group(0), const SizedBox(width: 14), group(3), const SizedBox(width: 14), group(6)],
          ),
        ),
        // 透明输入层：承接键盘、粘贴与退格，视觉完全由上面的格子表达。
        Positioned.fill(
          child: TextField(
            key: const Key('device-code-field'),
            controller: controller,
            focusNode: focusNode,
            autofocus: false,
            showCursor: false,
            enableInteractiveSelection: false,
            keyboardType: TextInputType.number,
            maxLength: 9,
            onChanged: (_) => onChanged(),
            inputFormatters: [FilteringTextInputFormatter.digitsOnly, LengthLimitingTextInputFormatter(9)],
            style: const TextStyle(color: Colors.transparent, fontSize: 1),
            cursorColor: Colors.transparent,
            decoration: const InputDecoration(
              counterText: '',
              border: InputBorder.none,
              enabledBorder: InputBorder.none,
              focusedBorder: InputBorder.none,
              filled: false,
              isDense: true,
              contentPadding: EdgeInsets.zero,
            ),
          ),
        ),
      ],
    );
  }
}
