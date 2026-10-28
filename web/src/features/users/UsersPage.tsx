import { useState } from 'react';
import { App, Button, Card, Form, Input, Modal, Select, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { User, UserRole } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { USER_ROLE_LABELS } from '../../shared/lib/labels';
import { USER_ROLE_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { createUser, listUsers } from './api';

const PAGE_SIZE = 20;

interface UserFormValues {
  username: string;
  password: string;
  role: UserRole;
}

export function UsersPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const [form] = Form.useForm<UserFormValues>();

  const query = useQuery({
    queryKey: ['users', page],
    queryFn: ({ signal }) => listUsers({ page, page_size: PAGE_SIZE }, signal),
  });

  const create = useMutation({
    mutationFn: createUser,
    onSuccess: () => {
      message.success('用户已创建');
      setCreating(false);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const columns: ColumnsType<User> = [
    {
      title: '用户名',
      dataIndex: 'username',
      key: 'username',
      render: (name: string) => <span className="font-medium">{name}</span>,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 120,
      render: (role: UserRole) => (
        <Tag color={role === 'admin' ? 'red' : role === 'editor' ? 'blue' : 'default'}>{USER_ROLE_LABELS[role]}</Tag>
      ),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 100,
      render: (enabled: boolean) => (enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 180,
      render: (value: string) => formatDateTime(value),
    },
  ];

  const handleCreate = async () => {
    const values = await form.validateFields();
    create.mutate({ username: values.username.trim(), password: values.password, role: values.role });
  };

  return (
    <div className="page-stack">
      <PageHeader
        title="用户"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
            新建用户
          </Button>
        }
      />

      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isSuccess ? (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<User>
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
        title="新建用户"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={() => void handleCreate()}
        okText="创建"
        cancelText="取消"
        confirmLoading={create.isPending}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" initialValues={{ role: 'viewer' }}>
          <Form.Item
            name="username"
            label="用户名"
            rules={[
              { required: true, message: '请输入用户名' },
              { pattern: /^[A-Za-z0-9_-]{3,64}$/, message: '3-64 位字母、数字、下划线或连字符' },
            ]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="password"
            label="初始密码"
            rules={[
              { required: true, message: '请输入密码' },
              { min: 12, message: '至少 12 个字符' },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={USER_ROLE_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
