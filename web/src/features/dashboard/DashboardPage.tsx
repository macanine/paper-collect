import type { ReactNode } from 'react';
import { Anchor, Group, Paper, Skeleton, SimpleGrid, Stack, Text, ThemeIcon, Title, UnstyledButton } from '@mantine/core';
import { IconCircleCheck, IconFileSearch, IconFolders, IconHourglass, IconInbox, IconRefresh } from '@tabler/icons-react';
import { Link } from 'react-router';
import { formatDateTime } from '../../shared/lib/format';
import { ItemList, ItemRow } from '../../shared/ui/ItemList';
import { LinkButton } from '../../shared/ui/LinkButton';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, InlineError } from '../../shared/ui/States';
import { Tag, UploadStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { Completeness, PackMeta } from '../packs/PackBadges';
import { useLibraryStats, useRecentPacks, useRecentUploads } from './queries';
import { href } from '../../app/routes';

function StatCard({ to, title, value, icon, color = 'blue' }: { to: string; title: string; value: number | undefined; icon: ReactNode; color?: string }) {
  return (
    <UnstyledButton component={Link} to={to}>
      <Paper p="md" h="100%">
        <Group gap="md" wrap="nowrap">
          <ThemeIcon variant="light" color={color} size={40} radius="md">{icon}</ThemeIcon>
          <Stack gap={0}>
            <Text size="sm" c="dimmed">{title}</Text>
            <Text fz={26} fw={600} lh={1.2}>{value ?? '—'}</Text>
          </Stack>
        </Group>
      </Paper>
    </UnstyledButton>
  );
}

function Section({ title, extra, children }: { title: string; extra?: ReactNode; children: ReactNode }) {
  return (
    <Stack gap="sm" miw={0}>
      <Group justify="space-between">
        <Title order={4}>{title}</Title>
        {extra}
      </Group>
      {children}
    </Stack>
  );
}

function ListSkeleton() {
  return (
    <Paper p="md" aria-label="正在加载">
      <Stack gap="sm"><Skeleton h={14} w="68%" /><Skeleton h={12} /><Skeleton h={12} w="80%" /></Stack>
    </Paper>
  );
}

export function DashboardPage() {
  const { can } = useAuth();
  const canRead = can('materials:read');
  const canUpload = can('uploads:write');
  const stats = useLibraryStats(canRead);
  const packs = useRecentPacks(canRead);
  const uploads = useRecentUploads(canUpload);
  const s = stats.data;

  return (
    <Stack gap="lg">
      <PageHeader
        title="工作台"
        description="查看资料整理进度、最近更新和需要你处理的事项。"
        extra={canRead ? <LinkButton variant="default" to="/library">浏览资料库</LinkButton> : null}
      />
      {stats.error ? <InlineError error={stats.error} /> : null}

      {canRead ? (
        <SimpleGrid cols={{ base: 1, xs: 2, md: 3 }} spacing="md">
          <StatCard to="/inbox" title="需关注" value={s?.needs_attention} icon={<IconInbox size={22} />} color={s && s.needs_attention > 0 ? 'yellow' : 'blue'} />
          <StatCard to="/inbox?stage=processing" title="AI 整理中" value={s?.processing} icon={<IconRefresh size={22} />} />
          <StatCard to="/inbox?stage=waiting_ai" title="等待 AI" value={s?.waiting_ai} icon={<IconHourglass size={22} />} color={s && s.waiting_ai > 0 ? 'red' : 'blue'} />
          <StatCard to="/library?missing=answers" title="缺答案的试卷" value={s?.missing_answers} icon={<IconFileSearch size={22} />} />
          <StatCard to="/library" title="套卷" value={s?.packs} icon={<IconFolders size={22} />} />
          <StatCard to="/library" title="已入库文件" value={s?.ready} icon={<IconCircleCheck size={22} />} color="green" />
        </SimpleGrid>
      ) : null}

      {s && s.attention > 0 && can('storage:read') ? (
        <Paper p="sm" px="md">
          <Text size="sm">存储：{s.attention} 个文件同步需要注意。<Anchor component={Link} to="/storage" ml="xs" size="sm">查看存储</Anchor></Text>
        </Paper>
      ) : null}

      <SimpleGrid cols={{ base: 1, md: canRead && canUpload ? 2 : 1 }} spacing="lg">
        {canRead ? (
          <Section title="最近更新的套卷" extra={<Anchor component={Link} to="/library" size="sm">全部</Anchor>}>
            {packs.isPending ? <ListSkeleton /> : packs.data?.items.length ? (
              <ItemList>
                {packs.data.items.map((pack) => (
                  <ItemRow
                    key={pack.id}
                    main={
                      <Stack gap={4}>
                        <Anchor component={Link} to={href.pack(pack.id)} fw={600} c="inherit" truncate>{pack.title}</Anchor>
                        <Group gap="sm" wrap="wrap"><PackMeta pack={pack} /><Completeness pack={pack} /></Group>
                      </Stack>
                    }
                  />
                ))}
              </ItemList>
            ) : <Paper><EmptyState description="还没有资料，把文件拖到页面任意位置即可导入" /></Paper>}
          </Section>
        ) : null}
        {canUpload ? (
          <Section title="最近导入" extra={<Anchor component={Link} to="/imports" size="sm">导入中心</Anchor>}>
            {uploads.isPending ? <ListSkeleton /> : uploads.data?.items.length ? (
              <ItemList>
                {uploads.data.items.map((upload) => (
                  <ItemRow
                    key={upload.id}
                    main={
                      <Stack gap={4}>
                        <Anchor component={Link} to={href.upload(upload.id)} fw={600} c="inherit" truncate>{upload.title}</Anchor>
                        <Group gap={6}>
                          <Text size="sm" c="dimmed">
                            {formatDateTime(upload.created_at)}
                            {upload.status === 'completed' ? ` · 新增 ${upload.new_count} · 已存在 ${upload.duplicate_count}` : ''}
                          </Text>
                          {upload.status === 'completed' && upload.skipped.length > 0 ? <Tag>跳过 {upload.skipped.length}</Tag> : null}
                        </Group>
                      </Stack>
                    }
                    aside={<UploadStatusTag status={upload.status} />}
                  />
                ))}
              </ItemList>
            ) : <Paper><EmptyState description="暂无导入" /></Paper>}
          </Section>
        ) : null}
      </SimpleGrid>
    </Stack>
  );
}
