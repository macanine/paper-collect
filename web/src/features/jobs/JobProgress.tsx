import { Group, Stack, Text, Timeline } from '@mantine/core';
import { Tag } from '../../shared/ui/StatusTags';
import type { Job } from '../../shared/api/types';
import { formatBytes, formatDateTime, formatDuration, formatRate } from '../../shared/lib/format';

export function progressText(job: Job): string {
  if (job.progress_message) return job.progress_message;
  if (job.status === 'failed') return job.error ? `处理失败：${job.error}` : '处理失败，请重试';
  if (job.status === 'succeeded') return '处理完成';
  if (job.status === 'running') return '正在处理';
  return '等待执行';
}

export function JobProgress({ job }: { job: Job }) {
  const logs = job.logs ?? [];
  if (logs.length === 0) return <Text c="dimmed" size="sm">{progressText(job)}。此任务没有保存更早的处理记录。</Text>;
  const started = new Date(logs[0]?.time ?? job.started_at ?? job.created_at).getTime();
  return (
    <Timeline bulletSize={14} lineWidth={2} active={logs.length}>
      {logs.map((entry, index) => (
        <Timeline.Item key={index} color={entry.level === 'error' ? 'red' : 'blue'}>
          <Stack gap={4}>
            <Group gap={6} wrap="nowrap" align="flex-start">
              {entry.level === 'error' ? <Tag color="error">错误</Tag> : null}
              <Text size="sm">{entry.message}</Text>
            </Group>
            <Group gap="md">
              <Text size="xs" c="dimmed">{formatDateTime(entry.time)}</Text>
              <Text size="xs" c="dimmed">T+{formatDuration(entry.elapsed_ms ?? Math.max(0, new Date(entry.time).getTime() - started))}</Text>
              {entry.duration_ms ? <Text size="xs" c="dimmed">耗时 {formatDuration(entry.duration_ms)}</Text> : null}
            </Group>
            {entry.metrics ? <JobMetrics metrics={entry.metrics} /> : null}
          </Stack>
        </Timeline.Item>
      ))}
    </Timeline>
  );
}

const METRIC_LABELS: Record<string, string> = {
  provider: '端点',
  ocr_provider: 'OCR',
  model: '模型',
  size_bytes: '数据量',
  throughput_bytes_per_s: '吞吐',
  page_count: '页数',
  file_count: '文件',
  archive_count: '压缩包',
  skipped_count: '跳过',
  question_count: '题目',
  llm_calls: '模型调用',
  failed_batches: '失败批次',
  verification: '校验',
  share_status: '分享',
  duration_ms: '耗时',
};

function metricText(key: string, value: string | number | boolean): string {
  if (key === 'duration_ms' && typeof value === 'number') return formatDuration(value);
  if (key === 'size_bytes' && typeof value === 'number') return formatBytes(value);
  if (key === 'throughput_bytes_per_s' && typeof value === 'number') return formatRate(value);
  return String(value);
}

function JobMetrics({ metrics }: { metrics: Record<string, string | number | boolean> }) {
  const entries = Object.entries(metrics).filter(([key]) => key !== 'duration_ms');
  if (entries.length === 0) return null;
  return (
    <Group gap={4}>
      {entries.map(([key, value]) => (
        <Tag key={key}>
          {METRIC_LABELS[key] ?? key}：{metricText(key, value)}
        </Tag>
      ))}
    </Group>
  );
}
