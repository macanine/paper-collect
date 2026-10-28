import { useEffect, useState } from 'react';
import { App, AutoComplete, Button, Card, Form, Input, InputNumber, Modal, Space, Switch, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { useAuth } from '../auth/AuthContext';
import { describeError } from '../../shared/api/errors';
import type { AIProvider } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
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

export function AiModelsPage() {
  const { can } = useAuth();
  const canWrite = can('ai:write');
  const { message, modal } = App.useApp();
  const providers = useAIProviders();
  const create = useCreateAIProvider();
  const update = useUpdateAIProvider();
  const probe = useProbeModels();
  const probeSaved = useProbeSavedModels();
  const test = useTestAIProvider();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AIProvider | null>(null);
  const [form] = Form.useForm<ProviderFormValues>();
  const [models, setModels] = useState<string[]>([]);
  const [probing, setProbing] = useState(false);

  useEffect(() => {
    if (!open) return;
    const initial: ProviderFormValues = editing
      ? {
          name: editing.name,
          base_url: editing.base_url,
          api_key: '',
          priority: editing.priority,
          enabled: editing.enabled,
          default_model: editing.default_model,
          vision_model: editing.vision_model,
        }
      : EMPTY_FORM;
    form.setFieldsValue(initial);
    setModels(editing?.models ?? []);
  }, [open, editing, form]);

  const openCreate = () => {
    setEditing(null);
    setOpen(true);
  };

  const openEdit = (provider: AIProvider) => {
    setEditing(provider);
    setOpen(true);
  };

  const handleProbe = async () => {
    const values = await form.validateFields(['base_url', 'api_key']);
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
      if (result.models.length > 0 && !form.getFieldValue('default_model')) {
        form.setFieldsValue({ default_model: result.models[0] });
      }
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    } finally {
      setProbing(false);
    }
  };

  const handleSubmit = async () => {
    const values = await form.validateFields();
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
  };

  const handleTest = (provider: AIProvider) => {
    test.mutate(
      { id: provider.id, model: provider.default_model },
      {
        onSuccess: (result) =>
          modal.info({
            title: result.healthy ? `${provider.name}：调用成功` : `${provider.name}：调用失败`,
            content: (
              <div className="flex flex-col gap-1">
                <span>模型：{result.model || '未配置'}</span>
                <span>{result.message}</span>
              </div>
            ),
          }),
        onError: (error) => message.error(describeError(error).title),
      },
    );
  };

  const columns: ColumnsType<AIProvider> = [
    {
      title: '端点',
      dataIndex: 'name',
      key: 'name',
      render: (name: string) => <span className="font-medium">{name}</span>,
    },
    {
      title: '默认模型',
      dataIndex: 'default_model',
      key: 'default_model',
      render: (value: string) => value || '—',
    },
    {
      title: '视觉模型',
      dataIndex: 'vision_model',
      key: 'vision_model',
      render: (value: string) => value || '—',
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      width: 90,
      render: (value: number) => <span className="font-mono text-sm">{value}</span>,
    },
    {
      title: '密钥',
      dataIndex: 'api_key_configured',
      key: 'key',
      width: 90,
      render: (configured: boolean) => (configured ? <Tag color="green">已保存</Tag> : <Tag color="orange">未设置</Tag>),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 90,
      render: (enabled: boolean, record) => (
        <Switch
          size="small"
          checked={enabled}
          disabled={!canWrite}
          onChange={(next) =>
            update.mutate(
              { id: record.id, patch: { enabled: next } },
              { onError: (error) => message.error(describeError(error).title) },
            )
          }
        />
      ),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      render: (_: unknown, record) => (
        <Space size={4}>
          <Button size="small" onClick={() => handleTest(record)} disabled={!canWrite || !record.default_model}>
            测试
          </Button>
          <Button size="small" onClick={() => openEdit(record)} disabled={!canWrite}>
            编辑
          </Button>
        </Space>
      ),
    },
  ];

  const modelOptions = models.map((model) => ({ value: model }));

  return (
    <div className="page-stack">
      <PageHeader
        title="AI 模型"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate} disabled={!canWrite}>
            新增端点
          </Button>
        }
      />

      {providers.isPending ? <LoadingState /> : null}
      {providers.isError ? <ErrorState error={providers.error} onRetry={() => void providers.refetch()} /> : null}
      {providers.isSuccess ? (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<AIProvider>
            rowKey="id"
            size="middle"
            dataSource={providers.data}
            columns={columns}
            locale={{ emptyText: <EmptyState /> }}
            pagination={false}
          />
        </Card>
      ) : null}

      <Modal
        title={editing ? '编辑模型端点' : '新增模型端点'}
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => void handleSubmit()}
        okText="保存"
        cancelText="取消"
        confirmLoading={create.isPending || update.isPending}
        width={640}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" initialValues={EMPTY_FORM}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }, { max: 128 }]}>
            <Input placeholder="例如：主模型网关" />
          </Form.Item>
          <Form.Item
            name="base_url"
            label="Base URL"
            rules={[
              { required: true, message: '请输入 OpenAI 兼容根地址' },
              {
                validator: async (_rule, value: string) => {
                  try {
                    const url = new URL((value ?? '').trim());
                    if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new Error('scheme');
                  } catch {
                    throw new Error('请输入完整的 http(s) 地址，通常以 /v1 结尾');
                  }
                },
              },
            ]}
          >
            <Input placeholder="https://api.example.com/v1" />
          </Form.Item>
          <Form.Item
            name="api_key"
            label={editing?.api_key_configured ? 'API Key（留空保留原密钥）' : 'API Key'}
            rules={editing ? [] : [{ required: true, message: '请输入 API Key' }]}
          >
            <Input.Password autoComplete="new-password" placeholder="API Key" />
          </Form.Item>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Form.Item name="priority" label="优先级" rules={[{ required: true }]}>
              <InputNumber min={0} max={1000} className="w-full" />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </div>
          <div className="flex items-center justify-between gap-2">
            <Button icon={<ThunderboltOutlined />} onClick={() => void handleProbe()} loading={probing}>
              探测模型
            </Button>
          </div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Form.Item name="default_model" label="默认模型">
              <AutoComplete options={modelOptions} placeholder="选择或输入" filterOption />
            </Form.Item>
            <Form.Item name="vision_model" label="视觉模型">
              <AutoComplete options={modelOptions} placeholder="选择或输入" filterOption />
            </Form.Item>
          </div>
        </Form>
      </Modal>
    </div>
  );
}
