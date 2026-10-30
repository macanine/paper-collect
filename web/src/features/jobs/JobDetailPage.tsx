import { App, Button, Card, Descriptions, Popconfirm, Progress, Typography } from 'antd';
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons';
import { Link, useParams } from 'react-router';
import { describeError } from '../../shared/api/errors';
import { formatDateTime, formatDuration } from '../../shared/lib/format';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { CopyButton } from '../../shared/ui/CopyButton';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { JobStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { useJobDetail, useRetryJob } from './queries';
import { JobProgress, progressText } from './JobProgress';

export function JobDetailPage() {
  const { jobId = '' } = useParams();
  const { can } = useAuth();
  const { message } = App.useApp();
  const query = useJobDetail(jobId);
  const retry = useRetryJob();

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <div className="page-stack">
        <BackLink />
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="任务加载失败" />
      </div>
    );
  }

  const job = query.data;
  const canRetry = can('jobs:write') && job.status === 'failed';
  const duration = job.duration_ms || (
    job.started_at && !job.finished_at
      ? Math.max(0, Date.now() - new Date(job.started_at).getTime())
      : 0
  );
  const progressPercent = job.progress_total > 0
    ? Math.min(100, Math.round((job.progress_current / job.progress_total) * 100))
    : undefined;

  return (
    <div className="page-stack">
      <BackLink />
      <PageHeader
        title={JOB_KIND_LABELS[job.kind] ?? '任务详情'}
        extra={
          <div className="flex items-center gap-2">
            <JobStatusTag status={job.status} />
            <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()} loading={query.isFetching}>
              刷新
            </Button>
            {canRetry ? (
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
                <Button danger loading={retry.isPending}>
                  重新排队
                </Button>
              </Popconfirm>
            ) : null}
          </div>
        }
      />

      <Card title="任务信息">
        <div className="job-summary">
          <div>
            <span>执行耗时</span>
            <strong>{formatDuration(duration)}</strong>
          </div>
          <div>
            <span>尝试次数</span>
            <strong>{job.attempts}</strong>
          </div>
          <div>
            <span>开始时间</span>
            <strong>{formatDateTime(job.started_at)}</strong>
          </div>
          <div>
            <span>结束时间</span>
            <strong>{formatDateTime(job.finished_at)}</strong>
          </div>
        </div>
        {progressPercent !== undefined ? (
          <div className="job-stage-progress">
            <div className="flex items-center justify-between text-sm">
              <span>{job.progress_message}</span>
              <span className="tabular-nums text-slate-500">
                {job.progress_current.toLocaleString()} / {job.progress_total.toLocaleString()}
              </span>
            </div>
            <Progress percent={progressPercent} showInfo={false} status={job.status === 'failed' ? 'exception' : undefined} />
          </div>
        ) : null}
        <Descriptions
          size="small"
          column={{ xs: 1, sm: 1, md: 2 }}
          items={[
            {
              key: 'id',
              label: '任务 ID',
              span: { xs: 1, md: 2 },
              children: (
                <div className="detail-code-row">
                  <Typography.Text code className="inline-code-value">{job.id}</Typography.Text>
                  <CopyButton value={job.id} label="复制" />
                </div>
              ),
            },
            { key: 'progress', label: '当前进度', span: { xs: 1, md: 2 }, children: progressText(job) },
            ...(job.upload_id && can('uploads:write') ? [{ key: 'upload', label: '关联清单', children: <Link to={`/uploads/${job.upload_id}`}>查看上传清单</Link> }] : []),
            ...(job.material_id && can('materials:read') ? [{ key: 'material', label: '关联资料', children: <Link to={`/materials/${job.material_id}`}>查看资料</Link> }] : []),
            ...(job.replica_id && can('jobs:read') ? [{
              key: 'replica',
              label: '关联副本',
              children: (
                <div className="detail-code-row">
                  <Typography.Text code className="inline-code-value">{job.replica_id}</Typography.Text>
                  <CopyButton value={job.replica_id} label="复制" />
                  <Link to={`/jobs?replica_id=${job.replica_id}`}>查看同副本任务</Link>
                </div>
              ),
            }] : []),
            { key: 'created', label: '创建时间', children: formatDateTime(job.created_at) },
            { key: 'updated', label: '更新时间', children: formatDateTime(job.updated_at) },
          ]}
        />
      </Card>

      <Card title="处理记录">
        <JobProgress job={job} />
      </Card>
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/jobs" className="inline-flex items-center gap-1 text-sm">
      <ArrowLeftOutlined />
      返回任务列表
    </Link>
  );
}
