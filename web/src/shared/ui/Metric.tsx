import type { ReactNode } from 'react';
import { Stack, Text } from '@mantine/core';

/** 一个数值指标：说明在上（14px），数值在下（xl 28px / md 20px）。不带图标和底色。 */
export function Metric({ label, value, hint, size = 'md' }: { label: ReactNode; value: ReactNode; hint?: ReactNode; size?: 'md' | 'xl' }) {
  return (
    <Stack gap={4} miw={0}>
      <Text size="sm" c="dimmed">{label}</Text>
      <Text fw={600} fz={size === 'xl' ? 'h2' : 'xl'} lh={1.25} style={{ fontVariantNumeric: 'tabular-nums' }}>{value ?? '—'}</Text>
      {hint ? <Text size="xs" c="dimmed">{hint}</Text> : null}
    </Stack>
  );
}
