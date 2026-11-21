import 'package:flutter/material.dart';

import '../api.dart';
import '../models.dart';
import '../shell.dart';
import '../theme.dart';

/// 登录页（原型 s01）：左侧品牌面板，右侧单表单。
///
/// 版式按 prototype/index.html#s01 回正：
///   手机号 → 短信验证码（右侧「获取验证码」）→ 登录 / 注册 → 协议勾选
///   →「或」→「微信登录 V1」（后端未提供，禁用）/「邮箱登录」（可点，弹窗内走真实接口）。
/// 之前把手机号 / 邮箱做成两个大 Tab 是自己的发挥，会挤掉原型版式，这里不再保留。
class LoginPage extends StatefulWidget {
  const LoginPage({super.key, required this.client});

  final GatewayClient client;

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _phone = TextEditingController();
  final _code = TextEditingController();
  bool _busy = false;
  bool _agreed = true;
  String? _error;
  String? _notice;

  @override
  void dispose() {
    _phone.dispose();
    _code.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
      _notice = null;
    });
    try {
      await action();
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _sendCode() async {
    await _run(() async {
      await widget.client.sendSms(_phone.text.trim());
      setState(() => _notice = '验证码已发送（开发环境固定 123456）');
    });
  }

  /// 登录成功后的公共收尾：绑定本机设备，进主框架。
  Future<void> _enter() async {
    final device = await widget.client.bindDevice(currentPlatformName());
    if (!mounted) return;
    Navigator.of(context).pushReplacement(
      MaterialPageRoute<void>(builder: (_) => HomeShell(client: widget.client, device: device)),
    );
  }

  Future<void> _loginPhone() async {
    if (!_agreed) {
      setState(() => _error = '请先阅读并同意用户协议与隐私政策');
      return;
    }
    await _run(() async {
      await widget.client.login(_phone.text.trim(), _code.text.trim());
      await _enter();
    });
  }

  Future<void> _loginEmail() async {
    if (!_agreed) {
      setState(() => _error = '请先阅读并同意用户协议与隐私政策');
      return;
    }
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => _EmailLoginDialog(client: widget.client),
    );
    if (ok != true || !mounted) return;
    // 弹窗里已经换到令牌，这里只做绑定设备与跳转。
    await _run(_enter);
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;

    final form = _LoginForm(
      phone: _phone,
      code: _code,
      busy: _busy,
      agreed: _agreed,
      error: _error,
      notice: _notice,
      onAgree: (v) => setState(() => _agreed = v),
      onSendCode: _busy ? null : _sendCode,
      onSubmit: _busy ? null : _loginPhone,
      onEmailLogin: _busy ? null : _loginEmail,
    );

    return Scaffold(
      body: Stack(
        children: [
          if (wide)
            Row(
              children: [
                const Expanded(flex: 44, child: _BrandPanel()),
                Expanded(
                  flex: 56,
                  child: ColoredBox(
                    color: scheme.surface,
                    child: Center(
                      child: SingleChildScrollView(
                        padding: const EdgeInsets.symmetric(horizontal: 40, vertical: 32),
                        child: ConstrainedBox(constraints: const BoxConstraints(maxWidth: 352), child: form),
                      ),
                    ),
                  ),
                ),
              ],
            )
          else
            SingleChildScrollView(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const _BrandBanner(),
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 24, 20, 32),
                    child: form,
                  ),
                ],
              ),
            ),
          Positioned(
            top: 12,
            right: 16,
            child: Container(
              decoration: BoxDecoration(
                color: wide ? Colors.transparent : scheme.surfaceContainerHigh.withValues(alpha: 0.7),
                borderRadius: BorderRadius.circular(ErTokens.radiusSm),
              ),
              child: const ThemeModeButton(),
            ),
          ),
        ],
      ),
    );
  }
}

class _LoginForm extends StatelessWidget {
  const _LoginForm({
    required this.phone,
    required this.code,
    required this.busy,
    required this.agreed,
    required this.error,
    required this.notice,
    required this.onAgree,
    required this.onSendCode,
    required this.onSubmit,
    required this.onEmailLogin,
  });

