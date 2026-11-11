import { useState } from 'react';
import { ActionIcon, Alert, Anchor, Button, Group, Menu, Select, Stack, Text, TextInput, Textarea } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconChevronDown, IconDots, IconDownload, IconEye } from '@tabler/icons-react';
import { Link, useNavigate, useParams } from 'react-router';
import type { Pack, PackMember, Part } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { saveBlob } from '../../shared/lib/download';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { DOC_TYPE_LABELS, GRADE_LABELS, SUBJECT_LABELS, TERM_LABELS } from '../../shared/lib/labels';
import { PART_OPTIONS } from '../../shared/lib/options';
import { BackLink } from '../../shared/ui/BackLink';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Descriptions } from '../../shared/ui/Descriptions';
import { PageHeader } from '../../shared/ui/PageHeader';
import { Section } from '../../shared/ui/Section';
import { ConfirmButton, message, modal } from '../../shared/ui/feedback';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { StageTag, Tag } from '../../shared/ui/StatusTags';
import { PackAIPanel } from '../ai/AIPanels';
import { PURGE_NOTE } from '../trash/TrashPage';
import { useAuth } from '../auth/AuthContext';
import { downloadMaterial, updateMaterial } from '../materials/api';
import { invalidateLibrary } from '../materials/queries';
import { FilePreviewModal, canPreview } from '../materials/FilePreview';
import { useQueryClient } from '@tanstack/react-query';
import { downloadPack } from './api';
import { AddToCollectionModal } from './BulkModals';
import { MovePackModal } from './MovePackModal';
import { Completeness, PackMeta } from './PackBadges';
import { PackCatalogFields, formToUpdate, packToForm, validateCatalog, type CatalogField, type CatalogFormValues } from './PackCatalogFields';
import { usePack, usePackFacets, usePackMutations, useUpdatePack } from './queries';
import { href } from '../../app/routes';

const EDITABLE_FIELDS: (CatalogField | 'title')[] = ['title', 'doc_type', 'subject_codes', 'grade', 'cohort', 'term', 'region', 'organization', 'exam_date', 'exam_name', 'chapter', 'tags'];

function CatalogView({ pack }: { pack: Pack }) {
  const dash = (value: string | number | undefined) => (value ? value : '—');
  return (
    <Descriptions
      cols={3}
      items={[
        { key: 'type', label: '资料类型', children: pack.doc_type ? DOC_TYPE_LABELS[pack.doc_type] : '—' },
        { key: 'subject', label: '学科', children: pack.subject_codes.map((c) => SUBJECT_LABELS[c]).join('、') || '—' },
        { key: 'grade', label: '年级', children: pack.grade ? GRADE_LABELS[pack.grade] : '—' },
        { key: 'cohort', label: '届别', children: pack.cohort ? `${pack.cohort} 届` : '—' },
        { key: 'term', label: '学期', children: pack.term ? TERM_LABELS[pack.term] : '—' },
        { key: 'date', label: '考试时间', children: dash(pack.exam_date) },
        { key: 'region', label: '地区', children: dash(pack.region) },
        { key: 'org', label: '来源机构', children: dash(pack.organization) },
        { key: 'exam', label: '考试名称', children: dash(pack.exam_name) },
        { key: 'chapter', label: '章节 / 知识点', children: dash(pack.chapter) },
        { key: 'tags', label: '标签', children: pack.tags.length > 0 ? <Group gap={4}>{pack.tags.map((t) => <Tag key={t}>{t}</Tag>)}</Group> : '—' },
        { key: 'desc', label: '备注', children: dash(pack.description) },
      ]}
    />
  );
}

