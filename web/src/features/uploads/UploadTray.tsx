import { useState } from 'react';
import { ActionIcon, Anchor, Box, Button, Group, Paper, Progress, Stack, Text } from '@mantine/core';
import { IconChevronDown, IconChevronUp, IconX } from '@tabler/icons-react';
import { Link } from 'react-router';
import { formatBytes } from '../../shared/lib/format';
import { Tag, type TagColor } from '../../shared/ui/StatusTags';
import { useUploads, type UploadBatch } from './UploadManager';
import { href } from '../../app/routes';

const PHASE_TEXT: Record<UploadBatch['phase'], string> = {
  preparing: '比对内容',
  uploading: '传输中',
  processing: '去重、识别中',
  done: '已完成',
  failed: '失败',
};

const PHASE_COLOR: Record<UploadBatch['phase'], TagColor> = {
  preparing: 'default',
  uploading: 'processing',
  processing: 'processing',
  done: 'success',
  failed: 'error',
};

function BatchRow({ batch, onDismiss }: { batch: UploadBatch; onDismiss: () => void }) {
  const percent = batch.totalBytes > 0 ? Math.round((batch.sentBytes / batch.totalBytes) * 100) : 0;
  const finished = batch.phase === 'done' || batch.phase === 'failed';
  return (
    <Stack gap="xs" p="sm" miw={0} style={{ borderBottom: '1px solid var(--mantine-color-gray-2)' }}>
      <Group justify="space-between" wrap="nowrap" gap="xs">
        <Text fw={600} size="sm" truncate flex={1} miw={0} title={batch.title}>
          {batch.uploadId ? <Anchor component={Link} to={href.upload(batch.uploadId)} c="inherit" size="sm" fw={600}>{batch.title}</Anchor> : batch.title}
        </Text>
        <Tag color={PHASE_COLOR[batch.phase]}>{PHASE_TEXT[batch.phase]}</Tag>
        {finished ? <ActionIcon variant="subtle" color="gray" size="sm" aria-label="移除" onClick={onDismiss}><IconX size={14} /></ActionIcon> : null}
      </Group>
      {batch.phase === 'uploading' || batch.phase === 'preparing' ? <Progress size="sm" value={percent} animated /> : null}
      {batch.phase === 'done' ? (
        <Text size="xs" c="dimmed">新增 {batch.newCount ?? 0} · 已存在 {batch.duplicateCount ?? 0}{batch.skipped.length > 0 ? ` · 跳过 ${batch.skipped.length}` : ''}</Text>
      ) : batch.phase === 'failed' ? (
        <Text size="xs" c="red" lineClamp={2} title={batch.error}>{batch.error}</Text>
      ) : (
        <Text size="xs" c="dimmed">{batch.fileCount} 个文件 · {formatBytes(batch.totalBytes)}{batch.knownCount > 0 ? ` · ${batch.knownCount} 个已存在免传` : ''}</Text>
      )}
    </Stack>
  );
}

/** 右下角常驻的导入进度：不打断当前页面。 */
export function UploadTray() {
  const { batches, dismiss, clearFinished } = useUploads();
  const [collapsed, setCollapsed] = useState(false);
  if (batches.length === 0) return null;
  const active = batches.filter((b) => b.phase !== 'done' && b.phase !== 'failed').length;
  return (
    <Paper component="section" aria-label="导入进度" shadow="md" pos="fixed" bottom={16} right={16} w={{ base: 'calc(100vw - 32px)', sm: 360 }} style={{ zIndex: 200, overflow: 'hidden' }}>
      <Group justify="space-between" px="sm" py={8} bg="gray.0" style={{ borderBottom: collapsed ? undefined : '1px solid var(--mantine-color-gray-2)' }}>
        <Text fw={600} size="sm">{active > 0 ? `正在导入 ${active} 批` : '导入完成'}</Text>
        <Group gap={4}>
          {active < batches.length ? <Button variant="subtle" size="compact-sm" onClick={clearFinished}>清除已完成</Button> : null}
          <ActionIcon variant="subtle" color="gray" aria-label={collapsed ? '展开' : '收起'} onClick={() => setCollapsed((v) => !v)}>{collapsed ? <IconChevronUp size={16} /> : <IconChevronDown size={16} />}</ActionIcon>
        </Group>
      </Group>
      {collapsed ? null : (
        <Box>
          <Box mah={320} style={{ overflowY: 'auto' }}>
            {batches.map((batch) => <BatchRow key={batch.key} batch={batch} onDismiss={() => dismiss(batch.key)} />)}
          </Box>
          {batches.some((b) => b.phase === 'done') ? <Box px="sm" py={8}><Anchor component={Link} to="/inbox" size="sm">查看需关注 →</Anchor></Box> : null}
        </Box>
      )}
    </Paper>
  );
}
