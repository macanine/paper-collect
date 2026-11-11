import type { ReactNode } from 'react';
import { Group, Stack, Text, Title } from '@mantine/core';

interface PageHeaderProps {
  title: ReactNode;
  description?: ReactNode;
  extra?: ReactNode;
}

export function PageHeader({ title, description, extra }: PageHeaderProps) {
  return (
    <Group justify="space-between" align="flex-start" wrap="wrap" gap="md">
      <Stack gap={4} miw={0}>
        <Title order={2} fz={24}>{title}</Title>
        {description ? <Text c="dimmed" size="sm">{description}</Text> : null}
      </Stack>
      {extra ? <Group gap="sm" wrap="wrap">{extra}</Group> : null}
    </Group>
  );
}
