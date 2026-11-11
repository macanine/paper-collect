import { Anchor, Group } from '@mantine/core';
import { IconArrowLeft } from '@tabler/icons-react';
import { Link } from 'react-router';

export function BackLink({ to, children }: { to: string; children: string }) {
  return (
    <Anchor component={Link} to={to} size="sm" c="dimmed" w="fit-content">
      <Group gap={4} wrap="nowrap"><IconArrowLeft size={16} />{children}</Group>
    </Anchor>
  );
}
