import { useState } from 'react';
import { Anchor, Button, Group, Paper, Stack, Text } from '@mantine/core';
import { Link } from 'react-router';
import type { Pack } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ConfirmButton, message } from '../../shared/ui/feedback';
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

  const columns: Column<Pack>[] = [
    {
      key: 'title', wrap: true,
      title: '套卷',
      render: (pack) => (
        <Stack gap={4}>
          <Anchor component={Link} to={href.pack(pack.id)} fw={600} c="inherit">{pack.title}</Anchor>
          <PackMeta pack={pack} />
        </Stack>
      ),
    },
    { key: 'files', title: '文件', width: 70, render: (pack) => pack.members.length },
    { key: 'deleted', title: '删除时间', width: 170, render: (pack) => formatDateTime(pack.deleted_at) },
    { key: 'left', title: '剩余', width: 90, render: (pack) => `${daysLeft(pack.deleted_at)} 天` },
    {
      key: 'actions',
      width: 180,
      align: 'right',
      render: (pack) => (
        <Group gap="xs" justify="flex-end" wrap="nowrap">
          <Button variant="subtle" size="compact-sm" onClick={() => void run('restore', [pack.id])}>恢复</Button>
          <ConfirmButton variant="subtle" color="red" size="compact-sm" confirmTitle="彻底删除？" confirmDescription={PURGE_NOTE} okText="彻底删除" danger onConfirm={() => run('purge', [pack.id])}>彻底删除</ConfirmButton>
        </Group>
      ),
    },
  ];

  return (
    <Stack gap="lg">
      <PageHeader title="回收站" description={`删除的套卷会在这里保留 ${RETENTION_DAYS} 天，之后自动彻底删除。再次上传同一文件也会自动恢复。`} />
      {selected.length > 0 ? (
        <Paper p="sm" px="md">
          <Group>
            <Text fw={600} size="sm">已选 {selected.length} 个</Text>
            <Button size="compact-md" onClick={() => void run('restore', selected)}>恢复</Button>
            <ConfirmButton size="compact-md" variant="default" color="red" confirmTitle="彻底删除后无法恢复，确定？" confirmDescription={PURGE_NOTE} okText="彻底删除" danger onConfirm={() => run('purge', selected)}>彻底删除</ConfirmButton>
          </Group>
        </Paper>
      ) : null}
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <DataTable
          rows={query.data?.items ?? []}
          columns={columns}
          rowKey={(p) => p.id}
          loading={query.isFetching}
          selected={selected}
          onSelectedChange={(keys) => setSelected(keys as string[])}
          empty={<EmptyState description="回收站是空的" />}
          pagination={{
            page: list.page, pageSize: list.pageSize, total: query.data?.total ?? 0,
            onChange: (page, pageSize) => { setSelected([]); list.setPage(page, pageSize); },
          }}
        />
      )}
    </Stack>
  );
}
