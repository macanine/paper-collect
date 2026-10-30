// Browser-side API observability. Samples stay in memory and never contain
// request bodies, response bodies, tokens or query strings.

export type NetworkOperation = 'request' | 'download' | 'upload';

export interface NetworkSample {
  id: string;
  method: string;
  path: string;
  operation: NetworkOperation;
  startedAt: number;
  durationMs: number;
  status: number;
  ok: boolean;
  requestId?: string;
  bytes?: number;
  errorCode?: string;
}

export interface NetworkTelemetryState {
  samples: readonly NetworkSample[];
  inflight: number;
  completed: number;
  failed: number;
  totalBytes: number;
}

export interface NetworkFinish {
  status: number;
  requestId?: string;
  bytes?: number;
  errorCode?: string;
  ok?: boolean;
}

export interface NetworkSummary {
  sampleCount: number;
  successRate: number;
  averageMs: number;
  medianMs: number;
  p95Ms: number;
  slowestMs: number;
  errorCount: number;
}

const SAMPLE_LIMIT = 100;
let fallbackId = 0;

function newSampleId(): string {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch {
    // Fall through for restricted browser contexts.
  }
  fallbackId += 1;
  return `net-${Date.now()}-${fallbackId}`;
}

function now(): number {
  return typeof performance !== 'undefined' ? performance.now() : Date.now();
}

function safePath(path: string): string {
  const query = path.indexOf('?');
  return query >= 0 ? path.slice(0, query) : path;
}

export class NetworkTelemetryStore {
  private state: NetworkTelemetryState = {
    samples: [],
    inflight: 0,
    completed: 0,
    failed: 0,
    totalBytes: 0,
  };
  private listeners = new Set<() => void>();

  getSnapshot = (): NetworkTelemetryState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  begin(method: string, path: string, operation: NetworkOperation): (result: NetworkFinish) => void {
    const startedAt = Date.now();
    const started = now();
    this.setState({ ...this.state, inflight: this.state.inflight + 1 });
    let finished = false;
    return (result) => {
      if (finished) return;
      finished = true;
      const durationMs = Math.max(0, now() - started);
      const ok = result.ok ?? (result.status >= 200 && result.status < 400);
      const sample: NetworkSample = {
        id: newSampleId(),
        method: method.toUpperCase(),
        path: safePath(path),
        operation,
        startedAt,
        durationMs,
        status: result.status,
        ok,
        requestId: result.requestId,
        bytes: result.bytes,
        errorCode: result.errorCode,
      };
      const samples = [...this.state.samples, sample].slice(-SAMPLE_LIMIT);
      this.setState({
        samples,
        inflight: Math.max(0, this.state.inflight - 1),
        completed: this.state.completed + 1,
        failed: this.state.failed + (ok ? 0 : 1),
        totalBytes: this.state.totalBytes + Math.max(0, result.bytes ?? 0),
      });
    };
  }

  clear() {
    this.state = {
      samples: [],
      inflight: this.state.inflight,
      completed: 0,
      failed: 0,
      totalBytes: 0,
    };
    this.emit();
  }

  private setState(state: NetworkTelemetryState) {
    this.state = state;
    this.emit();
  }

  private emit() {
    for (const listener of this.listeners) listener();
  }
}

export const networkTelemetry = new NetworkTelemetryStore();

function percentile(sorted: readonly number[], ratio: number): number {
  if (sorted.length === 0) return 0;
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * ratio) - 1));
  return sorted[index] ?? 0;
}

export function summarizeNetwork(samples: readonly NetworkSample[]): NetworkSummary {
  const durations = samples.map((sample) => sample.durationMs).sort((left, right) => left - right);
  const total = durations.reduce((sum, value) => sum + value, 0);
  const errorCount = samples.filter((sample) => !sample.ok).length;
  return {
    sampleCount: samples.length,
    successRate: samples.length === 0 ? 1 : (samples.length - errorCount) / samples.length,
    averageMs: durations.length === 0 ? 0 : total / durations.length,
    medianMs: percentile(durations, 0.5),
    p95Ms: percentile(durations, 0.95),
    slowestMs: durations.at(-1) ?? 0,
    errorCount,
  };
}
