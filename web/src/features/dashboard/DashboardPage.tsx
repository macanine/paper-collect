import { Alert, Anchor, Group, Paper, Skeleton, SimpleGrid, Stack, Text } from '@mantine/core';
import { Link } from 'react-router';
import { formatDateTime } from '../../shared/lib/format';
import { ItemList, ItemRow } from '../../shared/ui/ItemList';
import { LinkButton } from '../../shared/ui/LinkButton';
import { Metric } from '../../shared/ui/Metric';
import { PageHeader } from '../../shared/ui/PageHeader';
import { Section } from '../../shared/ui/Section';
import { EmptyState, InlineError } from '../../shared/ui/States';
import { Tag, UploadStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { Completeness, PackMeta } from '../packs/PackBadges';
import { useLibraryStats, useRecentPacks, useRecentUploads } from './queries';
import { href } from '../../app/routes';

/** 指标的说明文字本身是链接：悬停有下划线，键盘可达，数值保持醒目。 */
function StatLink({ to, label }: { to: string; label: string }) {
  return <Anchor component={Link} to={to} c="dimmed" size="sm">{label}</Anchor>;
}

/** 与真实行同高的占位：标题一行、元信息一行。 */
function ListSkeleton() {
  return (
    <ItemList bare>
      {[0, 1, 2].map((i) => (
        <ItemRow key={i} main={<Stack gap="xs" aria-hidden><Skeleton h={16} w="60%" /><Skeleton h={14} w="40%" /></Stack>} />
      ))}
    </ItemList>
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

  const summary = !s ? '查看资料整理进度、最近更新和需要你处理的事项。'
    : s.needs_attention > 0 ? `有 ${s.needs_attention} 个文件等你确认，其余资料由 AI 自动整理。`
    : '没有需要你处理的资料，AI 会继续整理新导入的文件。';

  return (
    <Stack gap="lg">
      <PageHeader
        title="工作台"
        description={summary}
        extra={canRead ? (
          s && s.needs_attention > 0
            ? <LinkButton variant="default" to="/inbox">去处理</LinkButton>
            : <LinkButton variant="default" to="/library">浏览资料库</LinkButton>
        ) : null}
      />
      {stats.error ? <InlineError error={stats.error} /> : null}

      {canRead ? (
        <Paper p="lg" aria-label="资料概况">
          <SimpleGrid cols={{ base: 2, sm: 3, lg: 6 }} spacing="lg" verticalSpacing="lg">
            <Metric size="xl" label={<StatLink to="/inbox" label="需关注" />} value={s?.needs_attention} />
            <Metric size="xl" label={<StatLink to="/inbox?stage=processing" label="AI 整理中" />} value={s?.processing} />
            <Metric size="xl" label={<StatLink to="/inbox?stage=waiting_ai" label="等待 AI" />} value={s?.waiting_ai} />
            <Metric size="xl" label={<StatLink to="/library?missing=answers" label="缺答案的试卷" />} value={s?.missing_answers} />
            <Metric size="xl" label={<StatLink to="/library" label="套卷" />} value={s?.packs} />
            <Metric size="xl" label={<StatLink to="/library" label="已入库文件" />} value={s?.ready} />
          </SimpleGrid>
        </Paper>
      ) : null}

      {s && s.attention > 0 && can('storage:read') ? (
        <Alert color="orange">
          {s.attention} 个文件的网盘同步需要注意。<Anchor component={Link} to="/storage" ml="xs" size="sm">查看存储</Anchor>
        </Alert>
      ) : null}

      <SimpleGrid cols={{ base: 1, md: canRead && canUpload ? 2 : 1 }} spacing="lg">
        {canRead ? (
          <Section flush title="最近更新的套卷" extra={<Anchor component={Link} to="/library" size="sm">全部</Anchor>}>
            {packs.isPending ? <ListSkeleton /> : packs.data?.items.length ? (
              <ItemList bare>
                {packs.data.items.map((pack) => (
                  <ItemRow
                    key={pack.id}
                    main={
                      <Stack gap={4}>
                        <Anchor component={Link} to={href.pack(pack.id)} fw={600} size="sm" c="inherit" truncate>{pack.title}</Anchor>
                        <PackMeta pack={pack} />
                      </Stack>
                    }
                    aside={<Completeness pack={pack} />}
                  />
                ))}
              </ItemList>
            ) : <EmptyState description="还没有资料，把文件拖到页面任意位置即可导入" />}
          </Section>
        ) : null}
        {canUpload ? (
          <Section flush title="最近导入" extra={<Anchor component={Link} to="/imports" size="sm">导入中心</Anchor>}>
            {uploads.isPending ? <ListSkeleton /> : uploads.data?.items.length ? (
              <ItemList bare>
                {uploads.data.items.map((upload) => (
                  <ItemRow
                    key={upload.id}
                    main={
                      <Stack gap={4}>
                        <Anchor component={Link} to={href.upload(upload.id)} fw={600} size="sm" c="inherit" truncate>{upload.title}</Anchor>
                        <Group gap="xs">
                          <Text size="sm" c="dimmed">
                            {formatDateTime(upload.created_at)}
                            {upload.status === 'completed' ? ` · 新增 ${upload.new_count} · 已存在 ${upload.duplicate_count}` : ''}
                          </Text>
                          {upload.status === 'completed' && upload.skipped.length > 0 ? <Tag color="warning">跳过 {upload.skipped.length}</Tag> : null}
                        </Group>
                      </Stack>
                    }
                    aside={<UploadStatusTag status={upload.status} />}
                  />
                ))}
              </ItemList>
            ) : <EmptyState description="暂无导入" />}
          </Section>
        ) : null}
      </SimpleGrid>
    </Stack>
  );
}
