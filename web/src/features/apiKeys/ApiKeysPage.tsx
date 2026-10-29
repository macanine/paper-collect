import { useState } from 'react';
import { Alert, App, Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import { SCOPES, type APIKey, type CreatedAPIKey, type Scope } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { SCOPE_LABELS } from '../../shared/lib/labels';
import { PageHeader } from '../../shared/ui/PageHeader';
import { CopyButton } from '../../shared/ui/CopyButton';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { createKey, listKeys, revokeKey } from './api';
import { buildCreateKeyBody, type KeyFormValues } from './keyForm';

const KEY_SCOPE_OPTIONS = SCOPES.map((scope) => ({
  value: scope,
  label: SCOPE_LABELS[scope],
}));

const PAGE_SIZE = 20;

export function ApiKeysPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedAPIKey | null>(null);
  const [form] = Form.useForm<KeyFormValues>();

  const query = useQuery({
    queryKey: ['api-keys', page],
    queryFn: ({ signal }) => listKeys({ page, page_size: PAGE_SIZE }, signal),
  });

  const create = useMutation({
    mutationFn: createKey,
    onSuccess: (result) => {
      setCreated(result);
      setCreating(false);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ['api-keys'] });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const revoke = useMutation({
    mutationFn: revokeKey,
    onSuccess: () => {
      message.success('密钥已撤销，使用它的请求会立即失效');
      void queryClient.invalidateQueries({ queryKey: ['api-keys'] });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const columns: ColumnsType<APIKey> = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (name: string) => <span className="font-medium">{name}</span>,
    },
    {
      title: '权限范围',
      dataIndex: 'scopes',
      key: 'scopes',
      render: (scopes: Scope[]) => (
        <Space size={[4, 4]} wrap>
          {scopes.length === 0 ? <span className="text-slate-400">—</span> : null}
          {scopes.map((scope) => (
            <Tag key={scope}>{SCOPE_LABELS[scope] ?? scope}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '状态',
      key: 'status',
      width: 110,
      render: (_: unknown, record) => {
        if (record.revoked) return <Tag color="red">已撤销</Tag>;
        if (record.expires_at && new Date(record.expires_at).getTime() < Date.now()) return <Tag color="orange">已过期</Tag>;
        return <Tag color="green">有效</Tag>;
      },
    },
    {
      title: '过期时间',
      dataIndex: 'expires_at',
      key: 'expires_at',
      width: 180,
      render: (value: string | null) => (value ? formatDateTime(value) : '永不过期'),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 180,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 110,
      render: (_: unknown, record) =>
        record.revoked ? null : (
          <Popconfirm
            title="撤销该密钥？"
            okText="撤销"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => revoke.mutate(record.id)}
          >
            <Button size="small" danger loading={revoke.isPending && revoke.variables === record.id}>
              撤销
            </Button>
          </Popconfirm>
        ),
    },
  ];

  const handleCreate = async () => {
    const values = await form.validateFields();
    create.mutate(buildCreateKeyBody(values));
  };

  return (
    <div className="page-stack">
      <PageHeader
        title="API 密钥"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
            新建密钥
          </Button>
        }
      />

      <Alert type="info" showIcon message="密钥默认长期有效，可直接用于 API 和 MCP。" description="在请求头填写 Authorization: Bearer 加上密钥；有效权限由授权范围和所属用户的角色共同决定。管理权限可用于创建用户或密钥。" />

      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isSuccess ? (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<APIKey>
            rowKey="id"
            size="middle"
            dataSource={query.data.items}
            columns={columns}
            locale={{ emptyText: <EmptyState /> }}
            pagination={{
              current: query.data.page,
              pageSize: query.data.page_size,
              total: query.data.total,
              showSizeChanger: false,
              onChange: setPage,
            }}
          />
        </Card>
      ) : null}

      <Modal
        title="新建 API 密钥"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={() => void handleCreate()}
        okText="创建"
        cancelText="取消"
        confirmLoading={create.isPending}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" initialValues={{ scopes: ['materials:read', 'collections:read', 'jobs:read'] }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }, { max: 128 }]}>
            <Input placeholder="例如：资料检索 Agent" />
          </Form.Item>
          <Form.Item name="scopes" label="权限范围" rules={[{ required: true, message: '至少选择一个权限' }]}>
            <Select mode="multiple" options={KEY_SCOPE_OPTIONS} placeholder="选择权限" />
          </Form.Item>
          <Form.Item
            name="expires_at"
            label="过期时间（可选）"
            rules={[
              {
                validator: async (_rule, value: string) => {
                  const trimmed = (value ?? '').trim();
                  if (!trimmed) return;
                  const time = new Date(trimmed);
                  if (Number.isNaN(time.getTime())) throw new Error('请输入 RFC3339 时间，例如 2026-12-31T00:00:00Z');
                  if (time.getTime() <= Date.now()) throw new Error('过期时间必须晚于当前时间');
                },
              },
            ]}
          >
            <Input placeholder="留空长期有效，或填写 RFC3339 时间" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="密钥已创建"
        open={created !== null}
        onCancel={() => setCreated(null)}
        onOk={() => setCreated(null)}
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
        maskClosable={false}
      >
        <Typography.Paragraph>密钥仅在创建时显示一次。请妥善保存，随后可直接作为 Bearer 凭据使用。</Typography.Paragraph>
        <div className="mb-3 rounded-md bg-gray-50 p-3 font-mono text-sm break-all">{created?.secret}</div>
        {created ? <CopyButton value={created.secret} label="复制密钥" /> : null}
      </Modal>
    </div>
  );
}
