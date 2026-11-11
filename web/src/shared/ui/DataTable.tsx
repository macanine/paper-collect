import type { Key, MouseEvent, ReactNode } from 'react';
import { Box, Checkbox, Group, Loader, Pagination, Paper, Select, Stack, Table, Text } from '@mantine/core';

export interface Column<T> {
  key: string;
  title?: ReactNode;
  render: (row: T, index: number) => ReactNode;
  width?: number | string;
  align?: 'left' | 'center' | 'right';
  /** 自由文本列允许换行；默认单行，避免短内容被挤压成竖排。 */
  wrap?: boolean;
  /** 窄屏隐藏（仅保留主要列）。 */
  hideOnCompact?: boolean;
}

export interface TablePagination {
  page: number;
  pageSize: number;
  total: number;
  onChange: (page: number, pageSize: number) => void;
  pageSizes?: number[];
}

interface DataTableProps<T> {
  rows: readonly T[];
  columns: Column<T>[];
  rowKey: (row: T) => Key;
  loading?: boolean;
  empty?: ReactNode;
  pagination?: TablePagination;
  selected?: Key[];
  onSelectedChange?: (keys: Key[]) => void;
  onRowClick?: (row: T, event: MouseEvent<HTMLElement>) => void;
  minWidth?: number;
  /** 不带外框，用于嵌入卡片。 */
  bare?: boolean;
}

const INTERACTIVE = 'a,button,input,select,textarea,label,[role="button"],[role="checkbox"],[role="switch"]';

/** 全站统一的数据表：列、选择、分页都在这里对齐，页面只声明列。 */
export function DataTable<T>({
  rows, columns, rowKey, loading, empty, pagination, selected, onSelectedChange, onRowClick, minWidth = 720, bare,
}: DataTableProps<T>) {
  const selectable = !!onSelectedChange;
  const keys = rows.map(rowKey);
  const chosen = new Set(selected ?? []);
  const allChecked = keys.length > 0 && keys.every((k) => chosen.has(k));
  const someChecked = keys.some((k) => chosen.has(k));

  const toggleAll = () => onSelectedChange?.(allChecked ? (selected ?? []).filter((k) => !keys.includes(k)) : Array.from(new Set([...(selected ?? []), ...keys])));
  const toggleOne = (k: Key) => onSelectedChange?.(chosen.has(k) ? (selected ?? []).filter((x) => x !== k) : [...(selected ?? []), k]);

  const body = (
    <Box pos="relative">
      <Table.ScrollContainer minWidth={minWidth} type="native">
        <Table stickyHeader verticalSpacing="sm" horizontalSpacing="md" highlightOnHover={!!onRowClick}>
          <Table.Thead bg="gray.0">
            <Table.Tr>
              {selectable ? (
                <Table.Th w={44}>
                  <Checkbox aria-label="全选" checked={allChecked} indeterminate={!allChecked && someChecked} onChange={toggleAll} />
                </Table.Th>
              ) : null}
              {columns.map((c) => (
                <Table.Th key={c.key} w={c.width} ta={c.align} style={{ whiteSpace: 'nowrap' }} visibleFrom={c.hideOnCompact ? 'sm' : undefined}>{c.title}</Table.Th>
              ))}
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((row, index) => {
              const k = rowKey(row);
              return (
                <Table.Tr
                  key={k}
                  bg={chosen.has(k) ? 'blue.0' : undefined}
                  style={{ cursor: onRowClick ? 'pointer' : undefined }}
                  onClick={onRowClick ? (e) => { if (!(e.target as Element).closest(INTERACTIVE)) onRowClick(row, e); } : undefined}
                >
                  {selectable ? (
                    <Table.Td w={44}>
                      <Checkbox aria-label="选择此行" checked={chosen.has(k)} onChange={() => toggleOne(k)} />
                    </Table.Td>
                  ) : null}
                  {columns.map((c) => (
                    <Table.Td key={c.key} ta={c.align} style={{ verticalAlign: 'middle', whiteSpace: c.wrap ? 'normal' : 'nowrap', minWidth: c.wrap ? 200 : undefined }} visibleFrom={c.hideOnCompact ? 'sm' : undefined}>{c.render(row, index)}</Table.Td>
                  ))}
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      {!loading && rows.length === 0 ? (
        <Stack align="center" py={48} gap="xs">{empty ?? <Text c="dimmed" size="sm">暂无数据</Text>}</Stack>
      ) : null}
      {loading ? (
        <Group justify="center" py="md" pos={rows.length ? 'absolute' : 'static'} inset={0} bg={rows.length ? 'rgba(255,255,255,.6)' : undefined} style={{ zIndex: 2 }}>
          <Loader size="sm" />
        </Group>
      ) : null}
    </Box>
  );

  const pages = pagination ? Math.max(1, Math.ceil(pagination.total / pagination.pageSize)) : 0;
  const footer = pagination && pagination.total > 0 ? (
    <Group justify="space-between" p="sm" style={{ borderTop: '1px solid var(--mantine-color-gray-2)' }} wrap="wrap">
      <Text size="sm" c="dimmed">共 {pagination.total} 条</Text>
      <Group gap="sm">
        <Select
          w={110}
          size="xs"
          aria-label="每页条数"
          data={(pagination.pageSizes ?? [10, 20, 50, 100]).map((n) => ({ value: String(n), label: `${n} 条/页` }))}
          value={String(pagination.pageSize)}
          onChange={(v) => v && pagination.onChange(1, Number(v))}
        />
        <Pagination size="sm" total={pages} value={pagination.page} onChange={(p) => pagination.onChange(p, pagination.pageSize)} />
      </Group>
    </Group>
  ) : null;

  if (bare) return <>{body}{footer}</>;
  return <Paper style={{ overflow: 'hidden' }}>{body}{footer}</Paper>;
}
