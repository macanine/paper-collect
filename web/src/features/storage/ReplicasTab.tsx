import { useState } from 'react';
import { Alert, Anchor, Button, Group, Select, Stack, Text, Tooltip } from '@mantine/core';
import { Link } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { Replica } from '../../shared/api/types';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Section } from '../../shared/ui/Section';
import { message, modal } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { ReplicaStatusTag, ShareStatusTag } from '../../shared/ui/StatusTags';
import { driveName } from './ruleUtils';
import { useBulkReplicas, useDrives, useReplicas } from './queries';
import type { ReplicaBulkAction } from './api';
import { href } from '../../app/routes';

const PAGE_SIZE = 20;
const ALL = 'all';

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: ALL, label: '全部状态' },
  { value: 'failed', label: '失败' },
  { value: 'skipped', label: '已跳过' },
  { value: 'orphaned', label: '规则外' },
  { value: 'drift', label: '路径偏离' },
  { value: 'pending', label: '待处理' },
  { value: 'uploading', label: '上传中' },
  { value: 'moving', label: '移动中' },
  { value: 'ready', label: '已就绪' },
];

function bulkActionLabel(action: ReplicaBulkAction): string {
  if (action === 'retry') return '重试';
  if (action === 'delete_remote') return '删除远端文件';
  return '移除记录';
}

export function ReplicasTab({ canWrite }: { canWrite: boolean }) {
  const [status, setStatus] = useState(ALL);
  const [driveId, setDriveId] = useState(ALL);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<string[]>([]);

  const drives = useDrives();
  const replicas = useReplicas(status === ALL ? '' : status, driveId === ALL ? '' : driveId, page, PAGE_SIZE);
  const bulk = useBulkReplicas();

  const runBulk = (action: ReplicaBulkAction) => {
    const ids = selected;
    const execute = () => {
      bulk.mutate(
        { ids, action },
        {
          onSuccess: (results) => {
            const failed = results.filter((r) => !r.success);
            if (failed.length === 0) message.success(`已${bulkActionLabel(action)} ${results.length} 个副本`);
            else message.warning(`成功 ${results.length - failed.length} 个，失败 ${failed.length} 个：${failed[0]?.error ?? ''}`);
            setSelected([]);
          },
          onError: (error) => message.error(describeError(error).title),
        },
      );
    };
    if (action === 'delete_remote') {
      modal.confirm({ title: `删除 ${ids.length} 个远端文件？`, content: '网盘上的文件会被删除，本机原件不受影响。此操作不可撤销。', okText: '删除', danger: true, onOk: execute });
    } else if (action === 'forget') {
      modal.confirm({ title: `移除 ${ids.length} 条副本记录？`, content: '只移除本系统中的记录，网盘上的文件保留。', okText: '移除', onOk: execute });
    } else {
      execute();
    }
  };

  const columns: Column<Replica>[] = [
    {
      key: 'status', wrap: true, title: '状态', width: 130,
      render: (r) => (
        <Stack gap={4} align="flex-start">
          <ReplicaStatusTag status={r.status} />
          {r.skip_reason ? <Text size="xs" c="dimmed">{r.skip_reason}</Text> : null}
          {r.error ? <Tooltip label={r.error} multiline maw={360}><Text size="xs" c="red" maw={160} truncate>{r.error}</Text></Tooltip> : null}
        </Stack>
      ),
    },
    { key: 'drive', title: '网盘', width: 150, render: (r) => driveName(drives.data ?? [], r.drive_id) },
    {
      key: 'path', wrap: true, title: '期望路径',
      render: (r) => (
        <Stack gap={0}>
          <Text size="xs" style={{ wordBreak: 'break-all' }}>{r.desired_path || '—'}</Text>
          {r.remote_path && r.remote_path !== r.desired_path ? <Text size="xs" c="orange" style={{ wordBreak: 'break-all' }}>当前：{r.remote_path}</Text> : null}
        </Stack>
      ),
    },
    { key: 'size', title: '大小', width: 100, align: 'right', render: (r) => formatBytes(r.size_bytes) },
    { key: 'share', title: '分享', width: 100, render: (r) => (r.share || r.share_status ? <ShareStatusTag status={r.share_status || 'pending'} /> : <Text c="dimmed" size="sm">—</Text>) },
    { key: 'job', title: '任务', width: 80, render: (r) => <Anchor component={Link} to={href.jobs({ replica_id: r.id })} size="sm">任务</Anchor> },
    { key: 'updated', title: '更新时间', width: 170, render: (r) => formatDateTime(r.updated_at) },
  ];

  const selectedRows = (replicas.data?.items ?? []).filter((r) => selected.includes(r.id));
  const allOrphaned = selectedRows.length > 0 && selectedRows.every((r) => r.status === 'orphaned');
  const anyActive = selectedRows.some((r) => r.status === 'uploading' || r.status === 'moving' || r.status === 'pending');

  return (
    <Section title="副本" flush>
      <Stack gap="sm" p="lg" pt={0}>
        <Group gap="sm">
          <Select w={150} allowDeselect={false} value={status} data={STATUS_OPTIONS} onChange={(v) => { setStatus(v ?? ALL); setPage(1); setSelected([]); }} />
          <Select w={190} allowDeselect={false} value={driveId} data={[{ value: ALL, label: '全部网盘' }, ...(drives.data ?? []).map((d) => ({ value: d.id, label: d.name }))]} onChange={(v) => { setDriveId(v ?? ALL); setPage(1); setSelected([]); }} />
          {canWrite && selected.length > 0 ? (
            <Group gap="xs">
              <Text size="sm" c="dimmed">已选 {selected.length} 项</Text>
              <Button size="compact-sm" variant="default" loading={bulk.isPending} disabled={anyActive} onClick={() => runBulk('retry')}>重试</Button>
              <Button size="compact-sm" variant="default" color="red" loading={bulk.isPending} disabled={!allOrphaned} onClick={() => runBulk('delete_remote')}>删除远端</Button>
              <Button size="compact-sm" variant="default" loading={bulk.isPending} disabled={!allOrphaned} onClick={() => runBulk('forget')}>移除记录</Button>
            </Group>
          ) : null}
        </Group>
        {canWrite && status === ALL && driveId === ALL ? <Alert color="blue">选择「失败 / 已跳过 / 规则外 / 路径偏离」状态，可筛选出需要处理的副本并批量操作。</Alert> : null}
      </Stack>
      {replicas.isPending ? <LoadingState /> : null}
      {replicas.isError ? <ErrorState error={replicas.error} onRetry={() => void replicas.refetch()} /> : null}
      {replicas.isSuccess ? (
        <DataTable
          bare
          rows={replicas.data.items}
          columns={columns}
          rowKey={(r) => r.id}
          minWidth={1100}
          selected={canWrite ? selected : undefined}
          onSelectedChange={canWrite ? (keys) => setSelected(keys.map(String)) : undefined}
          empty={<EmptyState />}
          pagination={{ page, pageSize: PAGE_SIZE, total: replicas.data.total, pageSizes: [PAGE_SIZE], onChange: (next) => { setPage(next); setSelected([]); } }}
        />
      ) : null}
    </Section>
  );
}
