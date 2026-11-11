import { useState } from 'react';
import { Alert, Box, Button, Group, Modal, MultiSelect, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconInfoCircle, IconPlus } from '@tabler/icons-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import { SCOPES, type APIKey, type CreatedAPIKey } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { SCOPE_LABELS } from '../../shared/lib/labels';
import { CopyButton } from '../../shared/ui/CopyButton';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { FormModal } from '../../shared/ui/FormModal';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import { createKey, listKeys, revokeKey } from './api';
import { buildCreateKeyBody, type KeyFormValues } from './keyForm';
import { qk } from '../../app/queryKeys';

const KEY_SCOPE_OPTIONS = SCOPES.map((scope) => ({ value: scope, label: SCOPE_LABELS[scope] }));
const PAGE_SIZE = 20;

export function ApiKeysPage() {
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedAPIKey | null>(null);
  const form = useForm<KeyFormValues & { expires_at: string }>({
    initialValues: { name: '', scopes: ['materials:read', 'collections:read', 'jobs:read'], expires_at: '' },
    validate: {
      name: (v) => (!v.trim() ? '请输入名称' : v.length > 128 ? '最多 128 个字符' : null),
      scopes: (v) => (v.length ? null : '至少选择一个权限'),
      expires_at: (v) => {
        const trimmed = v.trim();
        if (!trimmed) return null;
        const time = new Date(trimmed);
        if (Number.isNaN(time.getTime())) return '请输入 RFC3339 时间，例如 2026-12-31T00:00:00Z';
        if (time.getTime() <= Date.now()) return '过期时间必须晚于当前时间';
        return null;
      },
    },
  });

  const query = useQuery({
    queryKey: qk.apiKeys.list(page),
    queryFn: ({ signal }) => listKeys({ page, page_size: PAGE_SIZE }, signal),
  });

  const create = useMutation({
    mutationFn: createKey,
    onSuccess: (result) => {
      setCreated(result);
      setCreating(false);
      form.reset();
      void queryClient.invalidateQueries({ queryKey: qk.apiKeys.all });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const revoke = useMutation({
    mutationFn: revokeKey,
    onSuccess: () => {
      message.success('密钥已撤销，使用它的请求会立即失效');
      void queryClient.invalidateQueries({ queryKey: qk.apiKeys.all });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const columns: Column<APIKey>[] = [
    { key: 'name', title: '名称', render: (k) => <Text fw={600} size="sm">{k.name}</Text> },
    {
      key: 'scopes', wrap: true,
      title: '权限范围',
      render: (k) => (
        <Group gap={4}>
          {k.scopes.length === 0 ? <Text c="dimmed" span>—</Text> : null}
          {k.scopes.map((scope) => <Tag key={scope}>{SCOPE_LABELS[scope] ?? scope}</Tag>)}
        </Group>
      ),
    },
    {
      key: 'status',
      title: '状态',
      width: 110,
      render: (k) => {
        if (k.revoked) return <Tag color="error">已撤销</Tag>;
        if (k.expires_at && new Date(k.expires_at).getTime() < Date.now()) return <Tag color="warning">已过期</Tag>;
        return <Tag color="success">有效</Tag>;
      },
    },
    { key: 'expires_at', title: '过期时间', width: 180, render: (k) => (k.expires_at ? formatDateTime(k.expires_at) : '永不过期') },
    { key: 'created_at', title: '创建时间', width: 180, render: (k) => formatDateTime(k.created_at) },
    {
      key: 'actions',
      title: '操作',
      width: 100,
      render: (k) => k.revoked ? null : (
        <ConfirmButton size="compact-sm" variant="default" color="red" confirmTitle="撤销该密钥？" okText="撤销" danger loading={revoke.isPending && revoke.variables === k.id} onConfirm={() => revoke.mutateAsync(k.id)}>撤销</ConfirmButton>
      ),
    },
  ];

  return (
    <Stack gap="lg">
      <PageHeader title="API 密钥" extra={<Button leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)}>新建密钥</Button>} />

      <Alert icon={<IconInfoCircle size={18} />} color="blue" title="密钥默认长期有效，可直接用于 API 和 MCP。">
        在请求头填写 Authorization: Bearer 加上密钥；有效权限由授权范围和所属用户的角色共同决定。管理权限可用于创建用户或密钥。
      </Alert>

      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isSuccess ? (
        <DataTable
          rows={query.data.items}
          columns={columns}
          rowKey={(k) => k.id}
          empty={<EmptyState />}
          minWidth={820}
          pagination={{ page: query.data.page, pageSize: query.data.page_size, total: query.data.total, pageSizes: [query.data.page_size], onChange: (p) => setPage(p) }}
        />
      ) : null}

      <FormModal
        opened={creating}
        onClose={() => setCreating(false)}
        title="新建 API 密钥"
        okText="创建"
        loading={create.isPending}
        onSubmit={form.onSubmit((v) => create.mutate(buildCreateKeyBody(v)))}
      >
        <TextInput label="名称" withAsterisk placeholder="例如：资料检索 Agent" data-autofocus {...form.getInputProps('name')} />
        <MultiSelect label="权限范围" withAsterisk data={KEY_SCOPE_OPTIONS} placeholder="选择权限" {...form.getInputProps('scopes')} />
        <TextInput label="过期时间（可选）" placeholder="留空长期有效，或填写 RFC3339 时间" {...form.getInputProps('expires_at')} />
      </FormModal>

      <Modal opened={created !== null} onClose={() => setCreated(null)} title="密钥已创建" closeOnClickOutside={false} closeOnEscape={false} withCloseButton={false}>
        <Stack gap="md">
          <Text size="sm">密钥仅在创建时显示一次。请妥善保存，随后可直接作为 Bearer 凭据使用。</Text>
          <Box p="sm" bg="gray.0" className="mono" style={{ borderRadius: 'var(--mantine-radius-md)' }}>{created?.secret}</Box>
          <Group justify="space-between">
            {created ? <CopyButton value={created.secret} label="复制密钥" /> : <span />}
            <Button onClick={() => setCreated(null)}>我已保存</Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
