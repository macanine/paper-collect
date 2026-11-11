import { Alert, Anchor, Button, Group, Progress, Select, Stack, Text, Tooltip } from '@mantine/core';
import { IconInfoCircle } from '@tabler/icons-react';
import { Link, useNavigate } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { Job, JobKind, JobStatus } from '../../shared/api/types';
import { formatDateTime, formatDuration } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { JOB_KIND_OPTIONS, JOB_STATUS_OPTIONS } from '../../shared/lib/options';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { JobKindTag, JobStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { CancelJobButton } from './CancelJobButton';
import { useJobsList, useRetryJob } from './queries';
import { progressText } from './JobProgress';
import { href } from '../../app/routes';

const FILTER_KEYS = ['status', 'kind', 'material_id', 'upload_id', 'replica_id', 'node_id'] as const;
type FilterKey = (typeof FILTER_KEYS)[number];

export function JobsListPage() {
  const { can } = useAuth();
  const navigate = useNavigate();
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
    node_id: list.filters.node_id || undefined,
  });
  const retry = useRetryJob();
  const canRetry = can('jobs:write');

  const columns: Column<Job>[] = [
    {
      key: 'id',
      title: '任务',
      width: 150,
      render: (job) => (
        <Stack gap={2}>
          <Anchor component={Link} to={href.job(job.id)} size="xs" ff="monospace">{job.id.slice(0, 8)}</Anchor>
          <Text size="xs" c="dimmed">更新 {formatDateTime(job.updated_at)}</Text>
        </Stack>
      ),
    },
    { key: 'kind', title: '类型', width: 100, render: (job) => <JobKindTag kind={job.kind} /> },
    { key: 'status', title: '状态', width: 90, render: (job) => <JobStatusTag status={job.status} /> },
    {
      key: 'progress', wrap: true,
      title: '处理进度',
      render: (job) => (
        <Stack gap={4} maw={360}>
          <Tooltip label={progressText(job)} multiline maw={360}>
            <Text size="sm" c={job.status === 'failed' ? 'red' : undefined} truncate>{progressText(job)}</Text>
          </Tooltip>
          {job.progress_total > 0 ? <Progress size="xs" value={Math.min(100, Math.round((job.progress_current / job.progress_total) * 100))} /> : null}
          {job.attempts > 1 ? <Text size="xs" c="dimmed">第 {job.attempts} 次尝试</Text> : null}
        </Stack>
      ),
    },
    {
      key: 'node',
      title: '节点',
      width: 110,
      hideOnCompact: true,
      render: (job) => job.node_id ? <Anchor component={Link} to={href.jobs({ node_id: job.node_id })} size="sm">{job.node_name || '节点'}</Anchor> : '—',
    },
    { key: 'duration', title: '耗时', width: 90, align: 'right', render: (job) => formatDuration(job.duration_ms) },
    {
      key: 'links', wrap: true,
      title: '关联',
      width: 120,
      hideOnCompact: true,
      render: (job) => (
        <Stack gap={0}>
          {job.upload_id && can('uploads:write') ? <Anchor component={Link} to={href.upload(job.upload_id)} size="xs">查看导入</Anchor> : null}
          {job.material_id && can('materials:read') ? <Anchor component={Link} to={href.material(job.material_id)} size="xs">查看资料</Anchor> : null}
          {job.replica_id ? <Anchor component={Link} to={href.jobs({ replica_id: job.replica_id })} size="xs">同副本任务</Anchor> : null}
        </Stack>
      ),
    },
    {
      key: 'actions',
      title: '操作',
      width: 100,
      render: (job) =>
        canRetry && job.status === 'failed' ? (
          <ConfirmButton
            size="compact-sm"
            variant="default"
            color="red"
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
            重试
          </ConfirmButton>
        ) : canRetry ? <CancelJobButton job={job} size="small" /> : null,
    },
  ];

  if (query.isError && !query.data) {
    return (
      <Stack gap="lg">
        <PageHeader title="任务" />
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="任务列表加载失败" />
      </Stack>
    );
  }

  return (
    <Stack gap="lg">
      <PageHeader title="任务" />
      <Group gap="sm">
        <Select clearable allowDeselect placeholder="状态" w={160} data={JOB_STATUS_OPTIONS} value={list.filters.status || null} onChange={(v) => setFilters({ status: v ?? '' })} />
        <Select clearable allowDeselect placeholder="类型" w={160} data={JOB_KIND_OPTIONS} value={list.filters.kind || null} onChange={(v) => setFilters({ kind: v ?? '' })} />
      </Group>
      {list.filters.material_id || list.filters.upload_id || list.filters.replica_id ? (
        <Alert color="blue" icon={<IconInfoCircle size={18} />} title="正在查看关联任务">
          <Group justify="space-between">
            <Text size="sm">
              {list.filters.material_id ? `资料 ${list.filters.material_id.slice(0, 8)}` : ''}
              {list.filters.upload_id ? `导入 ${list.filters.upload_id.slice(0, 8)}` : ''}
              {list.filters.replica_id ? `存储副本 ${list.filters.replica_id.slice(0, 8)}` : ''}
            </Text>
            <Button size="compact-sm" variant="default" onClick={() => setFilters({ material_id: '', upload_id: '', replica_id: '' })}>清除关联筛选</Button>
          </Group>
        </Alert>
      ) : null}
      <DataTable
        rows={query.data?.items ?? []}
        columns={columns}
        rowKey={(j) => j.id}
        loading={query.isFetching}
        empty={<EmptyState />}
        minWidth={820}
        onRowClick={(job) => navigate(href.job(job.id))}
        pagination={{ page: list.page, pageSize: list.pageSize, total: query.data?.total ?? 0, onChange: list.setPage }}
      />
    </Stack>
  );
}
