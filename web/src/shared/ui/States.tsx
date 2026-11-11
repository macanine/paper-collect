import type { ReactNode } from 'react';
import { Alert, Button, Center, Code, Group, Loader, Stack, Text, Title } from '@mantine/core';
import { IconAlertCircle, IconInbox } from '@tabler/icons-react';
import { describeError, isApiError } from '../api/errors';
import { CopyButton } from './CopyButton';
import { LinkButton } from './LinkButton';

export function LoadingState() {
  return (
    <Center mih={160} py="xl">
      <Loader size="sm" />
    </Center>
  );
}

export function EmptyState({ description = '暂无数据', action }: { description?: ReactNode; action?: ReactNode }) {
  return (
    <Stack align="center" gap="xs" py="xl">
      <IconInbox size={36} stroke={1.2} color="var(--mantine-color-gray-5)" />
      <Text c="dimmed" size="sm" ta="center">{description}</Text>
      {action}
    </Stack>
  );
}

export function ErrorState({ error, onRetry, title = '加载失败' }: { error: unknown; onRetry?: () => void; title?: string }) {
  const { title: message, requestId } = describeError(error);
  const code = isApiError(error) ? error.code : undefined;
  return (
    <Stack align="center" gap="xs" py="xl" maw={520} mx="auto">
      <IconAlertCircle size={40} stroke={1.4} color="var(--mantine-color-red-6)" />
      <Title order={4}>{title}</Title>
      <Text c="dimmed" size="sm" ta="center">{message}</Text>
      {code || requestId ? (
        <Group gap="xs" justify="center">
          {code ? <Code>{code}</Code> : null}
          {requestId ? (
            <>
              <Text size="xs" c="dimmed">请求 ID {requestId}</Text>
              <CopyButton value={requestId} label="复制" />
            </>
          ) : null}
        </Group>
      ) : null}
      {onRetry ? <Button mt="xs" onClick={onRetry}>重试</Button> : null}
    </Stack>
  );
}

export function FullPageLoading() {
  return (
    <Center h="100vh">
      <Loader />
    </Center>
  );
}

function StatusPage({ code, title }: { code: string; title: string }) {
  return (
    <Stack align="center" gap="xs" py={80}>
      <Title order={1} c="dimmed" fz={56}>{code}</Title>
      <Text>{title}</Text>
      <LinkButton to="/" mt="sm">返回工作台</LinkButton>
    </Stack>
  );
}

export function ForbiddenState() {
  return <StatusPage code="403" title="没有访问权限" />;
}

export function NotFoundPage() {
  return <StatusPage code="404" title="页面不存在" />;
}

export function InlineError({ error }: { error: unknown }) {
  return <Alert color="red" icon={<IconAlertCircle size={16} />}>{describeError(error).title}</Alert>;
}
