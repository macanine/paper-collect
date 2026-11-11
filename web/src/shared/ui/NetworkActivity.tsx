import { useMemo, useState, useSyncExternalStore } from 'react';
import { Box, Button, Drawer, Group, Paper, SimpleGrid, Stack, Text, Tooltip, UnstyledButton } from '@mantine/core';
import { IconApi, IconBolt, IconTrash } from '@tabler/icons-react';
import { networkTelemetry, summarizeNetwork, type NetworkSample } from '../api/telemetry';
import { formatBytes, shortId } from '../lib/format';
import { CopyButton } from './CopyButton';
import { DataTable, type Column } from './DataTable';
import { EmptyState } from './States';
import { Tag } from './StatusTags';

const clockFormatter = new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

const OPERATION_LABELS: Record<NetworkSample['operation'], string> = { request: 'API', download: '下载', upload: '上传' };

function latencyText(value: number): string {
  if (value < 1) return '<1 ms';
  if (value < 1000) return `${Math.round(value)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
}

function statusTag(sample: NetworkSample) {
  if (sample.status === 0) return <Tag color="error">连接失败</Tag>;
  if (sample.status < 400) return <Tag color="success">{sample.status}</Tag>;
  if (sample.status < 500) return <Tag color="warning">{sample.status}</Tag>;
  return <Tag color="error">{sample.status}</Tag>;
}

const COLUMNS: Column<NetworkSample>[] = [
  { key: 'time', title: '时间', width: 84, render: (s) => <Text size="xs" c="dimmed" ff="monospace">{clockFormatter.format(s.startedAt)}</Text> },
  {
    key: 'request',
    title: '请求',
    render: (s) => (
      <Stack gap={2} miw={0}>
        <Group gap="xs" wrap="nowrap">
          <Tag>{s.method}</Tag>
          <Text size="xs" ff="monospace" truncate="end" maw={260} title={s.path}>{s.path}</Text>
        </Group>
        <Text size="xs" c="dimmed">{OPERATION_LABELS[s.operation]}</Text>
      </Stack>
    ),
  },
  { key: 'status', title: '结果', width: 90, render: statusTag },
  {
    key: 'duration', title: '耗时', width: 90, align: 'right',
    render: (s) => <Text size="sm" c={s.durationMs >= 3000 ? 'red' : s.durationMs >= 1000 ? 'orange' : undefined}>{latencyText(s.durationMs)}</Text>,
  },
  { key: 'bytes', title: '数据', width: 90, align: 'right', render: (s) => <Text size="xs" c="dimmed">{s.bytes === undefined ? '—' : formatBytes(s.bytes)}</Text> },
  {
    key: 'rid', title: '请求 ID', width: 170,
    render: (s) => s.requestId ? (
      <Group gap={6} wrap="nowrap">
        <Tooltip label={s.requestId}><Text size="xs" ff="monospace">{shortId(s.requestId)}</Text></Tooltip>
        <CopyButton value={s.requestId} label="复制" />
      </Group>
    ) : <Text size="xs" c="dimmed">—</Text>,
  },
];

function Stat({ label, value, hint }: { label: string; value: string | number; hint: string }) {
  return (
    <Paper p="sm">
      <Text size="xs" c="dimmed">{label}</Text>
      <Text fw={600} fz={22} lh={1.3}>{value}</Text>
      <Text size="xs" c="dimmed">{hint}</Text>
    </Paper>
  );
}

export function NetworkActivity() {
  const [open, setOpen] = useState(false);
  const telemetry = useSyncExternalStore(networkTelemetry.subscribe, networkTelemetry.getSnapshot, networkTelemetry.getSnapshot);
  const summary = useMemo(() => summarizeNetwork(telemetry.samples), [telemetry.samples]);
  const recent = useMemo(() => [...telemetry.samples].reverse(), [telemetry.samples]);
  const latest = telemetry.samples.at(-1);
  const live = telemetry.inflight > 0;

  return (
    <>
      <UnstyledButton onClick={() => setOpen(true)} aria-label="打开网络与性能观测" visibleFrom="lg" style={{ border: '1px solid var(--mantine-color-gray-3)', borderRadius: 'var(--mantine-radius-md)', padding: '0 10px', height: 32 }}>
        <Group gap={6} h="100%" wrap="nowrap">
          {live ? <IconBolt size={15} color="var(--mantine-color-blue-6)" /> : <IconApi size={15} color="var(--mantine-color-dimmed)" />}
          <Text size="xs">网络</Text>
          <Text size="xs" c="dimmed">{live ? `${telemetry.inflight} 项进行中` : latest ? latencyText(latest.durationMs) : '等待请求'}</Text>
        </Group>
      </UnstyledButton>

      <Drawer
        opened={open}
        onClose={() => setOpen(false)}
        position="right"
        size="min(720px, 100vw)"
        title={<Stack gap={0}><Text fw={600}>网络与性能</Text><Text size="xs" c="dimmed">当前浏览器会话 · 不记录正文、凭据或查询参数</Text></Stack>}
      >
        <Stack gap="md">
          <Group justify="flex-end">
            <Button size="compact-sm" variant="default" leftSection={<IconTrash size={14} />} disabled={telemetry.samples.length === 0} onClick={() => networkTelemetry.clear()}>清空</Button>
          </Group>
          <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="sm">
            <Stat label="P95 延迟" value={latencyText(summary.p95Ms)} hint={`最近 ${summary.sampleCount} 次采样`} />
            <Stat label="成功率" value={`${Math.round(summary.successRate * 100)}%`} hint={`${summary.errorCount} 次失败`} />
            <Stat label="传输数据" value={formatBytes(telemetry.totalBytes)} hint="响应体与文件总量" />
            <Stat label="进行中" value={telemetry.inflight} hint={`已完成 ${telemetry.completed} 次`} />
          </SimpleGrid>
          <Paper p="sm">
            <Group justify="space-between" mb="xs">
              <Text size="sm" fw={600}>延迟轨迹</Text>
              <Text size="xs" c="dimmed">平均 {latencyText(summary.averageMs)} · 中位 {latencyText(summary.medianMs)}</Text>
            </Group>
            <LatencyBars samples={telemetry.samples.slice(-32)} />
          </Paper>
          <DataTable rows={recent} columns={COLUMNS} rowKey={(s) => s.id} minWidth={640} empty={<EmptyState description="发出请求后，这里会显示真实的网络与耗时数据" />} />
        </Stack>
      </Drawer>
    </>
  );
}

function LatencyBars({ samples }: { samples: readonly NetworkSample[] }) {
  if (samples.length === 0) return <Text size="sm" c="dimmed" ta="center" py="md">暂无网络采样</Text>;
  const max = Math.max(1, ...samples.map((s) => s.durationMs));
  return (
    <Group gap={3} align="flex-end" h={72} wrap="nowrap" aria-label="最近请求延迟柱状图">
      {samples.map((s) => (
        <Tooltip key={s.id} label={`${s.method} ${s.path} · ${latencyText(s.durationMs)}${s.ok ? '' : ' · 失败'}`}>
          <Box flex={1} h={`${Math.max(5, Math.round((s.durationMs / max) * 100))}%`} bg={s.ok ? 'blue.4' : 'red.5'} style={{ borderRadius: 2, minWidth: 3 }} />
        </Tooltip>
      ))}
    </Group>
  );
}

