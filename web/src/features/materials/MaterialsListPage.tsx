import { ArrowRightOutlined, CloudUploadOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Input, Select, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link, useNavigate } from 'react-router';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { MaterialStatusTag, RoleTag, SubjectTags } from '../../shared/ui/StatusTags';
import { useListSearch } from '../../shared/lib/listSearch';
import { GRADE_LABELS } from '../../shared/lib/labels';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { GRADE_OPTIONS, MATERIAL_STATUS_OPTIONS, ROLE_OPTIONS, SUBJECT_OPTIONS } from '../../shared/lib/options';
import { useCompactViewport } from '../../shared/lib/useCompactViewport';
import { useDebouncedFilterInput } from '../../shared/lib/useDebouncedFilterInput';
import { navigateTableRow } from '../../shared/lib/tableNavigation';
import type { Grade, Material, MaterialStatus, Role, SubjectCode } from '../../shared/api/types';
import { useAuth } from '../auth/AuthContext';
import { CopyButton } from '../../shared/ui/CopyButton';
import { LinkButton } from '../../shared/ui/LinkButton';
import { useMaterialsList } from './queries';

const FILTER_KEYS = ['q', 'subject', 'role', 'status', 'grade', 'sha256'] as const;
type FilterKey = (typeof FILTER_KEYS)[number];

