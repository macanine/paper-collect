import type { ReactNode } from 'react';
import { Card, List, Statistic, Tag } from 'antd';
import {
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  FileSearchOutlined,
  FolderOpenOutlined,
  InboxOutlined,
  SyncOutlined,
} from '@ant-design/icons';
import { Link } from 'react-router';
import { formatDateTime } from '../../shared/lib/format';
import { PageHeader } from '../../shared/ui/PageHeader';
import { InlineError } from '../../shared/ui/States';
import { UploadStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { Completeness, PackMeta } from '../packs/PackBadges';
import { useLibraryStats, useRecentPacks, useRecentUploads } from './queries';

function StatCard({ to, title, value, icon, tone }: { to: string; title: string; value: number | undefined; icon: ReactNode; tone?: 'warning' | 'error' }) {
  return (
    <Link to={to} className={`stat-card${tone ? ` stat-card--${tone}` : ''}`}>
      <Card hoverable size="small">
        <Statistic title={title} value={value ?? '—'} prefix={icon} />
      </Card>
    </Link>
  );
}

export function DashboardPage() {
  const { can } = useAuth();
  const canRead = can('materials:read');
  const canUpload = can('uploads:write');
  const stats = useLibraryStats(canRead);
  const packs = useRecentPacks(canRead);
  const uploads = useRecentUploads(canUpload);
  const s = stats.data;

  return (
    <div className="page-stack">
      <PageHeader title="工作台" />
      {stats.error ? <InlineError error={stats.error} /> : null}

      {canRead ? (
        <div className="stat-grid">
          <StatCard to="/inbox" title="待整理" value={s?.review} icon={<InboxOutlined />} tone={s && s.review > 0 ? 'warning' : undefined} />
          <StatCard to="/inbox?stage=processing" title="识别中" value={s?.processing} icon={<SyncOutlined />} />
          <StatCard to="/inbox?stage=failed" title="识别失败" value={s?.failed} icon={<ExclamationCircleOutlined />} tone={s && s.failed > 0 ? 'error' : undefined} />
          <StatCard to="/library?missing=answers" title="缺答案的试卷" value={s?.missing_answers} icon={<FileSearchOutlined />} />
          <StatCard to="/library" title="套卷" value={s?.packs} icon={<FolderOpenOutlined />} />
          <StatCard to="/library" title="已入库文件" value={s?.ready} icon={<CheckCircleOutlined />} />
        </div>
      ) : null}

      {s && s.attention > 0 && can('storage:read') ? (
        <Card size="small">
          存储：{s.attention} 个文件同步需要注意。
          <Link to="/storage" className="ml-2">
            查看存储
          </Link>
        </Card>
      ) : null}

      <div className="dashboard-columns">
        {canRead ? (
          <Card title="最近更新的套卷" extra={<Link to="/library">全部</Link>}>
            <List
              loading={packs.isPending}
              dataSource={packs.data?.items ?? []}
              locale={{ emptyText: '还没有资料，把文件拖到页面任意位置即可上传' }}
              renderItem={(pack) => (
                <List.Item>
                  <List.Item.Meta
                    title={<Link to={`/packs/${pack.id}`}>{pack.title}</Link>}
                    description={
                      <span className="flex flex-wrap items-center gap-2">
                        <PackMeta pack={pack} />
                        <Completeness pack={pack} />
                      </span>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        ) : null}
        {canUpload ? (
          <Card title="最近上传" extra={<Link to="/uploads">上传记录</Link>}>
            <List
              loading={uploads.isPending}
              dataSource={uploads.data?.items ?? []}
              locale={{ emptyText: '暂无上传' }}
              renderItem={(upload) => (
                <List.Item extra={<UploadStatusTag status={upload.status} />}>
                  <List.Item.Meta
                    title={<Link to={`/uploads/${upload.id}`}>{upload.title}</Link>}
                    description={
                      <span>
                        {formatDateTime(upload.created_at)}
                        {upload.status === 'completed' ? (
                          <>
                            {' · '}新增 {upload.new_count} · 已存在 {upload.duplicate_count}
                            {upload.skipped.length > 0 ? <Tag className="ml-2">跳过 {upload.skipped.length}</Tag> : null}
                          </>
                        ) : null}
                      </span>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        ) : null}
      </div>
    </div>
  );
}
