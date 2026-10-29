import { App, Button, Card, Popconfirm, Select, Table, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { Job, JobKind, JobStatus } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { JOB_KIND_OPTIONS, JOB_STATUS_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { JobKindTag, JobStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { useJobsList, useRetryJob } from './queries';
import { progressText } from './JobProgress';

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
      width: 90,
      render: (_: unknown, job) => (
        <Link to={`/jobs/${job.id}`} className="whitespace-nowrap">
          查看详情
        </Link>
      ),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 100,
      render: (kind: JobKind) => <JobKindTag kind={kind} />,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 90,
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
      title: '处理进度',
      key: 'progress',
      render: (_: unknown, job) => (
        <Tooltip title={progressText(job)}>
          <Typography.Text type={job.status === 'failed' ? 'danger' : undefined} ellipsis className="block max-w-[320px]">{progressText(job)}</Typography.Text>
        </Tooltip>
      ),
    },
    {
      title: '关联',
      key: 'links',
      width: 160,
      render: (_: unknown, job) => (
        <div className="flex flex-col text-xs">
          {job.upload_id && can('uploads:write') ? <Link to={`/uploads/${job.upload_id}`}>查看清单</Link> : null}
          {job.material_id && can('materials:read') ? <Link to={`/materials/${job.material_id}`}>查看资料</Link> : null}
          {job.replica_id ? <span className="text-slate-500">存储副本</span> : null}
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
