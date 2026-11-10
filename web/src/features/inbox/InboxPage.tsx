import { useCallback, useEffect, useState } from 'react';
import { Alert, App, Button, Card, Checkbox, Collapse, Empty, Form, InputNumber, Segmented, Select, Space, Tag, Typography } from 'antd';
import { EyeOutlined } from '@ant-design/icons';
import { Link, useSearchParams } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import type { PackMember, Part, ReviewPack, Stage } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { ATTENTION_REASON_LABELS, attentionReasonLabel } from '../../shared/lib/labels';
import { PART_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { PartTag, StageTag } from '../../shared/ui/StatusTags';
import { PackAIPanel } from '../ai/AnalysisPanel';
import { updateMaterial } from '../materials/api';
import { FilePreviewModal, canPreview } from '../materials/FilePreview';
import { invalidateLibrary, useAcceptMaterials, useReviewPacks } from '../materials/queries';
import { updatePack } from '../packs/api';
import { Completeness } from '../packs/PackBadges';
import { PackCatalogFields, formToUpdate, packToForm, type CatalogField, type CatalogFormValues } from '../packs/PackCatalogFields';
import { usePackFacets } from '../packs/queries';

const PRIMARY_CATALOG_FIELDS: CatalogField[] = ['doc_type', 'subject_codes', 'grade', 'cohort', 'region', 'organization', 'exam_name'];
const ADVANCED_CATALOG_FIELDS: CatalogField[] = ['term', 'exam_date', 'chapter', 'tags'];
const CATALOG_FIELDS = [...PRIMARY_CATALOG_FIELDS, ...ADVANCED_CATALOG_FIELDS];
const EMPTY_REVIEW_PACKS: ReviewPack[] = [];

interface MemberEdit {
  part: Part;
  answer_start_page: number;
}

function initialMemberEdits(members: PackMember[]): Record<string, MemberEdit> {
  return Object.fromEntries(members.map((member) => [member.id, { part: member.part, answer_start_page: member.answer_start_page }]));
}

function packReasons(group: ReviewPack): string[] {
  return group.items.flatMap((item) => item.attention_reasons).filter((reason, index, all) => all.indexOf(reason) === index);
}

/** 右侧：一套需关注资料。AI 的编目与校验问题在上，人工修改（会被锁定）在下。 */
function AttentionPanel({ group, stage, onDone }: { group: ReviewPack; stage: Stage; onDone: () => void }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const facets = usePackFacets();
  const accept = useAcceptMaterials();
  const [form] = Form.useForm<CatalogFormValues>();
  const [saving, setSaving] = useState(false);
  const [preview, setPreview] = useState<PackMember | null>(null);
  const [editing, setEditing] = useState(false);
  const [memberEdits, setMemberEdits] = useState<Record<string, MemberEdit>>(() => initialMemberEdits(group.pack.members));
  const editable = stage === 'attention';
  const showEdit = editable && editing;
  const attentionIds = group.pack.members.filter((member) => member.stage === 'attention').map((member) => member.id);

  useEffect(() => {
    form.resetFields();
    form.setFieldsValue(packToForm(group.pack));
    setMemberEdits(initialMemberEdits(group.pack.members));
  }, [form, group.pack]);

  const updateMember = (member: PackMember, patch: Partial<MemberEdit>) => {
    setMemberEdits((current) => ({
      ...current,
      [member.id]: { ...(current[member.id] ?? { part: member.part, answer_start_page: member.answer_start_page }), ...patch },
    }));
  };

  const confirm = useCallback(async () => {
    if (!editable || saving || attentionIds.length === 0) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      const touched = CATALOG_FIELDS.filter((field) => form.isFieldTouched(field));
      if (touched.length > 0) await updatePack(group.pack.id, formToUpdate(values, touched));
      const changes = group.pack.members.filter((member) => {
        const next = memberEdits[member.id];
        return next && (next.part !== member.part || next.answer_start_page !== member.answer_start_page);
      });
      await Promise.all(
        changes.map((member) => {
          const next = memberEdits[member.id]!;
          return updateMaterial(member.id, { part: next.part, answer_start_page: next.answer_start_page });
        }),
      );
      await accept.mutateAsync({ ids: attentionIds });
      invalidateLibrary(queryClient);
      message.success(`已确认《${group.pack.title}》`);
      onDone();
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      setSaving(false);
    }
  }, [accept, attentionIds, editable, form, group.pack, memberEdits, message, onDone, queryClient, saving]);

  return (
    <Card
      className="inbox-panel"
      title={
        <div className="flex flex-col">
          <Typography.Text strong ellipsis>{group.pack.title || '未命名资料'}</Typography.Text>
          <Typography.Text type="secondary" className="text-xs">
            {group.pack.members.length} 个文件 · {group.pack.is_exam ? '试卷资料' : '普通资料'}
          </Typography.Text>
        </div>
      }
      extra={
        <Space>
          <Completeness pack={group.pack} />
          <Link to={`/packs/${group.pack.id}`}>打开套卷</Link>
        </Space>
      }
    >
      <div className="page-stack">
        <Space wrap>
          <StageTag stage={stage} />
          {packReasons(group).map((reason) => (
            <Tag key={reason} color="warning">{attentionReasonLabel(reason)}</Tag>
          ))}
        </Space>

        {stage === 'waiting_ai' ? (
          <Alert
            type="info"
            showIcon
            title="AI 暂不可用"
            description={
              <span>
                请检查 <Link to="/admin/ai-models">AI 模型</Link> 和 <Link to="/admin/ai-settings">AI 解析</Link> 的配置；保存后这些文件会自动继续。
              </span>
            }
          />
        ) : null}
        {stage === 'processing' ? <Alert type="info" showIcon title="AI 正在阅读、分组或编目，完成后会自动入库" /> : null}

        <PackAIPanel packId={group.pack.id} canWrite={editable} />

        {showEdit ? (
          <Form form={form} layout="vertical" disabled={saving}>
            <Typography.Paragraph type="secondary">在下方修改的字段会被锁定，之后 AI 不再改动；未修改的字段保持 AI 的结果。</Typography.Paragraph>
            <PackCatalogFields fields={PRIMARY_CATALOG_FIELDS} facets={facets.data} columns={2} />
            <Collapse
              size="small"
              items={[{ key: 'advanced', label: '更多编目信息（可选）', children: <PackCatalogFields fields={ADVANCED_CATALOG_FIELDS} facets={facets.data} columns={2} /> }]}
            />
          </Form>
        ) : null}

        <Card size="small" title={`文件（${group.pack.members.length}）`} styles={{ body: { padding: 0 } }}>
          <ul className="inbox-pack-members">
            {group.pack.members.map((member) => {
              const edit = memberEdits[member.id] ?? { part: member.part, answer_start_page: member.answer_start_page };
              return (
                <li key={member.id} className="inbox-pack-member">
                  <div className="inbox-pack-member__main">
                    <div className="inbox-pack-member__title">
                      {canPreview(member.media_type) ? <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => setPreview(member)} aria-label={`预览 ${member.file_name}`} /> : null}
                      <Link to={`/materials/${member.id}`}>{member.file_name}</Link>
                    </div>
                    <span className="inbox-pack-member__meta">
                      <PartTag part={edit.part} />
                      <span>{formatBytes(member.size_bytes)}</span>
                      <StageTag stage={member.stage} />
                      {edit.part === 'full' && edit.answer_start_page > 0 ? <span>答案从第 {edit.answer_start_page} 页开始</span> : null}
                    </span>
                  </div>
                  {showEdit ? (
                    <Space wrap size={8} className="inbox-pack-member__edit">
                      <Select<Part> size="small" value={edit.part} options={PART_OPTIONS} onChange={(part) => updateMember(member, { part })} style={{ width: 128 }} aria-label={`${member.file_name} 的部件`} />
                      {edit.part === 'full' ? <InputNumber size="small" min={1} max={10000} value={edit.answer_start_page || undefined} placeholder="答案起始页" onChange={(value) => updateMember(member, { answer_start_page: value ?? 0 })} /> : null}
                    </Space>
                  ) : null}
                </li>
              );
            })}
          </ul>
        </Card>

        {editable ? (
          <Space>
            <Button type="primary" loading={saving || accept.isPending} disabled={attentionIds.length === 0} onClick={() => void confirm()}>
              {showEdit ? '保存并确认' : `确认这套 ${attentionIds.length} 个文件`}
            </Button>
            <Button type="text" onClick={() => setEditing((v) => !v)}>
              {showEdit ? '取消修改' : '修改编目'}
            </Button>
          </Space>
        ) : null}
      </div>
      {preview ? <FilePreviewModal target={{ id: preview.id, title: preview.title, fileName: preview.file_name, mediaType: preview.media_type }} onClose={() => setPreview(null)} /> : null}
    </Card>
  );
}

