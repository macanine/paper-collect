import { App, Button, Card, Popconfirm, Select, Table, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { Job, JobKind, JobStatus } from '../../shared/api/types';
import { formatDateTime, shortId } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { JOB_KIND_OPTIONS, JOB_STATUS_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { JobKindTag, JobStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { useJobsList, useRetryJob } from './queries';

const FILTER_KEYS = ['status', 'kind'] as const;
type FilterKey = (typeof FILTER_KEYS)[number];

export function JobsListPage() {
  const { can } = useAuth();
  const { message } = App.useApp();
  const list = useListSearch<FilterKey>(FILTER_KEYS);
  const { setFilters } = list;
  const query = useJobsList({
    page: list.page,
    page_size: list.pageSize,
    status: (list.filters.status as JobStatus) || undefined,
    kind: (list.filters.kind as JobKind) || undefined,
  });
  const retry = useRetryJob();
  const canRetry = can('jobs:write');

  const columns: ColumnsType<Job> = [
    {
      title: '任务',
      key: 'id',
      width: 130,
      render: (_: unknown, job) => (
        <Link to={`/jobs/${job.id}`} className="font-mono text-xs">
          {shortId(job.id)}
        </Link>
      ),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 110,
      render: (kind: JobKind) => <JobKindTag kind={kind} />,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (status: JobStatus) => <JobStatusTag status={status} />,
    },
    {
      title: '尝试',
      dataIndex: 'attempts',
      key: 'attempts',
      width: 80,
      align: 'right',
    },
    {
      title: '错误信息',
      dataIndex: 'error',
      key: 'error',
      render: (error: string) =>
        error ? (
          <Tooltip title={error}>
            <Typography.Text type="danger" ellipsis className="block max-w-[320px]">
              {error}
            </Typography.Text>
          </Tooltip>
        ) : (
          <span className="text-slate-400">—</span>
        ),
    },
    {
      title: '关联',
      key: 'links',
      width: 160,
      render: (_: unknown, job) => (
        <div className="flex flex-col text-xs">
          {job.upload_id && can('uploads:write') ? <Link to={`/uploads/${job.upload_id}`}>清单 {shortId(job.upload_id)}</Link> : null}
          {job.replica_id ? <span className="font-mono text-slate-500">副本 {shortId(job.replica_id)}</span> : null}
        </div>
      ),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      fixed: 'right',
      render: (_: unknown, job) =>
        canRetry && job.status === 'failed' ? (
          <Popconfirm
            title="重新排队该任务？"
            okText="重新排队"
            cancelText="取消"
            onConfirm={async () => {
              try {
                await retry.mutateAsync(job.id);
                message.success('任务已重新排队');
              } catch (error) {
                const { title, detail } = describeError(error);
                message.error(detail ? `${title}：${detail}` : title);
              }
            }}
          >
            <Button size="small" danger>
              重试
            </Button>
          </Popconfirm>
        ) : null,
    },
  ];

  if (query.isError && !query.data) {
    return (
      <div className="page-stack">
        <PageHeader title="任务" />
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="任务列表加载失败" />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <PageHeader title="任务" />
      <div className="filter-row">
        <Select
          allowClear
          placeholder="状态"
          style={{ width: 160 }}
          options={JOB_STATUS_OPTIONS}
          value={list.filters.status || undefined}
          onChange={(value: string | undefined) => setFilters({ status: value ?? '' })}
        />
        <Select
          allowClear
          placeholder="类型"
          style={{ width: 160 }}
          options={JOB_KIND_OPTIONS}
          value={list.filters.kind || undefined}
          onChange={(value: string | undefined) => setFilters({ kind: value ?? '' })}
        />
      </div>
      <Card styles={{ body: { padding: 0 } }}>
        <Table<Job>
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={query.data?.items ?? []}
          loading={query.isFetching}
          scroll={{ x: 1000 }}
          locale={{ emptyText: <EmptyState /> }}
          pagination={{
            current: list.page,
            pageSize: list.pageSize,
            total: query.data?.total ?? 0,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, size) => list.setPage(page, size),
          }}
        />
      </Card>
    </div>
  );
}
