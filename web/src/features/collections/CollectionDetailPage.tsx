import { useState } from 'react';
import { App, Button, Card, Descriptions, Popconfirm, Space, Typography } from 'antd';
import { ArrowLeftOutlined, EditOutlined } from '@ant-design/icons';
import { Link, useNavigate, useParams } from 'react-router';
import { describeError } from '../../shared/api/errors';
import { formatDateTime } from '../../shared/lib/format';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { CollectionKindTag, SubjectTags } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { CollectionFormModal } from './CollectionFormModal';
import { CollectionItemsEditor } from './CollectionItemsEditor';
import { useCollection, useCollectionsList } from './queries';
import { deleteCollection } from './api';

export function CollectionDetailPage() {
  const { collectionId = '' } = useParams();
  const { can } = useAuth();
  const { message } = App.useApp();
  const navigate = useNavigate();
  const query = useCollection(collectionId);
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <div className="page-stack">
        <BackLink />
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="合集加载失败" />
      </div>
    );
  }

  const collection = query.data;
  const canWrite = can('collections:write');

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteCollection(collection.id, true);
      message.success('合集已删除；其中的资料仍然保留');
      navigate('/collections', { replace: true });
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="page-stack">
      <BackLink />
      <PageHeader
        title={collection.title}
        extra={
          <Space wrap>
            <CollectionKindTag kind={collection.kind} />
            {canWrite ? (
              <Button icon={<EditOutlined />} onClick={() => setEditing(true)}>
                编辑信息
              </Button>
            ) : null}
            {canWrite ? (
              <Popconfirm
                title="删除这个合集？"
                okText="删除"
                okButtonProps={{ danger: true }}
                cancelText="取消"
                onConfirm={() => void handleDelete()}
              >
                <Button danger loading={deleting}>
                  删除合集
                </Button>
              </Popconfirm>
            ) : null}
          </Space>
        }
      />

      <Card title="合集信息">
        <Descriptions
          size="small"
          column={{ xs: 1, md: 2 }}
          items={[
            { key: 'kind', label: '类型', children: <CollectionKindTag kind={collection.kind} /> },
            {
              key: 'parent',
              label: '父合集',
              children: collection.parent_id ? (
                <Link to={`/collections/${collection.parent_id}`}>
                  查看父合集
                </Link>
              ) : (
                '顶层合集'
              ),
            },
            {
              key: 'subjects',
              label: '学科',
              children: <SubjectTags codes={collection.subject_codes} />,
            },
            { key: 'count', label: '条目数', children: `${collection.items.length} 条` },
            { key: 'created', label: '创建时间', children: formatDateTime(collection.created_at) },
            { key: 'updated', label: '更新时间', children: formatDateTime(collection.updated_at) },
            {
              key: 'description',
              label: '描述',
              span: { xs: 1, md: 2 },
              children: collection.description ? (
                <Typography.Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{collection.description}</Typography.Paragraph>
              ) : (
                '—'
              ),
            },
          ]}
        />
      </Card>

      <ChildCollections collectionId={collection.id} />

      <Card title={`合集条目（${collection.items.length}）`}>
        <CollectionItemsEditor collection={collection} canWrite={canWrite} />
      </Card>

      {editing ? (
        <CollectionFormModal
          open={editing}
          collection={collection}
          onClose={() => setEditing(false)}
          onSaved={() => void query.refetch()}
        />
      ) : null}
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/collections" className="inline-flex items-center gap-1 text-sm">
      <ArrowLeftOutlined />
      返回合集列表
    </Link>
  );
}


/** 子合集（ZIP 包等）。顶层列表不展示它们，避免出现两个同名合集。 */
function ChildCollections({ collectionId }: { collectionId: string }) {
  const query = useCollectionsList({ page: 1, page_size: 100, parent_id: collectionId });
  const items = query.data?.items ?? [];
  if (query.isError || items.length === 0) return null;
  return (
    <Card title={`子合集（${items.length}）`}>
      <div className="flex flex-col gap-2">
        {items.map((item) => (
          <Link key={item.id} to={`/collections/${item.id}`} className="flex flex-wrap items-center gap-2">
            <CollectionKindTag kind={item.kind} />
            <span className="font-medium">{item.title}</span>
          </Link>
        ))}
      </div>
    </Card>
  );
}
