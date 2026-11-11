import { useState } from 'react';
import { Alert, App, Button, Card, Descriptions, Dropdown, Form, Input, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { DownloadOutlined, DownOutlined, EyeOutlined, MoreOutlined } from '@ant-design/icons';
import { Link, useNavigate, useParams } from 'react-router';
import type { Pack, PackMember, Part } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { saveBlob } from '../../shared/lib/download';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { DOC_TYPE_LABELS, GRADE_LABELS, SUBJECT_LABELS, TERM_LABELS } from '../../shared/lib/labels';
import { PART_OPTIONS } from '../../shared/lib/options';
import { BackLink } from '../../shared/ui/BackLink';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { StageTag } from '../../shared/ui/StatusTags';
import { PackAIPanel } from '../ai/AnalysisPanel';
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
import { PackCatalogFields, formToUpdate, packToForm, type CatalogField, type CatalogFormValues } from './PackCatalogFields';
import { usePack, usePackFacets, usePackMutations, useUpdatePack } from './queries';
import { href } from '../../app/routes';

const EDITABLE_FIELDS: (CatalogField | 'title')[] = ['title', 'doc_type', 'subject_codes', 'grade', 'cohort', 'term', 'region', 'organization', 'exam_date', 'exam_name', 'chapter', 'tags'];

function CatalogView({ pack }: { pack: Pack }) {
  const dash = (value: string | number | undefined) => (value ? value : '—');
  return (
    <Descriptions
      size="small"
      column={{ xs: 1, md: 2, xl: 3 }}
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
        { key: 'tags', label: '标签', children: pack.tags.length > 0 ? pack.tags.map((t) => <Tag key={t}>{t}</Tag>) : '—' },
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
  const { message, modal } = App.useApp();
  const query = usePack(packId);
  const facets = usePackFacets();
  const update = useUpdatePack(packId);
  const { trash, restore, purge, reorder } = usePackMutations();
  const [form] = Form.useForm<CatalogFormValues & { description?: string }>();
  const [editing, setEditing] = useState(false);
  const [preview, setPreview] = useState<PackMember | null>(null);
  const [moving, setMoving] = useState<PackMember | null>(null);
  const [topicOpen, setTopicOpen] = useState(false);
  const [downloading, setDownloading] = useState(false);

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <div className="page-stack">
        <BackLink to="/library">返回资料库</BackLink>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="套卷加载失败" />
      </div>
    );
  }
  const pack = query.data;
  const trashed = pack.deleted_at !== null;

  const startEdit = () => {
    form.resetFields(); // clears touched state from an earlier edit
    form.setFieldsValue({ ...packToForm(pack), description: pack.description || undefined });
    setEditing(true);
  };
  const save = async () => {
    const values = await form.validateFields();
    // Only fields the person changed are sent: each one sent becomes locked.
    const touched = EDITABLE_FIELDS.filter((field) => form.isFieldTouched(field));
    const patch = formToUpdate(values, touched);
    if (form.isFieldTouched('description')) patch.description = values.description ?? '';
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
  };
  const unlockAll = () =>
    run(() => update.mutateAsync({ unlock_fields: pack.locked_fields }), 'AI 将重新编目这套资料');
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
  const run = async (action: () => Promise<unknown>, done: string, after?: () => void) => {
    try {
      await action();
      message.success(done);
      after?.();
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const columns: ColumnsType<PackMember> = [
    {
      title: '部件',
      key: 'part',
      width: 150,
      render: (_: unknown, member) =>
        canWrite && !trashed && editing ? (
          <Select size="small" value={member.part} options={PART_OPTIONS} onChange={(part) => void changePart(member, part)} style={{ width: 128 }} />
        ) : (
          PART_OPTIONS.find((o) => o.value === member.part)?.label
        ),
    },
    {
      title: '文件',
      key: 'file',
      render: (_: unknown, member) => (
        <div className="flex flex-col gap-1">
          <Link to={href.material(member.id)}>{member.file_name}</Link>
          <span className="table-subtext">
            {formatBytes(member.size_bytes)}
            {member.part === 'full' && member.answer_start_page > 0 ? ` · 答案从第 ${member.answer_start_page} 页开始` : ''}
          </span>
        </div>
      ),
    },
    { title: '状态', key: 'stage', width: 100, render: (_: unknown, member) => <StageTag stage={member.stage} /> },
    {
      title: '',
      key: 'actions',
      width: 120,
      align: 'right',
      render: (_: unknown, member, index) => (
        <Space size={0}>
          {canPreview(member.media_type) ? (
            <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => setPreview(member)}>
              预览
            </Button>
          ) : null}
          {canWrite && !trashed ? (
            <Dropdown
              trigger={['click']}
              menu={{
                items: [
                  { key: 'move', label: '移到其他套卷' },
                  { key: 'up', label: '上移', disabled: index === 0 },
                  { key: 'down', label: '下移', disabled: index === pack.members.length - 1 },
                ],
                onClick: ({ key }) => {
                  if (key === 'move') setMoving(member);
                  else void move(index, key === 'up' ? -1 : 1);
                },
              }}
            >
              <Button type="text" size="small" aria-label="更多操作" icon={<MoreOutlined />} />
            </Dropdown>
          ) : null}
        </Space>
      ),
    },
  ];

  return (
    <div className="page-stack">
      <BackLink to={trashed ? '/trash' : '/library'}>{trashed ? '返回回收站' : '返回资料库'}</BackLink>
      <PageHeader
        title={pack.title}
        description={
          <span className="flex flex-wrap items-center gap-2">
            <PackMeta pack={pack} />
            <Completeness pack={pack} />
          </span>
        }
        extra={
          trashed ? (
            canWrite ? (
              <Space wrap>
                <Button type="primary" onClick={() => void run(() => restore.mutateAsync([pack.id]), '已恢复')}>
                  恢复
                </Button>
                <Popconfirm title="彻底删除后无法恢复，确定？" description={PURGE_NOTE} okButtonProps={{ danger: true }} onConfirm={() => void run(() => purge.mutateAsync([pack.id]), '已彻底删除', () => navigate('/trash', { replace: true }))}>
                  <Button danger>彻底删除</Button>
                </Popconfirm>
              </Space>
            ) : null
          ) : (
            <Space wrap>
              <Button type={editing ? 'default' : 'primary'} icon={<DownloadOutlined />} loading={downloading} onClick={() => void downloadAll()}>
                {pack.members.length > 1 ? '下载整套' : '下载'}
              </Button>
              {canWrite ? (
                <Dropdown
                  trigger={['click']}
                  menu={{
                    items: [
                      { key: 'edit', label: '编辑编目与部件', disabled: editing },
                      { key: 'topic', label: '加入专题' },
                      { type: 'divider' },
                      { key: 'trash', label: '移入回收站', danger: true },
                    ],
                    onClick: ({ key }) => {
                      if (key === 'edit') startEdit();
                      else if (key === 'topic') setTopicOpen(true);
                      else
                        modal.confirm({
                          title: '移入回收站？',
                          content: '30 天内可以恢复。',
                          okText: '移入',
                          okButtonProps: { danger: true },
                          cancelText: '取消',
                          onOk: () => run(() => trash.mutateAsync([pack.id]), '已移入回收站', () => navigate('/library', { replace: true })),
                        });
                    },
                  }}
                >
                  <Button>
                    更多 <DownOutlined />
                  </Button>
                </Dropdown>
              ) : null}
            </Space>
          )
        }
      />

      {trashed ? <Alert type="warning" showIcon title={`已于 ${formatDateTime(pack.deleted_at)} 移入回收站，30 天后自动彻底删除`} /> : null}
      {pack.is_exam && !pack.has_answers && !trashed ? (
        <Alert
          type="info"
          showIcon
          title="这套试卷还缺答案"
          description="上传答案后，AI 会判断它是否属于这套试卷并自动归入；也可以在资料库里勾选两个套卷后“合并为一套”，合并后 AI 会重新编目。"
        />
      ) : null}

      <Card
        title="编目信息"
        extra={
          <Space size={4}>
            <Typography.Text type="secondary">
              {pack.stage === 'processing' ? 'AI 整理中' : pack.locked_fields.length > 0 ? `人工锁定 ${pack.locked_fields.length} 个字段` : '由 AI 编目'}
            </Typography.Text>
            {canWrite && !trashed && !editing && pack.locked_fields.length > 0 ? (
              <Popconfirm title="交还 AI？" description="人工设定的字段解除锁定，AI 会重新编目这套资料。" onConfirm={() => void unlockAll()}>
                <Button type="link" size="small" loading={update.isPending}>
                  交还 AI
                </Button>
              </Popconfirm>
            ) : null}
          </Space>
        }
      >
        {editing ? (
          <Form form={form} layout="vertical">
            <Form.Item name="title" label="自定义标题" tooltip="留空则按编目信息自动命名">
              <Input allowClear placeholder={pack.title} maxLength={255} />
            </Form.Item>
            <PackCatalogFields facets={facets.data} columns={3} locked={pack.locked_fields} />
            <Form.Item name="description" label="备注">
              <Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} maxLength={4000} />
            </Form.Item>
            <Typography.Paragraph type="secondary">只有你修改过的字段会被锁定，其余字段继续由 AI 维护。部件修改立即生效。</Typography.Paragraph>
            <Space>
              <Button type="primary" loading={update.isPending} onClick={() => void save()}>
                保存
              </Button>
              <Button onClick={() => setEditing(false)}>取消</Button>
            </Space>
          </Form>
        ) : (
          <CatalogView pack={pack} />
        )}
      </Card>

      <Card title={`文件（${pack.members.length}）`} styles={{ body: { padding: 0 } }}>
        <Table<PackMember> rowKey="id" size="middle" columns={columns} dataSource={pack.members} pagination={false} scroll={{ x: 720 }} />
      </Card>

      {!trashed ? <PackAIPanel packId={pack.id} canWrite={canWrite} /> : null}

      {pack.collections && pack.collections.length > 0 ? (
        <Card title="所在专题">
          <Space wrap>
            {pack.collections.map((c) => (
              <Link key={c.id} to={href.collection(c.id)}>
                <Tag>{c.title}</Tag>
              </Link>
            ))}
          </Space>
        </Card>
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
    </div>
  );
}
