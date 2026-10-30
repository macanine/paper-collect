import { useMemo, type ReactNode } from 'react';
import { Alert, Button, Card, Space, Statistic, Table } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  AppstoreOutlined,
  ArrowRightOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloudServerOutlined,
  CloudUploadOutlined,
  ExclamationCircleOutlined,
  FileTextOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { Link } from 'react-router';
import type { Job, Material } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { JOB_KIND_LABELS, STORAGE_MODE_LABELS } from '../../shared/lib/labels';
import { usePolicy, useProviders } from '../storage/queries';
import { InlineError } from '../../shared/ui/States';
import { JobKindTag, JobStatusTag, MaterialStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import {
  useJobStats,
  useMaterialStats,
  useRecentJobs,
  useRecentMaterials,
} from './queries';

const MATERIAL_COUNT_ITEMS = [
  { key: 'archived', title: '已归档' },
  { key: 'pending_storage', title: '待存储' },
  { key: 'degraded', title: '降级' },
] as const;

export function DashboardPage() {
  const { can } = useAuth();
  const canMaterials = can('materials:read');
  const canJobs = can('jobs:read');
  const canStorage = can('storage:read');

  const materialStats = useMaterialStats(canMaterials);
  const recentMaterials = useRecentMaterials(canMaterials);
  const jobStats = useJobStats(canJobs);
  const recentJobs = useRecentJobs(canJobs);
  const providers = useProviders(canStorage);
  const policy = usePolicy(canStorage);

  const materialCounts = materialStats.data;
  const problems = [
    ...(canMaterials && materialStats.error ? [materialStats.error] : []),
    ...(canJobs && jobStats.error ? [jobStats.error] : []),
    ...(canStorage && providers.error ? [providers.error] : []),
    ...(canStorage && policy.error ? [policy.error] : []),
  ];

  const providerStats = useMemo(() => {
    const list = providers.data ?? [];
    return {
      enabled: list.filter((provider) => provider.enabled).length,
      primary: list.filter((provider) => provider.enabled && provider.role === 'primary').length,
      backup: list.filter((provider) => provider.enabled && provider.role === 'backup').length,
      total: list.length,
    };
  }, [providers.data]);

  const refreshAll = () => {
    if (canMaterials) {
      void materialStats.refetch();
      void recentMaterials.refetch();
    }
    if (canJobs) {
      void jobStats.refetch();
      void recentJobs.refetch();
    }
    if (canStorage) {
      void providers.refetch();
      void policy.refetch();
    }
  };

  const materialColumns: ColumnsType<Material> = [
    {
      title: '资料',
      key: 'title',
      render: (_: unknown, record) => (
        <div className="cell-stack">
          <Link to={`/materials/${record.id}`} className="truncate font-medium">
            {record.title}
          </Link>
          <span className="table-subtext">{record.file_name}</span>
        </div>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: Material['status']) => <MaterialStatusTag status={status} />,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 160,
      render: (value: string) => formatDateTime(value),
    },
  ];

  const jobColumns: ColumnsType<Job> = [
    {
      title: '任务',
      key: 'job',
      render: (_: unknown, record) => (
        <div className="flex min-w-0 items-center gap-2">
          <JobKindTag kind={record.kind} />
          <Link to={`/jobs/${record.id}`} className="truncate">
            {record.progress_message || JOB_KIND_LABELS[record.kind]}
          </Link>
        </div>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (status: Job['status']) => <JobStatusTag status={status} />,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 160,
      render: (value: string) => formatDateTime(value),
    },
  ];

  return (
    <div className="page-stack">
      <section className="dashboard-hero">
        <div className="dashboard-hero__copy">
          <span className="dashboard-hero__eyebrow">
            <ClockCircleOutlined />
            资料归档工作台
          </span>
          <h1>仪表盘</h1>
          <p>集中查看资料归档、后台任务与存储端点的实时状态。</p>
        </div>
        <div className="dashboard-hero__actions">
            {can('uploads:write') ? (
              <Link to="/uploads/new">
                <Button type="primary" icon={<CloudUploadOutlined />}>
                  上传资料
                </Button>
              </Link>
            ) : null}
            {can('collections:write') ? (
              <Link to="/collections">
                <Button icon={<AppstoreOutlined />}>管理合集</Button>
              </Link>
            ) : null}
            <Button icon={<ReloadOutlined />} onClick={refreshAll}>
              刷新
            </Button>
        </div>
      </section>

      {problems.length > 0 ? (
        <Alert
          type="warning"
          showIcon
          message="部分数据加载失败"
          action={
            <Button size="small" onClick={refreshAll}>
              重试
            </Button>
          }
        />
      ) : null}

      {canMaterials ? (
        <section>
          <div className="section-heading">
            <span className="section-heading__title">资料概览</span>
            <span className="section-heading__meta">按归档状态统计</span>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <CountCard
              title="资料总数"
              value={materialCounts?.total}
              loading={materialStats.isPending}
              icon={<FileTextOutlined />}
              footer="全部逻辑文件"
              to="/materials"
            />
            {MATERIAL_COUNT_ITEMS.map((item) => (
              <CountCard
                key={item.key}
                title={item.title}
                value={materialCounts?.[item.key]}
                icon={
                  item.key === 'archived' ? (
                    <CheckCircleOutlined />
                  ) : item.key === 'pending_storage' ? (
                    <ClockCircleOutlined />
                  ) : (
                    <ExclamationCircleOutlined />
                  )
                }
                tone={item.key === 'archived' ? 'success' : item.key === 'pending_storage' ? 'warning' : 'error'}
                footer={
                  item.key === 'archived'
                    ? '已完成副本写入'
                    : item.key === 'pending_storage'
                      ? '等待存储任务处理'
                      : '部分副本不可用'
                }
                loading={materialStats.isPending}
                to={`/materials?status=${item.key}`}
              />
            ))}
          </div>
        </section>
      ) : null}

      {canJobs ? (
        <section>
          <div className="section-heading">
            <span className="section-heading__title">任务队列</span>
            <span className="section-heading__meta">后台 worker 运行状态</span>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <CountCard title="排队中" value={jobStats.data?.pending} icon={<ClockCircleOutlined />} footer="等待 worker 领取" loading={jobStats.isPending} to="/jobs?status=pending" />
            <CountCard title="执行中" value={jobStats.data?.running} icon={<ReloadOutlined />} footer="正在处理" loading={jobStats.isPending} to="/jobs?status=running" />
            <CountCard title="失败" value={jobStats.data?.failed} icon={<ExclamationCircleOutlined />} tone="error" footer="需要人工检查" loading={jobStats.isPending} to="/jobs?status=failed" />
            <CountCard title="已完成" value={jobStats.data?.succeeded} icon={<CheckCircleOutlined />} tone="success" footer="累计成功任务" loading={jobStats.isPending} to="/jobs?status=succeeded" />
          </div>
        </section>
      ) : null}

      {canStorage ? (
        <section>
          <div className="section-heading">
            <span className="section-heading__title">存储运行状态</span>
            <span className="section-heading__meta">端点与副本策略</span>
          </div>
          <Card>
            <div className="flex flex-wrap items-center justify-between gap-4">
              <Space size={32} wrap>
                <Statistic title="启用的端点" value={providerStats.enabled} suffix={`/ ${providerStats.total}`} />
                <Statistic title="主端" value={providerStats.primary} />
                <Statistic title="备端" value={providerStats.backup} />
                <Statistic
                  title="副本策略"
                  value={policy.data ? STORAGE_MODE_LABELS[policy.data.mode] : policy.isPending ? '加载中…' : '—'}
                  suffix={policy.data ? `（最低 ${policy.data.minimum_replicas} 份）` : undefined}
                />
              </Space>
              <Link to="/storage">
                <Button icon={<CloudServerOutlined />}>存储设置</Button>
              </Link>
            </div>
          </Card>
        </section>
      ) : null}

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        {canMaterials ? (
          <Card
            title="最近资料"
            extra={
              <Link to="/materials">
                <Button size="small" type="link">
                  全部资料
                </Button>
              </Link>
            }
          >
            {recentMaterials.error ? (
              <div className="flex flex-col gap-2">
                <InlineError error={recentMaterials.error} />
                <Button size="small" onClick={() => void recentMaterials.refetch()}>
                  重试
                </Button>
              </div>
            ) : (
              <Table<Material>
                rowKey="id"
                size="small"
                columns={materialColumns}
                dataSource={recentMaterials.data?.items ?? []}
                loading={recentMaterials.isPending}
                pagination={false}
                locale={{ emptyText: '暂无资料' }}
              />
            )}
          </Card>
        ) : null}

        {canJobs ? (
          <Card
            title="最近任务"
            extra={
              <Link to="/jobs">
                <Button size="small" type="link">
                  全部任务
                </Button>
              </Link>
            }
          >
            {recentJobs.error ? (
              <div className="flex flex-col gap-2">
                <InlineError error={recentJobs.error} />
                <Button size="small" onClick={() => void recentJobs.refetch()}>
                  重试
                </Button>
              </div>
            ) : (
              <Table<Job>
                rowKey="id"
                size="small"
                columns={jobColumns}
                dataSource={recentJobs.data?.items ?? []}
                loading={recentJobs.isPending}
                pagination={false}
                locale={{ emptyText: '暂无任务' }}
              />
            )}
          </Card>
        ) : null}
      </div>
    </div>
  );
}

function CountCard({
  title,
  value,
  loading = false,
  icon,
  tone = 'primary',
  footer,
  to,
}: {
  title: string;
  value: number | string | undefined;
  loading?: boolean;
  icon?: ReactNode;
  tone?: 'primary' | 'success' | 'warning' | 'error';
  footer?: string;
  to: string;
}) {
  return (
    <Link to={to} className="metric-card-link" aria-label={`${title}，查看详情`}>
      <Card className="metric-card" loading={loading}>
        <div className="metric-card__top">
          <span className="metric-card__label">{title}</span>
          {icon ? <span className={`metric-card__icon is-${tone}`}>{icon}</span> : null}
        </div>
        <div className="metric-card__value">{value ?? '—'}</div>
        <div className="metric-card__footer">
          <span>{footer}</span>
          <ArrowRightOutlined />
        </div>
      </Card>
    </Link>
  );
}
