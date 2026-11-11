import { useState } from 'react';
import { Anchor, Code, Stack } from '@mantine/core';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import type { Audit } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { listAuditLogs } from './api';
import { href } from '../../app/routes';
import { qk } from '../../app/queryKeys';

const PAGE_SIZE = 20;

function target(record: Audit) {
  const value = record.target_id;
  const action = record.action.replace(/^agent:/, '');
  if (!value) return '系统设置';
  if (action.startsWith('material.') && action !== 'material.delete') return <Anchor component={Link} to={href.material(value)} size="sm">查看资料</Anchor>;
  if (action.startsWith('collection.') && action !== 'collection.delete') return <Anchor component={Link} to={href.collection(value)} size="sm">查看合集</Anchor>;
  if (action === 'job.retry') return <Anchor component={Link} to={href.job(value)} size="sm">查看任务</Anchor>;
  if (action.startsWith('upload.') || action === 'ingest.complete') return <Anchor component={Link} to={href.upload(value)} size="sm">查看导入</Anchor>;
  if (action.startsWith('storage.')) return '存储设置';
  if (action.startsWith('ai.')) return '智能识别设置';
  if (action.startsWith('user.') || action.startsWith('auth.')) return '用户账户';
  if (action.startsWith('api_key.')) return '接口密钥';
  if (action.endsWith('.delete')) return '已删除的内容';
  return '相关内容';
}

const COLUMNS: Column<Audit>[] = [
  { key: 'action', title: '动作', render: (a) => <Code>{a.action}</Code> },
  { key: 'user', title: '操作者', width: 150, render: (a) => a.username || (a.user_id ? '用户不可用' : '系统') },
  { key: 'target', title: '目标', width: 150, render: target },
  { key: 'created_at', title: '时间', width: 190, render: (a) => formatDateTime(a.created_at) },
];

export function AuditLogsPage() {
  const [page, setPage] = useState(1);
  const query = useQuery({
    queryKey: qk.audit.list(page),
    queryFn: ({ signal }) => listAuditLogs({ page, page_size: PAGE_SIZE }, signal),
  });

  return (
    <Stack gap="lg">
      <PageHeader title="审计日志" />
      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isSuccess ? (
        <DataTable
          rows={query.data.items}
          columns={COLUMNS}
          rowKey={(a) => a.id}
          empty={<EmptyState />}
          minWidth={600}
          pagination={{ page: query.data.page, pageSize: query.data.page_size, total: query.data.total, pageSizes: [query.data.page_size], onChange: (p) => setPage(p) }}
        />
      ) : null}
    </Stack>
  );
}
