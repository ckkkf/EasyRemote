// ADM-UI-01a：仓库内 shadcn primitive 的行为断言。
// 用 renderToStaticMarkup 直接渲染成 HTML 字符串，不依赖 jsdom。
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';

import { Button } from '../src/components/ui/button';

describe('ui primitives', () => {
  it('TestButtonPrimitiveRenders', () => {
    // outline 变体必须来自 cva 变体表，而不是调用方随手拼的类名。
    const outline = renderToStaticMarkup(<Button variant="outline">取消</Button>);
    expect(outline).toContain('border-input');
    expect(outline).toContain('bg-background');

    // disabled 透传到原生属性，样式侧由 disabled: 变体负责。
    const disabled = renderToStaticMarkup(<Button disabled>登录中…</Button>);
    expect(disabled).toContain('disabled');
    expect(disabled).toContain('disabled:pointer-events-none');

    // 默认 type="button"：避免在非表单场景意外提交。
    const plain = renderToStaticMarkup(<Button>登录</Button>);
    expect(plain).toContain('type="button"');

    // asChild 走 Radix Slot：不渲染 <button>，把样式与行为合并到子元素。
    const asChild = renderToStaticMarkup(
      <Button asChild>
        <a href="#docs">文档</a>
      </Button>,
    );
    expect(asChild).not.toContain('<button');
    expect(asChild).toContain('<a href="#docs"');
    expect(asChild).toContain('inline-flex');
  });
});
