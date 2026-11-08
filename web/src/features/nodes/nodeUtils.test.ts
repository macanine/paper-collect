import { describe, expect, it } from 'vitest';
import type { ClusterNode, NodePoint } from '../../shared/api/types';
import { formatAgo, linePath, originNeedsTLS, runtimeFormValues, totalSlots, workerEnv } from './nodeUtils';

const point = (time: string, cpu: number): NodePoint => ({
  time, cpu_percent: cpu, mem_used: 0, mem_total: 0, net_rx_bps: 0, net_tx_bps: 0, running: 0,
});

describe('节点工具', () => {
  it('把长时间缺失的采样画成断开的折线', () => {
    const path = linePath(
      [point('2026-01-01T00:00:00Z', 0), point('2026-01-01T00:00:10Z', 50), point('2026-01-01T00:01:40Z', 100)],
      (p) => p.cpu_percent, 100, 100, 100,
    );
    expect(path.match(/M/g)).toHaveLength(2);
    expect(path.startsWith('M0.0 100.0')).toBe(true);
    expect(path.endsWith('100.0 0.0')).toBe(true);
  });

  it('没有采样时不画线', () => {
    expect(linePath([], (p) => p.cpu_percent, 100, 100, 100)).toBe('');
  });

  it('要求远程主端地址使用 HTTPS', () => {
    expect(originNeedsTLS('http://paper.example.com')).toBe(true);
    expect(originNeedsTLS('https://paper.example.com')).toBe(false);
    expect(originNeedsTLS('http://localhost:5173')).toBe(false);
  });

  it('生成从端配置并统计槽位', () => {
    const env = workerEnv('https://paper.example.com', 'nd-abc');
    expect(env).toContain('MASTER_URL=https://paper.example.com');
    expect(env).toContain('NODE_TOKEN=nd-abc');
    expect(env).toContain('CACHE_DIR=/app/cache');
    expect(env).not.toContain('WORKER_SLOTS');
    expect(totalSlots({ replicate: 2, index: 1 })).toBe(3);
  });

  it('把未配置的节点任务显式显示为零槽', () => {
    const node = {
      configured_slots: { replicate: 1 },
      cache_max_bytes: 8 * 1024 ** 3,
      ocr_provider_ids: ['ocr'],
    } as ClusterNode;
    expect(runtimeFormValues(node)).toEqual({
      slots: { replicate: 1, analyze: 0, index: 0 },
      cache_max_gib: 8,
      ocr_provider_ids: ['ocr'],
    });
  });

  it('格式化心跳时间', () => {
    const now = new Date('2026-01-01T00:02:00Z').getTime();
    expect(formatAgo('2026-01-01T00:01:57Z', now)).toBe('3 秒前');
    expect(formatAgo('2026-01-01T00:00:00Z', now)).toBe('2 分钟前');
    expect(formatAgo(null, now)).toBe('从未');
  });
});
