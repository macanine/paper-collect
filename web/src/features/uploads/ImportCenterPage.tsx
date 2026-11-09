import { Button, Card, Table, Tag } from 'antd';
import { CloudUploadOutlined } from '@ant-design/icons';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import type { Upload } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { navigateTableRow } from '../../shared/lib/tableNavigation';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { UploadStatusTag } from '../../shared/ui/StatusTags';
import { listUploads } from './api';
import { useUploads } from './UploadManager';

export function ImportCenterPage() {
  const navigate = useNavigate();
  const { openDialog } = useUploads();
  const list = useListSearch([] as const);
  const query = useQuery({
    queryKey: ['uploads', 'list', list.page, list.pageSize],
    queryFn: ({ signal }) => listUploads({ page: list.page, page_size: list.pageSize }, signal),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (q.state.data?.items.some((u) => u.status === 'pending' || u.status === 'processing') ? 3000 : false),
  });
  return (
    <div className="page-stack">
      <PageHeader
        title="导入中心"
        description="查看资料导入进度；失败的导入可以从这里继续。"
        extra={
          <Button icon={<CloudUploadOutlined />} onClick={openDialog}>
            导入资料
          </Button>
        }
      />
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<Upload>
            rowKey="id"
            loading={query.isFetching}
            dataSource={query.data?.items ?? []}
            onRow={(upload) => ({ onClick: (event) => navigateTableRow(event, `/imports/${upload.id}`, navigate), className: 'cursor-pointer' })}
            locale={{ emptyText: <EmptyState description="还没有导入资料" /> }}
            pagination={{ current: list.page, pageSize: list.pageSize, total: query.data?.total ?? 0, onChange: list.setPage, hideOnSinglePage: true }}
            columns={[
              { title: '导入批次', key: 'title', render: (_: unknown, u) => <Link to={`/imports/${u.id}`}>{u.title}</Link> },
              { title: '状态', key: 'status', width: 110, render: (_: unknown, u) => <UploadStatusTag status={u.status} /> },
              {
                title: '结果',
                key: 'result',
                render: (_: unknown, u) =>
                  u.status === 'completed' ? (
                    <span className="flex flex-wrap gap-1">
                      <Tag color="blue">新增 {u.new_count}</Tag>
                      <Tag>已存在 {u.duplicate_count}</Tag>
                      {u.skipped.length > 0 ? <Tag color="warning">跳过 {u.skipped.length}</Tag> : null}
                    </span>
                  ) : (
                    '—'
                  ),
              },
              { title: '时间', key: 'created', width: 170, render: (_: unknown, u) => formatDateTime(u.created_at) },
            ]}
          />
        </Card>
      )}
    </div>
  );
}
