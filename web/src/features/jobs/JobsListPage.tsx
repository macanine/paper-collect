import { Alert, App, Button, Card, Popconfirm, Progress, Select, Table, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowRightOutlined } from '@ant-design/icons';
import { Link } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { Job, JobKind, JobStatus } from '../../shared/api/types';
import { formatDateTime, formatDuration } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { JOB_KIND_OPTIONS, JOB_STATUS_OPTIONS } from '../../shared/lib/options';
import { useCompactViewport } from '../../shared/lib/useCompactViewport';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { JobKindTag, JobStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { useJobsList, useRetryJob } from './queries';
import { progressText } from './JobProgress';

const FILTER_KEYS = ['status', 'kind', 'material_id', 'upload_id', 'replica_id'] as const;
type FilterKey = (typeof FILTER_KEYS)[number];

export function JobsListPage() {
  const { can } = useAuth();
  const compact = useCompactViewport();
  const { message } = App.useApp();
  const list = useListSearch<FilterKey>(FILTER_KEYS);
  const { setFilters } = list;
  const query = useJobsList({
    page: list.page,
    page_size: list.pageSize,
    status: (list.filters.status as JobStatus) || undefined,
    kind: (list.filters.kind as JobKind) || undefined,
    material_id: list.filters.material_id || undefined,
    upload_id: list.filters.upload_id || undefined,
    replica_id: list.filters.replica_id || undefined,
  });
  const retry = useRetryJob();
  const canRetry = can('jobs:write');

  const desktopColumns: ColumnsType<Job> = [
    {
      title: '任务',
      key: 'id',
      width: 150,
      render: (_: unknown, job) => (
        <div className="cell-stack">
          <Link to={`/jobs/${job.id}`} className="font-mono text-xs">
            {job.id.slice(0, 8)}
          </Link>
          <span className="table-subtext">{formatDateTime(job.created_at)}</span>
        </div>
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
        <div className="progress-cell">
          <Tooltip title={progressText(job)}>
            <Typography.Text
              type={job.status === 'failed' ? 'danger' : undefined}
              ellipsis
              className="block max-w-[360px]"
            >
              {progressText(job)}
            </Typography.Text>
          </Tooltip>
          {job.progress_total > 0 ? (
            <Progress
              percent={Math.min(100, Math.round((job.progress_current / job.progress_total) * 100))}
              size="small"
              showInfo={false}
            />
          ) : null}
        </div>
      ),
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      key: 'duration',
      width: 100,
      align: 'right',
      render: (value: number) => <span className="tabular-nums">{formatDuration(value)}</span>,
    },
    {
      title: '关联',
      key: 'links',
      width: 120,
      render: (_: unknown, job) => (
        <div className="flex flex-col text-xs">
          {job.upload_id && can('uploads:write') ? <Link to={`/uploads/${job.upload_id}`}>查看清单</Link> : null}
          {job.material_id && can('materials:read') ? <Link to={`/materials/${job.material_id}`}>查看资料</Link> : null}
          {job.replica_id ? (
            <Tooltip title={job.replica_id}>
              <Link to={`/jobs?replica_id=${job.replica_id}`}>同副本任务</Link>
            </Tooltip>
          ) : null}
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

  const compactColumns: ColumnsType<Job> = [
    {
      title: '任务',
      key: 'summary',
      render: (_: unknown, job) => (
        <div className="compact-entity">
          <div className="compact-entity__heading">
            <Link to={`/jobs/${job.id}`} className="compact-entity__title font-mono">
              任务 {job.id.slice(0, 8)}
            </Link>
            <ArrowRightOutlined className="compact-entity__arrow" />
          </div>
          <div className="compact-entity__tags">
            <JobStatusTag status={job.status} />
            <JobKindTag kind={job.kind} />
          </div>
          <div className="compact-entity__progress">
            <span className={job.status === 'failed' ? 'text-red-600' : undefined}>{progressText(job)}</span>
            {job.progress_total > 0 ? (
              <Progress
                percent={Math.min(100, Math.round((job.progress_current / job.progress_total) * 100))}
                size="small"
                showInfo={false}
              />
            ) : null}
          </div>
          <div className="compact-entity__meta">
            <span>{formatDateTime(job.updated_at)}</span>
            <span>耗时 {formatDuration(job.duration_ms)}</span>
            <span>尝试 {job.attempts} 次</span>
          </div>
          <div className="compact-entity__links">
            {job.upload_id && can('uploads:write') ? <Link to={`/uploads/${job.upload_id}`}>上传清单</Link> : null}
            {job.material_id && can('materials:read') ? <Link to={`/materials/${job.material_id}`}>关联资料</Link> : null}
            {job.replica_id ? <Link to={`/jobs?replica_id=${job.replica_id}`}>同副本任务</Link> : null}
            {canRetry && job.status === 'failed' ? (
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
            ) : null}
          </div>
        </div>
      ),
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
      {list.filters.material_id || list.filters.upload_id || list.filters.replica_id ? (
        <Alert
          type="info"
          showIcon
          message="正在查看关联任务"
          description={
            <span>
              {list.filters.material_id ? `资料 ${list.filters.material_id.slice(0, 8)}` : ''}
              {list.filters.upload_id ? `上传清单 ${list.filters.upload_id.slice(0, 8)}` : ''}
              {list.filters.replica_id ? `存储副本 ${list.filters.replica_id.slice(0, 8)}` : ''}
            </span>
          }
          action={
            <Button
              size="small"
              onClick={() => setFilters({ material_id: '', upload_id: '', replica_id: '' })}
            >
              清除关联筛选
            </Button>
          }
        />
      ) : null}
      <Card styles={{ body: { padding: 0 } }}>
        <Table<Job>
          rowKey="id"
          size="middle"
          className={compact ? 'compact-list-table' : undefined}
          columns={compact ? compactColumns : desktopColumns}
          dataSource={query.data?.items ?? []}
          loading={query.isFetching}
          showHeader={!compact}
          scroll={compact ? undefined : { x: 1080 }}
          locale={{ emptyText: <EmptyState /> }}
          pagination={{
            current: list.page,
            pageSize: list.pageSize,
            total: query.data?.total ?? 0,
            showSizeChanger: !compact,
            pageSizeOptions: [10, 20, 50, 100],
            showTotal: (total) => `共 ${total} 条`,
            simple: compact,
            onChange: (page, size) => list.setPage(page, size),
          }}
        />
      </Card>
    </div>
  );
}
