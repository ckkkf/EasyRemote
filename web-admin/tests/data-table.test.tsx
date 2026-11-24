// ADM-UI-05：表格只有一份实现，列定义/行渲染/空态/可点击行都由 DataTable 统一。
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { DataTable } from '../src/components/business/data-table';

type Row = { id: string; name: string };

const columns = [
  { key: 'id', header: 'ID', className: 'mono', render: (r: Row) => r.id },
  { key: 'name', header: '名称', render: (r: Row) => r.name },
];

describe('data table', () => {
  it('TestDataTableRendersColumnsAndRows', () => {
    const html = renderToStaticMarkup(
      <DataTable ariaLabel="测试表" rows={[{ id: 'a1', name: '甲' }, { id: 'a2', name: '乙' }]} rowKey={(r) => r.id} columns={columns} />,
    );
    // 断言语义而不是类名：视觉层换成 shadcn Table 后类名会变，但结构与可访问性不能变。
    expect(html).toContain('aria-label="测试表"');
    expect(html).toContain('data-testid="data-table"');
    expect(html).toMatch(/<th[^>]*>ID<\/th>/);
    expect(html).toContain('甲');
    expect(html).toContain('乙');
    expect((html.match(/<tr/g) || []).length).toBe(3); // 表头 + 2 行
    expect(html).not.toContain('cursor-pointer');
  });

  it('TestDataTableEmptyState', () => {
    const html = renderToStaticMarkup(
      <DataTable rows={[]} rowKey={(r: Row) => r.id} columns={columns} empty={<p>没有数据</p>} />,
    );
    expect(html).toBe('<p>没有数据</p>');
  });

  it('TestDataTableClickableRows', () => {
    const html = renderToStaticMarkup(
      <DataTable rows={[{ id: 'a1', name: '甲' }]} rowKey={(r) => r.id} columns={columns} onRowClick={() => {}} rowTitle="打开详情" />,
    );
    expect(html).toContain('cursor-pointer');
    expect(html).toContain('title="打开详情"');
  });
});
