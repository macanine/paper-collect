import { Anchor, Button, Group, Stack, Text, Tooltip } from '@mantine/core';
import { Link } from 'react-router';
import { CopyButton } from '../../shared/ui/CopyButton';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { ReplicaStatusTag, ShareStatusTag, Tag, VerificationTag } from '../../shared/ui/StatusTags';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import type { Drive, Replica } from '../../shared/api/types';
import { href } from '../../app/routes';

interface ReplicaTableProps {
  replicas: Replica[];
  drives: Map<string, Drive> | null;
  canRefreshShare: boolean;
  refreshingId: string | null;
  onRefreshShare: (replica: Replica) => void;
}

export function ReplicaTable({ replicas, drives, canRefreshShare, refreshingId, onRefreshShare }: ReplicaTableProps) {
  const columns: Column<Replica>[] = [
    {
      key: 'drive', title: '网盘', width: 150,
      render: (r) => { const drive = drives?.get(r.drive_id); return drive ? <Anchor component={Link} to="/storage" size="sm">{drive.name}</Anchor> : <Text size="sm" c="dimmed">网盘</Text>; },
    },
    {
      key: 'path', wrap: true, title: '路径', width: 300,
      render: (r) => {
        const drifted = Boolean(r.remote_path) && r.remote_path !== r.desired_path;
        if (!drifted) { const v = r.remote_path || r.desired_path; return v ? <Text size="xs" style={{ wordBreak: 'break-all' }}>{v}</Text> : '—'; }
        return (
          <Stack gap={0}>
            {r.desired_path ? <Text size="xs" style={{ wordBreak: 'break-all' }}>期望：{r.desired_path}</Text> : null}
            <Text size="xs" c="orange" style={{ wordBreak: 'break-all' }}>当前：{r.remote_path}</Text>
          </Stack>
        );
      },
    },
    {
      key: 'status', wrap: true, title: '副本状态', width: 150,
      render: (r) => (
        <Stack gap={4} align="flex-start">
          <ReplicaStatusTag status={r.status} />
          {r.required ? <Tag>必需</Tag> : null}
          {r.skip_reason ? <Text size="xs" c="dimmed">{r.skip_reason}</Text> : null}
          {r.error ? <Tooltip label={r.error} multiline maw={360}><Text size="xs" c="red" maw={140} truncate>{r.error}</Text></Tooltip> : null}
        </Stack>
      ),
    },
    { key: 'verification', title: '校验级别', width: 110, render: (r) => <VerificationTag verification={r.verification} /> },
    { key: 'size', title: '大小', width: 90, align: 'right', render: (r) => formatBytes(r.size_bytes) },
    {
      key: 'share', wrap: true, title: '分享', width: 280,
      render: (r) => {
        const expired = Boolean(r.share_expires_at && new Date(r.share_expires_at).getTime() <= Date.now());
        return (
          <Stack gap={4} align="flex-start">
            {expired ? <Tag color="warning">链接已过期</Tag> : r.share || r.share_status ? <ShareStatusTag status={r.share_status || 'pending'} /> : <Text size="sm" c="dimmed">不分享</Text>}
            {r.share_url && !expired ? (
              <Group gap="xs" wrap="nowrap">
                <Anchor href={r.share_url} target="_blank" rel="noreferrer noopener" size="xs">打开分享链接</Anchor>
                <CopyButton value={r.share_url} label="复制链接" />
              </Group>
            ) : null}
            {r.share_password ? <Text size="xs">提取码：<Text span ff="monospace" size="xs">{r.share_password}</Text></Text> : null}
            {r.share_expires_at ? <Text size="xs" c="dimmed">过期时间：{formatDateTime(r.share_expires_at)}</Text> : null}
          </Stack>
        );
      },
    },
    { key: 'job', title: '任务', width: 100, render: (r) => <Anchor component={Link} to={href.jobs({ replica_id: r.id })} size="sm">复制任务</Anchor> },
    { key: 'updated', title: '更新时间', width: 170, render: (r) => formatDateTime(r.updated_at) },
    ...(canRefreshShare ? [{
      key: 'actions', title: '操作', width: 110,
      render: (r: Replica) => <Button size="compact-sm" variant="default" disabled={r.status !== 'ready'} loading={refreshingId === r.id} onClick={() => onRefreshShare(r)}>刷新分享</Button>,
    }] : []),
  ];
  return <DataTable bare rows={replicas} columns={columns} rowKey={(r) => r.id} minWidth={1300} empty={<Text size="sm" c="dimmed">暂无副本</Text>} />;
}
