import { useState } from 'react';
import { App, Button, Card, Form, Input, List, Modal } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { Link, useNavigate } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { useDebouncedFilterInput } from '../../shared/lib/useDebouncedFilterInput';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { useCollectionMutations, useCollectionsList } from './queries';

export function CollectionsListPage() {
  const { can } = useAuth();
  const { message } = App.useApp();
  const navigate = useNavigate();
  const list = useListSearch(['q'] as const);
  const [qInput, setQInput] = useDebouncedFilterInput(list.filters.q, (q) => list.setFilters({ q }));
  const query = useCollectionsList({ page: list.page, page_size: list.pageSize, q: list.filters.q || undefined });
  const { create } = useCollectionMutations();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<{ title: string; description?: string }>();

  const submit = async () => {
    const values = await form.validateFields();
    try {
      const created = await create.mutateAsync({ title: values.title, description: values.description ?? '' });
      message.success('专题已创建，可在资料库勾选套卷后加入');
      setOpen(false);
      form.resetFields();
      navigate(`/collections/${created.id}`);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  return (
    <div className="page-stack">
      <PageHeader
        title="专题"
        description="把散落各处的套卷按复习主题整理在一起，如“导数压轴题”“一轮复习”。"
        extra={
          can('collections:write') ? (
            <Button icon={<PlusOutlined />} onClick={() => setOpen(true)}>
              新建专题
            </Button>
          ) : null
        }
      />
      <Input.Search allowClear placeholder="搜索专题" value={qInput} onChange={(e) => setQInput(e.target.value)} style={{ maxWidth: 360 }} />
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <List
            loading={query.isPending}
            dataSource={query.data?.items ?? []}
            locale={{ emptyText: <EmptyState description="还没有专题" /> }}
            pagination={{ current: list.page, pageSize: list.pageSize, total: query.data?.total ?? 0, onChange: list.setPage, hideOnSinglePage: true }}
            renderItem={(c) => (
              <List.Item className="px-6" extra={<span className="table-subtext">{formatDateTime(c.updated_at)}</span>}>
                <List.Item.Meta title={<Link to={`/collections/${c.id}`}>{c.title}</Link>} description={`${c.pack_count} 个套卷${c.description ? ` · ${c.description}` : ''}`} />
              </List.Item>
            )}
          />
        </Card>
      )}
      <Modal open={open} title="新建专题" okText="创建" onOk={() => void submit()} onCancel={() => setOpen(false)} confirmLoading={create.isPending} destroyOnHidden>
        <Form form={form} layout="vertical">
          <Form.Item name="title" label="名称" rules={[{ required: true, whitespace: true, message: '请填写名称' }]}>
            <Input maxLength={255} placeholder="如 导数压轴题汇编" />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea autoSize={{ minRows: 2 }} maxLength={4000} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
