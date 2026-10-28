import { useState } from 'react';
import { Card, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useQuery } from '@tanstack/react-query';
import type { Audit } from '../../shared/api/types';
import { formatDateTime, shortId } from '../../shared/lib/format';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { listAuditLogs } from './api';

const PAGE_SIZE = 20;

export function AuditLogsPage() {
  const [page, setPage] = useState(1);
  const query = useQuery({
    queryKey: ['audit', page],
    queryFn: ({ signal }) => listAuditLogs({ page, page_size: PAGE_SIZE }, signal),
  });

  const columns: ColumnsType<Audit> = [
    {
      title: '动作',
      dataIndex: 'action',
      key: 'action',
      render: (action: string) => <Typography.Text code>{action}</Typography.Text>,
    },
    {
      title: '操作者',
      dataIndex: 'user_id',
      key: 'user_id',
      width: 150,
      render: (value: string) => <span className="font-mono text-xs">{shortId(value)}</span>,
    },
    {
      title: '目标',
      dataIndex: 'target_id',
      key: 'target_id',
      width: 150,
      render: (value: string) => (value ? <span className="font-mono text-xs">{shortId(value)}</span> : '—'),
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 190,
      render: (value: string) => formatDateTime(value),
    },
  ];

  return (
    <div className="page-stack">
      <PageHeader title="审计日志" />
      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isSuccess ? (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<Audit>
            rowKey="id"
            size="middle"
            dataSource={query.data.items}
            columns={columns}
            locale={{ emptyText: <EmptyState /> }}
            pagination={{
              current: query.data.page,
              pageSize: query.data.page_size,
              total: query.data.total,
              showSizeChanger: false,
              onChange: setPage,
            }}
          />
        </Card>
      ) : null}
    </div>
  );
}
