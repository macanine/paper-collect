import { lazy, Suspense, useMemo, useState } from 'react';
import { App, Button, Card, Descriptions, Popconfirm, Space, Spin, Tag, Typography } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined, DownloadOutlined, EditOutlined, EyeOutlined } from '@ant-design/icons';
import { Link, useNavigate, useParams } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { Replica, StorageProvider } from '../../shared/api/types';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { saveBlob } from '../../shared/lib/download';
import { GRADE_LABELS } from '../../shared/lib/labels';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { MaterialStatusTag, RoleTag, SubjectTags } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { AnalysisPanel } from '../ai/AnalysisPanel';
import { IndexPanel } from '../search/IndexPanel';
import { useProviders, useRefreshShare } from '../storage/queries';
import { deleteMaterial, downloadMaterial, previewMaterial } from './api';
import { MaterialEditModal } from './MaterialEditModal';
import { ReplicaTable } from './ReplicaTable';
import { useMaterial } from './queries';

const MaterialPreviewModal = lazy(() => import('./MaterialPreviewModal').then((module) => ({ default: module.MaterialPreviewModal })));

export function MaterialDetailPage() {
  const { materialId = '' } = useParams();
  const { can } = useAuth();
  const { message } = App.useApp();
  const navigate = useNavigate();
  const query = useMaterial(materialId);
  const canReadProviders = can('storage:read');
  const providersQuery = useProviders(canReadProviders);
  const refreshShare = useRefreshShare();
  const [editing, setEditing] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [converting, setConverting] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [refreshingId, setRefreshingId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const providerMap = useMemo<Map<string, StorageProvider> | null>(() => {
    if (!canReadProviders || !providersQuery.data) return null;
    return new Map(providersQuery.data.map((provider) => [provider.id, provider]));
  }, [canReadProviders, providersQuery.data]);

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <div className="page-stack">
        <BackLink />
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="资料加载失败" />
      </div>
    );
  }

  const material = query.data;
  const isWord = material.media_type === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
  const pdfFileName = `${material.file_name.replace(/\.docx$/i, '')}.pdf`;

  const handleDownload = async () => {
    setDownloading(true);
    try {
      const { blob, filename } = await downloadMaterial(material.id);
      saveBlob(blob, filename ?? material.file_name);
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    } finally {
      setDownloading(false);
    }
  };

  const handleDownloadPdf = async () => {
    setConverting(true);
    try {
      const { blob } = await previewMaterial(material.id);
      saveBlob(blob, pdfFileName);
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    } finally {
      setConverting(false);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteMaterial(material.id, true);
      message.success('资料已删除（云端副本不会被远端删除）');
      navigate('/materials', { replace: true });
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    } finally {
      setDeleting(false);
    }
  };

  const handleRefreshShare = async (replica: Replica) => {
    setRefreshingId(replica.id);
    try {
      const updated = await refreshShare.mutateAsync(replica.id);
      if (updated.share_status === 'ready') message.success('分享链接已刷新');
      else if (updated.share_status === 'unsupported') message.info('该端点不支持分享');
      else if (updated.share_status === 'failed') message.warning(updated.error ? `分享失败：${updated.error}` : '分享失败，副本仍然可用');
      else message.info('分享状态已更新');
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    } finally {
      setRefreshingId(null);
    }
  };

  return (
    <div className="page-stack">
      <BackLink />
      <PageHeader
        title={material.title}
        extra={
          <Space wrap>
            <MaterialStatusTag status={material.status} />
            {can('materials:write') ? (
              <Button icon={<EditOutlined />} onClick={() => setEditing(true)}>
                编辑
              </Button>
            ) : null}
            {can('materials:read') ? (
              material.media_type === 'application/pdf' || isWord ? (
                <Button icon={<EyeOutlined />} onClick={() => setPreviewing(true)}>预览</Button>
              ) : null
            ) : null}
            {can('materials:read') && isWord ? (
              <Button icon={<DownloadOutlined />} loading={converting} onClick={() => void handleDownloadPdf()}>下载 PDF</Button>
            ) : null}
            {can('materials:read') ? (
              <Button type="primary" icon={<DownloadOutlined />} loading={downloading} onClick={() => void handleDownload()}>
                下载原件
              </Button>
            ) : null}
            {can('materials:write') ? (
              <Popconfirm
                title="删除这份资料？"
                okText="删除"
                okButtonProps={{ danger: true }}
                cancelText="取消"
                onConfirm={() => void handleDelete()}
              >
                <Button danger icon={<DeleteOutlined />} loading={deleting}>
                  删除
                </Button>
              </Popconfirm>
            ) : null}
          </Space>
        }
      />

      <Card title="基本信息">
        <Descriptions
          column={{ xs: 1, md: 2 }}
          size="small"
          items={[
            { key: 'role', label: '角色', children: <RoleTag role={material.role} /> },
            { key: 'subjects', label: '学科', children: <SubjectTags codes={material.subject_codes} /> },
            { key: 'grade', label: '年级', children: GRADE_LABELS[material.grade] },
            {
              key: 'year',
              label: '学年',
              children: material.academic_year > 0 ? `${material.academic_year} 学年` : '未指定',
            },
            {
              key: 'tags',
              label: '标签',
              children:
                material.tags.length > 0 ? (
                  <span className="inline-flex flex-wrap gap-1">
                    {material.tags.map((tag) => (
                      <Tag key={tag}>{tag}</Tag>
                    ))}
                  </span>
                ) : (
                  '—'
                ),
            },
            {
              key: 'description',
              label: '描述',
              span: { xs: 1, md: 2 },
              children: material.description ? (
                <Typography.Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{material.description}</Typography.Paragraph>
              ) : (
                '—'
              ),
            },
            { key: 'file', label: '文件名', children: material.file_name },
            { key: 'media', label: '文件类型', children: fileTypeName(material.media_type) },
            { key: 'size', label: '大小', children: formatBytes(material.size_bytes) },
            { key: 'source', label: '原始路径', span: { xs: 1, md: 2 }, children: material.source_path || '—' },
            {
              key: 'parent',
              label: '所属原件',
              span: { xs: 1, md: 2 },
              children: material.parent_material_id ? (
                <Link to={`/materials/${material.parent_material_id}`}>查看原件</Link>
              ) : (
                '—'
              ),
            },
            { key: 'created', label: '创建时间', children: formatDateTime(material.created_at) },
            { key: 'updated', label: '更新时间', children: formatDateTime(material.updated_at) },
          ]}
        />
      </Card>

      <AnalysisPanel materialId={material.id} canWrite={can('materials:write')} />

      <IndexPanel materialId={material.id} canWrite={can('materials:write')} />

      <Card title="副本与分享">
        <ReplicaTable
          replicas={material.replicas}
          providers={providerMap}
          canRefreshShare={can('storage:write')}
          refreshingId={refreshingId}
          onRefreshShare={(replica) => void handleRefreshShare(replica)}
        />
      </Card>

      {editing ? <MaterialEditModal material={material} open={editing} onClose={() => setEditing(false)} /> : null}
      {previewing ? <Suspense fallback={<Spin fullscreen tip="正在加载预览" />}><MaterialPreviewModal materialId={material.id} title={material.title} isWord={isWord} pdfFileName={pdfFileName} onClose={() => setPreviewing(false)} /></Suspense> : null}
    </div>
  );
}

function fileTypeName(mediaType: string): string {
  const names: Record<string, string> = {
    'application/pdf': 'PDF 文档',
    'application/msword': 'Word 文档（DOC）',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'Word 文档（DOCX）',
    'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'PowerPoint 演示文稿',
    'application/zip': 'ZIP 压缩包',
  };
  return names[mediaType] ?? '其他文件';
}

function BackLink() {
  return (
    <Link to="/materials" className="inline-flex items-center gap-1 text-sm">
      <ArrowLeftOutlined />
      返回资料列表
    </Link>
  );
}
