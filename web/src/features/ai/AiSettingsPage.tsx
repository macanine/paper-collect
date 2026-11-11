import { useEffect, useState } from 'react';
import { Button, Group, NumberInput, PasswordInput, Select, SimpleGrid, Stack, Switch, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { modals } from '@mantine/modals';
import { IconPlus } from '@tabler/icons-react';
import { useAuth } from '../auth/AuthContext';
import { describeError } from '../../shared/api/errors';
import type { OCRProvider, OCRProviderKind } from '../../shared/api/types';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { FormModal } from '../../shared/ui/FormModal';
import { PageHeader } from '../../shared/ui/PageHeader';
import { Section } from '../../shared/ui/Section';
import { message } from '../../shared/ui/feedback';
import { RerunCard } from './RerunCard';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import {
  useAISettings,
  useCreateOCRProvider,
  useOCRProviders,
  useTestOCRProvider,
  useUpdateAISettings,
  useUpdateOCRProvider,
} from './queries';

interface OCRFormValues {
  name: string;
  kind: OCRProviderKind;
  base_url: string;
  model: string;
  token: string;
  enabled: boolean;
  priority: number;
}

const PROVIDER_PRESETS: Record<OCRProviderKind, Pick<OCRFormValues, 'name' | 'base_url' | 'model'>> = {
  rapidocr: {
    name: 'RapidOCR（自建）',
    base_url: 'http://rapidocr:9003',
    model: 'PP-OCRv5',
  },
  paddleocr: {
    name: 'PaddleOCR',
    base_url: 'https://paddleocr.aistudio-app.com',
    model: 'PP-OCRv6',
  },
  mineru: {
    name: 'MinerU',
    base_url: 'https://mineru.net',
    model: '',
  },
};

const EMPTY_PROVIDER: OCRFormValues = {
  ...PROVIDER_PRESETS.paddleocr,
  kind: 'paddleocr',
  token: '',
  enabled: true,
  priority: 100,
};

const KIND_LABELS: Record<OCRProviderKind, string> = {
  paddleocr: 'PaddleOCR',
  mineru: 'MinerU',
  rapidocr: 'RapidOCR',
};

export function AiSettingsPage() {
  const { can } = useAuth();
  const canWrite = can('ai:write');
  const settings = useAISettings();
  const providers = useOCRProviders();
  const save = useUpdateAISettings();
  const createProvider = useCreateOCRProvider();
  const updateProvider = useUpdateOCRProvider();
  const testProvider = useTestOCRProvider();
  const form = useForm<{ ocr_provider_id: string | null; max_pages: number; max_images: number }>({
    initialValues: { ocr_provider_id: null, max_pages: 1, max_images: 0 },
    validate: {
      max_pages: (v) => (typeof v === 'number' && v >= 1 && v <= 10 ? null : '请输入 1 到 10'),
      max_images: (v) => (typeof v === 'number' && v >= 0 && v <= 4 ? null : '请输入 0 到 4'),
    },
  });
  const [providerOpen, setProviderOpen] = useState(false);
  const [editingProvider, setEditingProvider] = useState<OCRProvider | null>(null);
  const providerForm = useForm<OCRFormValues>({
    initialValues: EMPTY_PROVIDER,
    validate: {
      name: (v) => (!v.trim() ? '请输入名称' : v.length > 128 ? '最多 128 个字符' : null),
      base_url: (v) => { try { const u = new URL(v.trim()); return u.protocol === 'http:' || u.protocol === 'https:' ? null : '请输入完整的 http(s) 地址'; } catch { return '请输入完整的 http(s) 地址'; } },
    },
  });
  const providerKind = providerForm.values.kind;

  useEffect(() => {
    if (!settings.data) return;
    const values = { ocr_provider_id: settings.data.ocr_provider_id || null, max_pages: settings.data.max_pages, max_images: settings.data.max_images };
    form.setValues(values);
    form.resetDirty(values);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings.data]);

  const handleSave = form.onSubmit(async (values) => {
    try {
      await save.mutateAsync({ ocr_provider_id: values.ocr_provider_id ?? '', max_pages: values.max_pages, max_images: values.max_images });
      message.success('AI 解析设置已保存，等待 AI 的文件会自动继续');
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    }
  });

  const openProvider = (provider?: OCRProvider) => {
    setEditingProvider(provider ?? null);
    const values = provider
      ? { name: provider.name, kind: provider.kind, base_url: provider.base_url, model: provider.model, token: '', enabled: provider.enabled, priority: provider.priority }
      : EMPTY_PROVIDER;
    providerForm.setValues(values);
    providerForm.resetDirty(values);
    providerForm.clearErrors();
    setProviderOpen(true);
  };

  const handleKindChange = (kind: OCRProviderKind) => {
    const preset = PROVIDER_PRESETS[kind];
    providerForm.setValues({ kind, base_url: preset.base_url, model: preset.model, name: preset.name });
  };

  const handleProviderSubmit = providerForm.onSubmit(async (values) => {
    const payload = {
      name: values.name.trim(),
      base_url: values.base_url.trim(),
      model: values.model.trim(),
      enabled: values.enabled,
      priority: values.priority,
      ...(values.token.trim() ? { token: values.token.trim() } : {}),
    };
    try {
      if (editingProvider) {
        await updateProvider.mutateAsync({ id: editingProvider.id, patch: payload });
        message.success('OCR 服务商已保存；令牌不会回显');
      } else {
        await createProvider.mutateAsync({ ...payload, kind: values.kind });
        message.success('OCR 服务商已创建');
      }
      setProviderOpen(false);
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    }
  });

  const handleTest = (provider: OCRProvider) => {
    testProvider.mutate(provider.id, {
      onSuccess: (result) =>
        modals.open({
          title: result.healthy ? `${provider.name}：连接成功` : `${provider.name}：连接失败`,
          children: (
            <Stack gap="md">
              <Text size="sm">{result.message}</Text>
              <Group justify="flex-end"><Button onClick={() => modals.closeAll()}>知道了</Button></Group>
            </Stack>
          ),
        }),
      onError: (error) => {
        const { title, detail } = describeError(error);
        message.error(detail ? `${title}：${detail}` : title);
      },
    });
  };

  const providerColumns: Column<OCRProvider>[] = [
    {
      key: 'name', title: '服务商',
      render: (row) => <Stack gap={4}><Text fw={600} size="sm">{row.name}</Text><Text ff="monospace" size="xs" c="dimmed">{KIND_LABELS[row.kind]}</Text></Stack>,
    },
    { key: 'model', title: '模型', render: (row) => row.model || '—' },
    { key: 'token', title: '令牌', width: 90, render: (row) => (row.token_configured ? <Tag color="success">已保存</Tag> : <Tag color="warning">未设置</Tag>) },
    { key: 'priority', title: '优先级', width: 85, render: (row) => row.priority },
    {
      key: 'enabled', title: '启用', width: 80,
      render: (row) => (
        <Switch
          checked={row.enabled}
          disabled={!canWrite}
          aria-label={`启用 ${row.name}`}
          onChange={(e) => updateProvider.mutate({ id: row.id, patch: { enabled: e.currentTarget.checked } }, { onError: (error) => message.error(describeError(error).title) })}
        />
      ),
    },
    {
      key: 'actions', title: '操作', width: 140,
      render: (row) => (
        <Group gap="xs" wrap="nowrap">
          <Button size="compact-sm" variant="default" onClick={() => handleTest(row)} disabled={!canWrite}>测试</Button>
          <Button size="compact-sm" variant="default" onClick={() => openProvider(row)} disabled={!canWrite}>编辑</Button>
        </Group>
      ),
    },
  ];

  const providerOptions = (providers.data ?? []).map((provider) => ({
    value: provider.id,
    label: `${provider.name} · ${KIND_LABELS[provider.kind]}${provider.model ? ` · ${provider.model}` : ''}`,
    disabled: !provider.enabled,
  }));

  return (
    <Stack gap="lg">
      <PageHeader title="AI 解析" extra={<Button leftSection={<IconPlus size={16} />} onClick={() => openProvider()} disabled={!canWrite}>新增 OCR 服务商</Button>} />

      {settings.isPending ? <LoadingState /> : null}
      {settings.isError ? <ErrorState error={settings.error} onRetry={() => void settings.refetch()} /> : null}
      {providers.isError ? <ErrorState error={providers.error} onRetry={() => void providers.refetch()} /> : null}
      {settings.isSuccess ? (
        <Section title="解析设置">
          <form onSubmit={handleSave}>
            <Stack gap="md">
              <Text size="sm" c="dimmed">每个新文件都由 AI 阅读、分组成套并确定编目，结果通过校验后自动入库；没有可用的模型或 OCR 时文件会等待，配置好后自动继续。</Text>
              <Select label="OCR 服务商" description="分析与搜题索引共用当前选中的服务商；切换后旧解析缓存会自动失效。" clearable allowDeselect placeholder="选择服务商" data={providerOptions} nothingFoundMessage="请先新增 OCR 服务商" {...form.getInputProps('ocr_provider_id')} />
              <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
                <NumberInput label="最多解析页数" min={1} max={10} allowDecimal={false} {...form.getInputProps('max_pages')} />
                <NumberInput label="发送给视觉模型的图片数" min={0} max={4} allowDecimal={false} {...form.getInputProps('max_images')} />
              </SimpleGrid>
              <Group><Button type="submit" loading={save.isPending} disabled={!canWrite}>保存设置</Button></Group>
            </Stack>
          </form>
        </Section>
      ) : null}

      <RerunCard canWrite={canWrite} />

      {providers.isPending ? <LoadingState /> : null}
      {providers.isSuccess ? (
        <Section title="OCR 服务商" flush>
          <DataTable bare rows={providers.data} columns={providerColumns} rowKey={(p) => p.id} empty={<EmptyState />} minWidth={640} />
        </Section>
      ) : null}

      <FormModal opened={providerOpen} onClose={() => setProviderOpen(false)} title={editingProvider ? '编辑 OCR 服务商' : '新增 OCR 服务商'} size={640} loading={createProvider.isPending || updateProvider.isPending} onSubmit={handleProviderSubmit}>
        <Select
          label="类型"
          withAsterisk
          disabled={Boolean(editingProvider)}
          data={[{ value: 'rapidocr', label: 'RapidOCR（自建）' }, { value: 'paddleocr', label: 'PaddleOCR' }, { value: 'mineru', label: 'MinerU' }]}
          value={providerKind}
          onChange={(value) => value && handleKindChange(value as OCRProviderKind)}
        />
        <TextInput label="名称" withAsterisk placeholder="例如：PaddleOCR 主服务" {...providerForm.getInputProps('name')} />
        <TextInput label="Base URL" withAsterisk {...providerForm.getInputProps('base_url')} />
        <TextInput label="模型" placeholder={providerKind === 'paddleocr' ? 'PP-OCRv6' : providerKind === 'rapidocr' ? 'PP-OCRv5' : 'MinerU 不需要模型名'} {...providerForm.getInputProps('model')} />
        <PasswordInput
          label={providerKind === 'rapidocr' ? '令牌（自建服务可留空）' : editingProvider?.token_configured ? '令牌（留空保留原令牌）' : '令牌'}
          description={providerKind === 'paddleocr' ? '在 AI Studio 的 PaddleOCR 任务页获取 access token。' : providerKind === 'rapidocr' ? '自建 RapidOCR 默认不校验令牌。' : undefined}
          autoComplete="new-password"
          {...providerForm.getInputProps('token')}
        />
        <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
          <NumberInput label="优先级" min={0} max={1000} allowDecimal={false} {...providerForm.getInputProps('priority')} />
          <Switch label="启用" mt={{ base: 0, sm: 28 }} {...providerForm.getInputProps('enabled', { type: 'checkbox' })} />
        </SimpleGrid>
      </FormModal>
    </Stack>
  );
}
