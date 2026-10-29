import { Timeline, Typography } from 'antd';
import type { Job } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';

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
  return (
    <Timeline items={logs.map((entry) => ({
      color: entry.level === 'error' ? 'red' : 'blue',
      content: <div><div>{entry.message}</div><Typography.Text type="secondary" className="text-xs">{formatDateTime(entry.time)}</Typography.Text></div>,
    }))} />
  );
}
