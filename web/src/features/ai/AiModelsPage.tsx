import { useEffect, useState } from 'react';
import { Autocomplete, Button, Group, NumberInput, PasswordInput, SimpleGrid, Stack, Switch, Text, TextInput } from '@mantine/core';
import { modals } from '@mantine/modals';
import { useForm } from '@mantine/form';
import { IconBolt, IconPlus } from '@tabler/icons-react';
import { useAuth } from '../auth/AuthContext';
import { describeError } from '../../shared/api/errors';
import type { AIProvider } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { FormModal } from '../../shared/ui/FormModal';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import {
  useAIProviders,
  useCreateAIProvider,
  useProbeModels,
  useProbeSavedModels,
  useTestAIProvider,
  useUpdateAIProvider,
} from './queries';

interface ProviderFormValues {
  name: string;
  base_url: string;
  api_key: string;
  priority: number;
  enabled: boolean;
  default_model: string;
  vision_model: string;
}

const EMPTY_FORM: ProviderFormValues = {
  name: '',
  base_url: 'https://api.openai.com/v1',
  api_key: '',
  priority: 100,
  enabled: true,
  default_model: '',
  vision_model: '',
};

function validUrl(value: string): boolean {
  try {
    const url = new URL(value.trim());
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

export function AiModelsPage() {
  const { can } = useAuth();
  const canWrite = can('ai:write');
  const providers = useAIProviders();
  const create = useCreateAIProvider();
  const update = useUpdateAIProvider();
  const probe = useProbeModels();
  const probeSaved = useProbeSavedModels();
  const test = useTestAIProvider();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AIProvider | null>(null);
  const [models, setModels] = useState<string[]>([]);
  const [probing, setProbing] = useState(false);
  const form = useForm<ProviderFormValues>({
    initialValues: EMPTY_FORM,
    validate: {
      name: (v) => (!v.trim() ? '请输入名称' : v.length > 128 ? '最多 128 个字符' : null),
      base_url: (v) => (!v.trim() ? '请输入 OpenAI 兼容根地址' : validUrl(v) ? null : '请输入完整的 http(s) 地址，通常以 /v1 结尾'),
      api_key: (v) => (editing || v.trim() ? null : '请输入 API Key'),
    },
  });

  useEffect(() => {
    if (!open) return;
    const initial: ProviderFormValues = editing
      ? { name: editing.name, base_url: editing.base_url, api_key: '', priority: editing.priority, enabled: editing.enabled, default_model: editing.default_model, vision_model: editing.vision_model }
      : EMPTY_FORM;
    form.setValues(initial);
    form.resetDirty(initial);
    form.clearErrors();
    setModels(editing?.models ?? []);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing]);

  const openCreate = () => { setEditing(null); setOpen(true); };
  const openEdit = (provider: AIProvider) => { setEditing(provider); setOpen(true); };

  const handleProbe = async () => {
    const urlError = !validUrl(form.values.base_url) ? '请输入完整的 http(s) 地址，通常以 /v1 结尾' : null;
    if (urlError) { form.setFieldError('base_url', urlError); return; }
    const values = form.values;
    setProbing(true);
    try {
      let result;
      if (editing && !values.api_key.trim() && values.base_url.trim() === editing.base_url) {
        result = await probeSaved.mutateAsync(editing.id);
      } else {
        result = await probe.mutateAsync({ base_url: values.base_url.trim(), api_key: values.api_key.trim() || undefined });
      }
      setModels(result.models);
      message.success(`探测到 ${result.count} 个模型`);
      if (result.models.length > 0 && !form.values.default_model) form.setFieldValue('default_model', result.models[0]!);
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    } finally {
      setProbing(false);
    }
  };

  const handleSubmit = form.onSubmit(async (values) => {
    const payload = {
      name: values.name.trim(),
      base_url: values.base_url.trim(),
      priority: values.priority,
      enabled: values.enabled,
      default_model: values.default_model.trim(),
      vision_model: values.vision_model.trim(),
      models,
      ...(values.api_key.trim() ? { api_key: values.api_key.trim() } : {}),
    };
    try {
      if (editing) {
        await update.mutateAsync({ id: editing.id, patch: payload });
        message.success('模型端点已保存；密钥不会回显');
      } else {
        await create.mutateAsync(payload);
        message.success('模型端点已创建');
      }
      setOpen(false);
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    }
  });

  const handleTest = (provider: AIProvider) => {
    test.mutate(
      { id: provider.id, model: provider.default_model },
      {
        onSuccess: (result) =>
          modals.open({
            title: result.healthy ? `${provider.name}：调用成功` : `${provider.name}：调用失败`,
            children: (
              <Stack gap={4}>
                <Text size="sm">模型：{result.model || '未配置'}</Text>
                <Text size="sm">{result.message}</Text>
                <Group justify="flex-end" mt="md"><Button onClick={() => modals.closeAll()}>知道了</Button></Group>
              </Stack>
            ),
          }),
        onError: (error) => message.error(describeError(error).title),
      },
    );
  };

  const columns: Column<AIProvider>[] = [
    { key: 'name', title: '端点', render: (p) => <Text fw={600} size="sm">{p.name}</Text> },
    { key: 'default_model', title: '默认模型', render: (p) => p.default_model || '—' },
    { key: 'vision_model', title: '视觉模型', render: (p) => p.vision_model || '—' },
    { key: 'priority', title: '优先级', width: 90, render: (p) => <Text ff="monospace" size="sm">{p.priority}</Text> },
    { key: 'key', title: '密钥', width: 90, render: (p) => (p.api_key_configured ? <Tag color="green">已保存</Tag> : <Tag color="warning">未设置</Tag>) },
    {
      key: 'enabled', title: '启用', width: 90,
      render: (p) => (
        <Switch
          checked={p.enabled}
          disabled={!canWrite}
          aria-label={`启用 ${p.name}`}
          onChange={(e) => update.mutate({ id: p.id, patch: { enabled: e.currentTarget.checked } }, { onError: (error) => message.error(describeError(error).title) })}
        />
      ),
    },
    { key: 'updated_at', title: '更新时间', width: 170, render: (p) => formatDateTime(p.updated_at) },
    {
      key: 'actions', title: '操作', width: 150,
      render: (p) => (
        <Group gap={6} wrap="nowrap">
          <Button size="compact-sm" variant="default" onClick={() => handleTest(p)} disabled={!canWrite || !p.default_model}>测试</Button>
          <Button size="compact-sm" variant="default" onClick={() => openEdit(p)} disabled={!canWrite}>编辑</Button>
        </Group>
      ),
    },
  ];

  return (
    <Stack gap="lg">
      <PageHeader title="AI 模型" extra={<Button leftSection={<IconPlus size={16} />} onClick={openCreate} disabled={!canWrite}>新增端点</Button>} />

      {providers.isPending ? <LoadingState /> : null}
      {providers.isError ? <ErrorState error={providers.error} onRetry={() => void providers.refetch()} /> : null}
      {providers.isSuccess ? <DataTable rows={providers.data} columns={columns} rowKey={(p) => p.id} empty={<EmptyState />} minWidth={900} /> : null}

      <FormModal opened={open} onClose={() => setOpen(false)} title={editing ? '编辑模型端点' : '新增模型端点'} size={640} loading={create.isPending || update.isPending} onSubmit={handleSubmit}>
        <TextInput label="名称" withAsterisk placeholder="例如：主模型网关" data-autofocus {...form.getInputProps('name')} />
        <TextInput label="Base URL" withAsterisk placeholder="https://api.example.com/v1" {...form.getInputProps('base_url')} />
        <PasswordInput label={editing?.api_key_configured ? 'API Key（留空保留原密钥）' : 'API Key'} withAsterisk={!editing} autoComplete="new-password" placeholder="API Key" {...form.getInputProps('api_key')} />
        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
          <NumberInput label="优先级" min={0} max={1000} allowDecimal={false} {...form.getInputProps('priority')} />
          <Switch label="启用" mt={{ base: 0, sm: 28 }} {...form.getInputProps('enabled', { type: 'checkbox' })} />
        </SimpleGrid>
        <Group><Button variant="default" leftSection={<IconBolt size={16} />} onClick={() => void handleProbe()} loading={probing}>探测模型</Button></Group>
        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
          <Autocomplete label="默认模型" data={models} placeholder="选择或输入" {...form.getInputProps('default_model')} />
          <Autocomplete label="视觉模型" data={models} placeholder="选择或输入" {...form.getInputProps('vision_model')} />
        </SimpleGrid>
      </FormModal>
    </Stack>
  );
}
