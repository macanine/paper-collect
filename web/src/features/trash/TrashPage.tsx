import { useState } from 'react';
import { App, Button, Card, Popconfirm, Space, Table, Typography } from 'antd';
import { Link } from 'react-router';
import type { Pack } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { PackMeta } from '../packs/PackBadges';
import { usePackMutations, usePacksList } from '../packs/queries';
import { href } from '../../app/routes';

const RETENTION_DAYS = 30;

/** 彻底删除只清理本机文件；已上传到云盘的副本保留为规则外副本。 */
export const PURGE_NOTE = '本机原件会被删除；云盘上的副本保留，可在“存储 → 副本”中删除。';

function daysLeft(deletedAt: string | null): number {
  if (!deletedAt) return RETENTION_DAYS;
  const elapsed = (Date.now() - new Date(deletedAt).getTime()) / 86_400_000;
  return Math.max(0, Math.ceil(RETENTION_DAYS - elapsed));
}

export function TrashPage() {
  const { message } = App.useApp();
  const list = useListSearch([] as const);
  const query = usePacksList({ page: list.page, page_size: list.pageSize, trashed: true });
  const { restore, purge } = usePackMutations();
  const [selected, setSelected] = useState<string[]>([]);

  const run = async (action: 'restore' | 'purge', ids: string[]) => {
    try {
      const result = action === 'restore' ? await restore.mutateAsync(ids) : await purge.mutateAsync(ids);
      message.success(action === 'restore' ? `已恢复 ${result.count} 个套卷` : `已彻底删除 ${result.count} 个套卷`);
      setSelected([]);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  return (
    <div className="page-stack">
      <PageHeader title="回收站" description={`删除的套卷会在这里保留 ${RETENTION_DAYS} 天，之后自动彻底删除。再次上传同一文件也会自动恢复。`} />
      {selected.length > 0 ? (
        <Card size="small">
          <Space wrap>
            <Typography.Text strong>已选 {selected.length} 个</Typography.Text>
            <Button type="primary" onClick={() => void run('restore', selected)}>
              恢复
            </Button>
            <Popconfirm title="彻底删除后无法恢复，确定？" description={PURGE_NOTE} okButtonProps={{ danger: true }} onConfirm={() => void run('purge', selected)}>
              <Button danger>彻底删除</Button>
            </Popconfirm>
          </Space>
        </Card>
      ) : null}
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<Pack>
            rowKey="id"
            loading={query.isFetching}
            dataSource={query.data?.items ?? []}
            rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys as string[]) }}
            locale={{ emptyText: <EmptyState description="回收站是空的" /> }}
            pagination={{ current: list.page, pageSize: list.pageSize, total: query.data?.total ?? 0, onChange: (page, pageSize) => {
                // A bulk action only applies to rows the person can see.
                setSelected([]);
                list.setPage(page, pageSize);
              },
              hideOnSinglePage: true }}
            columns={[
              {
                title: '套卷',
                key: 'title',
                render: (_: unknown, pack) => (
                  <div className="flex flex-col gap-1">
                    <Link to={href.pack(pack.id)}>{pack.title}</Link>
                    <PackMeta pack={pack} />
                  </div>
                ),
              },
              { title: '文件', key: 'files', width: 70, render: (_: unknown, pack) => pack.members.length },
              { title: '删除时间', key: 'deleted', width: 170, render: (_: unknown, pack) => formatDateTime(pack.deleted_at) },
              { title: '剩余', key: 'left', width: 90, render: (_: unknown, pack) => `${daysLeft(pack.deleted_at)} 天` },
              {
                title: '',
                key: 'actions',
                width: 160,
                render: (_: unknown, pack) => (
                  <Space size={0}>
                    <Button type="link" size="small" onClick={() => void run('restore', [pack.id])}>
                      恢复
                    </Button>
                    <Popconfirm title="彻底删除？" description={PURGE_NOTE} okButtonProps={{ danger: true }} onConfirm={() => void run('purge', [pack.id])}>
                      <Button type="link" size="small" danger>
                        彻底删除
                      </Button>
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      )}
    </div>
  );
}
