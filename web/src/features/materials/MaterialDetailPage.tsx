import { useMemo, useState } from 'react';
import { Anchor, Button, Code, Group, Select, Stack, Text } from '@mantine/core';
import { IconDownload, IconEye, IconTrash } from '@tabler/icons-react';
import { Link, useNavigate, useParams } from 'react-router';
import { describeError, errorMessage } from '../../shared/api/errors';
import type { Part, Replica, Drive } from '../../shared/api/types';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { saveBlob } from '../../shared/lib/download';
import { AI_STEP_LABELS, MATERIAL_STATUS_LABELS, SYNC_STATUS_LABELS, attentionReasonLabel } from '../../shared/lib/labels';
import { PART_OPTIONS } from '../../shared/lib/options';
import { CopyButton } from '../../shared/ui/CopyButton';
import { BackLink } from '../../shared/ui/BackLink';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Descriptions } from '../../shared/ui/Descriptions';
import { PageHeader } from '../../shared/ui/PageHeader';
import { Section } from '../../shared/ui/Section';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { MaterialStatusTag, PartTag, StageTag, SyncStatusTag, Tag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { AnalysisPanel } from '../ai/AIPanels';
import { IndexPanel } from '../search/IndexPanel';
import { useDrives, useRefreshShare, useSyncMaterial } from '../storage/queries';
import { MovePackModal } from '../packs/MovePackModal';
import { PackMeta } from '../packs/PackBadges';
import { downloadMaterial, previewMaterial, trashMaterial } from './api';
import { FilePreviewModal, canPreview } from './FilePreview';
import { ReplicaTable } from './ReplicaTable';
import { invalidateLibrary, useMaterial, useUpdateMaterial } from './queries';
import { useQueryClient } from '@tanstack/react-query';
import { href } from '../../app/routes';

const FILE_TYPES: Record<string, string> = {
  'application/pdf': 'PDF',
  'application/msword': 'Word（DOC）',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'Word（DOCX）',
  'application/vnd.ms-powerpoint': 'PowerPoint（PPT）',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'PowerPoint（PPTX）',
  'application/vnd.ms-excel': 'Excel（XLS）',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'Excel（XLSX）',
  'image/jpeg': 'JPEG 图片',
  'image/png': 'PNG 图片',
  'image/webp': 'WebP 图片',
  'audio/mpeg': 'MP3 音频',
  'audio/mp4': 'M4A 音频',
  'audio/wav': 'WAV 音频',
};

export function MaterialDetailPage() {
  const { materialId = '' } = useParams();
  const { can } = useAuth();
  const canWrite = can('materials:write');
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const query = useMaterial(materialId);
  const update = useUpdateMaterial(materialId);
  const canReadStorage = can('storage:read');
  const drivesQuery = useDrives(canReadStorage);
  const refreshShare = useRefreshShare();
  const syncMaterialMutation = useSyncMaterial();
  const [downloading, setDownloading] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [moving, setMoving] = useState(false);
  const [refreshingId, setRefreshingId] = useState<string | null>(null);

  const driveMap = useMemo<Map<string, Drive> | null>(() => {
    if (!canReadStorage || !drivesQuery.data) return null;
    return new Map(drivesQuery.data.map((drive) => [drive.id, drive]));
  }, [canReadStorage, drivesQuery.data]);

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <Stack gap="lg">
        <BackLink to="/library">返回资料库</BackLink>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="文件加载失败" />
      </Stack>
    );
  }

  const material = query.data;
  const isWord = material.media_type === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
  const showError = (error: unknown) => {
    const { title, detail } = describeError(error);
    message.error(detail ? `${title}：${detail}` : title);
  };

  const handleDownload = async (pdf: boolean) => {
    setDownloading(true);
    try {
      const { blob, filename } = pdf ? await previewMaterial(material.id) : await downloadMaterial(material.id);
      saveBlob(blob, pdf ? material.file_name.replace(/\.docx$/i, '.pdf') : (filename ?? material.file_name));
    } catch (error) {
      showError(error);
    } finally {
      setDownloading(false);
    }
  };

  const handleTrash = async () => {
    try {
      await trashMaterial(material.id);
      invalidateLibrary(queryClient);
      message.success('已移入回收站，30 天内可恢复');
      navigate(material.pack_id ? '/library' : '/trash', { replace: true });
    } catch (error) {
      showError(error);
    }
  };

  const changePart = async (part: Part) => {
    try {
      await update.mutateAsync({ part });
      message.success('已更新部件，文件名同步调整');
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const handleRefreshShare = async (replica: Replica) => {
    setRefreshingId(replica.id);
    try {
      const updated = await refreshShare.mutateAsync(replica.id);
      if (updated.share_status === 'ready') message.success('分享链接已刷新');
      else if (updated.share_status === 'unsupported') message.info('该网盘不支持分享');
      else if (updated.share_status === 'failed') message.warning(updated.error ? `分享失败：${updated.error}` : '分享失败，副本仍然可用');
      else message.info('分享状态已更新');
    } catch (error) {
      showError(error);
    } finally {
      setRefreshingId(null);
    }
  };

  const handleSyncNow = async () => {
    try {
      const result = await syncMaterialMutation.mutateAsync(material.id);
      message.success(result.queued > 0 ? `已生成 ${result.queued} 个同步任务` : '已按当前编目重新规划');
    } catch (error) {
      showError(error);
    }
  };

  const sourceColumns: Column<(typeof material.sources)[number]>[] = [
    { key: 'path', wrap: true, title: '来源路径', render: (src) => <Text size="sm" style={{ wordBreak: 'break-all' }}>{src.path}</Text> },
    { key: 'dup', width: 110, render: (src) => (src.duplicate ? <Tag>重复上传</Tag> : <Tag color="blue">首次上传</Tag>) },
    { key: 'created', title: '时间', width: 170, render: (src) => formatDateTime(src.created_at) },
    { key: 'upload', width: 90, render: (src) => (src.upload_id && can('uploads:write') ? <Anchor component={Link} to={href.upload(src.upload_id)} size="sm">导入</Anchor> : null) },
  ];

  return (
    <Stack gap="lg">
      <BackLink to={material.pack_id ? `/packs/${material.pack_id}` : '/library'}>返回套卷</BackLink>
      <PageHeader
        title={material.file_name}
        description={
          <Group gap="xs">
            <PartTag part={material.part} />
            <StageTag stage={material.stage} />
            {material.stage === 'processing' && material.ai_step ? <Tag>{AI_STEP_LABELS[material.ai_step]}</Tag> : null}
            {material.part_locked ? <Tag>部件由人工设定</Tag> : null}
            {material.attention_reasons.map((r) => <Tag key={r} color="warning">{attentionReasonLabel(r)}</Tag>)}
          </Group>
        }
        extra={
          <>
            <Button leftSection={<IconDownload size={16} />} loading={downloading} onClick={() => void handleDownload(false)}>下载原件</Button>
            {canPreview(material.media_type) ? <Button variant="default" leftSection={<IconEye size={16} />} onClick={() => setPreviewing(true)}>预览</Button> : null}
            {isWord ? <Button variant="default" onClick={() => void handleDownload(true)}>下载 PDF</Button> : null}
            {canWrite ? (
              <>
                <Button variant="default" onClick={() => setMoving(true)}>移动到其他套卷</Button>
                <ConfirmButton variant="default" color="red" leftSection={<IconTrash size={16} />} confirmTitle="把这个文件移入回收站？" okText="移入" danger onConfirm={handleTrash}>移入回收站</ConfirmButton>
              </>
            ) : null}
          </>
        }
      />

      {material.pack ? (
        <Section title="所属套卷">
          <Stack gap={4}>
            <Anchor component={Link} to={href.pack(material.pack.id)} fw={600} c="inherit">{material.pack.title}</Anchor>
            <PackMeta pack={material.pack} />
          </Stack>
        </Section>
      ) : null}

      <Section title="文件信息">
        <Descriptions
          items={[
            { key: 'part', label: '部件', children: canWrite ? <Select w={150} size="xs" allowDeselect={false} value={material.part} data={PART_OPTIONS} onChange={(part) => part && void changePart(part as Part)} /> : <PartTag part={material.part} /> },
            material.part === 'full' && { key: 'answer', label: '答案起始页', children: material.answer_start_page > 0 ? `第 ${material.answer_start_page} 页` : '未识别' },
            { key: 'original', label: '原始文件名', children: material.original_name },
            { key: 'type', label: '文件类型', children: FILE_TYPES[material.media_type] ?? material.media_type },
            { key: 'size', label: '大小', children: formatBytes(material.size_bytes) },
            { key: 'sha', label: '内容指纹', children: <Group gap="xs"><Code>{material.sha256.slice(0, 16)}…</Code><CopyButton value={material.sha256} label="复制" /></Group> },
            { key: 'storage', label: '归档', children: <span title={MATERIAL_STATUS_LABELS[material.status]}><MaterialStatusTag status={material.status} /></span> },
            {
              key: 'sync', label: '同步',
              children: (
                <Group gap="xs">
                  <span title={SYNC_STATUS_LABELS[material.sync_status]}><SyncStatusTag status={material.sync_status} /></span>
                  {can('storage:write') && material.sync_status === 'waiting_catalog' ? <Button size="compact-sm" variant="default" loading={syncMaterialMutation.isPending} onClick={() => void handleSyncNow()}>按当前编目同步</Button> : null}
                </Group>
              ),
            },
            { key: 'created', label: '入库时间', children: formatDateTime(material.created_at) },
          ]}
        />
      </Section>

      <Section title={`来源记录（${material.sources.length}）`} flush>
        <DataTable bare rows={material.sources} columns={sourceColumns} rowKey={(s) => s.id} minWidth={650} />
      </Section>

      <AnalysisPanel materialId={material.id} canWrite={canWrite} />
      <IndexPanel materialId={material.id} canWrite={canWrite} />

      <Section title="副本与分享" flush>
        <ReplicaTable
          replicas={material.replicas}
          drives={driveMap}
          canRefreshShare={can('storage:write')}
          refreshingId={refreshingId}
          onRefreshShare={(replica) => void handleRefreshShare(replica)}
        />
      </Section>

      {previewing ? (
        <FilePreviewModal target={{ id: material.id, title: material.file_name, fileName: material.file_name, mediaType: material.media_type }} onClose={() => setPreviewing(false)} />
      ) : null}
      {moving ? <MovePackModal open materialId={material.id} currentPackId={material.pack_id} onClose={() => setMoving(false)} /> : null}
    </Stack>
  );
}
