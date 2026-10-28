import { App, Button, Card, Descriptions, Popconfirm } from 'antd';
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons';
import { Link, useParams } from 'react-router';
import { describeError } from '../../shared/api/errors';
import { formatDateTime } from '../../shared/lib/format';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { JobStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { useJobDetail, useRetryJob } from './queries';

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

  return (
    <div className="page-stack">
      <BackLink />
      <PageHeader
        title={`任务 ${job.id.slice(0, 8)}`}
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
        <Descriptions
          size="small"
          column={{ xs: 1, md: 2 }}
          items={[
            { key: 'id', label: '任务编号', span: { xs: 1, md: 2 }, children: <span className="mono-text">{job.id}</span> },
            { key: 'kind', label: '类型', children: JOB_KIND_LABELS[job.kind] ?? job.kind },
            { key: 'status', label: '状态', children: <JobStatusTag status={job.status} /> },
            { key: 'attempts', label: '尝试次数', children: job.attempts },
            {
              key: 'upload',
              label: '关联清单',
              children:
                job.upload_id && can('uploads:write') ? <Link to={`/uploads/${job.upload_id}`}>{job.upload_id}</Link> : job.upload_id || '—',
            },
            { key: 'replica', label: '关联副本', children: job.replica_id ? <span className="mono-text">{job.replica_id}</span> : '—' },
            { key: 'created', label: '创建时间', children: formatDateTime(job.created_at) },
            { key: 'updated', label: '更新时间', children: formatDateTime(job.updated_at) },
          ]}
        />
      </Card>

      <Card title="错误信息">
        {job.error ? (
          <pre className="m-0 whitespace-pre-wrap break-all rounded bg-red-50 p-3 text-sm text-red-700">{job.error}</pre>
        ) : (
          '—'
        )}
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
