import { Tag, Timeline, Typography } from 'antd';
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
  if (logs.length === 0) return <Typography.Text type="secondary">{progressText(job)}。此任务没有保存更早的处理记录。</Typography.Text>;
  const started = new Date(logs[0]?.time ?? job.started_at ?? job.created_at).getTime();
  return (
    <Timeline items={logs.map((entry) => ({
      color: entry.level === 'error' ? 'red' : 'blue',
      content: (
        <div className="job-log">
          <div className="job-log__message">
            {entry.level === 'error' ? <Tag color="red">错误</Tag> : null}
            <span>{entry.message}</span>
          </div>
          <div className="job-log__meta">
            <span>{formatDateTime(entry.time)}</span>
            <span>T+{formatDuration(entry.elapsed_ms ?? Math.max(0, new Date(entry.time).getTime() - started))}</span>
            {entry.duration_ms ? <span>耗时 {formatDuration(entry.duration_ms)}</span> : null}
          </div>
          {entry.metrics ? <JobMetrics metrics={entry.metrics} /> : null}
        </div>
      ),
    }))} />
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
    <div className="job-log__metrics">
      {entries.map(([key, value]) => (
        <Tag key={key}>
          {METRIC_LABELS[key] ?? key}：{metricText(key, value)}
        </Tag>
      ))}
    </div>
  );
}
