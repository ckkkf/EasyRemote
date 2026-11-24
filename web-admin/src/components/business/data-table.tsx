// 业务表格的唯一实现（PRD §6.7.1 第 4 条：统一 Data Table，禁止每页各写一套）。
// 只负责列定义与行渲染；分页、筛选、请求仍由页面负责，服务端分页参数不改。
//
// 视觉层用仓库内 shadcn/ui Table（components/ui/table.tsx），不再维护 .tbl 那套手写样式
// （ADM-UI-11a）：表头/行高/边框/悬停都走设计令牌，避免"每页一张表、样式各不同"。
import type { ReactNode } from 'react';

import { cn } from '../../lib/utils';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../ui/table';

export type DataTableColumn<T> = {
  key: string;
  header: string;
  className?: string;
  render: (row: T) => ReactNode;
};

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  onRowClick,
  rowTitle,
  empty,
  ariaLabel,
}: {
  columns: DataTableColumn<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  onRowClick?: (row: T) => void;
  rowTitle?: string;
  empty?: ReactNode;
  ariaLabel?: string;
}) {
  if (rows.length === 0 && empty) return <>{empty}</>;
  return (
    <Table aria-label={ariaLabel} data-testid="data-table">
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          {columns.map((col) => (
            <TableHead key={col.key} className={cn('whitespace-nowrap', col.className)}>
              {col.header}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => (
          <TableRow
            key={rowKey(row)}
            className={onRowClick ? 'cursor-pointer' : undefined}
            onClick={onRowClick ? () => onRowClick(row) : undefined}
            title={onRowClick ? rowTitle : undefined}
          >
            {columns.map((col) => (
              <TableCell key={col.key} className={cn('text-[13px]', col.className)}>
                {col.render(row)}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
