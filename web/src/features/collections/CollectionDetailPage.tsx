import { useState } from 'react';
import { App, Button, Card, Form, Input, Modal, Popconfirm, Space } from 'antd';
import { ArrowDownOutlined, ArrowUpOutlined } from '@ant-design/icons';
import { Link, useNavigate, useParams } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { BackLink } from '../../shared/ui/BackLink';
import { LinkButton } from '../../shared/ui/LinkButton';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { Completeness, PackMeta } from '../packs/PackBadges';
import { useCollection, useCollectionMutations } from './queries';
import { href } from '../../app/routes';

export function CollectionDetailPage() {
  const { collectionId = '' } = useParams();
  const { can } = useAuth();
  const canWrite = can('collections:write');
  const { message } = App.useApp();
  const navigate = useNavigate();
  const query = useCollection(collectionId);
  const { update, replacePacks, remove } = useCollectionMutations();
  const [editing, setEditing] = useState(false);
  const [form] = Form.useForm<{ title: string; description?: string }>();

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <div className="page-stack">
        <BackLink to="/collections">返回专题</BackLink>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="专题加载失败" />
      </div>
    );
  }
  const collection = query.data;
  const order = collection.packs.map((p) => p.id);

  const save = async () => {
    const values = await form.validateFields();
    try {
      await update.mutateAsync({ id: collection.id, body: { title: values.title, description: values.description ?? '' } });
      setEditing(false);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  const setOrder = async (next: string[]) => {
    try {
      await replacePacks.mutateAsync({ id: collection.id, packIds: next });
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  const move = (index: number, delta: number) => {
    const next = [...order];
    const [id] = next.splice(index, 1);
    next.splice(index + delta, 0, id as string);
    void setOrder(next);
  };

  return (
    <div className="page-stack">
      <BackLink to="/collections">返回专题</BackLink>
      <PageHeader
        title={collection.title}
        description={collection.description || `${collection.packs.length} 个套卷`}
        extra={
          <Space wrap>
            <LinkButton type="primary" to={`/library?collection_id=${collection.id}`}>
              在资料库中查看
            </LinkButton>
            {canWrite ? (
              <>
                <Button
                  onClick={() => {
                    form.setFieldsValue({ title: collection.title, description: collection.description });
                    setEditing(true);
                  }}
                >
                  编辑
                </Button>
                <Popconfirm title="删除专题？其中的套卷不会被删除" onConfirm={() => void remove.mutateAsync(collection.id).then(() => navigate('/collections', { replace: true }))}>
                  <Button danger>删除专题</Button>
                </Popconfirm>
              </>
            ) : null}
          </Space>
        }
      />
      <Card styles={{ body: { padding: 0 } }}>
        {collection.packs.length === 0 ? (
          <EmptyState description="专题还是空的：在资料库勾选套卷，点“加入专题”" />
        ) : (
          <ul className="collection-pack-list">
            {collection.packs.map((pack, index) => (
              <li key={pack.id} className="collection-pack-list__item">
                <div className="collection-pack-list__content">
                  <Link to={href.pack(pack.id)} className="collection-pack-list__title">
                    {pack.title}
                  </Link>
                  <div className="collection-pack-list__meta">
                    <PackMeta pack={pack} />
                    <Completeness pack={pack} />
                  </div>
                </div>
                {canWrite ? (
                  <Space size={4} className="collection-pack-list__actions">
                    <Button type="text" size="small" aria-label="上移" icon={<ArrowUpOutlined />} disabled={index === 0} onClick={() => move(index, -1)} />
                    <Button type="text" size="small" aria-label="下移" icon={<ArrowDownOutlined />} disabled={index === order.length - 1} onClick={() => move(index, 1)} />
                    <Button type="link" size="small" danger onClick={() => void setOrder(order.filter((id) => id !== pack.id))}>
                      移出
                    </Button>
                  </Space>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Modal open={editing} title="编辑专题" okText="保存" onOk={() => void save()} onCancel={() => setEditing(false)} confirmLoading={update.isPending} destroyOnHidden>
        <Form form={form} layout="vertical">
          <Form.Item name="title" label="名称" rules={[{ required: true, whitespace: true, message: '请填写名称' }]}>
            <Input maxLength={255} />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea autoSize={{ minRows: 2 }} maxLength={4000} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
