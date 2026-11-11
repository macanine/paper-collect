import { useState } from 'react';
import { Button, PasswordInput, Select, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconPlus } from '@tabler/icons-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { User, UserRole } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { USER_ROLE_LABELS } from '../../shared/lib/labels';
import { USER_ROLE_OPTIONS } from '../../shared/lib/options';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { FormModal } from '../../shared/ui/FormModal';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import { createUser, listUsers } from './api';
import { qk } from '../../app/queryKeys';

const PAGE_SIZE = 20;

const ROLE_COLOR: Record<UserRole, string> = { admin: 'error', editor: 'blue', viewer: 'default' };

export function UsersPage() {
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const form = useForm<{ username: string; password: string; role: UserRole }>({
    initialValues: { username: '', password: '', role: 'viewer' },
    validate: {
      username: (v) => (/^[A-Za-z0-9_-]{3,64}$/.test(v.trim()) ? null : '3-64 位字母、数字、下划线或连字符'),
      password: (v) => (v.length >= 12 ? null : '至少 12 个字符'),
    },
  });

  const query = useQuery({
    queryKey: qk.users.list(page),
    queryFn: ({ signal }) => listUsers({ page, page_size: PAGE_SIZE }, signal),
  });

  const create = useMutation({
    mutationFn: createUser,
    onSuccess: () => {
      message.success('用户已创建');
      setCreating(false);
      form.reset();
      void queryClient.invalidateQueries({ queryKey: qk.users.all });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const columns: Column<User>[] = [
    { key: 'username', title: '用户名', render: (u) => <Text fw={600} size="sm">{u.username}</Text> },
    { key: 'role', title: '角色', width: 120, render: (u) => <Tag color={ROLE_COLOR[u.role]}>{USER_ROLE_LABELS[u.role]}</Tag> },
    { key: 'enabled', title: '状态', width: 100, render: (u) => (u.enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>) },
    { key: 'created_at', title: '创建时间', width: 180, render: (u) => formatDateTime(u.created_at) },
  ];

  return (
    <Stack gap="lg">
      <PageHeader title="用户" extra={<Button leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)}>新建用户</Button>} />
      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isSuccess ? (
        <DataTable
          rows={query.data.items}
          columns={columns}
          rowKey={(u) => u.id}
          empty={<EmptyState />}
          minWidth={560}
          pagination={{ page: query.data.page, pageSize: query.data.page_size, total: query.data.total, pageSizes: [query.data.page_size], onChange: (p) => setPage(p) }}
        />
      ) : null}

      <FormModal
        opened={creating}
        onClose={() => setCreating(false)}
        title="新建用户"
        okText="创建"
        loading={create.isPending}
        onSubmit={form.onSubmit((v) => create.mutate({ username: v.username.trim(), password: v.password, role: v.role }))}
      >
        <TextInput label="用户名" withAsterisk autoComplete="off" data-autofocus {...form.getInputProps('username')} />
        <PasswordInput label="初始密码" withAsterisk autoComplete="new-password" {...form.getInputProps('password')} />
        <Select label="角色" withAsterisk data={USER_ROLE_OPTIONS} {...form.getInputProps('role')} />
      </FormModal>
    </Stack>
  );
}
