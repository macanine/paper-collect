import type { ReactNode } from 'react';
import { Group, Stack, Text, Title } from '@mantine/core';

interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  extra?: ReactNode;
}

/** 页面标题区：h2（28px）+ 一行说明，右侧放本页操作。每页只有一个。 */
export function PageHeader({ title, description, extra }: PageHeaderProps) {
  return (
    <Group justify="space-between" align="center" wrap="wrap" gap="md">
      <Stack gap="xs" miw={0} maw={720}>
        <Title order={2}>{title}</Title>
        {description ? <Text component="div" c="dimmed" size="sm">{description}</Text> : null}
      </Stack>
      {extra ? <Group gap="sm" wrap="wrap">{extra}</Group> : null}
    </Group>
  );
}
