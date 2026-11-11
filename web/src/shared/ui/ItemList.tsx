import type { ReactNode } from 'react';
import { Box, Group, Paper, Stack } from '@mantine/core';

/** 带分隔线的行列表：左侧主体（标题 + 元信息），右侧操作/状态，所有行对齐一致。 */
export function ItemList({ children, bare }: { children: ReactNode; bare?: boolean }) {
  const list = <Stack gap={0}>{children}</Stack>;
  return bare ? list : <Paper style={{ overflow: 'hidden' }}>{list}</Paper>;
}

export function ItemRow({ main, aside }: { main: ReactNode; aside?: ReactNode }) {
  return (
    <Group
      justify="space-between"
      align="center"
      wrap="nowrap"
      gap="md"
      px="md"
      py="sm"
      mih={56}
      style={{ borderBottom: '1px solid var(--mantine-color-gray-2)' }}
      className="item-row"
    >
      <Box flex={1} miw={0}>{main}</Box>
      {aside ? <Group gap="xs" wrap="nowrap" style={{ flexShrink: 0 }}>{aside}</Group> : null}
    </Group>
  );
}
