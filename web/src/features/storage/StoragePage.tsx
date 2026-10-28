import { useEffect, useState } from 'react';
import { App, Button, Card, Form, InputNumber, Radio, Space, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import { describeError } from '../../shared/api/errors';
import type { ProviderKind, ProviderRole, StorageMode, StorageProvider } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { ProviderFormModal } from './ProviderFormModal';
import { PROVIDER_FIELD_SPECS } from './providerFields';
import { POLICY_MODE_OPTIONS } from './policyCopy';
import { usePolicy, useProviders, useReconcile, useTestProvider, useUpdatePolicy } from './queries';

const ROLE_LABELS: Record<ProviderRole, string> = { primary: '主端', backup: '备端' };

export function StoragePage() {
  const { can } = useAuth();
  const { message, modal } = App.useApp();
  const canWrite = can('storage:write');

  const providers = useProviders();
  const policy = usePolicy(can('storage:read'));
  const testProvider = useTestProvider();
  const updatePolicy = useUpdatePolicy();
  const reconcile = useReconcile();

  const [editing, setEditing] = useState<StorageProvider | null>(null);
  const [creating, setCreating] = useState(false);
  const [mode, setMode] = useState<StorageMode>('mirror_all');
  const [minimum, setMinimum] = useState(1);

  useEffect(() => {
    if (policy.data) {
      setMode(policy.data.mode);
      setMinimum(policy.data.minimum_replicas);
    }
  }, [policy.data]);

  const handleTest = (provider: StorageProvider) => {
    testProvider.mutate(provider.id, {
      onSuccess: (result) => {
        modal.info({
          title: result.healthy ? `${provider.name}：连接正常` : `${provider.name}：连接失败`,
          content: result.message || undefined,
        });
      },
      onError: (error) => message.error(describeError(error).title),
    });
  };

  const savePolicy = () => {
    updatePolicy.mutate(
      { mode, minimum_replicas: minimum },
      {
        onSuccess: () => message.success('存储策略已保存'),
        onError: (error) => message.error(describeError(error).title),
      },
    );
  };

  const columns: ColumnsType<StorageProvider> = [
    {
      title: '端点',
      dataIndex: 'name',
      key: 'name',
      render: (name: string) => <span className="font-medium">{name}</span>,
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 120,
      render: (kind: ProviderKind) => <Tag>{PROVIDER_FIELD_SPECS[kind].label}</Tag>,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 90,
      render: (role: ProviderRole) => <Tag color={role === 'primary' ? 'blue' : 'default'}>{ROLE_LABELS[role]}</Tag>,
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (enabled: boolean) => (enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>),
    },
    {
      title: '优先级 / 权重',
      key: 'scheduling',
      width: 130,
      render: (_: unknown, record) => (
        <span className="text-sm">
          {record.priority} / {record.weight}
        </span>
      ),
    },
    {
      title: '凭据',
      dataIndex: 'secret_configured',
      key: 'secret',
      width: 100,
      render: (configured: boolean, record) =>
        record.kind === 'local' ? '—' : configured ? <Tag color="green">已保存</Tag> : <Tag color="orange">未设置</Tag>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 180,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 170,
      render: (_: unknown, record) => (
        <Space size={4}>
          <Button
            size="small"
            loading={testProvider.isPending && testProvider.variables === record.id}
            onClick={() => handleTest(record)}
            disabled={!canWrite}
          >
            测试连接
          </Button>
          <Button size="small" onClick={() => setEditing(record)} disabled={!canWrite}>
            编辑
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <div className="page-stack">
      <PageHeader
        title="存储"
        extra={
          <Space>
            <Button
              onClick={() =>
                reconcile.mutate(undefined, {
                  onSuccess: (result) => message.success(`已补建 ${result.queued} 个复制任务`),
                  onError: (error) => message.error(describeError(error).title),
                })
              }
              loading={reconcile.isPending}
              disabled={!canWrite}
            >
              补齐副本
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)} disabled={!canWrite}>
              新增端点
            </Button>
          </Space>
        }
      />

      <Card title="副本策略">
        {policy.isPending ? <LoadingState /> : null}
        {policy.isError ? <ErrorState error={policy.error} onRetry={() => void policy.refetch()} /> : null}
        {policy.isSuccess ? (
          <Space direction="vertical" size={16} className="w-full">
            <Radio.Group
              optionType="button"
              buttonStyle="solid"
              value={mode}
              onChange={(event) => setMode(event.target.value as StorageMode)}
              options={POLICY_MODE_OPTIONS}
              disabled={!canWrite}
            />
            <Space align="end" wrap>
              <Form layout="inline">
                <Form.Item label="最低副本数" className="mb-0">
                  <InputNumber
                    min={1}
                    max={20}
                    value={minimum}
                    onChange={(value) => setMinimum(typeof value === 'number' ? value : 1)}
                    disabled={!canWrite}
                  />
                </Form.Item>
              </Form>
              <Button type="primary" onClick={savePolicy} loading={updatePolicy.isPending} disabled={!canWrite}>
                保存策略
              </Button>
            </Space>
          </Space>
        ) : null}
      </Card>

      <Card title="存储端点" styles={{ body: { padding: 0 } }}>
        {providers.isPending ? <LoadingState /> : null}
        {providers.isError ? <ErrorState error={providers.error} onRetry={() => void providers.refetch()} /> : null}
        {providers.isSuccess ? (
          <Table<StorageProvider>
            rowKey="id"
            size="middle"
            dataSource={providers.data}
            columns={columns}
            locale={{ emptyText: <EmptyState /> }}
            pagination={false}
          />
        ) : null}
      </Card>

      <ProviderFormModal
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={() => {
          setCreating(false);
          void providers.refetch();
        }}
      />
      <ProviderFormModal
        open={editing !== null}
        provider={editing ?? undefined}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          void providers.refetch();
        }}
      />
    </div>
  );
}
