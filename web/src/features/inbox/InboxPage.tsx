import { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, App, Button, Card, Checkbox, Empty, Form, InputNumber, Segmented, Select, Space, Tag, Typography } from 'antd';
import { EyeOutlined, ReloadOutlined } from '@ant-design/icons';
import { Link, useSearchParams } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import type { Material, Part, Stage } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { formatDateTime } from '../../shared/lib/format';
import { REVIEW_REASON_LABELS } from '../../shared/lib/labels';
import { PART_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState } from '../../shared/ui/States';
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

const CATALOG_FIELDS: CatalogField[] = ['doc_type', 'subject_codes', 'grade', 'cohort', 'term', 'region', 'organization', 'exam_date', 'exam_name', 'chapter', 'tags'];

type FormValues = CatalogFormValues & { part?: Part; answer_start_page?: number };

/** 右侧：当前文件的核对表单。改动过的字段才会写回，确认后移出待整理。 */
function ReviewPanel({ material, onDone }: { material: Material; onDone: () => void }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const facets = usePackFacets();
  const analysis = useMaterialAnalysis(material.id);
  const accept = useAcceptMaterials();
  const [form] = Form.useForm<FormValues>();
  const [saving, setSaving] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const part = Form.useWatch('part', form);
  const pack = material.pack;

  useEffect(() => {
    form.resetFields();
    form.setFieldsValue({ ...(pack ? packToForm(pack) : {}), title: undefined, part: material.part, answer_start_page: material.answer_start_page || undefined });
  }, [form, material.id, material.part, material.answer_start_page, pack]);

  const confirm = useCallback(async () => {
    if (saving) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      const touched = CATALOG_FIELDS.filter((field) => form.isFieldTouched(field));
      if (pack && touched.length > 0) await updatePack(pack.id, formToUpdate(values, touched));
      if (values.part !== material.part || (values.answer_start_page ?? 0) !== material.answer_start_page) {
        await updateMaterial(material.id, { part: values.part, answer_start_page: values.answer_start_page ?? 0 });
      }
      await accept.mutateAsync({ ids: [material.id] });
      invalidateLibrary(queryClient);
      message.success('已确认入库');
      onDone();
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      setSaving(false);
    }
  }, [accept, form, material, message, onDone, pack, queryClient, saving]);

  const siblingsInReview = (pack?.members ?? []).filter((m) => m.id !== material.id && m.stage === 'review').map((m) => m.id);
  const confirmPack = async () => {
    await confirm();
    if (siblingsInReview.length === 0) return;
    try {
      await accept.mutateAsync({ ids: siblingsInReview });
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const acceptSuggestion = useCallback(async () => {
    try {
      await accept.mutateAsync({ ids: [material.id], acceptSuggestion: true });
      message.success(`已并入《${material.suggested_pack?.title ?? ''}》`);
      onDone();
    } catch (error) {
      message.error(errorMessage(error));
    }
  }, [accept, material, message, onDone]);

  const retry = async () => {
    try {
      await enqueueMaterialAnalysis(material.id, true);
      invalidateLibrary(queryClient);
      message.success('已重新加入识别队列');
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  // 键盘：Enter 确认，A 接受配套建议，P 预览。输入框内不拦截。
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (target.closest('input, textarea, select, button, a, [role="button"], .ant-select') || target.isContentEditable)) return;
      if (event.key === 'Enter' && !event.metaKey && !event.ctrlKey) {
        event.preventDefault();
        void confirm();
      } else if ((event.key === 'a' || event.key === 'A') && material.suggested_pack) {
        void acceptSuggestion();
      } else if ((event.key === 'p' || event.key === 'P') && canPreview(material.media_type)) {
        setPreviewing(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [acceptSuggestion, confirm, material]);

  const uncertain = analysis.data?.suggestions.uncertain ?? [];
  return (
    <Card
      className="inbox-panel"
      title={
        <div className="flex flex-col">
          <Typography.Text strong ellipsis>
            {material.original_name}
          </Typography.Text>
          <Typography.Text type="secondary" className="text-xs">
            将命名为：{material.file_name}
          </Typography.Text>
        </div>
      }
      extra={
        <Space>
          {canPreview(material.media_type) ? (
            <Button icon={<EyeOutlined />} onClick={() => setPreviewing(true)}>
              预览 (P)
            </Button>
          ) : null}
          <Link to={`/materials/${material.id}`}>详情</Link>
        </Space>
      }
    >
      <div className="page-stack">
        <Space wrap>
          <StageTag stage={material.stage} />
          {material.review_reasons.map((reason) => (
            <Tag key={reason} color="warning">
              {REVIEW_REASON_LABELS[reason]}
            </Tag>
          ))}
          {analysis.data?.status === 'succeeded' ? <Tag>AI 把握 {Math.round(analysis.data.confidence * 100)}%</Tag> : null}
          {pack ? <Completeness pack={pack} /> : null}
        </Space>

        {material.stage === 'failed' ? (
          <Alert
            type="error"
            showIcon
            title="识别失败"
            description={analysis.data?.error || '可以重新识别，或直接手动填写后确认。'}
            action={
              <Button size="small" icon={<ReloadOutlined />} onClick={() => void retry()}>
                重新识别
              </Button>
            }
          />
        ) : null}

        {material.suggested_pack ? (
          <Alert
            type="info"
            showIcon
            title={
              <span>
                可能和《<Link to={`/packs/${material.suggested_pack.id}`}>{material.suggested_pack.title}</Link>》是一套
              </span>
            }
            action={
              <Button size="small" type="primary" onClick={() => void acceptSuggestion()}>
                并入 (A)
              </Button>
            }
          />
        ) : null}

        {pack && pack.members.length > 1 ? (
          <Typography.Text type="secondary">
            同套还有：{pack.members.filter((m) => m.id !== material.id).map((m) => m.file_name).join('、')}
          </Typography.Text>
        ) : null}

        <Form form={form} layout="vertical" disabled={saving}>
          <div className="catalog-grid catalog-grid--2">
            <Form.Item name="part" label="这份文件是" {...(uncertain.includes('part') ? { validateStatus: 'warning' as const, help: 'AI 不确定，请核对' } : {})}>
              <Select options={PART_OPTIONS} />
            </Form.Item>
            {part === 'full' ? (
              <Form.Item name="answer_start_page" label="答案从第几页开始">
                <InputNumber min={1} max={10000} className="w-full" />
              </Form.Item>
            ) : (
              <span />
            )}
          </div>
          <Typography.Text type="secondary" className="block mb-2">
            以下是整套的编目信息（同套文件共用）
          </Typography.Text>
          <PackCatalogFields fields={CATALOG_FIELDS} facets={facets.data} uncertain={uncertain} />
        </Form>
        <Space>
          <Button type="primary" loading={saving} onClick={() => void confirm()}>
            确认入库 (Enter)
          </Button>
          {siblingsInReview.length > 0 ? (
            <Button loading={saving} onClick={() => void confirmPack()}>
              整套 {siblingsInReview.length + 1} 个一起确认
            </Button>
          ) : null}
          {pack ? <Link to={`/packs/${pack.id}`}>打开套卷</Link> : null}
        </Space>
      </div>
      {previewing ? (
        <FilePreviewModal
          target={{ id: material.id, title: material.original_name, fileName: material.file_name, mediaType: material.media_type }}
          onClose={() => setPreviewing(false)}
        />
      ) : null}
    </Card>
  );
}

const TABS: { value: Stage; label: string }[] = [
  { value: 'review', label: '待整理' },
  { value: 'processing', label: '识别中' },
  { value: 'failed', label: '识别失败' },
];

export function InboxPage() {
  const { message } = App.useApp();
  const [params, setParams] = useSearchParams();
  const stage = (TABS.find((t) => t.value === params.get('stage'))?.value ?? 'review') as Stage;
  const reason = params.get('reason') ?? '';
  const [page, setPage] = useState(1);
  const query = useMaterialsList(
    { page, page_size: 50, stage, reason: (reason as Material['review_reasons'][number]) || undefined },
    // 待整理列表由用户主动确认，持续轮询只会让表单和列表频繁重排；
    // 识别中/失败状态才需要低频刷新。
    { poll: stage === 'review' ? false : 8000 },
  );
  const accept = useAcceptMaterials();
  const items = useMemo(() => query.data?.items ?? [], [query.data]);
  const [activeId, setActiveId] = useState<string>();
  const [checked, setChecked] = useState<string[]>([]);
  const active = items.find((m) => m.id === activeId) ?? items[0];

  useEffect(() => {
    setChecked([]);
    setActiveId(undefined);
  }, [stage, reason]);

  useEffect(() => {
    setChecked((current) => {
      const next = current.filter((id) => items.some((item) => item.id === id));
      return next.length === current.length ? current : next;
    });
    setActiveId((current) => (current && items.some((item) => item.id === current) ? current : items[0]?.id));
  }, [items]);

  const advance = useCallback(() => {
    const index = items.findIndex((m) => m.id === active?.id);
    if (index < 0) {
      setActiveId(items[0]?.id);
      return;
    }
    const next = items[index + 1] ?? items[index - 1];
    setActiveId(next?.id);
  }, [active?.id, items]);

  // J / K 在列表中移动。
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (target.closest('input, textarea, select, button, a, [role="button"], .ant-select') || target.isContentEditable)) return;
      const index = items.findIndex((m) => m.id === active?.id);
      if (event.key === 'j' || event.key === 'J') setActiveId(items[Math.min(items.length - 1, index + 1)]?.id);
      if (event.key === 'k' || event.key === 'K') setActiveId(items[Math.max(0, index - 1)]?.id);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [active?.id, items]);

  const bulkAccept = async (acceptSuggestion: boolean) => {
    try {
      const result = await accept.mutateAsync({ ids: checked, acceptSuggestion });
      message.success(`已确认 ${result.count} 个文件`);
      setChecked([]);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const reasonOptions = Object.entries(REVIEW_REASON_LABELS).map(([value, label]) => ({ value, label }));
  return (
    <div className="page-stack">
      <PageHeader
        title="待整理"
        description="AI 把握不足、配套待定或识别失败的文件在这里等你确认；高把握的已自动入库。快捷键：J/K 切换，Enter 确认，A 接受配套，P 预览。"
      />
      <div className="flex flex-wrap items-center gap-3">
        <Segmented value={stage} onChange={(value) => setParams({ stage: String(value) })} options={TABS} />
        {stage === 'review' ? (
          <Select allowClear placeholder="按原因筛选" value={reason || undefined} options={reasonOptions} onChange={(v) => setParams(v ? { stage, reason: v } : { stage })} style={{ width: 160 }} />
        ) : null}
        <Typography.Text type="secondary">共 {query.data?.total ?? 0} 个</Typography.Text>
      </div>

      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}

      {items.length === 0 && !query.isPending ? (
        <Card>
          <Empty description={stage === 'review' ? '都整理好了，没有需要确认的文件' : stage === 'processing' ? '没有正在识别的文件' : '没有识别失败的文件'} />
        </Card>
      ) : (
        <div className="inbox-layout">
          <Card className="inbox-list" size="small" styles={{ body: { padding: 0 } }}>
            {stage === 'review' ? (
              <div className="inbox-list__toolbar">
                <Checkbox
                  checked={checked.length > 0 && checked.length === items.length}
                  indeterminate={checked.length > 0 && checked.length < items.length}
                  onChange={(e) => setChecked(e.target.checked ? items.map((m) => m.id) : [])}
                >
                  全选
                </Checkbox>
                {checked.length > 0 ? (
                  <Space size={4}>
                    <Button size="small" onClick={() => void bulkAccept(false)} loading={accept.isPending}>
                      确认 {checked.length} 个
                    </Button>
                    {items.some((m) => checked.includes(m.id) && m.suggested_pack) ? (
                      <Button size="small" onClick={() => void bulkAccept(true)} loading={accept.isPending}>
                        接受配套建议
                      </Button>
                    ) : null}
                  </Space>
                ) : null}
              </div>
            ) : null}
            <ul className="inbox-list__items">
              {items.map((m) => (
                <li
                  key={m.id}
                  className={`inbox-item${m.id === active?.id ? ' is-active' : ''}`}
                  role="button"
                  tabIndex={0}
                  aria-current={m.id === active?.id ? 'true' : undefined}
                  onClick={() => {
                    setActiveId(m.id);
                    // 窄屏时表单在列表下方，点选后滚过去。
                    if (window.matchMedia('(max-width: 991px)').matches) {
                      window.setTimeout(() => document.querySelector('.inbox-panel')?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 50);
                    }
                  }}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      setActiveId(m.id);
                    }
                  }}
                >
                  {stage === 'review' ? (
                    <Checkbox
                      checked={checked.includes(m.id)}
                      onClick={(e) => e.stopPropagation()}
                      onKeyDown={(e) => e.stopPropagation()}
                      onChange={(e) => setChecked((list) => (e.target.checked ? [...list, m.id] : list.filter((id) => id !== m.id)))}
                    />
                  ) : null}
                  <div className="inbox-item__body">
                    <span className="inbox-item__title">{m.original_name}</span>
                    <span className="inbox-item__meta">
                      <PartTag part={m.part} />
                      {m.review_reasons.slice(0, 2).map((r) => (
                        <Tag key={r} color="warning">
                          {REVIEW_REASON_LABELS[r]}
                        </Tag>
                      ))}
                      <span>{formatDateTime(m.created_at)}</span>
                    </span>
                  </div>
                </li>
              ))}
            </ul>
            {(query.data?.total ?? 0) > 50 ? (
              <div className="inbox-list__pager">
                <Button size="small" disabled={page === 1} onClick={() => setPage(page - 1)}>
                  上一页
                </Button>
                <span>第 {page} 页</span>
                <Button size="small" disabled={page * 50 >= (query.data?.total ?? 0)} onClick={() => setPage(page + 1)}>
                  下一页
                </Button>
              </div>
            ) : null}
          </Card>
          {active ? <ReviewPanel key={active.id} material={active} onDone={advance} /> : null}
        </div>
      )}
    </div>
  );
}
