import { useMemo, useState, useSyncExternalStore } from 'react';
import { Button, Drawer, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ApiOutlined, ClearOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { networkTelemetry, summarizeNetwork, type NetworkSample } from '../api/telemetry';
import { formatBytes, shortId } from '../lib/format';
import { useCompactViewport } from '../lib/useCompactViewport';
import { CopyButton } from './CopyButton';
import { EmptyState } from './States';

const clockFormatter = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
});

const OPERATION_LABELS: Record<NetworkSample['operation'], string> = {
  request: 'API',
  download: '下载',
  upload: '上传',
};

function latencyText(value: number): string {
  if (value < 1) return '<1 ms';
  if (value < 1000) return `${Math.round(value)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
}

function statusTag(sample: NetworkSample) {
  if (sample.status === 0) return <Tag color="red">连接失败</Tag>;
  if (sample.status < 400) return <Tag color="green">{sample.status}</Tag>;
  if (sample.status < 500) return <Tag color="orange">{sample.status}</Tag>;
  return <Tag color="red">{sample.status}</Tag>;
}

export function NetworkActivity() {
  const [open, setOpen] = useState(false);
  const compact = useCompactViewport();
  const telemetry = useSyncExternalStore(
    networkTelemetry.subscribe,
    networkTelemetry.getSnapshot,
    networkTelemetry.getSnapshot,
  );
  const summary = useMemo(() => summarizeNetwork(telemetry.samples), [telemetry.samples]);
  const recent = useMemo(() => [...telemetry.samples].reverse(), [telemetry.samples]);
  const latest = telemetry.samples.at(-1);

  const desktopColumns: ColumnsType<NetworkSample> = [
    {
      title: '时间',
      dataIndex: 'startedAt',
      key: 'startedAt',
      width: 82,
      render: (value: number) => (
        <span className="tabular-nums text-xs text-slate-500">{clockFormatter.format(value)}</span>
      ),
    },
    {
      title: '请求',
      key: 'request',
      render: (_: unknown, sample) => (
        <div className="network-request-cell">
          <div className="flex min-w-0 items-center gap-2">
            <Tag className="network-method">{sample.method}</Tag>
            <Typography.Text ellipsis={{ tooltip: sample.path }} className="min-w-0 font-mono text-xs">
              {sample.path}
            </Typography.Text>
          </div>
          <span className="text-xs text-slate-500">{OPERATION_LABELS[sample.operation]}</span>
        </div>
      ),
    },
    {
      title: '结果',
      key: 'status',
      width: 90,
      render: (_: unknown, sample) => statusTag(sample),
    },
    {
      title: '耗时',
      dataIndex: 'durationMs',
      key: 'durationMs',
      width: 88,
      align: 'right',
      render: (value: number) => (
        <span className={`tabular-nums ${value >= 3000 ? 'text-red-600' : value >= 1000 ? 'text-amber-600' : ''}`}>
          {latencyText(value)}
        </span>
      ),
    },
    {
      title: '数据',
      dataIndex: 'bytes',
      key: 'bytes',
      width: 88,
      align: 'right',
      render: (value: number | undefined) => (
        <span className="tabular-nums text-xs text-slate-500">{value === undefined ? '—' : formatBytes(value)}</span>
      ),
    },
    {
      title: '请求 ID',
      dataIndex: 'requestId',
      key: 'requestId',
      width: 150,
      render: (value: string | undefined) =>
        value ? (
          <div className="flex items-center gap-1">
            <Tooltip title={value}>
              <span className="font-mono text-xs">{shortId(value)}</span>
            </Tooltip>
            <CopyButton value={value} label="复制" />
          </div>
        ) : (
          <span className="text-xs text-slate-400">—</span>
        ),
    },
  ];

  const compactColumns: ColumnsType<NetworkSample> = [
    {
      title: '请求',
      key: 'summary',
      render: (_: unknown, sample) => (
        <div className="compact-entity">
          <div className="compact-entity__heading">
            <span className="compact-entity__title flex min-w-0 items-center gap-2">
              <Tag className="network-method">{sample.method}</Tag>
              <span className="min-w-0 break-all font-mono text-xs">{sample.path}</span>
            </span>
            {statusTag(sample)}
          </div>
          <div className="compact-entity__meta">
            <span>{clockFormatter.format(sample.startedAt)}</span>
            <span>{OPERATION_LABELS[sample.operation]}</span>
            <span
              className={`tabular-nums ${sample.durationMs >= 3000 ? 'text-red-600' : sample.durationMs >= 1000 ? 'text-amber-600' : ''}`}
            >
              {latencyText(sample.durationMs)}
            </span>
            <span className="tabular-nums">
              {sample.bytes === undefined ? '数据量未知' : formatBytes(sample.bytes)}
            </span>
          </div>
          <div className="compact-entity__links">
            {sample.requestId ? (
              <>
                <span className="font-mono text-xs text-slate-500">请求 ID {shortId(sample.requestId)}</span>
                <CopyButton value={sample.requestId} label="复制" />
              </>
            ) : (
              <span className="text-xs text-slate-400">无请求 ID</span>
            )}
          </div>
        </div>
      ),
    },
  ];

  return (
    <>
      <button
        type="button"
        className={`app-observe-button${telemetry.inflight > 0 ? ' is-live' : ''}`}
        onClick={() => setOpen(true)}
        aria-label="打开网络与性能观测"
      >
        <span className="app-observe-button__icon">
          {telemetry.inflight > 0 ? <ThunderboltOutlined /> : <ApiOutlined />}
        </span>
        <span className="app-observe-button__copy">
          <span>网络</span>
          <small>
            {telemetry.inflight > 0
              ? `${telemetry.inflight} 项进行中`
              : latest
                ? latencyText(latest.durationMs)
                : '等待请求'}
          </small>
        </span>
      </button>

      <Drawer
        open={open}
        onClose={() => setOpen(false)}
        placement="right"
        size={720}
        title={
          <div className="network-drawer__title">
            <span>网络与性能</span>
            <small>当前浏览器会话 · 不记录正文、凭据或查询参数</small>
          </div>
        }
        extra={
          <Button
            size="small"
            icon={<ClearOutlined />}
            disabled={telemetry.samples.length === 0}
            onClick={() => networkTelemetry.clear()}
          >
            清空
          </Button>
        }
        rootClassName="network-drawer"
      >
        <div className="network-summary">
          <div className="network-summary__item">
            <span>P95 延迟</span>
            <strong>{latencyText(summary.p95Ms)}</strong>
            <small>最近 {summary.sampleCount} 次采样</small>
          </div>
          <div className="network-summary__item">
            <span>成功率</span>
            <strong>{Math.round(summary.successRate * 100)}%</strong>
            <small>{summary.errorCount} 次失败</small>
          </div>
          <div className="network-summary__item">
            <span>传输数据</span>
            <strong>{formatBytes(telemetry.totalBytes)}</strong>
            <small>响应体与文件总量</small>
          </div>
          <div className="network-summary__item">
            <span>进行中</span>
            <strong>{telemetry.inflight}</strong>
            <small>已完成 {telemetry.completed} 次</small>
          </div>
        </div>

        <div className="network-latency">
          <div className="network-latency__heading">
            <span>延迟轨迹</span>
            <span>平均 {latencyText(summary.averageMs)} · 中位 {latencyText(summary.medianMs)}</span>
          </div>
          <LatencyBars samples={telemetry.samples.slice(-32)} />
        </div>

        <Table<NetworkSample>
          rowKey="id"
          size="small"
          className={compact ? 'compact-list-table' : undefined}
          columns={compact ? compactColumns : desktopColumns}
          dataSource={recent}
          showHeader={!compact}
          pagination={false}
          scroll={compact ? undefined : { x: 660, y: 420 }}
          locale={{ emptyText: <EmptyState description="发出请求后，这里会显示真实的网络与耗时数据" /> }}
        />
      </Drawer>
    </>
  );
}

function LatencyBars({ samples }: { samples: readonly NetworkSample[] }) {
  if (samples.length === 0) {
    return <div className="network-latency__empty">暂无网络采样</div>;
  }
  const max = Math.max(1, ...samples.map((sample) => sample.durationMs));
  return (
    <div className="network-latency__bars" aria-label="最近请求延迟柱状图">
      {samples.map((sample) => {
        const height = Math.max(4, Math.round((sample.durationMs / max) * 100));
        return (
          <Tooltip
            key={sample.id}
            title={`${sample.method} ${sample.path} · ${latencyText(sample.durationMs)}${sample.ok ? '' : ' · 失败'}`}
          >
            <span
              className={`network-latency__bar${sample.ok ? '' : ' is-error'}`}
              style={{ height: `${height}%` }}
            />
          </Tooltip>
        );
      })}
    </div>
  );
}