export function PackDetailPage() {
  const { packId = '' } = useParams();
  const { can } = useAuth();
  const canWrite = can('materials:write');
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const query = usePack(packId);
  const facets = usePackFacets();
  const update = useUpdatePack(packId);
  const { trash, restore, purge, reorder } = usePackMutations();
  const form = useForm<CatalogFormValues & { description?: string }>({ initialValues: {}, validate: validateCatalog });
  const [editing, setEditing] = useState(false);
  const [preview, setPreview] = useState<PackMember | null>(null);
  const [moving, setMoving] = useState<PackMember | null>(null);
  const [topicOpen, setTopicOpen] = useState(false);
  const [downloading, setDownloading] = useState(false);

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <Stack gap="lg">
        <BackLink to="/library">返回资料库</BackLink>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="套卷加载失败" />
      </Stack>
    );
  }
  const pack = query.data;
  const trashed = pack.deleted_at !== null;

  const startEdit = () => {
    const values = { ...packToForm(pack), description: pack.description || undefined };
    form.setValues(values);
    form.resetDirty(values);
    setEditing(true);
  };
  const save = form.onSubmit(async (values) => {
    // Only fields the person changed are sent: each one sent becomes locked.
    const touched = EDITABLE_FIELDS.filter((field) => form.isDirty(field));
    const patch = formToUpdate(values, touched);
    if (form.isDirty('description')) patch.description = values.description ?? '';
    if (Object.keys(patch).length === 0) {
      setEditing(false);
      return;
    }
    try {
      await update.mutateAsync(patch);
      message.success(touched.length > 0 ? '已保存，修改的字段由人工锁定' : '已保存');
      setEditing(false);
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
  const run = async (action: () => Promise<unknown>, done: string, after?: () => void) => {
    try {
      await action();
      message.success(done);
      after?.();
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  const unlockAll = () => run(() => update.mutateAsync({ unlock_fields: pack.locked_fields }), 'AI 将重新编目这套资料');
  const changePart = async (member: PackMember, part: Part) => {
    try {
      await updateMaterial(member.id, { part });
      invalidateLibrary(queryClient);
      message.success(`已将 ${member.file_name} 设为${PART_OPTIONS.find((o) => o.value === part)?.label ?? part}`);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  const move = async (index: number, delta: number) => {
    const order = pack.members.map((m) => m.id);
    const [item] = order.splice(index, 1);
    order.splice(index + delta, 0, item as string);
    try {
      await reorder.mutateAsync({ id: pack.id, order });
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  const downloadAll = async () => {
    setDownloading(true);
    try {
      const { blob, filename } = pack.members.length === 1 ? await downloadMaterial(pack.members[0]!.id) : await downloadPack(pack.id);
      saveBlob(blob, filename ?? (pack.members.length === 1 ? pack.members[0]!.file_name : `${pack.title}.zip`));
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      setDownloading(false);
    }
  };

  const columns: Column<PackMember>[] = [
    {
      key: 'part',
      title: '部件',
      width: 160,
      render: (member) =>
        canWrite && !trashed && editing ? (
          <Select size="xs" w={128} allowDeselect={false} value={member.part} data={PART_OPTIONS} onChange={(part) => part && void changePart(member, part as Part)} />
        ) : (
          PART_OPTIONS.find((o) => o.value === member.part)?.label
        ),
    },
    {
      key: 'file', wrap: true,
      title: '文件',
      render: (member) => (
        <Stack gap={2}>
          <Anchor component={Link} to={href.material(member.id)} size="sm">{member.file_name}</Anchor>
          <Text size="xs" c="dimmed">
            {formatBytes(member.size_bytes)}
            {member.part === 'full' && member.answer_start_page > 0 ? ` · 答案从第 ${member.answer_start_page} 页开始` : ''}
          </Text>
        </Stack>
      ),
    },
    { key: 'stage', title: '状态', width: 100, render: (member) => <StageTag stage={member.stage} /> },
    {
      key: 'actions',
      width: 140,
      align: 'right',
      render: (member) => {
        const index = pack.members.findIndex((m) => m.id === member.id);
        return (
          <Group gap={4} justify="flex-end" wrap="nowrap">
            {canPreview(member.media_type) ? <Button variant="subtle" size="compact-sm" leftSection={<IconEye size={14} />} onClick={() => setPreview(member)}>预览</Button> : null}
            {canWrite && !trashed ? (
              <Menu position="bottom-end">
                <Menu.Target><ActionIcon variant="subtle" color="gray" aria-label="更多操作"><IconDots size={16} /></ActionIcon></Menu.Target>
                <Menu.Dropdown>
                  <Menu.Item onClick={() => setMoving(member)}>移到其他套卷</Menu.Item>
                  <Menu.Item disabled={index === 0} onClick={() => void move(index, -1)}>上移</Menu.Item>
                  <Menu.Item disabled={index === pack.members.length - 1} onClick={() => void move(index, 1)}>下移</Menu.Item>
                </Menu.Dropdown>
              </Menu>
            ) : null}
          </Group>
        );
      },
    },
  ];

  return (
    <Stack gap="lg">
      <BackLink to={trashed ? '/trash' : '/library'}>{trashed ? '返回回收站' : '返回资料库'}</BackLink>
      <PageHeader
        title={pack.title}
        description={<Group gap="sm"><PackMeta pack={pack} /><Completeness pack={pack} /></Group>}
        extra={
          trashed ? (
            canWrite ? (
              <>
                <Button onClick={() => void run(() => restore.mutateAsync([pack.id]), '已恢复')}>恢复</Button>
                <ConfirmButton variant="default" color="red" confirmTitle="彻底删除后无法恢复，确定？" confirmDescription={PURGE_NOTE} okText="彻底删除" danger onConfirm={() => run(() => purge.mutateAsync([pack.id]), '已彻底删除', () => navigate('/trash', { replace: true }))}>彻底删除</ConfirmButton>
              </>
            ) : null
          ) : (
            <>
              <Button variant={editing ? 'default' : 'filled'} leftSection={<IconDownload size={16} />} loading={downloading} onClick={() => void downloadAll()}>
                {pack.members.length > 1 ? '下载整套' : '下载'}
              </Button>
              {canWrite ? (
                <Menu position="bottom-end">
                  <Menu.Target><Button variant="default" rightSection={<IconChevronDown size={14} />}>更多</Button></Menu.Target>
                  <Menu.Dropdown>
                    <Menu.Item disabled={editing} onClick={startEdit}>编辑编目与部件</Menu.Item>
                    <Menu.Item onClick={() => setTopicOpen(true)}>加入专题</Menu.Item>
                    <Menu.Divider />
                    <Menu.Item color="red" onClick={() => modal.confirm({ title: '移入回收站？', content: '30 天内可以恢复。', okText: '移入', danger: true, onOk: () => run(() => trash.mutateAsync([pack.id]), '已移入回收站', () => navigate('/library', { replace: true })) })}>移入回收站</Menu.Item>
                  </Menu.Dropdown>
                </Menu>
              ) : null}
            </>
          )
        }
      />

      {trashed ? <Alert color="yellow">{`已于 ${formatDateTime(pack.deleted_at)} 移入回收站，30 天后自动彻底删除`}</Alert> : null}
      {pack.is_exam && !pack.has_answers && !trashed ? (
        <Alert color="blue" title="这套试卷还缺答案">上传答案后，AI 会判断它是否属于这套试卷并自动归入；也可以在资料库里勾选两个套卷后“合并为一套”，合并后 AI 会重新编目。</Alert>
      ) : null}

      <Section
        title="编目信息"
        extra={
          <Group gap="xs">
            <Text size="sm" c="dimmed">{pack.stage === 'processing' ? 'AI 整理中' : pack.locked_fields.length > 0 ? `人工锁定 ${pack.locked_fields.length} 个字段` : '由 AI 编目'}</Text>
            {canWrite && !trashed && !editing && pack.locked_fields.length > 0 ? (
              <ConfirmButton variant="subtle" size="compact-sm" loading={update.isPending} confirmTitle="交还 AI？" confirmDescription="人工设定的字段解除锁定，AI 会重新编目这套资料。" onConfirm={unlockAll}>交还 AI</ConfirmButton>
            ) : null}
          </Group>
        }
      >
        {editing ? (
          <form onSubmit={save}>
            <Stack gap="md">
              <TextInput label="自定义标题" description="留空则按编目信息自动命名" placeholder={pack.title} maxLength={255} value={form.values.title ?? ''} onChange={(e) => form.setFieldValue('title', e.currentTarget.value || undefined)} />
              <PackCatalogFields form={form} facets={facets.data} columns={3} locked={pack.locked_fields} />
              <Textarea label="备注" autosize minRows={2} maxRows={6} maxLength={4000} value={form.values.description ?? ''} onChange={(e) => form.setFieldValue('description', e.currentTarget.value || undefined)} />
              <Text size="sm" c="dimmed">只有你修改过的字段会被锁定，其余字段继续由 AI 维护。部件修改立即生效。</Text>
              <Group><Button type="submit" loading={update.isPending}>保存</Button><Button variant="default" onClick={() => setEditing(false)}>取消</Button></Group>
            </Stack>
          </form>
        ) : (
          <CatalogView pack={pack} />
        )}
      </Section>

      <Stack gap="sm">
        <Text fw={600}>文件（{pack.members.length}）</Text>
        <DataTable rows={pack.members} columns={columns} rowKey={(m) => m.id} minWidth={640} />
      </Stack>

      {!trashed ? <PackAIPanel packId={pack.id} canWrite={canWrite} /> : null}

      {pack.collections && pack.collections.length > 0 ? (
        <Section title="所在专题">
          <Group gap="xs">
            {pack.collections.map((c) => (
              <Anchor key={c.id} component={Link} to={href.collection(c.id)}><Tag>{c.title}</Tag></Anchor>
            ))}
          </Group>
        </Section>
      ) : null}

      <FilePreviewModal
        target={preview ? { id: preview.id, title: preview.title, fileName: preview.file_name, mediaType: preview.media_type, initialPage: preview.part === 'full' && preview.answer_start_page > 0 ? 1 : undefined } : null}
        onClose={() => setPreview(null)}
      />
      {moving ? (
        <MovePackModal
          open
          materialId={moving.id}
          currentPackId={pack.id}
          onClose={() => setMoving(null)}
          onMoved={() => {
            if (pack.members.length === 1) navigate('/library', { replace: true });
          }}
        />
      ) : null}
      <AddToCollectionModal ids={[pack.id]} open={topicOpen} onClose={() => setTopicOpen(false)} />
    </Stack>
  );
}
