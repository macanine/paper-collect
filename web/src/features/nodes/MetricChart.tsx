import { useMemo, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { Box, Group, Paper, Stack, Text } from '@mantine/core';
import { useQuery } from '@tanstack/react-query';
import type { NodePoint } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { getNodeMetrics } from './api';
import { NODE_POLL_MS, linePath } from './nodeUtils';
import { qk } from '../../app/queryKeys';

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

function Key({ color }: { color: string }) {
  return <Box w={10} h={10} bg={color} style={{ borderRadius: 2, display: 'inline-block' }} />;
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
      if (gap < distance) { best = index; distance = gap; }
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

  return (
    <Stack component="figure" gap="xs" m={0} miw={0}>
      <Group component="figcaption" justify="space-between" wrap="wrap" gap="xs">
        <Text fw={600} size="sm">{title}</Text>
        <Group gap="sm">
          {series.map((item) => (
            <Group key={item.label} gap={4}>
              {series.length > 1 ? <Key color={item.color} /> : null}
              <Text size="xs" c="dimmed">{item.label} {end ? format(item.value(end)) : '—'}</Text>
            </Group>
          ))}
        </Group>
      </Group>
      <Box
        pos="relative"
        h={HEIGHT}
        role="img"
        tabIndex={0}
        aria-label={`${title}最近 1 小时趋势，使用左右方向键查看各时间点`}
        onPointerMove={onPointer}
        onPointerLeave={() => setActive(null)}
        onBlur={() => setActive(null)}
        onKeyDown={onKey}
        style={{ border: '1px solid var(--mantine-color-gray-2)', borderRadius: 'var(--mantine-radius-md)', outline: 'none' }}
      >
        <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} preserveAspectRatio="none" aria-hidden="true" width="100%" height="100%" style={{ display: 'block' }}>
          {[0, 0.5, 1].map((ratio) => (
            <line key={ratio} x1="0" x2={WIDTH} y1={ratio * HEIGHT} y2={ratio * HEIGHT} stroke="var(--mantine-color-gray-2)" vectorEffect="non-scaling-stroke" />
          ))}
          {series.map((item) => (
            <path key={item.label} d={linePath(points, item.value, WIDTH, HEIGHT, scale)} fill="none" stroke={item.color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
          ))}
          {current ? <line x1={xOf(current) * WIDTH} x2={xOf(current) * WIDTH} y1="0" y2={HEIGHT} stroke="var(--mantine-color-gray-5)" vectorEffect="non-scaling-stroke" /> : null}
        </svg>
        <Text size="xs" c="dimmed" pos="absolute" top={4} left={8}>{format(scale)}</Text>
        {points.length === 0 ? <Text size="xs" c="dimmed" pos="absolute" top={64} left={8}>等待采样，主端重启后历史会清空</Text> : null}
        {current ? (
          <Paper p="xs" shadow="sm" pos="absolute" top={8} style={xOf(current) > 0.6 ? { right: `${(1 - xOf(current)) * 100}%` } : { left: `${xOf(current) * 100}%` }}>
            <Text size="xs" c="dimmed">{formatDateTime(current.time)}</Text>
            {series.map((item) => (
              <Group key={item.label} gap={6} wrap="nowrap">
                {series.length > 1 ? <Key color={item.color} /> : null}
                <Text size="xs" fw={600}>{format(item.value(current))}</Text>
                <Text size="xs" c="dimmed">{item.label}</Text>
              </Group>
            ))}
          </Paper>
        ) : null}
      </Box>
    </Stack>
  );
}

/** 列表行内的 CPU 迷你趋势，只用于看走势，具体数值在旁边的文字里。 */
export function CpuSparkline({ nodeId }: { nodeId: string }) {
  const query = useQuery({
    queryKey: qk.nodes.metrics(nodeId),
    queryFn: ({ signal }) => getNodeMetrics(nodeId, signal),
    refetchInterval: NODE_POLL_MS,
  });
  const points = (query.data ?? []).slice(-30);
  return (
    <svg viewBox="0 0 64 20" preserveAspectRatio="none" aria-hidden="true" width={64} height={20} style={{ color: 'var(--mantine-color-blue-6)' }}>
      <path d={linePath(points, (point) => point.cpu_percent, 64, 20, 100)} fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
