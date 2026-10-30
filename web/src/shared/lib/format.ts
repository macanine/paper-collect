const KIB = 1024;
const MIB = KIB * 1024;
const GIB = MIB * 1024;

/** 以二进制单位格式化字节数，保留合理小数位。 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes < KIB) return `${bytes} B`;
  if (bytes < MIB) return `${(bytes / KIB).toFixed(1)} KB`;
  if (bytes < GIB) return `${(bytes / MIB).toFixed(1)} MB`;
  return `${(bytes / GIB).toFixed(2)} GB`;
}

/** 格式化后台任务耗时，保留用户能感知的精度。 */
export function formatDuration(milliseconds: number | null | undefined): string {
  if (milliseconds === null || milliseconds === undefined || !Number.isFinite(milliseconds) || milliseconds < 0) {
    return '—';
  }
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`;
  const seconds = milliseconds / 1000;
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)} s`;
  const minutes = Math.floor(seconds / 60);
  const rest = Math.floor(seconds % 60);
  if (minutes < 60) return `${minutes}m ${rest}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

/** 格式化网络或磁盘吞吐。 */
export function formatRate(bytesPerSecond: number | null | undefined): string {
  if (
    bytesPerSecond === null ||
    bytesPerSecond === undefined ||
    !Number.isFinite(bytesPerSecond) ||
    bytesPerSecond < 0
  ) {
    return '—';
  }
  return `${formatBytes(bytesPerSecond)}/s`;
}

const dateTimeFormatter = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
});

/** 服务端 UTC RFC3339 时间转为本地显示；空值显示占位符。 */
export function formatDateTime(value: string | null | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return dateTimeFormatter.format(date);
}

export function shortId(id: string): string {
  return id.length > 8 ? `${id.slice(0, 8)}…` : id;
}
