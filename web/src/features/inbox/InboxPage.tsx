import { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, App, Button, Card, Checkbox, Collapse, Empty, Form, InputNumber, Segmented, Select, Space, Tag, Typography } from 'antd';
import { EyeOutlined, ReloadOutlined } from '@ant-design/icons';
import { Link, useSearchParams } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import type { Material, Pack, PackMember, Part, Stage } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { REVIEW_REASON_LABELS } from '../../shared/lib/labels';
import { PART_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { PartTag, StageTag } from '../../shared/ui/StatusTags';
import { enqueueMaterialAnalysis } from '../ai/api';
import { useMaterialAnalysis } from '../ai/queries';
import { updateMaterial } from '../materials/api';
import { FilePreviewModal, canPreview } from '../materials/FilePreview';
import { invalidateLibrary, useAcceptMaterials, useMaterialsList } from '../materials/queries';
import { updatePack } from '../packs/api';
import { Completeness } from '../packs/PackBadges';
import { PackCatalogFields, formToUpdate, packToForm, type CatalogField, type CatalogFormValues } from '../packs/PackCatalogFields';
import { usePackFacets } from '../packs/queries';

const PRIMARY_CATALOG_FIELDS: CatalogField[] = ['doc_type', 'subject_codes', 'grade', 'cohort', 'region', 'organization', 'exam_name'];
const ADVANCED_CATALOG_FIELDS: CatalogField[] = ['term', 'exam_date', 'chapter', 'tags'];
const CATALOG_FIELDS = [...PRIMARY_CATALOG_FIELDS, ...ADVANCED_CATALOG_FIELDS];

type FormValues = CatalogFormValues;

interface PackReviewGroup {
  pack: Pack;
  items: Material[];
  members: PackMember[];
  suggestedItems: Material[];
}

interface MemberEdit {
  part: Part;
  answer_start_page: number;
}

function groupMaterials(items: Material[]): PackReviewGroup[] {
  const groups = new Map<string, PackReviewGroup>();
  for (const item of items) {
    if (!item.pack) continue;
    const current = groups.get(item.pack.id) ?? {
      pack: item.pack,
      items: [],
      members: item.pack.members,
      suggestedItems: [],
    };
    current.items.push(item);
    if (item.suggested_pack) current.suggestedItems.push(item);
    groups.set(item.pack.id, current);
  }
  return [...groups.values()];
}

function initialMemberEdits(members: PackMember[]): Record<string, MemberEdit> {
  return Object.fromEntries(
    members.map((member) => [member.id, { part: member.part, answer_start_page: member.answer_start_page }]),
  );
}

/** 右侧：当前套卷的一次性核对表单。文件部件可以逐个调整，编目信息只填一次。 */
function ReviewPackPanel({ group, stage, onDone }: { group: PackReviewGroup; stage: Stage; onDone: () => void }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const facets = usePackFacets();
  const analysis = useMaterialAnalysis(group.items[0]?.id ?? '');
  const accept = useAcceptMaterials();
  const [form] = Form.useForm<FormValues>();
  const [saving, setSaving] = useState(false);
  const [preview, setPreview] = useState<Material | null>(null);
  const [memberEdits, setMemberEdits] = useState<Record<string, MemberEdit>>(() => initialMemberEdits(group.members));
  const editable = stage === 'review';
  const reviewIds = group.items.filter((item) => item.stage === 'review').map((item) => item.id);

  useEffect(() => {
    form.resetFields();
    form.setFieldsValue(packToForm(group.pack));
    setMemberEdits(initialMemberEdits(group.members));
  }, [form, group.pack, group.members]);

  const uncertain = analysis.data?.suggestions.uncertain ?? [];

  const updateMember = (member: PackMember, patch: Partial<MemberEdit>) => {
    setMemberEdits((current) => ({
      ...current,
      [member.id]: { ...(current[member.id] ?? { part: member.part, answer_start_page: member.answer_start_page }), ...patch },
    }));
  };

  const confirm = useCallback(async () => {
    if (!editable || saving || reviewIds.length === 0) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      const touched = CATALOG_FIELDS.filter((field) => form.isFieldTouched(field));
      if (touched.length > 0) await updatePack(group.pack.id, formToUpdate(values, touched));

      const changes = group.members.filter((member) => {
        const next = memberEdits[member.id];
        return next && (next.part !== member.part || next.answer_start_page !== member.answer_start_page);
      });
      await Promise.all(
        changes.map((member) => {
          const next = memberEdits[member.id]!;
          return updateMaterial(member.id, { part: next.part, answer_start_page: next.answer_start_page });
        }),
      );
      await accept.mutateAsync({ ids: reviewIds });
      invalidateLibrary(queryClient);
      message.success(`已确认《${group.pack.title}》`);
      onDone();
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      setSaving(false);
    }
  }, [accept, editable, form, group.members, group.pack.id, group.pack.title, memberEdits, message, onDone, queryClient, reviewIds, saving]);

  const acceptSuggestion = useCallback(async () => {
    if (group.suggestedItems.length === 0) return;
    try {
      await accept.mutateAsync({ ids: group.suggestedItems.map((item) => item.id), acceptSuggestion: true });
      message.success('已接受配套建议');
      onDone();
    } catch (error) {
      message.error(errorMessage(error));
    }
  }, [accept, group.suggestedItems, message, onDone]);

  const retry = async () => {
    try {
      await Promise.all(group.items.filter((item) => item.stage === 'failed').map((item) => enqueueMaterialAnalysis(item.id, true)));
      invalidateLibrary(queryClient);
      message.success('已重新加入识别队列');
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const itemByID = new Map(group.items.map((item) => [item.id, item]));
  return (
    <Card
      className="inbox-panel"
      title={
        <div className="flex flex-col">
          <Typography.Text strong ellipsis>{group.pack.title || '待命名套卷'}</Typography.Text>
          <Typography.Text type="secondary" className="text-xs">
            {group.members.length} 个文件 · {group.pack.is_exam ? '试卷资料' : '普通资料'}
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
          {group.items.flatMap((item) => item.review_reasons).filter((reason, index, all) => all.indexOf(reason) === index).map((reason) => (
            <Tag key={reason} color="warning">{REVIEW_REASON_LABELS[reason]}</Tag>
          ))}
          {analysis.data?.status === 'succeeded' ? <Tag>AI 把握 {Math.round(analysis.data.confidence * 100)}%</Tag> : null}
        </Space>

        {stage === 'failed' ? (
          <Alert
            type="error"
            showIcon
            title="识别失败"
            description="可以重新识别，成功后会回到待确认。"
            action={<Button size="small" icon={<ReloadOutlined />} onClick={() => void retry()}>重新识别</Button>}
          />
        ) : null}

        {group.suggestedItems.length > 0 ? (
          <Alert
            type="info"
            showIcon
            title="发现可能的配套资料"
            description={group.suggestedItems.map((item) => item.suggested_pack?.title).filter(Boolean).join('、')}
            action={editable ? <Button size="small" type="primary" onClick={() => void acceptSuggestion()}>接受建议</Button> : undefined}
          />
        ) : null}

        {stage === 'processing' ? <Alert type="info" showIcon title="正在识别，完成后会自动出现在待确认" /> : null}

        {editable ? (
          <Form form={form} layout="vertical" disabled={saving}>
            <PackCatalogFields fields={PRIMARY_CATALOG_FIELDS} facets={facets.data} uncertain={uncertain} columns={2} />
            <Collapse
              size="small"
              items={[{ key: 'advanced', label: '更多编目信息（可选）', children: <PackCatalogFields fields={ADVANCED_CATALOG_FIELDS} facets={facets.data} uncertain={uncertain} columns={2} /> }]}
            />
          </Form>
        ) : null}

        <Card size="small" title={`文件（${group.members.length}）`} styles={{ body: { padding: 0 } }}>
          <ul className="inbox-pack-members">
            {group.members.map((member) => {
              const item = itemByID.get(member.id);
              const edit = memberEdits[member.id] ?? { part: member.part, answer_start_page: member.answer_start_page };
              return (
                <li key={member.id} className="inbox-pack-member">
                  <div className="inbox-pack-member__main">
                    <div className="inbox-pack-member__title">
                      {canPreview(member.media_type) && item ? <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => setPreview(item)} aria-label={`预览 ${member.file_name}`} /> : null}
                      <Link to={`/materials/${member.id}`}>{member.file_name}</Link>
                    </div>
                    <span className="inbox-pack-member__meta">
                      <PartTag part={edit.part} />
                      <span>{formatBytes(member.size_bytes)}</span>
                      <StageTag stage={member.stage} />
                      {edit.part === 'full' && edit.answer_start_page > 0 ? <span>答案从第 {edit.answer_start_page} 页开始</span> : null}
                    </span>
                  </div>
                  {editable && member.stage === 'review' ? (
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
          <Space wrap>
            <Button type="primary" loading={saving || accept.isPending} onClick={() => void confirm()}>
              确认这套 {reviewIds.length} 个文件
            </Button>
            {group.suggestedItems.length > 0 ? <Button loading={accept.isPending} onClick={() => void acceptSuggestion()}>接受配套建议</Button> : null}
          </Space>
        ) : null}
      </div>
      {preview ? <FilePreviewModal target={{ id: preview.id, title: preview.original_name, fileName: preview.file_name, mediaType: preview.media_type }} onClose={() => setPreview(null)} /> : null}
    </Card>
  );
}

const TABS: { value: Stage; label: string }[] = [
  { value: 'review', label: '待确认' },
  { value: 'processing', label: '处理中' },
  { value: 'failed', label: '失败' },
];

export function InboxPage() {
  const { message } = App.useApp();
  const [params, setParams] = useSearchParams();
  const stage = (TABS.find((tab) => tab.value === params.get('stage'))?.value ?? 'review') as Stage;
  const reason = params.get('reason') ?? '';
  const [page, setPage] = useState(1);
  const query = useMaterialsList(
    { page, page_size: 100, stage, reason: (reason as Material['review_reasons'][number]) || undefined },
    { poll: stage === 'review' ? false : 8000 },
  );
  const accept = useAcceptMaterials();
  const groups = useMemo(() => groupMaterials(query.data?.items ?? []), [query.data?.items]);
  const [activeId, setActiveId] = useState<string>();
  const [checked, setChecked] = useState<string[]>([]);
  const active = groups.find((group) => group.pack.id === activeId) ?? groups[0];

  useEffect(() => {
    setChecked([]);
    setActiveId(undefined);
    setPage(1);
  }, [stage, reason]);

  useEffect(() => {
    setChecked((current) => current.filter((id) => groups.some((group) => group.pack.id === id)));
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

  const bulkAccept = async (acceptSuggestion: boolean) => {
    const ids = groups.filter((group) => checked.includes(group.pack.id)).flatMap((group) => (acceptSuggestion ? group.suggestedItems : group.items).map((item) => item.id));
    if (ids.length === 0) return;
    try {
      const result = await accept.mutateAsync({ ids, acceptSuggestion });
      message.success(`已确认 ${result.count} 个文件`);
      setChecked([]);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const reasonOptions = Object.entries(REVIEW_REASON_LABELS).map(([value, label]) => ({ value, label }));
  return (
    <div className="page-stack">
      <PageHeader title="待确认" description="按套卷确认识别结果；同一套里的文件只需核对一次。J/K 可切换套卷。" />
      <div className="flex flex-wrap items-center gap-3">
        <Segmented value={stage} onChange={(value) => setParams({ stage: String(value) })} options={TABS} />
        {stage === 'review' ? <Select allowClear placeholder="按原因筛选" value={reason || undefined} options={reasonOptions} onChange={(value) => setParams(value ? { stage, reason: value } : { stage })} style={{ width: 160 }} /> : null}
        <Typography.Text type="secondary">共 {query.data?.total ?? 0} 个文件 · {groups.length} 套</Typography.Text>
      </div>

      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isPending ? <LoadingState /> : null}
      {!query.isPending && groups.length === 0 ? (
        <Card><Empty description={stage === 'review' ? '都确认好了，没有需要处理的套卷' : stage === 'processing' ? '没有正在识别的套卷' : '没有识别失败的套卷'} /></Card>
      ) : null}
      {!query.isPending && groups.length > 0 ? (
        <div className="inbox-layout">
          <Card className="inbox-list" size="small" styles={{ body: { padding: 0 } }}>
            {stage === 'review' ? (
              <div className="inbox-list__toolbar">
                <Checkbox
                  checked={checked.length > 0 && checked.length === groups.length}
                  indeterminate={checked.length > 0 && checked.length < groups.length}
                  onChange={(event) => setChecked(event.target.checked ? groups.map((group) => group.pack.id) : [])}
                >
                  全选套卷
                </Checkbox>
                {checked.length > 0 ? (
                  <Space size={4}>
                    <Button size="small" onClick={() => void bulkAccept(false)} loading={accept.isPending}>确认 {checked.length} 套</Button>
                    {groups.some((group) => checked.includes(group.pack.id) && group.suggestedItems.length > 0) ? <Button size="small" onClick={() => void bulkAccept(true)} loading={accept.isPending}>接受配套</Button> : null}
                  </Space>
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
                  {stage === 'review' ? <Checkbox checked={checked.includes(group.pack.id)} onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()} onChange={(event) => setChecked((list) => (event.target.checked ? [...list, group.pack.id] : list.filter((id) => id !== group.pack.id)))} /> : null}
                  <div className="inbox-item__body">
                    <span className="inbox-item__title">{group.pack.title || '待命名套卷'}</span>
                    <span className="inbox-item__meta">
                      <span>{group.members.length} 个文件</span>
                      {group.items.flatMap((item) => item.review_reasons).filter((reasonValue, index, all) => all.indexOf(reasonValue) === index).slice(0, 2).map((reasonValue) => <Tag key={reasonValue} color="warning">{REVIEW_REASON_LABELS[reasonValue]}</Tag>)}
                      {group.suggestedItems.length > 0 ? <Tag color="blue">有配套建议</Tag> : null}
                      <span>{formatDateTime(group.items[0]?.created_at ?? group.pack.created_at)}</span>
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
          {active ? <ReviewPackPanel key={active.pack.id} group={active} stage={stage} onDone={advance} /> : null}
        </div>
      ) : null}
    </div>
  );
}
