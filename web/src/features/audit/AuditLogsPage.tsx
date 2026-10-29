import { useState } from 'react';
import { Card, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import type { Audit } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
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
      dataIndex: 'username',
      key: 'user_id',
      width: 150,
      render: (value: string, record) => value || (record.user_id ? '用户不可用' : '系统'),
    },
    {
      title: '目标',
      dataIndex: 'target_id',
      key: 'target_id',
      width: 150,
      render: (value: string, record) => {
        const action = record.action.replace(/^agent:/, '');
        if (!value) return '系统设置';
        if (action.startsWith('material.') && action !== 'material.delete') return <Link to={`/materials/${value}`}>查看资料</Link>;
        if (action.startsWith('collection.') && action !== 'collection.delete') return <Link to={`/collections/${value}`}>查看合集</Link>;
        if (action === 'job.retry') return <Link to={`/jobs/${value}`}>查看任务</Link>;
        if (action.startsWith('upload.') || action === 'ingest.complete') return <Link to={`/uploads/${value}`}>查看上传清单</Link>;
        if (action.startsWith('storage.')) return '存储设置';
        if (action.startsWith('ai.')) return '智能识别设置';
        if (action.startsWith('user.') || action.startsWith('auth.')) return '用户账户';
        if (action.startsWith('api_key.')) return '接口密钥';
        if (action.endsWith('.delete')) return '已删除的内容';
        return '相关内容';
      },
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
