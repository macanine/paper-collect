import { useEffect, useState } from 'react';
import {
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import { useAuth } from '../auth/AuthContext';
import { describeError } from '../../shared/api/errors';
import type { OCRProvider, OCRProviderKind } from '../../shared/api/types';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import {
  useAISettings,
  useCreateOCRProvider,
  useOCRProviders,
  useTestOCRProvider,
  useUpdateAISettings,
  useUpdateOCRProvider,
} from './queries';

interface SettingsFormValues {
  analysis_enabled: boolean;
  ocr_provider_id: string;
  max_pages: number;
  max_images: number;
}

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
  const { message, modal } = App.useApp();
  const settings = useAISettings();
  const providers = useOCRProviders();
  const save = useUpdateAISettings();
  const createProvider = useCreateOCRProvider();
  const updateProvider = useUpdateOCRProvider();
  const testProvider = useTestOCRProvider();
  const [form] = Form.useForm<SettingsFormValues>();
  const [providerForm] = Form.useForm<OCRFormValues>();
  const [providerOpen, setProviderOpen] = useState(false);
  const [editingProvider, setEditingProvider] = useState<OCRProvider | null>(null);
  const [providerKind, setProviderKind] = useState<OCRProviderKind>('paddleocr');

  useEffect(() => {
    if (!settings.data) return;
    form.setFieldsValue({
      analysis_enabled: settings.data.analysis_enabled,
      ocr_provider_id: settings.data.ocr_provider_id,
      max_pages: settings.data.max_pages,
      max_images: settings.data.max_images,
    });
  }, [settings.data, form]);

  const handleSave = async () => {
    const values = await form.validateFields();
    try {
      await save.mutateAsync({
        analysis_enabled: values.analysis_enabled,
        ocr_provider_id: values.ocr_provider_id ?? '',
        mineru_base_url: settings.data?.mineru_base_url ?? '',
        max_pages: values.max_pages,
        max_images: values.max_images,
      });
      message.success('AI 解析设置已保存');
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    }
  };

  const openProvider = (provider?: OCRProvider) => {
    setEditingProvider(provider ?? null);
    setProviderKind(provider?.kind ?? 'paddleocr');
    providerForm.setFieldsValue(
      provider
        ? {
            name: provider.name,
            kind: provider.kind,
            base_url: provider.base_url,
            model: provider.model,
            token: '',
            enabled: provider.enabled,
            priority: provider.priority,
          }
        : EMPTY_PROVIDER,
    );
    setProviderOpen(true);
  };

  const handleKindChange = (kind: OCRProviderKind) => {
    setProviderKind(kind);
    const preset = PROVIDER_PRESETS[kind];
    providerForm.setFieldsValue({
      kind,
      base_url: preset.base_url,
      model: preset.model,
      name: preset.name,
    });
  };

  const handleProviderSubmit = async () => {
    const values = await providerForm.validateFields();
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
  };

  const handleTest = (provider: OCRProvider) => {
    testProvider.mutate(provider.id, {
      onSuccess: (result) =>
        modal.info({
          title: result.healthy ? `${provider.name}：连接成功` : `${provider.name}：连接失败`,
          content: result.message,
        }),
      onError: (error) => {
        const { title, detail } = describeError(error);
        message.error(detail ? `${title}：${detail}` : title);
      },
    });
  };

  const providerColumns: ColumnsType<OCRProvider> = [
    {
      title: '服务商',
      key: 'name',
      render: (_: unknown, row) => (
        <div className="flex flex-col gap-1">
          <span className="font-medium">{row.name}</span>
          <span className="font-mono text-xs">{KIND_LABELS[row.kind]}</span>
        </div>
      ),
    },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      render: (value: string) => value || '—',
    },
    {
      title: '令牌',
      dataIndex: 'token_configured',
      key: 'token',
      width: 90,
      render: (configured: boolean) => (configured ? <Tag color="green">已保存</Tag> : <Tag color="orange">未设置</Tag>),
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 85,
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (enabled: boolean, row) => (
        <Switch
          size="small"
          checked={enabled}
          disabled={!canWrite}
          onChange={(next) =>
            updateProvider.mutate(
              { id: row.id, patch: { enabled: next } },
              { onError: (error) => message.error(describeError(error).title) },
            )
          }
        />
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 140,
      render: (_: unknown, row) => (
        <Space size={4}>
          <Button size="small" onClick={() => handleTest(row)} disabled={!canWrite}>
            测试
          </Button>
          <Button size="small" onClick={() => openProvider(row)} disabled={!canWrite}>
            编辑
          </Button>
        </Space>
      ),
    },
  ];

  const providerOptions = (providers.data ?? []).map((provider) => ({
    value: provider.id,
    label: `${provider.name} · ${KIND_LABELS[provider.kind]}${provider.model ? ` · ${provider.model}` : ''}`,
    disabled: !provider.enabled,
  }));

  return (
    <div className="page-stack">
      <PageHeader
        title="AI 解析"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => openProvider()} disabled={!canWrite}>
            新增 OCR 服务商
          </Button>
        }
      />

      {settings.isPending ? <LoadingState /> : null}
      {settings.isError ? <ErrorState error={settings.error} onRetry={() => void settings.refetch()} /> : null}
      {providers.isError ? <ErrorState error={providers.error} onRetry={() => void providers.refetch()} /> : null}
      {settings.isSuccess ? (
        <Card title="解析设置" className="max-w-[760px]">
          <Form form={form} layout="vertical">
            <Form.Item
              name="analysis_enabled"
              label="自动识别并应用"
              valuePropName="checked"
              extra="识别成功后自动更新标题、文件名和编目字段；仍可人工修改。"
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="ocr_provider_id"
              label="OCR 服务商"
              extra="分析与搜题索引共用当前选中的服务商；切换后旧解析缓存会自动失效。"
            >
              <Select allowClear placeholder="选择服务商" options={providerOptions} notFoundContent="请先新增 OCR 服务商" />
            </Form.Item>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Form.Item name="max_pages" label="最多解析页数" rules={[{ required: true }]}>
                <InputNumber min={1} max={10} className="w-full" />
              </Form.Item>
              <Form.Item name="max_images" label="发送给视觉模型的图片数" rules={[{ required: true }]}>
                <InputNumber min={0} max={4} className="w-full" />
              </Form.Item>
            </div>
            <Button type="primary" onClick={() => void handleSave()} loading={save.isPending} disabled={!canWrite}>
              保存设置
            </Button>
          </Form>
        </Card>
      ) : null}

      {providers.isPending ? <LoadingState /> : null}
      {providers.isSuccess ? (
        <Card title="OCR 服务商" styles={{ body: { padding: 0 } }}>
          <Table<OCRProvider>
            rowKey="id"
            size="middle"
            dataSource={providers.data}
            columns={providerColumns}
            locale={{ emptyText: <EmptyState /> }}
            pagination={false}
          />
        </Card>
      ) : null}

      <Modal
        title={editingProvider ? '编辑 OCR 服务商' : '新增 OCR 服务商'}
        open={providerOpen}
        onCancel={() => setProviderOpen(false)}
        onOk={() => void handleProviderSubmit()}
        okText="保存"
        cancelText="取消"
        confirmLoading={createProvider.isPending || updateProvider.isPending}
        width={640}
        destroyOnHidden
      >
        <Form form={providerForm} layout="vertical" initialValues={EMPTY_PROVIDER}>
          <Form.Item name="kind" label="类型" rules={[{ required: true }]}>
            <Select
              disabled={Boolean(editingProvider)}
              options={[
                { value: 'rapidocr', label: 'RapidOCR（自建）' },
                { value: 'paddleocr', label: 'PaddleOCR' },
                { value: 'mineru', label: 'MinerU' },
              ]}
              onChange={(value: OCRProviderKind) => handleKindChange(value)}
            />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }, { max: 128 }]}>
            <Input placeholder="例如：PaddleOCR 主服务" />
          </Form.Item>
          <Form.Item
            name="base_url"
            label="Base URL"
            rules={[
              { required: true },
              {
                validator: async (_rule, value: string) => {
                  try {
                    const url = new URL((value ?? '').trim());
                    if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new Error('scheme');
                  } catch {
                    throw new Error('请输入完整的 http(s) 地址');
                  }
                },
              },
            ]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="model" label="模型">
            <Input
              placeholder={
                providerKind === 'paddleocr'
                  ? 'PP-OCRv6'
                  : providerKind === 'rapidocr'
                    ? 'PP-OCRv5'
                    : 'MinerU 不需要模型名'
              }
            />
          </Form.Item>
          <Form.Item
            name="token"
            label={
              providerKind === 'rapidocr'
                ? '令牌（自建服务可留空）'
                : editingProvider?.token_configured
                  ? '令牌（留空保留原令牌）'
                  : '令牌'
            }
            extra={
              providerKind === 'paddleocr'
                ? '在 AI Studio 的 PaddleOCR 任务页获取 access token。'
                : providerKind === 'rapidocr'
                  ? '自建 RapidOCR 默认不校验令牌。'
                  : undefined
            }
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Form.Item name="priority" label="优先级" rules={[{ required: true }]}>
              <InputNumber min={0} max={1000} className="w-full" />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </div>
        </Form>
      </Modal>
    </div>
  );
}
