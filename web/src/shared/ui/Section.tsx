import type { ReactNode } from 'react';
import { Box, Group, Paper, Stack, Title } from '@mantine/core';

interface SectionProps {
  title?: ReactNode;
  extra?: ReactNode;
  children: ReactNode;
  /** 内容（表格、行列表）贴边，标题与行内容同样左对齐 16px。 */
  flush?: boolean;
  /** 已在另一张卡片里：不再加边框和内边距，只保留标题与内容，避免卡片套卡片。 */
  bare?: boolean;
}

/** 带标题的内容区。默认是白色带边框卡片，内容区 24px 内边距。 */
export function Section({ title, extra, children, flush, bare }: SectionProps) {
  if (bare) {
    return (
      <Stack gap="sm">
        {title || extra ? (
          <Group justify="space-between" wrap="nowrap" gap="md" mih={40}>
            {title ? <Title order={4}>{title}</Title> : <span />}
            {extra ? <Group gap="sm" wrap="nowrap">{extra}</Group> : null}
          </Group>
        ) : null}
        {children}
      </Stack>
    );
  }
  const pad = flush ? 'md' : 'lg';
  const header = title || extra ? (
    <Group justify="space-between" wrap="nowrap" gap="md" px={pad} pt={pad} pb={flush ? 'sm' : 0} mih={40}>
      {title ? <Title order={4}>{title}</Title> : <span />}
      {extra ? <Group gap="sm" wrap="nowrap">{extra}</Group> : null}
    </Group>
  ) : null;
  return (
    <Paper style={{ overflow: 'hidden' }}>
      {header}
      {flush ? <Box>{children}</Box> : <Stack gap="md" p="lg" pt={header ? 'md' : 'lg'}>{children}</Stack>}
    </Paper>
  );
}
