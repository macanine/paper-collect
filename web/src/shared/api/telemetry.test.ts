import { describe, expect, it } from 'vitest';
import { NetworkTelemetryStore, summarizeNetwork, type NetworkSample } from './telemetry';

function sample(durationMs: number, ok = true, bytes?: number): NetworkSample {
  return {
    id: String(durationMs),
    method: 'GET',
    path: '/materials',
    operation: 'request',
    startedAt: 0,
    durationMs,
    status: ok ? 200 : 500,
    ok,
    bytes,
  };
}

describe('summarizeNetwork', () => {
  it('计算成功率、平均延迟与 P95', () => {
    const summary = summarizeNetwork([
      sample(10),
      sample(20),
      sample(30),
      sample(40),
      sample(500, false, 1024),
    ]);
    expect(summary.sampleCount).toBe(5);
    expect(summary.successRate).toBe(0.8);
    expect(summary.averageMs).toBe(120);
    expect(summary.medianMs).toBe(30);
    expect(summary.p95Ms).toBe(500);
    expect(summary.errorCount).toBe(1);
  });

  it('没有采样时返回稳定空值', () => {
    expect(summarizeNetwork([])).toEqual({
      sampleCount: 0,
      successRate: 1,
      averageMs: 0,
      medianMs: 0,
      p95Ms: 0,
      slowestMs: 0,
      errorCount: 0,
    });
  });
});

describe('NetworkTelemetryStore', () => {
  it('清空历史指标但保留仍在进行的请求', () => {
    const store = new NetworkTelemetryStore();
    const finish = store.begin('GET', '/materials?token=secret', 'request');
    finish({ status: 200, requestId: 'request-1', bytes: 512 });

    const pending = store.begin('POST', '/uploads', 'upload');
    store.clear();
    expect(store.getSnapshot()).toEqual({
      samples: [],
      inflight: 1,
      completed: 0,
      failed: 0,
      totalBytes: 0,
    });

    pending({ status: 201, bytes: 1024 });
    expect(store.getSnapshot()).toMatchObject({
      inflight: 0,
      completed: 1,
      failed: 0,
      totalBytes: 1024,
    });
    expect(store.getSnapshot().samples[0]?.path).toBe('/uploads');
  });
});
