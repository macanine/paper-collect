import { useState } from 'react';
import { Alert, Avatar, Button, Center, Group, Paper, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconBook2, IconLock, IconUser } from '@tabler/icons-react';
import { Navigate, useLocation, useNavigate } from 'react-router';
import { describeError, isApiError } from '../../shared/api/errors';
import { FullPageLoading } from '../../shared/ui/States';
import { useAuth } from './AuthContext';
import { loginRequest } from './api';

function loginErrorText(error: unknown): string {
  if (isApiError(error) && error.status === 401) return '用户名或口令错误';
  if (isApiError(error) && error.status === 429) return '登录尝试过于频繁，请稍后再试';
  return describeError(error).title;
}

export function LoginPage() {
  const { status, signIn } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [submitting, setSubmitting] = useState(false);
  const [errorText, setErrorText] = useState<string | null>(null);
  const form = useForm({
    initialValues: { username: '', password: '' },
    validate: {
      username: (v) => (v.trim() ? null : '请输入用户名'),
      password: (v) => (v ? null : '请输入口令'),
    },
  });

  const from = (location.state as { from?: string } | null)?.from ?? '/';

  if (status === 'authenticated') return <Navigate to={from} replace />;
  if (status === 'loading') return <FullPageLoading />;

  const onSubmit = form.onSubmit(async (values) => {
    setSubmitting(true);
    setErrorText(null);
    try {
      const session = await loginRequest(values.username.trim(), values.password);
      signIn(session);
      navigate(from, { replace: true });
    } catch (error) {
      setErrorText(loginErrorText(error));
    } finally {
      setSubmitting(false);
    }
  });

  return (
    <Center component="main" mih="100vh" p="md" bg="gray.0">
      <Paper w="100%" maw={400} p="xl" shadow="sm">
        <form onSubmit={onSubmit}>
          <Stack gap="md">
            <Group gap="sm" wrap="nowrap">
              <Avatar color="blue" variant="filled" radius="md" size={40}><IconBook2 size={22} /></Avatar>
              <Stack gap={0}>
                <Text fw={600}>高中资料归档</Text>
                <Text size="xs" c="dimmed">Paper Collect · 管理台</Text>
              </Stack>
            </Group>
            <Stack gap={4}>
              <Title order={2}>登录</Title>
              <Text size="sm" c="dimmed">使用管理员分配的账户继续</Text>
            </Stack>
            {errorText ? <Alert color="red">{errorText}</Alert> : null}
            <TextInput label="用户名" leftSection={<IconUser size={16} />} autoComplete="username" autoFocus disabled={submitting} {...form.getInputProps('username')} />
            <PasswordInput label="口令" leftSection={<IconLock size={16} />} autoComplete="current-password" disabled={submitting} {...form.getInputProps('password')} />
            <Button type="submit" fullWidth loading={submitting}>登录</Button>
            <Text size="sm" c="dimmed" ta="center">仅限授权人员访问 · 登录状态将在 15 分钟后自动续期</Text>
          </Stack>
        </form>
      </Paper>
    </Center>
  );
}