const TABS: { value: Stage; label: string }[] = [
  { value: 'attention', label: '需关注' },
  { value: 'processing', label: 'AI 整理中' },
  { value: 'waiting_ai', label: '等待 AI' },
];

const EMPTY_COPY: Partial<Record<Stage, string>> = {
  attention: '没有需要人工处理的套卷',
  processing: 'AI 当前没有正在整理的套卷',
  waiting_ai: '没有等待 AI 的套卷',
};

export function InboxPage() {
  const { message } = App.useApp();
  const [params, setParams] = useSearchParams();
  const stage = (TABS.find((tab) => tab.value === params.get('stage'))?.value ?? 'attention') as Stage;
  const reason = params.get('reason') ?? '';
  const [page, setPage] = useState(1);
  const query = useReviewPacks({ page, page_size: 100, stage, reason: reason || undefined }, { poll: stage === 'attention' ? false : 8000 });
  const accept = useAcceptMaterials();
  const groups = query.data?.items ?? EMPTY_REVIEW_PACKS;
  const [activeId, setActiveId] = useState<string>();
  const [checked, setChecked] = useState<string[]>([]);
  const [bulk, setBulk] = useState(false);
  const active = groups.find((group) => group.pack.id === activeId) ?? groups[0];

  useEffect(() => {
    setChecked([]);
    setBulk(false);
    setActiveId(undefined);
    setPage(1);
  }, [stage, reason]);

  useEffect(() => {
    setChecked((current) => {
      const next = current.filter((id) => groups.some((group) => group.pack.id === id));
      return next.length === current.length ? current : next;
    });
    setActiveId((current) => (current && groups.some((group) => group.pack.id === current) ? current : groups[0]?.pack.id));
  }, [groups]);

  const advance = useCallback(() => {
    const index = groups.findIndex((group) => group.pack.id === active?.pack.id);
    const next = groups[index + 1] ?? groups[index - 1];
    setActiveId(next?.pack.id);
  }, [active?.pack.id, groups]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (target.closest('input, textarea, select, button, a, [role="button"], .ant-select') || target.isContentEditable)) return;
      const index = groups.findIndex((group) => group.pack.id === active?.pack.id);
      if (event.key === 'j' || event.key === 'J') setActiveId(groups[Math.min(groups.length - 1, index + 1)]?.pack.id);
      if (event.key === 'k' || event.key === 'K') setActiveId(groups[Math.max(0, index - 1)]?.pack.id);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [active?.pack.id, groups]);

  const bulkAccept = async () => {
    const ids = groups
      .filter((group) => checked.includes(group.pack.id))
      .flatMap((group) => group.pack.members.filter((member) => member.stage === 'attention').map((member) => member.id));
    if (ids.length === 0) return;
    try {
      const result = await accept.mutateAsync({ ids });
      message.success(`已确认 ${result.count} 个文件`);
      setChecked([]);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const reasonOptions = Object.keys(ATTENTION_REASON_LABELS).map((value) => ({ value, label: attentionReasonLabel(value) }));
  const emptyCopy = reason ? '没有符合该原因的套卷' : (EMPTY_COPY[stage] ?? '没有套卷');
  return (
    <div className="page-stack">
      <PageHeader title="需关注" description="编目全部由 AI 完成；这里只列出 AI 没能通过校验的套卷。J/K 可切换套卷。" />
      <div className="flex flex-wrap items-center gap-3">
        <Segmented value={stage} onChange={(value) => setParams({ stage: String(value) })} options={TABS} />
        {stage === 'attention' ? <Select allowClear placeholder="按原因筛选" value={reason || undefined} options={reasonOptions} onChange={(value) => setParams(value ? { stage, reason: value } : { stage })} style={{ width: 180 }} /> : null}
        <Typography.Text type="secondary">共 {query.data?.total ?? 0} 套</Typography.Text>
        {stage === 'attention' && groups.length > 1 ? (
          <Button
            type="text"
            onClick={() => {
              setBulk((v) => !v);
              setChecked([]);
            }}
          >
            {bulk ? '完成' : '批量确认'}
          </Button>
        ) : null}
      </div>

      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isPending ? <LoadingState /> : null}
      {!query.isPending && groups.length === 0 ? (
        <Card>
          <Empty description={emptyCopy} />
        </Card>
      ) : null}
      {!query.isPending && groups.length > 0 ? (
        <div className="inbox-layout">
          <Card className="inbox-list" size="small" styles={{ body: { padding: 0 } }}>
            {stage === 'attention' && bulk ? (
              <div className="inbox-list__toolbar">
                <Checkbox
                  checked={checked.length > 0 && checked.length === groups.length}
                  indeterminate={checked.length > 0 && checked.length < groups.length}
                  onChange={(event) => setChecked(event.target.checked ? groups.map((group) => group.pack.id) : [])}
                >
                  全选套卷
                </Checkbox>
                {checked.length > 0 ? (
                  <Button size="small" onClick={() => void bulkAccept()} loading={accept.isPending}>
                    按当前编目确认 {checked.length} 套
                  </Button>
                ) : null}
              </div>
            ) : null}
            <ul className="inbox-list__items">
              {groups.map((group) => (
                <li
                  key={group.pack.id}
                  className={`inbox-item${group.pack.id === active?.pack.id ? ' is-active' : ''}`}
                  role="button"
                  tabIndex={0}
                  aria-current={group.pack.id === active?.pack.id ? 'true' : undefined}
                  onClick={() => setActiveId(group.pack.id)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      setActiveId(group.pack.id);
                    }
                  }}
                >
                  {stage === 'attention' && bulk ? (
                    <Checkbox
                      checked={checked.includes(group.pack.id)}
                      onClick={(event) => event.stopPropagation()}
                      onKeyDown={(event) => event.stopPropagation()}
                      onChange={(event) => setChecked((list) => (event.target.checked ? [...list, group.pack.id] : list.filter((id) => id !== group.pack.id)))}
                    />
                  ) : null}
                  <div className="inbox-item__body">
                    <span className="inbox-item__title">{group.pack.title || '未命名资料'}</span>
                    <span className="inbox-item__meta">
                      <span>{group.pack.members.length} 个文件</span>
                      {packReasons(group).slice(0, 2).map((reasonValue) => (
                        <Tag key={reasonValue} color="warning">{attentionReasonLabel(reasonValue)}</Tag>
                      ))}
                      <span>{formatDateTime(group.pack.created_at)}</span>
                    </span>
                  </div>
                </li>
              ))}
            </ul>
            {(query.data?.total ?? 0) > 100 ? (
              <div className="inbox-list__pager">
                <Button size="small" disabled={page === 1} onClick={() => setPage(page - 1)}>上一页</Button>
                <span>第 {page} 页</span>
                <Button size="small" disabled={page * 100 >= (query.data?.total ?? 0)} onClick={() => setPage(page + 1)}>下一页</Button>
              </div>
            ) : null}
          </Card>
          {active ? <AttentionPanel key={active.pack.id} group={active} stage={stage} onDone={advance} /> : null}
        </div>
      ) : null}
    </div>
  );
}
