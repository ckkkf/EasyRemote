// ADM-UI-03：登录页用仓库内 shadcn primitives，错误信封完整可见，且不泄露开发口令。
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { Login, LoginError } from '../src/App';
import { ApiError } from '../src/api';

const html = renderToStaticMarkup(<Login onDone={() => {}} />);

describe('admin login', () => {
  it('TestLoginRendersShadcnForm', () => {
    // 三个输入都有 id，且 label 通过 htmlFor 绑定（点击标签能聚焦控件）。
    for (const id of ['login-username', 'login-password', 'login-totp']) {
      expect(html).toContain('id="' + id + '"');
      expect(html).toContain('for="' + id + '"');
    }
    expect(html).toContain('type="password"');
    // SSR 会保留 React 的 camelCase 属性名，HTML 属性大小写不敏感；这里按不敏感比对。
    expect(html.toLowerCase()).toContain('autocomplete="one-time-code"');
    // 提交按钮来自 shadcn Button：默认 type 由组件给出，这里是 submit + 变体类。
    expect(html).toContain('type="submit"');
    expect(html).toContain('bg-primary');
    // 控件样式来自 primitives，不再有旧的自定义 .login-form 表单类（布局外壳 login-form-wrap 保留）。
    expect(html).not.toContain('class="login-form"');
  });

  it('TestLoginHasNoDevCredentials', () => {
    // 生产 UI 不展示开发账号、开发口令或关闭 MFA 的做法。
    expect(html).not.toContain('dev-pass');
    expect(html).not.toContain('ER_ADMIN_TOTP_OFF');
    expect(html).not.toContain('开发环境');
    expect(html).toContain('二次验证强制开启');
  });

  it('TestLoginErrorShowsEnvelope', () => {
    const err = new ApiError({
      code: 100001,
      name: 'AUTH_INVALID_CREDENTIAL',
      message: '账号或密码错误',
      retryable: false,
      trace_id: 'trace-login-1',
    });
    const markup = renderToStaticMarkup(<LoginError error={err} />);
    expect(markup).toContain('role="alert"');
    expect(markup).toContain('账号或密码错误');
    expect(markup).toContain('AUTH_INVALID_CREDENTIAL');
    expect(markup).toContain('100001');
    // trace_id 可复制：按钮存在，不把 trace 丢掉只留 message。
    expect(markup).toContain('复制 trace_id');
  });
});
