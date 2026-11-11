import type { ReactNode } from 'react';
import { Box, SimpleGrid, Text } from '@mantine/core';

export interface DescriptionItem {
  key: string;
  label: ReactNode;
  children: ReactNode;
  /** 占满整行。 */
  full?: boolean;
}

/** 键值信息网格：标签在上、值在下，列宽相等，所有详情页统一对齐。 */
export function Descriptions({ items, cols = 2 }: { items: (DescriptionItem | false | null | undefined)[]; cols?: number }) {
  const list = items.filter(Boolean) as DescriptionItem[];
  return (
    <SimpleGrid cols={{ base: 1, sm: cols }} spacing="md" verticalSpacing="md">
      {list.map((item) => (
        <Box key={item.key} miw={0} style={item.full ? { gridColumn: '1 / -1' } : undefined}>
          <Text size="xs" c="dimmed" mb={2}>{item.label}</Text>
          <Box fz="sm" style={{ wordBreak: 'break-word' }}>{item.children}</Box>
        </Box>
      ))}
    </SimpleGrid>
  );
}