export function MaterialsListPage() {
  const { can } = useAuth();
  const compact = useCompactViewport();
  const navigate = useNavigate();
  const list = useListSearch<FilterKey>(FILTER_KEYS);
  const { setFilters } = list;
  const [qInput, setQInput] = useDebouncedFilterInput(list.filters.q, (q) => setFilters({ q }));

  const query = useMaterialsList({
    page: list.page,
    page_size: list.pageSize,
    q: list.filters.q || undefined,
    subject: (list.filters.subject as SubjectCode) || undefined,
    role: (list.filters.role as Role) || undefined,
    status: (list.filters.status as MaterialStatus) || undefined,
    grade: (list.filters.grade as Grade) || undefined,
    sha256: list.filters.sha256 || undefined,
  });

  const hasFilters = FILTER_KEYS.some((key) => list.filters[key] !== '');

  const desktopColumns: ColumnsType<Material> = [
    {
      title: '标题',
      dataIndex: 'title',
      key: 'title',
      render: (_value: string, record) => (
        <Link to={`/materials/${record.id}`} className="truncate font-medium">
          {record.title}
        </Link>
      ),
    },
    {
      title: '学科',
      dataIndex: 'subject_codes',
      key: 'subject_codes',
      width: 220,
      render: (codes: SubjectCode[]) => <SubjectTags codes={codes} />,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 96,
      render: (role: Role) => <RoleTag role={role} />,
    },
    {
      title: '年级 / 学年',
      key: 'grade',
      width: 150,
      render: (_: unknown, record) => (
        <span className="text-sm">
          {GRADE_LABELS[record.grade]}
          {record.academic_year > 0 ? ` · ${record.academic_year} 学年` : ''}
        </span>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (status: MaterialStatus) => <MaterialStatusTag status={status} />,
    },
    {
      title: '大小',
      dataIndex: 'size_bytes',
      key: 'size_bytes',
      width: 100,
      align: 'right',
      render: (size: number) => formatBytes(size),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
  ];

  const compactColumns: ColumnsType<Material> = [
    {
      title: '资料',
      key: 'summary',
      render: (_: unknown, material) => (
        <Link to={`/materials/${material.id}`} className="compact-entity">
          <span className="compact-entity__heading">
            <span className="compact-entity__title">{material.title}</span>
            <ArrowRightOutlined className="compact-entity__arrow" />
          </span>
          <span className="compact-entity__tags">
            <MaterialStatusTag status={material.status} />
            <RoleTag role={material.role} />
            <SubjectTags codes={material.subject_codes} />
          </span>
          <span className="compact-entity__meta">
            <span>{GRADE_LABELS[material.grade]}</span>
            {material.academic_year > 0 ? <span>{material.academic_year} 学年</span> : null}
            <span>{formatBytes(material.size_bytes)}</span>
            <span>{formatDateTime(material.updated_at)}</span>
          </span>
        </Link>
      ),
    },
  ];

  const clearFilters = () => {
    setQInput('');
    setFilters({ q: '', subject: '', role: '', status: '', grade: '', sha256: '' });
  };

  const emptyAction = hasFilters ? (
    <Button onClick={clearFilters}>清除筛选</Button>
  ) : can('uploads:write') ? (
    <LinkButton type="primary" to="/uploads/new">上传资料</LinkButton>
  ) : undefined;

  return (
    <div className="page-stack">
      <PageHeader
        title="资料"
        extra={
          can('uploads:write') ? (
            <LinkButton type="primary" icon={<CloudUploadOutlined />} to="/uploads/new">
              上传资料
            </LinkButton>
          ) : null
        }
      />

      <div className="filter-row">
        <Input.Search
          allowClear
          aria-label="搜索标题或描述"
          placeholder="搜索标题或描述"
          value={qInput}
          onChange={(event) => setQInput(event.target.value)}
          onSearch={(value) => setFilters({ q: value.trim() })}
          style={{ width: 260 }}
          maxLength={200}
        />
        <Select
          allowClear
          aria-label="学科"
          placeholder="学科"
          style={{ width: 160 }}
          options={SUBJECT_OPTIONS}
          value={list.filters.subject || undefined}
          onChange={(value: string | undefined) => setFilters({ subject: value ?? '' })}
        />
        <Select
          allowClear
          aria-label="角色"
          placeholder="角色"
          style={{ width: 128 }}
          options={ROLE_OPTIONS}
          value={list.filters.role || undefined}
          onChange={(value: string | undefined) => setFilters({ role: value ?? '' })}
        />
        <Select
          allowClear
          aria-label="年级"
          placeholder="年级"
          style={{ width: 128 }}
          options={GRADE_OPTIONS}
          value={list.filters.grade || undefined}
          onChange={(value: string | undefined) => setFilters({ grade: value ?? '' })}
        />
        <Select
          allowClear
          aria-label="归档状态"
          placeholder="归档状态"
          style={{ width: 150 }}
          options={MATERIAL_STATUS_OPTIONS}
          value={list.filters.status || undefined}
          onChange={(value: string | undefined) => setFilters({ status: value ?? '' })}
        />
        {hasFilters || qInput ? <Button onClick={clearFilters}>清除筛选</Button> : null}
      </div>

      {list.filters.sha256 ? (
        <Alert
          type="info"
          showIcon
          message="正在查看相同内容指纹的资料"
          description={
            <div className="filter-fingerprint">
              <Typography.Text code className="inline-code-value">{list.filters.sha256}</Typography.Text>
              <CopyButton value={list.filters.sha256} label="复制指纹" />
            </div>
          }
          action={
            <Button size="small" onClick={() => setFilters({ sha256: '' })}>
              清除
            </Button>
          }
        />
      ) : null}

      {query.isError && !query.data ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="资料列表加载失败" />
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<Material>
            rowKey="id"
            size="middle"
            className={compact ? 'compact-list-table clickable-rows' : 'clickable-rows'}
            onRow={(material) => ({
              onClick: (event) => navigateTableRow(event, `/materials/${material.id}`, navigate),
            })}
            columns={compact ? compactColumns : desktopColumns}
            dataSource={query.data?.items ?? []}
            loading={query.isFetching}
            showHeader={!compact}
            scroll={compact ? undefined : { x: 960 }}
            locale={{
              emptyText: query.isPending ? (
                '正在加载…'
              ) : (
                <EmptyState
                  description={hasFilters ? '没有匹配的资料' : '暂无资料'}
                  action={emptyAction}
                />
              ),
            }}
            pagination={{
              current: list.page,
              pageSize: list.pageSize,
              total: query.data?.total ?? 0,
              showSizeChanger: !compact,
              pageSizeOptions: [10, 20, 50, 100],
              showTotal: (total) => `共 ${total} 条`,
              simple: compact,
              onChange: (page, size) => list.setPage(page, size),
            }}
          />
        </Card>
      )}
    </div>
  );
}
