import type { ClusterNode, NodeKind, NodePoint, OCRProvider } from '../../shared/api/types';

export const NODE_POLL_MS = 10_000;

/** 只有近期有心跳的节点，其指标才代表当前状态。 */
export function isLive(node: ClusterNode): boolean {
  return node.status === 'online' || node.status === 'draining';
}

export function totalSlots(slots: ClusterNode['slots']): number {
  return Object.values(slots).reduce<number>((sum, count) => sum + (count ?? 0), 0);
}

const KIND_ORDER: NodeKind[] = ['replicate', 'analyze', 'index'];
const GIB = 1024 ** 3;

const DEFAULT_NODE_SLOTS: Record<NodeKind, number> = {
  replicate: 2,
  analyze: 1,
  index: 1,
};

const DEFAULT_NODE_CACHE_GIB = 20;

export interface NodeRuntimeFormValues {
  slots: Partial<Record<NodeKind, number>>;
  cache_max_gib: number;
  ocr_provider_ids: string[];
}

export interface NodeRuntimeBody {
  slots: Partial<Record<NodeKind, number>>;
  cache_max_bytes: number;
  ocr_provider_ids: string[];
}

export function orderedKinds(kinds: NodeKind[]): NodeKind[] {
  return KIND_ORDER.filter((kind) => kinds.includes(kind));
}

export function runtimeFormValues(node?: ClusterNode): NodeRuntimeFormValues {
  return {
    slots: Object.fromEntries(
      KIND_ORDER.map((kind) => [kind, node ? node.configured_slots[kind] ?? 0 : DEFAULT_NODE_SLOTS[kind]]),
    ),
    cache_max_gib: node ? Math.round(node.cache_max_bytes / GIB) : DEFAULT_NODE_CACHE_GIB,
    ocr_provider_ids: node ? [...node.ocr_provider_ids] : [],
  };
}

export function runtimeBody(values: NodeRuntimeFormValues): NodeRuntimeBody {
  return {
    slots: Object.fromEntries(
      KIND_ORDER.flatMap((kind) => {
        const count = Math.max(0, Math.min(64, Math.trunc(values.slots?.[kind] ?? 0)));
        return count > 0 ? [[kind, count]] : [];
      }),
    ),
    cache_max_bytes: Math.max(0, Math.round((values.cache_max_gib ?? 0) * GIB)),
    ocr_provider_ids: values.ocr_provider_ids ?? [],
  };
}

export function ocrProviderLabel(provider: OCRProvider): string {
  const kind = provider.kind === 'paddleocr' ? 'PaddleOCR' : provider.kind === 'rapidocr' ? 'RapidOCR' : 'MinerU';
  return `${provider.name} · ${kind}${provider.model ? ` · ${provider.model}` : ''} · 优先级 ${provider.priority}`;
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
  return [`ROLE=worker`, `MASTER_URL=${origin}`, `NODE_TOKEN=${token}`, `CACHE_DIR=/app/cache`].join('\n');
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
