import { useState } from 'react';
import { Button, Group, Stack, Text, Tooltip } from '@mantine/core';
import { modals } from '@mantine/modals';
import { IconPlus } from '@tabler/icons-react';
import { useQuery } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { Drive, ProviderKind } from '../../shared/api/types';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Section } from '../../shared/ui/Section';
import { message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import { ProviderFormModal } from './ProviderFormModal';
import { PROVIDER_FIELD_SPECS } from './providerFields';
import { useDrives, useTestDrive } from './queries';
import { listNodes } from '../nodes/api';
import { qk } from '../../app/queryKeys';

function routeSummary(drive: Drive): string {
  const routes = drive.routes ?? [];
  if (drive.route_mode === 'pool' || routes.length === 0) return '自动分配';
  return routes
    .filter((route) => route.enabled)
    .sort((a, b) => a.priority - b.priority)
    .map((route) => `${route.node_name}${route.is_master ? '（主端）' : ''}`)
    .join(' → ') || '暂无可用节点';
}

function connectionMessage(drive: Drive, healthy: boolean, detail: string): string | undefined {
  if (drive.kind !== 'onedrive' || healthy) return detail || undefined;
  if (detail.includes('onedrive_token_rejected')) return '刷新令牌无效、过期或已撤销，请重新授权并更新网盘凭据。';
  if (drive.config.auth_mode === 'openlist' && (detail.includes('onedrive_token_network') || detail.includes('onedrive_token_timeout') || detail.includes('onedrive_token_http_'))) {
    return '无法通过 OpenList 续期令牌，请检查网络和服务可用性；如果持续失败，请重新获取刷新令牌。';
  }
  return detail || undefined;
}

function CapabilityTags({ drive }: { drive: Drive }) {
  const caps = drive.capabilities;
  return (
    <Group gap={4}>
      {caps.folders ? <Tag>子目录</Tag> : null}
      {caps.move ? <Tag>移动</Tag> : null}
      {caps.share ? <Tag>分享</Tag> : null}
      {caps.delete ? <Tag>删除</Tag> : null}
      {drive.max_file_bytes > 0 ? <Tag color="warning">上限 {formatBytes(drive.max_file_bytes)}</Tag> : null}
      {drive.allowed_exts.length > 0 ? <Tag color="warning">仅限 {drive.allowed_exts.join(' ')}</Tag> : null}
    </Group>
  );
}

export function DrivesTab({ canWrite, canRoute = false }: { canWrite: boolean; canRoute?: boolean }) {
  const drives = useDrives();
  const nodes = useQuery({ queryKey: qk.nodes.list, queryFn: ({ signal }) => listNodes(signal), enabled: canRoute });
  const testDrive = useTestDrive();
  const [editing, setEditing] = useState<Drive | null>(null);
  const [creating, setCreating] = useState(false);

  const handleTest = (drive: Drive) => {
    testDrive.mutate(drive.id, {
      onSuccess: (result) => {
        modals.open({
          title: result.healthy ? `${drive.name}：连接正常` : `${drive.name}：连接失败`,
          children: (
            <Stack gap="md">
              <Text size="sm">{connectionMessage(drive, result.healthy, result.message) ?? '—'}</Text>
              <Group justify="flex-end"><Button onClick={() => modals.closeAll()}>知道了</Button></Group>
            </Stack>
          ),
        });
      },
      onError: (error) => message.error(describeError(error).title),
    });
  };

  const columns: Column<Drive>[] = [
    {
      key: 'name', wrap: true, title: '网盘', width: 220,
      render: (d) => (
        <Stack gap={4}>
          <Text fw={600} size="sm">{d.name}</Text>
          <Text size="xs" c="dimmed">{PROVIDER_FIELD_SPECS[d.kind as ProviderKind].label}{d.root_path ? ` · 根目录 ${d.root_path}` : ''}</Text>
        </Stack>
      ),
    },
    { key: 'enabled', title: '启用', width: 80, render: (d) => (d.enabled ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>) },
    { key: 'route', wrap: true, title: '操作节点', width: 220, render: (d) => <Text size="sm" title={routeSummary(d)}>{d.kind === 'local' ? '主端本地目录' : routeSummary(d)}</Text> },
    { key: 'capabilities', wrap: true, title: '能力', width: 220, render: (d) => <CapabilityTags drive={d} /> },
    {
      key: 'stats', title: '副本', width: 200,
      render: (d) => {
        const stats = d.stats ?? {};
        const ready = stats.ready ?? 0;
        const failed = stats.failed ?? 0;
        const pending = (stats.pending ?? 0) + (stats.uploading ?? 0) + (stats.moving ?? 0);
        const orphaned = stats.orphaned ?? 0;
        return (
          <Text size="sm">
            已就绪 {ready}
            {pending > 0 ? ` · 进行中 ${pending}` : ''}
            {failed > 0 ? <Text span c="red" size="sm"> · 失败 {failed}</Text> : ''}
            {orphaned > 0 ? ` · 规则外 ${orphaned}` : ''}
          </Text>
        );
      },
    },
    { key: 'on_path_change', title: '路径变化', width: 100, render: (d) => (d.on_path_change === 'move' ? '随之移动' : '保持原位') },
    { key: 'secret', title: '凭据', width: 90, render: (d) => (d.kind === 'local' ? '—' : d.secret_configured ? <Tag color="success">已保存</Tag> : <Tag color="warning">未设置</Tag>) },
    { key: 'updated_at', title: '更新时间', width: 160, render: (d) => formatDateTime(d.updated_at) },
    {
      key: 'actions', title: '操作', width: 190,
      render: (d) => (
        <Group gap="xs" wrap="nowrap">
          <Tooltip label="123 云盘尚不支持同步" disabled={d.kind !== '123pan'}>
            <Button size="compact-sm" variant="default" loading={testDrive.isPending && testDrive.variables === d.id} onClick={() => handleTest(d)} disabled={!canWrite}>测试连接</Button>
          </Tooltip>
          <Button size="compact-sm" variant="default" onClick={() => setEditing(d)} disabled={!canWrite}>编辑</Button>
        </Group>
      ),
    },
  ];

  return (
    <>
      <Section title="网盘" flush extra={<Button leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)} disabled={!canWrite}>新增网盘</Button>}>
        {drives.isPending ? <LoadingState /> : null}
        {drives.isError ? <ErrorState error={drives.error} onRetry={() => void drives.refetch()} /> : null}
        {drives.isSuccess ? <DataTable bare rows={drives.data} columns={columns} rowKey={(d) => d.id} empty={<EmptyState description="还没有网盘，先添加一个" />} minWidth={1200} /> : null}
      </Section>

      <ProviderFormModal
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={() => {
          setCreating(false);
          void drives.refetch();
        }}
        nodes={nodes.data ?? []}
        canRoute={canRoute}
      />
      <ProviderFormModal
        open={editing !== null}
        drive={editing ?? undefined}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          void drives.refetch();
        }}
        nodes={nodes.data ?? []}
        canRoute={canRoute}
      />
    </>
  );
}
