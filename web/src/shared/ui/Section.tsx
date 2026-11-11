import type { ReactNode } from 'react';
import { Box, Group, Paper, Stack, Title } from '@mantine/core';

/** 带标题的白色内容卡片。flush 时内容（如表格）贴边，标题仍保持内边距。 */
export function Section({ title, extra, children, flush }: { title?: ReactNode; extra?: ReactNode; children: ReactNode; flush?: boolean }) {
  const header = title || extra ? (
    <Group justify="space-between" wrap="nowrap" px="lg" pt="lg" pb={flush ? 'sm' : 0}>
      {title ? <Title order={4}>{title}</Title> : <span />}
      {extra}
    </Group>
  ) : null;
  return (
    <Paper style={{ overflow: 'hidden' }}>
      {header}
      {flush ? <Box>{children}</Box> : <Stack gap="md" p="lg" pt={header ? 'md' : 'lg'}>{children}</Stack>}
    </Paper>
  );
}
