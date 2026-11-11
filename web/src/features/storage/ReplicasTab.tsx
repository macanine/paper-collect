import { useState } from 'react';
import { Alert, App, Button, Card, Select, Space, Table, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { Replica, ReplicaStatus } from '../../shared/api/types';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { ReplicaStatusTag, ShareStatusTag } from '../../shared/ui/StatusTags';
import { driveName } from './ruleUtils';
import { useBulkReplicas, useDrives, useReplicas } from './queries';
import type { ReplicaBulkAction } from './api';
import { href } from '../../app/routes';

const PAGE_SIZE = 20;

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '全部状态' },
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
  const { message, modal } = App.useApp();
  const [status, setStatus] = useState('');
  const [driveId, setDriveId] = useState('');
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<string[]>([]);

  const drives = useDrives();
  const replicas = useReplicas(status, driveId, page, PAGE_SIZE);
  const bulk = useBulkReplicas();

  const runBulk = (action: ReplicaBulkAction) => {
    const ids = selected;
    const execute = () => {
      bulk.mutate(
        { ids, action },
        {
          onSuccess: (results) => {
            const failed = results.filter((r) => !r.success);
            if (failed.length === 0) {
              message.success(`已${bulkActionLabel(action)} ${results.length} 个副本`);
            } else {
              message.warning(`成功 ${results.length - failed.length} 个，失败 ${failed.length} 个：${failed[0]?.error ?? ''}`);
            }
            setSelected([]);
          },
          onError: (error) => message.error(describeError(error).title),
        },
      );
    };
    if (action === 'delete_remote') {
      modal.confirm({
        title: `删除 ${ids.length} 个远端文件？`,
        content: '网盘上的文件会被删除，本机原件不受影响。此操作不可撤销。',
        okText: '删除',
        okButtonProps: { danger: true },
        onOk: execute,
      });
    } else if (action === 'forget') {
      modal.confirm({
        title: `移除 ${ids.length} 条副本记录？`,
        content: '只移除本系统中的记录，网盘上的文件保留。',
        okText: '移除',
        onOk: execute,
      });
    } else {
      execute();
    }
  };

  const columns: ColumnsType<Replica> = [
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 120,
      render: (value: ReplicaStatus, record) => (
        <div className="flex flex-col items-start gap-1">
          <ReplicaStatusTag status={value} />
          {record.skip_reason ? <span className="text-xs text-slate-500">{record.skip_reason}</span> : null}
          {record.error ? (
            <Tooltip title={record.error}>
              <span className="block max-w-40 truncate text-xs text-red-600">{record.error}</span>
            </Tooltip>
          ) : null}
        </div>
      ),
    },
    {
      title: '网盘',
      dataIndex: 'drive_id',
      key: 'drive_id',
      width: 150,
      render: (id: string) => driveName(drives.data ?? [], id),
    },
    {
      title: '期望路径',
      dataIndex: 'desired_path',
      key: 'desired_path',
      render: (path: string, record) => (
        <div className="flex flex-col text-xs">
          <span className="break-all">{path || '—'}</span>
          {record.remote_path && record.remote_path !== path ? (
            <span className="break-all text-orange-600">当前：{record.remote_path}</span>
          ) : null}
        </div>
      ),
    },
    { title: '大小', dataIndex: 'size_bytes', key: 'size_bytes', width: 100, align: 'right', render: (v: number) => formatBytes(v) },
    {
      title: '分享',
      dataIndex: 'share_status',
      key: 'share_status',
      width: 100,
      render: (value: Replica['share_status'], record) => (record.share || value ? <ShareStatusTag status={value || 'pending'} /> : <span className="text-slate-400">—</span>),
    },
    {
      title: '任务',
      key: 'job',
      width: 100,
      render: (_: unknown, record) => <Link to={href.jobs({ replica_id: record.id })}>任务</Link>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
  ];

  const selectedRows = (replicas.data?.items ?? []).filter((r) => selected.includes(r.id));
  const allOrphaned = selectedRows.length > 0 && selectedRows.every((r) => r.status === 'orphaned');
  const anyActive = selectedRows.some((r) => r.status === 'uploading' || r.status === 'moving' || r.status === 'pending');

  return (
    <Card title="副本" styles={{ body: { padding: 0 } }}>
      <div className="flex flex-wrap items-center gap-3 px-6 py-3">
        <Select
          value={status}
          onChange={(value) => {
            setStatus(value);
            setPage(1);
            setSelected([]);
          }}
          options={STATUS_OPTIONS}
          style={{ width: 140 }}
        />
        <Select
          value={driveId}
          onChange={(value) => {
            setDriveId(value);
            setPage(1);
            setSelected([]);
          }}
          options={[{ value: '', label: '全部网盘' }, ...(drives.data ?? []).map((d) => ({ value: d.id, label: d.name }))]}
          style={{ width: 180 }}
        />
        {canWrite && selected.length > 0 ? (
          <Space size={8}>
            <span className="text-sm text-slate-500">已选 {selected.length} 项</span>
            <Button size="small" loading={bulk.isPending} disabled={anyActive} onClick={() => runBulk('retry')}>
              重试
            </Button>
            <Button size="small" danger loading={bulk.isPending} disabled={!allOrphaned} onClick={() => runBulk('delete_remote')}>
              删除远端
            </Button>
            <Button size="small" loading={bulk.isPending} disabled={!allOrphaned} onClick={() => runBulk('forget')}>
              移除记录
            </Button>
          </Space>
        ) : null}
      </div>
      {!canWrite ? null : status === '' && !driveId ? (
        <Alert className="mx-6 mb-3" type="info" showIcon title="选择「失败 / 已跳过 / 规则外 / 路径偏离」状态，可筛选出需要处理的副本并批量操作。" />
      ) : null}
      {replicas.isPending ? <LoadingState /> : null}
      {replicas.isError ? <ErrorState error={replicas.error} onRetry={() => void replicas.refetch()} /> : null}
      {replicas.isSuccess ? (
        <Table<Replica>
          rowKey="id"
          size="middle"
          dataSource={replicas.data.items}
          columns={columns}
          scroll={{ x: 1100 }}
          rowSelection={
            canWrite
              ? {
                  selectedRowKeys: selected,
                  onChange: (keys) => setSelected(keys.map(String)),
                }
              : undefined
          }
          locale={{ emptyText: <EmptyState /> }}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total: replicas.data.total,
            showSizeChanger: false,
            showTotal: (total) => `共 ${total} 个副本`,
            onChange: (next) => {
              setPage(next);
              setSelected([]);
            },
          }}
        />
      ) : null}
    </Card>
  );
}
