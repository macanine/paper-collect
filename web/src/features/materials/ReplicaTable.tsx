import { Button, Table, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { CopyButton } from '../../shared/ui/CopyButton';
import { ReplicaStatusTag, ShareStatusTag, VerificationTag } from '../../shared/ui/StatusTags';
import { formatBytes, formatDateTime, shortId } from '../../shared/lib/format';
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
          return <span className="font-mono text-xs text-slate-500">端点 {shortId(record.provider_id)}</span>;
        }
        return <span>{provider.name}</span>;
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
      render: (_: unknown, record) => (
        <div className="flex flex-col gap-1">
          <ShareStatusTag status={record.share_status} />
          {record.share_url ? (
            <div className="flex min-w-0 items-center gap-2">
              <a href={record.share_url} target="_blank" rel="noreferrer noopener" className="min-w-0 truncate text-xs">
                {record.share_url}
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
        </div>
      ),
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
      scroll={{ x: 1100 }}
      locale={{ emptyText: '暂无副本' }}
    />
  );
}
