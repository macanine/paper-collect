import { useMemo, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { NodePoint } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { getNodeMetrics } from './api';
import { NODE_POLL_MS, linePath } from './nodeUtils';

const WIDTH = 600;
const HEIGHT = 140;

export interface MetricSeries {
  label: string;
  color: string;
  value: (point: NodePoint) => number;
}

interface MetricChartProps {
  title: string;
  points: NodePoint[];
  series: MetricSeries[];
  format: (value: number) => string;
  /** 固定量程，例如百分比；缺省时按数据最大值。 */
  max?: number;
}

export function MetricChart({ title, points, series, format, max }: MetricChartProps) {
  const [active, setActive] = useState<number | null>(null);
  const scale = useMemo(() => {
    if (max) return max;
    const peak = Math.max(0, ...points.flatMap((point) => series.map((item) => item.value(point))));
    return peak > 0 ? peak * 1.1 : 1;
  }, [max, points, series]);
  const start = points[0] ? new Date(points[0].time).getTime() : 0;
  const end = points[points.length - 1];
  const span = end ? Math.max(1, new Date(end.time).getTime() - start) : 1;
  const xOf = (point: NodePoint) => (new Date(point.time).getTime() - start) / span;
  const current = active !== null ? points[active] : undefined;

  const nearest = (ratio: number) => {
    let best = 0;
    let distance = Infinity;
    points.forEach((point, index) => {
      const gap = Math.abs(xOf(point) - ratio);
      if (gap < distance) {
        best = index;
        distance = gap;
      }
    });
    return best;
  };
  const onPointer = (event: PointerEvent<HTMLDivElement>) => {
    if (points.length === 0) return;
    const box = event.currentTarget.getBoundingClientRect();
    setActive(nearest((event.clientX - box.left) / box.width));
  };
  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (points.length === 0) return;
    const last = points.length - 1;
    if (event.key === 'ArrowLeft') setActive(Math.max(0, (active ?? last) - 1));
    if (event.key === 'ArrowRight') setActive(Math.min(last, (active ?? 0) + 1));
  };

  const latest = end;
  return (
    <figure style={{ margin: 0 }}>
      <figcaption className="node-chart__header">
        <span className="node-chart__title">{title}</span>
        <span className="node-chart__legend">
          {series.map((item) => (
            <span key={item.label}>
              {series.length > 1 ? <span className="node-chart__key" style={{ background: item.color }} /> : null}
              {item.label} {latest ? format(item.value(latest)) : '—'}
            </span>
          ))}
        </span>
      </figcaption>
      <div
        className="node-chart__plot"
        role="img"
        tabIndex={0}
        aria-label={`${title}最近 1 小时趋势，使用左右方向键查看各时间点`}
        onPointerMove={onPointer}
        onPointerLeave={() => setActive(null)}
        onBlur={() => setActive(null)}
        onKeyDown={onKey}
      >
        <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} preserveAspectRatio="none" aria-hidden="true">
          {[0, 0.5, 1].map((ratio) => (
            <line key={ratio} x1="0" x2={WIDTH} y1={ratio * HEIGHT} y2={ratio * HEIGHT} stroke="var(--ant-border-secondary)" vectorEffect="non-scaling-stroke" />
          ))}
          {series.map((item) => (
            <path key={item.label} d={linePath(points, item.value, WIDTH, HEIGHT, scale)} fill="none" stroke={item.color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
          ))}
          {current ? (
            <line x1={xOf(current) * WIDTH} x2={xOf(current) * WIDTH} y1="0" y2={HEIGHT} stroke="var(--ant-border)" vectorEffect="non-scaling-stroke" />
          ) : null}
        </svg>
        <span className="node-chart__scale" style={{ top: 4 }}>{format(scale)}</span>
        {points.length === 0 ? <span className="node-chart__scale" style={{ top: 64 }}>等待采样，主端重启后历史会清空</span> : null}
        {current ? (
          <div className="node-chart__tooltip" style={xOf(current) > 0.6 ? { right: `${(1 - xOf(current)) * 100}%` } : { left: `${xOf(current) * 100}%` }}>
            <div className="node-chart__tooltip-time">{formatDateTime(current.time)}</div>
            {series.map((item) => (
              <div key={item.label} className="node-chart__tooltip-row">
                {series.length > 1 ? <span className="node-chart__key" style={{ background: item.color }} /> : null}
                <strong>{format(item.value(current))}</strong>
                <span>{item.label}</span>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </figure>
  );
}

/** 列表行内的 CPU 迷你趋势，只用于看走势，具体数值在旁边的文字里。 */
export function CpuSparkline({ nodeId }: { nodeId: string }) {
  const query = useQuery({
    queryKey: ['nodes', nodeId, 'metrics'],
    queryFn: ({ signal }) => getNodeMetrics(nodeId, signal),
    refetchInterval: NODE_POLL_MS,
  });
  const points = (query.data ?? []).slice(-30);
  return (
    <svg className="node-sparkline" viewBox="0 0 64 20" preserveAspectRatio="none" aria-hidden="true">
      <path d={linePath(points, (point) => point.cpu_percent, 64, 20, 100)} fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
