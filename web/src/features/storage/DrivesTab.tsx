import { useState } from 'react';
import { App, Button, Card, Space, Table, Tag, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { Drive, ProviderKind } from '../../shared/api/types';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
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
    <span className="inline-flex flex-wrap gap-1">
      {caps.folders ? <Tag>子目录</Tag> : null}
      {caps.move ? <Tag>移动</Tag> : null}
      {caps.share ? <Tag>分享</Tag> : null}
      {caps.delete ? <Tag>删除</Tag> : null}
      {drive.max_file_bytes > 0 ? <Tag color="warning">上限 {formatBytes(drive.max_file_bytes)}</Tag> : null}
      {drive.allowed_exts.length > 0 ? <Tag color="warning">仅限 {drive.allowed_exts.join(' ')}</Tag> : null}
    </span>
  );
}

export function DrivesTab({ canWrite, canRoute = false }: { canWrite: boolean; canRoute?: boolean }) {
  const { message, modal } = App.useApp();
  const drives = useDrives();
  const nodes = useQuery({ queryKey: qk.nodes.list, queryFn: ({ signal }) => listNodes(signal), enabled: canRoute });
  const testDrive = useTestDrive();
  const [editing, setEditing] = useState<Drive | null>(null);
  const [creating, setCreating] = useState(false);

  const handleTest = (drive: Drive) => {
    testDrive.mutate(drive.id, {
      onSuccess: (result) => {
        modal.info({
          title: result.healthy ? `${drive.name}：连接正常` : `${drive.name}：连接失败`,
          content: connectionMessage(drive, result.healthy, result.message),
        });
      },
      onError: (error) => message.error(describeError(error).title),
    });
  };

  const columns: ColumnsType<Drive> = [
    {
      title: '网盘',
      dataIndex: 'name',
      key: 'name',
      width: 220,
      render: (name: string, record) => (
        <div className="flex flex-col">
          <span className="font-medium">{name}</span>
          <span className="text-xs text-slate-500">
            {PROVIDER_FIELD_SPECS[record.kind as ProviderKind].label}
            {record.root_path ? ` · 根目录 ${record.root_path}` : ''}
          </span>
        </div>
      ),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (enabled: boolean) => (enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>),
    },
    {
      title: '上传节点',
      key: 'route',
      width: 220,
      render: (_: unknown, record) => (
        <span className="text-sm" title={routeSummary(record)}>
          {record.kind === 'local' ? '主端本地目录' : routeSummary(record)}
        </span>
      ),
    },
    {
      title: '能力',
      key: 'capabilities',
      width: 220,
      render: (_: unknown, record) => <CapabilityTags drive={record} />,
    },
    {
      title: '副本',
      key: 'stats',
      width: 180,
      render: (_: unknown, record) => {
        const stats = record.stats ?? {};
        const ready = stats.ready ?? 0;
        const failed = stats.failed ?? 0;
        const pending = (stats.pending ?? 0) + (stats.uploading ?? 0) + (stats.moving ?? 0);
        const orphaned = stats.orphaned ?? 0;
        return (
          <span className="text-sm">
            已就绪 {ready}
            {pending > 0 ? ` · 进行中 ${pending}` : ''}
            {failed > 0 ? <span className="text-red-600"> · 失败 {failed}</span> : ''}
            {orphaned > 0 ? ` · 规则外 ${orphaned}` : ''}
          </span>
        );
      },
    },
    {
      title: '路径变化',
      dataIndex: 'on_path_change',
      key: 'on_path_change',
      width: 100,
      render: (value: Drive['on_path_change']) => (value === 'move' ? '随之移动' : '保持原位'),
    },
    {
      title: '凭据',
      dataIndex: 'secret_configured',
      key: 'secret',
      width: 90,
      render: (configured: boolean, record) =>
        record.kind === 'local' ? '—' : configured ? <Tag color="green">已保存</Tag> : <Tag color="orange">未设置</Tag>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 160,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 170,
      fixed: 'right' as const,
      render: (_: unknown, record) => (
        <Space size={4}>
          <Tooltip title={record.kind === '123pan' ? '123 云盘尚不支持同步' : undefined}>
            <Button
              size="small"
              loading={testDrive.isPending && testDrive.variables === record.id}
              onClick={() => handleTest(record)}
              disabled={!canWrite}
            >
              测试连接
            </Button>
          </Tooltip>
          <Button size="small" onClick={() => setEditing(record)} disabled={!canWrite}>
            编辑
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Card
        title="网盘"
        styles={{ body: { padding: 0 } }}
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)} disabled={!canWrite}>
            新增网盘
          </Button>
        }
      >
        {drives.isPending ? <LoadingState /> : null}
        {drives.isError ? <ErrorState error={drives.error} onRetry={() => void drives.refetch()} /> : null}
        {drives.isSuccess ? (
          <Table<Drive>
            rowKey="id"
            size="middle"
            dataSource={drives.data}
            columns={columns}
            locale={{ emptyText: <EmptyState description="还没有网盘，先添加一个" /> }}
            pagination={false}
            scroll={{ x: 1100 }}
          />
        ) : null}
      </Card>

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