  final TextEditingController phone;
  final TextEditingController code;
  final bool busy;
  final bool agreed;
  final String? error;
  final String? notice;
  final ValueChanged<bool> onAgree;
  final VoidCallback? onSendCode;
  final VoidCallback? onSubmit;
  final VoidCallback? onEmailLogin;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('登录', style: text.headlineMedium),
        const SizedBox(height: 8),
        Text(
          '登录后可查看设备列表，同账号设备免验证码直连。',
          style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
        ),
        const SizedBox(height: 24),
        _FieldLabel('手机号'),
        const SizedBox(height: 7),
        TextField(
          key: const Key('phone-field'),
          controller: phone,
          keyboardType: TextInputType.phone,
          style: const TextStyle(fontSize: 14, letterSpacing: 0.4),
          decoration: const InputDecoration(
            hintText: '138 0000 8882',
            prefixIcon: Padding(
              padding: EdgeInsets.only(left: 14, right: 10),
              child: Text('+86', style: TextStyle(fontSize: 13, color: Color(0xFF777C87))),
            ),
            prefixIconConstraints: BoxConstraints(minWidth: 0),
          ),
        ),
        const SizedBox(height: 16),
        _FieldLabel('短信验证码'),
        const SizedBox(height: 7),
        TextField(
          key: const Key('code-field'),
          controller: code,
          keyboardType: TextInputType.number,
          style: const TextStyle(fontSize: 14, letterSpacing: 2),
          decoration: InputDecoration(
            hintText: '6 位验证码',
            suffixIcon: TextButton(
              key: const Key('send-code'),
              onPressed: onSendCode,
              child: const Text('获取验证码'),
            ),
          ),
        ),
        if (notice != null) ...[
          const SizedBox(height: 10),
          Text(notice!, style: text.labelSmall?.copyWith(color: scheme.primary)),
        ],
        if (error != null) ...[
          const SizedBox(height: 10),
          Text(error!, style: text.bodySmall?.copyWith(color: scheme.error)),
        ],
        const SizedBox(height: 18),
        SizedBox(
          height: 46,
          child: FilledButton(
            key: const Key('login-button'),
            onPressed: onSubmit,
            child: busy
                ? const SizedBox(height: 18, width: 18, child: CircularProgressIndicator(strokeWidth: 2))
                : const Text('登录 / 注册'),
          ),
        ),
        const SizedBox(height: 16),
        _AgreementRow(agreed: agreed, onAgree: onAgree),
        const SizedBox(height: 26),
        Row(
          children: [
            Expanded(child: Divider(color: scheme.outlineVariant.withValues(alpha: 0.8))),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Text('或', style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
            ),
            Expanded(child: Divider(color: scheme.outlineVariant.withValues(alpha: 0.8))),
          ],
        ),
        const SizedBox(height: 16),
        Row(
          children: [
            Expanded(
              child: _ProviderButton(
                key: const Key('wechat-provider'),
                icon: Icons.chat_bubble_outline_rounded,
                label: '微信登录',
                enabled: false,
                onTap: null,
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: _ProviderButton(
                key: const Key('email-provider'),
                icon: Icons.alternate_email_rounded,
                label: '邮箱登录',
                // 原型画的是 V1 占位；后端 /v1/auth/email/* 已经可用，所以这里真的能点。
                enabled: onEmailLogin != null,
                onTap: onEmailLogin,
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _FieldLabel extends StatelessWidget {
  const _FieldLabel(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Text(
      text,
      style: Theme.of(context).textTheme.labelMedium?.copyWith(
            color: Theme.of(context).colorScheme.onSurfaceVariant,
            fontWeight: FontWeight.w600,
          ),
    );
  }
}

class _AgreementRow extends StatelessWidget {
  const _AgreementRow({required this.agreed, required this.onAgree});

  final bool agreed;
  final ValueChanged<bool> onAgree;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return InkWell(
      key: const Key('agree-row'),
      onTap: () => onAgree(!agreed),
      borderRadius: BorderRadius.circular(ErTokens.radiusSm),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 2),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              width: 15,
              height: 15,
              margin: const EdgeInsets.only(top: 2),
              decoration: BoxDecoration(
                color: agreed ? scheme.primary : Colors.transparent,
                borderRadius: BorderRadius.circular(4),
                border: Border.all(color: agreed ? scheme.primary : scheme.outline),
              ),
              child: agreed ? Icon(Icons.check_rounded, size: 11, color: scheme.onPrimary) : null,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text.rich(
                TextSpan(
                  style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant, height: 1.65),
                  children: [
                    const TextSpan(text: '我已阅读并同意 '),
                    TextSpan(
                      text: '用户协议',
                      style: TextStyle(color: scheme.primary, fontWeight: FontWeight.w600),
                    ),
                    const TextSpan(text: ' 与 '),
                    TextSpan(
                      text: '隐私政策',
                      style: TextStyle(color: scheme.primary, fontWeight: FontWeight.w600),
                    ),
                    const TextSpan(text: '，未注册的手机号将自动创建账号。'),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 第三方登录入口：原型里的两个 40px 方框按钮。
/// 禁用态（微信）只展示不响应；可用态（邮箱）点开弹窗。
class _ProviderButton extends StatelessWidget {
  const _ProviderButton({
    super.key,
    required this.icon,
    required this.label,
    required this.enabled,
    required this.onTap,
  });

  final IconData icon;
  final String label;
  final bool enabled;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final fg = enabled ? scheme.onSurface : scheme.onSurfaceVariant;

    final content = Container(
      height: 44,
      decoration: BoxDecoration(
        color: enabled ? scheme.surfaceContainerLowest : scheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: scheme.outlineVariant),
      ),
      alignment: Alignment.center,
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(icon, size: 16, color: fg),
          const SizedBox(width: 6),
          Text(label, style: text.labelMedium?.copyWith(color: fg)),
          const SizedBox(width: 5),
          Text(
            'V1',
            style: text.labelSmall?.copyWith(
              color: scheme.onSurfaceVariant.withValues(alpha: enabled ? 0.9 : 0.6),
              letterSpacing: 0,
            ),
          ),
        ],
      ),
    );

    if (!enabled) return content;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(10),
      child: content,
    );
  }
}

/// 邮箱登录弹窗：两个真实接口（/v1/auth/email/code、/v1/auth/email/login）。
///
/// 成功后令牌已经写进 [GatewayClient]，返回 true；父页面负责绑设备与跳转。
class _EmailLoginDialog extends StatefulWidget {
  const _EmailLoginDialog({required this.client});

  final GatewayClient client;

  @override
  State<_EmailLoginDialog> createState() => _EmailLoginDialogState();
}

class _EmailLoginDialogState extends State<_EmailLoginDialog> {
  final _email = TextEditingController();
  final _code = TextEditingController();
  bool _busy = false;
  String? _error;
  String? _notice;

  @override
  void dispose() {
    _email.dispose();
    _code.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    setState(() {
      _busy = true;
      _error = null;
      _notice = null;
    });
    try {
      await widget.client.sendEmailCode(_email.text.trim());
      if (!mounted) return;
      setState(() => _notice = '邮件验证码已发送（开发环境固定 654321）');
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _error = e.message);
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _submit() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.client.loginEmail(_email.text.trim(), _code.text.trim());
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.message;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return AlertDialog(
      title: const Text('邮箱登录'),
      content: SizedBox(
        width: 380,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              '未注册的邮箱登录时自动创建账号，与手机号是同一套账号体系。',
              style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
            const SizedBox(height: 16),
            TextField(
              key: const Key('email-field'),
              controller: _email,
              keyboardType: TextInputType.emailAddress,
              decoration: const InputDecoration(
                labelText: '邮箱',
                prefixIcon: Icon(Icons.alternate_email_rounded, size: 18),
              ),
            ),
            const SizedBox(height: 14),
            TextField(
              key: const Key('email-code-field'),
              controller: _code,
              keyboardType: TextInputType.number,
              decoration: InputDecoration(
                labelText: '邮件验证码',
                prefixIcon: const Icon(Icons.sms_outlined, size: 18),
                suffixIcon: TextButton(
                  key: const Key('send-email-code'),
                  onPressed: _busy ? null : _send,
                  child: const Text('发送验证码'),
                ),
              ),
            ),
            if (_notice != null) ...[
              const SizedBox(height: 10),
              Text(_notice!, style: text.labelSmall?.copyWith(color: scheme.primary)),
            ],
            if (_error != null) ...[
              const SizedBox(height: 10),
              Text(_error!, style: text.bodySmall?.copyWith(color: scheme.error)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(false), child: const Text('取消')),
        FilledButton(
          key: const Key('email-login-button'),
          onPressed: _busy ? null : _submit,
          child: _busy
              ? const SizedBox(height: 16, width: 16, child: CircularProgressIndicator(strokeWidth: 2))
              : const Text('登录'),
        ),
      ],
    );
  }
}

/// 左侧品牌面板：渐变底 + 大标题 + 三条卖点（对齐原型 s01 的 44% 栏）。
class _BrandPanel extends StatelessWidget {
  const _BrandPanel();

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFF3E6BEE), Color(0xFF1E4BD8), Color(0xFF152F9E)],
        ),
      ),
      padding: const EdgeInsets.fromLTRB(48, 52, 48, 44),
      child: Stack(
        children: [
          Positioned(
            top: -190,
            right: -190,
            child: Container(
              width: 520,
              height: 520,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                gradient: RadialGradient(colors: [Colors.white.withValues(alpha: 0.17), Colors.white.withValues(alpha: 0)]),
              ),
            ),
          ),
          Positioned(
            bottom: -200,
            left: -150,
            child: Container(
              width: 420,
              height: 420,
              decoration: const BoxDecoration(
                shape: BoxShape.circle,
                gradient: RadialGradient(colors: [Color(0x3D82AFFF), Color(0x0082AFFF)]),
              ),
            ),
          ),
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 42,
                    height: 42,
                    decoration: BoxDecoration(
                      color: Colors.white.withValues(alpha: 0.16),
                      borderRadius: BorderRadius.circular(12),
                      border: Border.all(color: Colors.white.withValues(alpha: 0.26)),
                    ),
                    alignment: Alignment.center,
                    child: const Text(
                      'ER',
                      style: TextStyle(color: Colors.white, fontSize: 13, fontWeight: FontWeight.w700, letterSpacing: 0.5),
                    ),
                  ),
                  const SizedBox(width: 12),
                  const Text(
                    'EasyRemote',
                    style: TextStyle(color: Colors.white, fontSize: 17, fontWeight: FontWeight.w600, letterSpacing: -0.2),
                  ),
                ],
              ),
              const Spacer(),
              const Text(
                '把任何一台电脑\n装进口袋',
                style: TextStyle(color: Colors.white, fontSize: 34, height: 1.22, fontWeight: FontWeight.w600, letterSpacing: -1),
              ),
              const SizedBox(height: 16),
              Text(
                '毫秒级响应、真彩还原、多端互连。\n核心功能不限时长、不限次数。',
                style: TextStyle(color: Colors.white.withValues(alpha: 0.74), fontSize: 13.5, height: 1.75),
              ),
              const Spacer(),
              for (final item in const [
                ('01', 'P2P 直连优先', '打洞成功即点对点传输，画面不经过服务器'),
                ('02', '网络诊断', '卡的时候告诉你为什么卡、该改什么'),
                ('03', '端到端加密', '中继节点只转发密文，架构上无法解密'),
              ]) ...[
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: 26,
                      height: 26,
                      decoration: BoxDecoration(
                        color: Colors.white.withValues(alpha: 0.15),
                        borderRadius: BorderRadius.circular(8),
                      ),
                      alignment: Alignment.center,
                      child: Text(
                        item.$1,
                        style: const TextStyle(color: Colors.white, fontSize: 11, fontWeight: FontWeight.w700),
                      ),
                    ),
                    const SizedBox(width: 11),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            item.$2,
                            style: const TextStyle(color: Colors.white, fontSize: 12.5, fontWeight: FontWeight.w600),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            item.$3,
                            style: TextStyle(color: Colors.white.withValues(alpha: 0.62), fontSize: 11.5, height: 1.5),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 16),
              ],
              const SizedBox(height: 10),
              Text(
                'v1.0.0 · Windows 10 及以上 / macOS 12 及以上',
                style: TextStyle(color: Colors.white.withValues(alpha: 0.44), fontSize: 10.5, letterSpacing: 0.2),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// 窄屏顶部品牌条（原型只有宽屏版式；小屏保留信息但不占满屏）。
class _BrandBanner extends StatelessWidget {
  const _BrandBanner();

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.fromLTRB(20, 28, 20, 24),
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFF3E6BEE), Color(0xFF1E4BD8)],
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(
              color: Colors.white.withValues(alpha: 0.16),
              borderRadius: BorderRadius.circular(11),
            ),
            alignment: Alignment.center,
            child: const Text('ER', style: TextStyle(color: Colors.white, fontSize: 12, fontWeight: FontWeight.w700)),
          ),
          const SizedBox(height: 12),
          const Text(
            'EasyRemote',
            style: TextStyle(color: Colors.white, fontSize: 26, fontWeight: FontWeight.w600, letterSpacing: -0.4),
          ),
          const SizedBox(height: 6),
          Text(
            '连得稳，看得清。弱网也能用的远程桌面。',
            style: TextStyle(color: Colors.white.withValues(alpha: 0.85), fontSize: 13),
          ),
        ],
      ),
    );
  }
}


