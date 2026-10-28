import { useEffect, useState } from 'react';
import { Button, Card, Input, Table } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import { Link, useNavigate } from 'react-router';
import type { Collection, CollectionKind, SubjectCode } from '../../shared/api/types';
import { shortId } from '../../shared/lib/format';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { CollectionKindTag, SubjectTags } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { CollectionFormModal } from './CollectionFormModal';
import { useCollectionsList } from './queries';

const FILTER_KEYS = ['q'] as const;
type FilterKey = (typeof FILTER_KEYS)[number];

export function CollectionsListPage() {
  const { can } = useAuth();
  const navigate = useNavigate();
  const list = useListSearch<FilterKey>(FILTER_KEYS);
  const { setFilters } = list;
  const [qInput, setQInput] = useState(list.filters.q);
  const [creating, setCreating] = useState(false);
  const canWrite = can('collections:write');

  useEffect(() => {
    const trimmed = qInput.trim();
    if (trimmed === list.filters.q) return;
    const timer = window.setTimeout(() => setFilters({ q: trimmed }), 300);
    return () => window.clearTimeout(timer);
  }, [qInput, list.filters.q, setFilters]);

  const query = useCollectionsList({
    page: list.page,
    page_size: list.pageSize,
    q: list.filters.q || undefined,
    roots: true,
  });

  const columns: ColumnsType<Collection> = [
    {
      title: '合集',
      dataIndex: 'title',
      key: 'title',
      render: (_value: string, record) => (
        <Link to={`/collections/${record.id}`} className="truncate font-medium">
          {record.title}
        </Link>
      ),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 110,
      render: (kind: CollectionKind) => <CollectionKindTag kind={kind} />,
    },
    {
      title: '学科',
      dataIndex: 'subject_codes',
      key: 'subject_codes',
      width: 220,
      render: (codes: SubjectCode[]) => <SubjectTags codes={codes} />,
    },
    {
      title: '父合集',
      dataIndex: 'parent_id',
      key: 'parent_id',
      width: 120,
      render: (parentId: string) =>
        parentId ? (
          <Link to={`/collections/${parentId}`} className="font-mono text-xs">
            {shortId(parentId)}
          </Link>
        ) : (
          <span className="text-slate-400">顶层</span>
        ),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
  ];

  const hasFilters = list.filters.q !== '';

  return (
    <div className="page-stack">
      <PageHeader
        title="合集"
        extra={
          canWrite ? (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
              新建合集
            </Button>
          ) : null
        }
      />

      <div className="filter-row">
        <Input.Search
          allowClear
          placeholder="搜索标题或描述"
          value={qInput}
          onChange={(event) => setQInput(event.target.value)}
          onSearch={(value) => setFilters({ q: value.trim() })}
          maxLength={200}
          style={{ width: 300 }}
        />
      </div>

      {query.isError && !query.data ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="合集列表加载失败" />
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<Collection>
            rowKey="id"
            size="middle"
            columns={columns}
            dataSource={query.data?.items ?? []}
            loading={query.isFetching}
            scroll={{ x: 900 }}
            locale={{
              emptyText: query.isPending ? (
                '正在加载…'
              ) : (
                <EmptyState
                  description={hasFilters ? '没有匹配的合集' : '暂无合集'}
                  action={
                    hasFilters ? (
                      <Button
                        onClick={() => {
                          setQInput('');
                          setFilters({ q: '' });
                        }}
                      >
                        清除搜索
                      </Button>
                    ) : canWrite ? (
                      <Button type="primary" onClick={() => setCreating(true)}>
                        新建合集
                      </Button>
                    ) : undefined
                  }
                />
              ),
            }}
            pagination={{
              current: list.page,
              pageSize: list.pageSize,
              total: query.data?.total ?? 0,
              showSizeChanger: true,
              pageSizeOptions: [10, 20, 50, 100],
              showTotal: (total) => `共 ${total} 条`,
              onChange: (page, size) => list.setPage(page, size),
            }}
          />
        </Card>
      )}

      <CollectionFormModal
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={(collection) => navigate(`/collections/${collection.id}`)}
      />
    </div>
  );
}
