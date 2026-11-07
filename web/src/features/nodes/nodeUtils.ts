import type { ClusterNode, NodeKind, NodePoint } from '../../shared/api/types';

export const NODE_POLL_MS = 10_000;

/** 只有近期有心跳的节点，其指标才代表当前状态。 */
export function isLive(node: ClusterNode): boolean {
  return node.status === 'online' || node.status === 'draining';
}

export function totalSlots(slots: ClusterNode['slots']): number {
  return Object.values(slots).reduce<number>((sum, count) => sum + (count ?? 0), 0);
}

const KIND_ORDER: NodeKind[] = ['replicate', 'analyze', 'index'];

export function orderedKinds(kinds: NodeKind[]): NodeKind[] {
  return KIND_ORDER.filter((kind) => kinds.includes(kind));
}

export function formatAgo(value: string | null, now: number): string {
  if (!value) return '从未';
  const seconds = Math.max(0, Math.round((now - new Date(value).getTime()) / 1000));
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return `${Math.floor(seconds / 86400)} 天前`;
}

/** 给从端使用的环境变量。令牌只在创建时可见。 */
export function workerEnv(origin: string, token: string): string {
  return [`ROLE=worker`, `MASTER_URL=${origin}`, `NODE_TOKEN=${token}`, `CACHE_DIR=/app/cache`, `WORKER_SLOTS=replicate=2,analyze=1,index=1`].join('\n');
}

/** 主端要求非本机地址使用 HTTPS，否则从端拒绝启动。 */
export function originNeedsTLS(origin: string): boolean {
  try {
    const url = new URL(origin);
    return url.protocol !== 'https:' && !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname);
  } catch {
    return false;
  }
}

/** 超过两个心跳周期没有采样的位置断开折线，不把缺失的数据画成平稳。 */
const GAP_MS = 35_000;

export function linePath(points: NodePoint[], value: (point: NodePoint) => number, width: number, height: number, max: number): string {
  const first = points[0];
  const last = points[points.length - 1];
  if (!first || !last || max <= 0) return '';
  const start = new Date(first.time).getTime();
  const span = Math.max(1, new Date(last.time).getTime() - start);
  let previous = 0;
  return points
    .map((point, index) => {
      const time = new Date(point.time).getTime();
      const x = ((time - start) / span) * width;
      const y = height - (Math.min(value(point), max) / max) * height;
      const command = index === 0 || time - previous > GAP_MS ? 'M' : 'L';
      previous = time;
      return `${command}${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(' ');
}
