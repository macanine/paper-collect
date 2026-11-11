import { Anchor, Button, Code, Group, Progress, SimpleGrid, Stack, Text } from '@mantine/core';
import { IconRefresh } from '@tabler/icons-react';
import { Link, useParams } from 'react-router';
import { describeError } from '../../shared/api/errors';
import { formatDateTime, formatDuration } from '../../shared/lib/format';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { CopyButton } from '../../shared/ui/CopyButton';
import { Descriptions } from '../../shared/ui/Descriptions';
import { PageHeader } from '../../shared/ui/PageHeader';
import { Section } from '../../shared/ui/Section';
import { BackLink } from '../../shared/ui/BackLink';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { JobStatusTag } from '../../shared/ui/StatusTags';
import { Metric } from '../../shared/ui/Metric';
import { useAuth } from '../auth/AuthContext';
import { CancelJobButton } from './CancelJobButton';
import { useJobDetail, useRetryJob } from './queries';
import { JobProgress, progressText } from './JobProgress';
import { href } from '../../app/routes';

export function JobDetailPage() {
  const { jobId = '' } = useParams();
  const { can } = useAuth();
  const query = useJobDetail(jobId);
  const retry = useRetryJob();

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <Stack gap="lg">
        <BackLink to="/jobs">返回任务列表</BackLink>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="任务加载失败" />
      </Stack>
    );
  }

  const job = query.data;
  const canRetry = can('jobs:write') && job.status === 'failed';
  const duration = job.duration_ms || (job.started_at && !job.finished_at ? Math.max(0, Date.now() - new Date(job.started_at).getTime()) : 0);
  const progressPercent = job.progress_total > 0 ? Math.min(100, Math.round((job.progress_current / job.progress_total) * 100)) : undefined;

  return (
    <Stack gap="lg">
      <BackLink to="/jobs">返回任务列表</BackLink>
      <PageHeader
        title={JOB_KIND_LABELS[job.kind] ?? '任务详情'}
        extra={
          <>
            <JobStatusTag status={job.status} />
            <Button variant="default" leftSection={<IconRefresh size={16} />} onClick={() => void query.refetch()} loading={query.isFetching}>刷新</Button>
            {canRetry ? (
              <ConfirmButton
                loading={retry.isPending}
                confirmTitle="重新排队该任务？"
                okText="重新排队"
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
                重新排队
              </ConfirmButton>
            ) : null}
            {can('jobs:write') ? <CancelJobButton job={job} /> : null}
          </>
        }
      />

      <Section title="任务信息">
        <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="md">
          <Metric label="执行耗时" value={formatDuration(duration)} />
          <Metric label="尝试次数" value={job.attempts} />
          <Metric label="开始时间" value={formatDateTime(job.started_at)} />
          <Metric label="结束时间" value={formatDateTime(job.finished_at)} />
        </SimpleGrid>
        {progressPercent !== undefined ? (
          <Stack gap="xs">
            <Group justify="space-between">
              <Text size="sm">{job.progress_message}</Text>
              <Text size="sm" c="dimmed">{job.progress_current.toLocaleString()} / {job.progress_total.toLocaleString()}</Text>
            </Group>
            <Progress value={progressPercent} color={job.status === 'failed' ? 'red' : 'blue'} />
          </Stack>
        ) : null}
        <Descriptions
          items={[
            { key: 'node', label: '执行节点', children: job.node_id ? <Anchor component={Link} to={href.jobs({ node_id: job.node_id })} size="sm">{job.node_name || '节点'}</Anchor> : '—' },
            { key: 'id', label: '任务 ID', full: true, children: <Group gap="xs"><Code>{job.id}</Code><CopyButton value={job.id} label="复制" /></Group> },
            { key: 'progress', label: '当前进度', full: true, children: progressText(job) },
            !!job.upload_id && can('uploads:write') && { key: 'upload', label: '关联导入', children: <Anchor component={Link} to={href.upload(job.upload_id)} size="sm">查看导入</Anchor> },
            !!job.material_id && can('materials:read') && { key: 'material', label: '关联资料', children: <Anchor component={Link} to={href.material(job.material_id)} size="sm">查看资料</Anchor> },
            !!job.replica_id && can('jobs:read') && {
              key: 'replica',
              label: '关联副本',
              full: true,
              children: (
                <Group gap="xs">
                  <Code>{job.replica_id}</Code>
                  <CopyButton value={job.replica_id} label="复制" />
                  <Anchor component={Link} to={href.jobs({ replica_id: job.replica_id })} size="sm">查看同副本任务</Anchor>
                </Group>
              ),
            },
            { key: 'created', label: '创建时间', children: formatDateTime(job.created_at) },
            { key: 'updated', label: '更新时间', children: formatDateTime(job.updated_at) },
          ]}
        />
      </Section>

      <Section title="处理记录">
        <JobProgress job={job} />
      </Section>
    </Stack>
  );
}
