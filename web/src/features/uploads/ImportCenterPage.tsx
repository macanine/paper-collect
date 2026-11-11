import { Anchor, Group, Stack, Button } from '@mantine/core';
import { IconCloudUpload } from '@tabler/icons-react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import type { Upload } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { Tag, UploadStatusTag } from '../../shared/ui/StatusTags';
import { listUploads } from './api';
import { useUploads } from './UploadManager';
import { href } from '../../app/routes';
import { qk } from '../../app/queryKeys';

const COLUMNS: Column<Upload>[] = [
  { key: 'title', title: '导入批次', wrap: true, render: (u) => <Anchor component={Link} to={href.upload(u.id)} fw={600} c="inherit">{u.title}</Anchor> },
  { key: 'status', title: '状态', width: 110, render: (u) => <UploadStatusTag status={u.status} /> },
  {
    key: 'result', title: '结果',
    render: (u) => u.status === 'completed' ? (
      <Group gap={4}>
        <Tag color="blue">新增 {u.new_count}</Tag>
        <Tag>已存在 {u.duplicate_count}</Tag>
        {u.skipped.length > 0 ? <Tag color="warning">跳过 {u.skipped.length}</Tag> : null}
      </Group>
    ) : '—',
  },
  { key: 'created', title: '时间', width: 170, render: (u) => formatDateTime(u.created_at) },
];

export function ImportCenterPage() {
  const navigate = useNavigate();
  const { openDialog } = useUploads();
  const list = useListSearch([] as const);
  const query = useQuery({
    queryKey: qk.uploads.list(list.page, list.pageSize),
    queryFn: ({ signal }) => listUploads({ page: list.page, page_size: list.pageSize }, signal),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (q.state.data?.items.some((u) => u.status === 'pending' || u.status === 'processing') ? 3000 : false),
  });
  return (
    <Stack gap="lg">
      <PageHeader
        title="导入中心"
        description="查看资料导入进度；失败的导入可以从这里继续。"
        extra={<Button leftSection={<IconCloudUpload size={16} />} onClick={openDialog}>导入资料</Button>}
      />
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <DataTable
          rows={query.data?.items ?? []}
          columns={COLUMNS}
          rowKey={(u) => u.id}
          loading={query.isFetching}
          minWidth={600}
          onRowClick={(u) => navigate(href.upload(u.id))}
          empty={<EmptyState description="还没有导入资料" />}
          pagination={{ page: list.page, pageSize: list.pageSize, total: query.data?.total ?? 0, onChange: list.setPage }}
        />
      )}
    </Stack>
  );
}
