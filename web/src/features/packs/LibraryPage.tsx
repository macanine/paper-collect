import { useMemo, useState } from 'react';
import { App, Button, Card, Checkbox, Input, Popconfirm, Select, Space, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link, useNavigate } from 'react-router';
import type { DocType, Grade, Pack, Stage, SubjectCode } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { saveBlob } from '../../shared/lib/download';
import { formatDateTime } from '../../shared/lib/format';
import { DOC_TYPE_LABELS, GRADE_LABELS, SUBJECT_LABELS } from '../../shared/lib/labels';
import { useListSearch } from '../../shared/lib/listSearch';
import { STAGE_OPTIONS } from '../../shared/lib/options';
import { navigateTableRow } from '../../shared/lib/tableNavigation';
import { useCompactViewport } from '../../shared/lib/useCompactViewport';
import { useDebouncedFilterInput } from '../../shared/lib/useDebouncedFilterInput';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { downloadPack, type PackSort } from './api';
import { AddToCollectionModal, BulkEditModal, MergeModal } from './BulkModals';
import { Completeness, PackMeta } from './PackBadges';
import { usePackFacets, usePackMutations, usePacksList } from './queries';

const FILTER_KEYS = ['q', 'content', 'subject', 'grade', 'doc_type', 'cohort', 'region', 'organization', 'tag', 'stage', 'missing', 'collection_id', 'sort'] as const;

function facetOptions<T extends string>(values: { value: string; count: number }[] | undefined, label: (v: T) => string) {
  return (values ?? []).map((f) => ({ value: f.value, label: `${label(f.value as T)}（${f.count}）` }));
}

