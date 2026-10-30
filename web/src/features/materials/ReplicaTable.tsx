import { Button, Table, Tag, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link } from 'react-router';
import { CopyButton } from '../../shared/ui/CopyButton';
import { ReplicaStatusTag, ShareStatusTag, VerificationTag } from '../../shared/ui/StatusTags';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import type { Replica, StorageProvider } from '../../shared/api/types';

interface ReplicaTableProps {
  replicas: Replica[];
  providers: Map<string, StorageProvider> | null;
  canRefreshShare: boolean;
  refreshingId: string | null;
  onRefreshShare: (replica: Replica) => void;
}

export function ReplicaTable({ replicas, providers, canRefreshShare, refreshingId, onRefreshShare }: ReplicaTableProps) {
  const columns: ColumnsType<Replica> = [
    {
      title: '端点',
      key: 'provider',
      width: 180,
      render: (_: unknown, record) => {
        const provider = providers?.get(record.provider_id);
        if (!provider) {
          return <span className="text-slate-500">存储端点</span>;
        }
        return <Link to={`/storage?provider=${provider.id}`}>{provider.name}</Link>;
      },
    },
    {
      title: '副本状态',
      dataIndex: 'status',
      key: 'status',
      width: 150,
      render: (_: unknown, record) => (
        <div className="flex flex-col items-start gap-1">
          <ReplicaStatusTag status={record.status} />
          {record.error ? (
            <Tooltip title={record.error}>
              <span className="block max-w-36 truncate text-xs text-red-600">{record.error}</span>
            </Tooltip>
          ) : null}
        </div>
      ),
    },
    {
      title: '校验级别',
      dataIndex: 'verification',
      key: 'verification',
      width: 150,
      render: (verification: Replica['verification']) => <VerificationTag verification={verification} />,
    },
    {
      title: '大小',
      dataIndex: 'size_bytes',
      key: 'size_bytes',
      width: 96,
      align: 'right',
      render: (size: number) => formatBytes(size),
    },
    {
      title: '分享',
      key: 'share',
      width: 320,
      render: (_: unknown, record) => {
        const expired = Boolean(record.share_expires_at && new Date(record.share_expires_at).getTime() <= Date.now());
        return <div className="flex flex-col gap-1">
          {expired ? <Tag color="warning">链接已过期</Tag> : <ShareStatusTag status={record.share_status} />}
          {record.share_url && !expired ? (
            <div className="flex min-w-0 items-center gap-2">
              <a href={record.share_url} target="_blank" rel="noreferrer noopener" className="min-w-0 truncate text-xs">
                打开分享链接
              </a>
              <CopyButton value={record.share_url} label="复制链接" />
            </div>
          ) : null}
          {record.share_password ? (
            <span className="text-xs text-slate-600">
              提取码：<span className="font-mono">{record.share_password}</span>
            </span>
          ) : null}
          {record.share_expires_at ? (
            <span className="text-xs text-slate-500">过期时间：{formatDateTime(record.share_expires_at)}</span>
          ) : null}
        </div>;
      },
    },
    {
      title: '任务',
      key: 'job',
      width: 100,
      render: (_: unknown, record) => <Link to={`/jobs?replica_id=${record.id}`}>复制任务</Link>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    ...(canRefreshShare
      ? [
          {
            title: '操作',
            key: 'actions',
            width: 110,
            fixed: 'right' as const,
            render: (_: unknown, record: Replica) => (
              <Button
                size="small"
                disabled={record.status !== 'ready'}
                loading={refreshingId === record.id}
                onClick={() => onRefreshShare(record)}
              >
                刷新分享
              </Button>
            ),
          },
        ]
      : []),
  ];

  return (
    <Table<Replica>
      rowKey="id"
      size="small"
      columns={columns}
      dataSource={replicas}
      pagination={false}
      scroll={{ x: 1200 }}
      locale={{ emptyText: '暂无副本' }}
    />
  );
}