export function LibraryPage() {
  const { can } = useAuth();
  const canWrite = can('materials:write');
  const compact = useCompactViewport();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const list = useListSearch(FILTER_KEYS);
  const { filters, setFilters } = list;
  const [qInput, setQInput] = useDebouncedFilterInput(filters.q, (q) => setFilters({ q }));
  const [selected, setSelected] = useState<Pack[]>([]);
  const [modal, setModal] = useState<'' | 'edit' | 'topic' | 'merge'>('');
  const facets = usePackFacets();
  const { trash } = usePackMutations();

  const query = usePacksList({
    page: list.page,
    page_size: list.pageSize,
    q: filters.q || undefined,
    content: filters.content === '1' || undefined,
    subject: (filters.subject as SubjectCode) || undefined,
    grade: (filters.grade as Grade) || undefined,
    doc_type: (filters.doc_type as DocType) || undefined,
    cohort: filters.cohort ? Number(filters.cohort) : undefined,
    region: filters.region || undefined,
    organization: filters.organization || undefined,
    tag: filters.tag || undefined,
    stage: (filters.stage as Stage) || undefined,
    missing: filters.missing === 'answers' ? 'answers' : undefined,
    collection_id: filters.collection_id || undefined,
    sort: (filters.sort as PackSort) || undefined,
  });
  const hasFilters = FILTER_KEYS.some((key) => key !== 'sort' && filters[key] !== '');
  const ids = useMemo(() => selected.map((p) => p.id), [selected]);

  const handleTrash = async () => {
    try {
      const result = await trash.mutateAsync(ids);
      message.success(`已移入回收站 ${result.count} 个套卷，30 天内可恢复`);
      setSelected([]);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  const handleDownload = async () => {
    for (const pack of selected) {
      try {
        const { blob, filename } = await downloadPack(pack.id);
        saveBlob(blob, filename ?? `${pack.title}.zip`);
      } catch (error) {
        message.error(`${pack.title}：${errorMessage(error)}`);
      }
    }
  };

  const columns: ColumnsType<Pack> = [
    {
      title: '套卷',
      key: 'title',
      render: (_: unknown, pack) => (
        <div className="flex flex-col gap-1">
          <Link to={`/packs/${pack.id}`} className="font-semibold">
            {pack.title}
          </Link>
          <PackMeta pack={pack} />
        </div>
      ),
    },
    ...(compact
      ? []
      : [
          { title: '齐全度', key: 'complete', width: 190, render: (_: unknown, pack: Pack) => <Completeness pack={pack} /> },
          { title: '文件', key: 'files', width: 70, align: 'right' as const, render: (_: unknown, pack: Pack) => pack.members.length },
          { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 170, render: (value: string) => formatDateTime(value) },
        ]),
  ];

  const clear = () => {
    setQInput('');
    setFilters(Object.fromEntries(FILTER_KEYS.filter((k) => k !== 'sort').map((k) => [k, ''])));
  };

  return (
    <div className="page-stack">
      <PageHeader
        title="资料库"
        extra={
          <Select
            value={filters.sort || 'updated'}
            onChange={(sort) => setFilters({ sort: sort === 'updated' ? '' : sort })}
            options={[
              { value: 'updated', label: '最近更新' },
              { value: 'cohort', label: '按届别与考试时间' },
              { value: 'title', label: '按名称' },
            ]}
            style={{ width: 168 }}
          />
        }
      />

      <Card size="small">
        <div className="library-filters">
          <Input.Search
            allowClear
            placeholder="搜索名称、考试、机构、地区、标签"
            value={qInput}
            onChange={(e) => setQInput(e.target.value)}
            className="library-filters__search"
          />
          <Checkbox checked={filters.content === '1'} onChange={(e) => setFilters({ content: e.target.checked ? '1' : '' })}>
            同时搜正文
          </Checkbox>
          <Select allowClear placeholder="学科" value={filters.subject || undefined} onChange={(v) => setFilters({ subject: v ?? '' })} options={facetOptions<SubjectCode>(facets.data?.subjects, (v) => SUBJECT_LABELS[v] ?? v)} />
          <Select allowClear placeholder="年级" value={filters.grade || undefined} onChange={(v) => setFilters({ grade: v ?? '' })} options={facetOptions<Grade>(facets.data?.grades, (v) => GRADE_LABELS[v] ?? v)} />
          <Select allowClear placeholder="类型" value={filters.doc_type || undefined} onChange={(v) => setFilters({ doc_type: v ?? '' })} options={facetOptions<DocType>(facets.data?.doc_types, (v) => DOC_TYPE_LABELS[v] ?? v)} />
          <Select allowClear placeholder="届别" value={filters.cohort || undefined} onChange={(v) => setFilters({ cohort: v ?? '' })} options={facetOptions<string>(facets.data?.cohorts, (v) => `${v} 届`)} />
          <Select allowClear showSearch placeholder="地区" value={filters.region || undefined} onChange={(v) => setFilters({ region: v ?? '' })} options={facetOptions<string>(facets.data?.regions, (v) => v)} />
          <Select allowClear showSearch placeholder="来源机构" value={filters.organization || undefined} onChange={(v) => setFilters({ organization: v ?? '' })} options={facetOptions<string>(facets.data?.organizations, (v) => v)} />
          <Select allowClear showSearch placeholder="标签" value={filters.tag || undefined} onChange={(v) => setFilters({ tag: v ?? '' })} options={facetOptions<string>(facets.data?.tags, (v) => v)} />
          <Select allowClear placeholder="整理状态" value={filters.stage || undefined} onChange={(v) => setFilters({ stage: v ?? '' })} options={STAGE_OPTIONS} />
          <Checkbox checked={filters.missing === 'answers'} onChange={(e) => setFilters({ missing: e.target.checked ? 'answers' : '' })}>
            只看缺答案
          </Checkbox>
          {hasFilters ? (
            <Button type="link" onClick={clear}>
              清除筛选
            </Button>
          ) : null}
        </div>
      </Card>

      {canWrite && selected.length > 0 ? (
        <Card size="small" className="bulk-bar">
          <Space wrap>
            <Typography.Text strong>已选 {selected.length} 个</Typography.Text>
            <Button onClick={() => setModal('edit')}>批量编目</Button>
            <Button onClick={() => setModal('topic')}>加入专题</Button>
            <Button disabled={selected.length < 2} onClick={() => setModal('merge')}>
              合并为一套
            </Button>
            <Button onClick={() => void handleDownload()}>下载</Button>
            <Popconfirm title={`把 ${selected.length} 个套卷移入回收站？`} okText="移入" onConfirm={() => void handleTrash()}>
              <Button danger>移入回收站</Button>
            </Popconfirm>
            <Button type="link" onClick={() => setSelected([])}>
              取消选择
            </Button>
          </Space>
        </Card>
      ) : null}

      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<Pack>
            rowKey="id"
            size="middle"
            loading={query.isFetching}
            columns={columns}
            dataSource={query.data?.items ?? []}
            rowSelection={
              canWrite
                ? { selectedRowKeys: ids, preserveSelectedRowKeys: true, onChange: (_keys, rows) => setSelected(rows.filter(Boolean)) }
                : undefined
            }
            onRow={(pack) => ({ onClick: (event) => navigateTableRow(event, `/packs/${pack.id}`, navigate), className: 'cursor-pointer' })}
            locale={{ emptyText: <EmptyState description={hasFilters ? '没有符合条件的套卷' : '资料库还是空的，把文件拖到页面任意位置即可上传'} /> }}
            pagination={{
              current: list.page,
              pageSize: list.pageSize,
              total: query.data?.total ?? 0,
              showSizeChanger: !compact,
              simple: compact,
              showTotal: (total) => `共 ${total} 套`,
              onChange: list.setPage,
            }}
          />
        </Card>
      )}

      <BulkEditModal ids={ids} open={modal === 'edit'} onClose={() => setModal('')} />
      <AddToCollectionModal ids={ids} open={modal === 'topic'} onClose={() => setModal('')} />
      <MergeModal packs={selected} open={modal === 'merge'} onClose={() => setModal('')} onMerged={() => setSelected([])} />
    </div>
  );
}
