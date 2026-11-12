import { useEffect, type ReactNode } from 'react';
import { Accordion, Alert, Stack, Text } from '@mantine/core';
import { useQueryClient } from '@tanstack/react-query';
import { ApiError, describeError } from '../../shared/api/errors';
import type { AICard, AIDecision, AIProblem } from '../../shared/api/types';
import { DOC_TYPE_LABELS, FIELD_LABELS, GRADE_LABELS, PART_LABELS, SUBJECT_LABELS, TERM_LABELS } from '../../shared/lib/labels';
import { formatDateTime } from '../../shared/lib/format';
import { Descriptions } from '../../shared/ui/Descriptions';
import { Section } from '../../shared/ui/Section';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import { materialKeys } from '../materials/queries';
import { analysisStatusCopy, cardFieldCount } from './format';
import { useMaterialCard, usePackAI, useRedecidePack, useRereadMaterial } from './queries';
import { qk } from '../../app/queryKeys';

interface AnalysisPanelProps {
  materialId: string;
  canWrite: boolean;
}

const EVIDENCE_LABELS: Record<string, string> = {
  title_text: '标题',
  grade: '年级',
  school_year_start: '学年',
  cohort: '届别',
  exam_date: '考试年月',
  exam_name: '考试名称',
  organization: '机构',
};

/** 文件详情：AI 对这一份文件的阅读记录（部件、标题、证据与校验问题）。 */
export function AnalysisPanel({ materialId, canWrite }: AnalysisPanelProps) {
  const queryClient = useQueryClient();
  const query = useMaterialCard(materialId);
  const reread = useRereadMaterial(materialId);
  const notFound = query.error instanceof ApiError && query.error.status === 404;

  // Reading finishes into grouping and a set decision; refresh the file so
  // its title, role and set appear without a reload.
  useEffect(() => {
    if (query.data?.status !== 'succeeded') return;
    void queryClient.invalidateQueries({ queryKey: materialKeys.detail(materialId) });
    void queryClient.invalidateQueries({ queryKey: qk.packs.all });
  }, [materialId, query.data?.status, queryClient]);

  const handleReread = () => {
    reread.mutate(undefined, {
      onSuccess: () => message.success('已交给 AI 重新阅读，完成后会重新确定所在套卷的编目'),
      onError: (error) => message.error(describeError(error).title),
    });
  };

  const busy = query.data?.status === 'pending' || query.data?.status === 'running';
  const extra = (
    <ConfirmButton size="compact-sm" variant="default" loading={reread.isPending} disabled={!canWrite || busy} confirmTitle="让 AI 重新阅读这个文件？" confirmDescription="所在套卷会随后重新编目，人工锁定的字段不变。" okText="重新阅读" onConfirm={handleReread}>重新阅读</ConfirmButton>
  );

  if (query.isPending) {
    return (
      <Section title="AI 阅读记录">
        <LoadingState />
      </Section>
    );
  }
  if (query.isError && !notFound) {
    return (
      <Section title="AI 阅读记录">
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      </Section>
    );
  }
  const view = query.data;
  if (notFound || !view || view.status === 'none') {
    return (
      <Section title="AI 阅读记录" extra={extra}>
        <Text c="dimmed" size="sm">AI 尚未阅读这个文件</Text>
      </Section>
    );
  }

  return (
    <Section title="AI 阅读记录" extra={extra}>
      <Descriptions
        items={[
          {
            key: 'status',
            label: '状态',
            children: <Tag color={view.status === 'failed' ? 'error' : view.status === 'succeeded' ? 'success' : 'processing'}>{analysisStatusCopy(view.status, view.error)}</Tag>,
          },
          { key: 'provider', label: '模型', children: view.model ? `${view.provider_name || '未记录'} / ${view.model}` : '—' },
          { key: 'pages', label: '页数', children: view.page_count || '—' },
          { key: 'updated', label: '更新时间', children: view.updated_at ? formatDateTime(view.updated_at) : '—' },
        ]}
      />
      {view.metadata_only ? <Alert color="yellow">没有读到正文，AI 只能依据文件名和路径判断</Alert> : null}
      {view.status === 'failed' ? <Alert color="red">{view.error || 'AI 未能读完这个文件'}</Alert> : null}
      {view.status === 'succeeded' ? (
        <Stack gap="md">
          <Text fw={600} size="sm">读到的信息（{cardFieldCount(view.card)} 项）</Text>
          <CardFields card={view.card} />
          <EvidenceList evidence={view.card.evidence} />
          <ProblemList problems={view.problems} />
          <Accordion variant="contained">
            <Accordion.Item value="markdown">
              <Accordion.Control>解析文本</Accordion.Control>
              <Accordion.Panel><Text size="sm" mah={320} style={{ overflow: 'auto', whiteSpace: 'pre-wrap' }}>{view.markdown_excerpt || '无文本'}</Text></Accordion.Panel>
            </Accordion.Item>
          </Accordion>
        </Stack>
      ) : null}
    </Section>
  );
}

/** 套卷详情：AI 最近一次为整套确定的编目与校验结果。 */
/** bare：嵌在需关注面板等已有卡片里时，不再画边框。 */
export function PackAIPanel({ packId, canWrite, bare }: { packId: string; canWrite: boolean; bare?: boolean }) {
  const query = usePackAI(packId);
  const redecide = useRedecidePack(packId);
  const busy = query.data?.status === 'pending' || query.data?.status === 'running';

  const handleRedecide = () => {
    redecide.mutate(undefined, {
      onSuccess: (result) => (result.queued ? message.success('已交给 AI 重新编目') : message.info('编目已是最新，未重复调用')),
      onError: (error) => message.error(describeError(error).title),
    });
  };

  const extra = (
    <ConfirmButton size="compact-sm" variant="default" loading={redecide.isPending} disabled={!canWrite || busy} confirmTitle="让 AI 重新确定整套编目？" confirmDescription="人工修改过的字段保持不变。" okText="重新编目" onConfirm={handleRedecide}>重新编目</ConfirmButton>
  );

  if (query.isPending) {
    return (
      <Section bare={bare} title="AI 编目">
        <LoadingState />
      </Section>
    );
  }
  if (query.isError) {
    return (
      <Section bare={bare} title="AI 编目">
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      </Section>
    );
  }
  const view = query.data;
  if (view.status === 'none') {
    return (
      <Section bare={bare} title="AI 编目" extra={extra}>
        <Text c="dimmed" size="sm">AI 还没有为这套资料确定编目</Text>
      </Section>
    );
  }
  return (
    <Section bare={bare} title="AI 编目" extra={extra}>
      <Descriptions
        items={[
          {
            key: 'status',
            label: '状态',
            children: <Tag color={view.status === 'failed' ? 'error' : view.status === 'succeeded' ? 'success' : 'processing'}>{analysisStatusCopy(view.status, view.error)}</Tag>,
          },
          { key: 'provider', label: '模型', children: view.model ? `${view.provider_name || '未记录'} / ${view.model}` : '—' },
          { key: 'decided', label: '确定时间', children: view.decided_at ? formatDateTime(view.decided_at) : '—' },
        ]}
      />
      {view.status === 'failed' ? <Alert color="red">{view.error || 'AI 未能确定编目'}</Alert> : null}
      {view.decision ? (
        <Stack gap="md">
          <DecisionFields decision={view.decision} />
          <EvidenceList evidence={view.decision.evidence} />
        </Stack>
      ) : null}
      <ProblemList problems={view.problems} />
    </Section>
  );
}

function fieldRows(entries: { key: string; label?: string; value: ReactNode }[]) {
  return entries.map((entry) => ({ key: entry.key, label: entry.label ?? FIELD_LABELS[entry.key] ?? entry.key, children: entry.value }));
}

function CardFields({ card }: { card: AICard }) {
  const entries: { key: string; label?: string; value: ReactNode }[] = [];
  entries.push({ key: 'part', label: '部件', value: PART_LABELS[card.role] });
  if (card.answer_start_page) entries.push({ key: 'answer_start_page', label: '答案起始页', value: `第 ${card.answer_start_page} 页` });
  if (card.title_text) entries.push({ key: 'title_text', label: '首页标题', value: card.title_text });
  if (card.doc_type) entries.push({ key: 'doc_type', value: DOC_TYPE_LABELS[card.doc_type] });
  if (card.subjects?.length) entries.push({ key: 'subject_codes', value: card.subjects.map((code) => SUBJECT_LABELS[code] ?? code).join('、') });
  if (card.grade) entries.push({ key: 'grade', value: GRADE_LABELS[card.grade] });
  if (card.school_year_start) entries.push({ key: 'school_year_start', label: '学年', value: `${card.school_year_start}-${card.school_year_start + 1} 学年` });
  if (card.cohort) entries.push({ key: 'cohort', value: `${card.cohort} 届` });
  if (card.exam_date) entries.push({ key: 'exam_date', value: card.exam_date });
  if (card.org_mentions?.length) entries.push({ key: 'org_mentions', label: '文中机构', value: card.org_mentions.join('、') });
  if (card.region_mentions?.length) entries.push({ key: 'region_mentions', label: '文中地区', value: card.region_mentions.join('、') });
  if (card.chapter) entries.push({ key: 'chapter', value: card.chapter });
  return <Descriptions items={fieldRows(entries)} />;
}

function DecisionFields({ decision }: { decision: AIDecision }) {
  const entries: { key: string; label?: string; value: ReactNode }[] = [];
  entries.push({ key: 'doc_type', value: DOC_TYPE_LABELS[decision.doc_type] });
  if (decision.subjects.length) entries.push({ key: 'subject_codes', value: decision.subjects.map((code) => SUBJECT_LABELS[code] ?? code).join('、') });
  if (decision.grade) entries.push({ key: 'grade', value: GRADE_LABELS[decision.grade] });
  if (decision.cohort) entries.push({ key: 'cohort', value: `${decision.cohort} 届` });
  if (decision.term) entries.push({ key: 'term', value: TERM_LABELS[decision.term] });
  if (decision.exam_date) entries.push({ key: 'exam_date', value: decision.exam_date });
  if (decision.region) entries.push({ key: 'region', value: decision.region });
  if (decision.exam_name) entries.push({ key: 'exam_name', value: decision.exam_name });
  if (decision.chapter) entries.push({ key: 'chapter', value: decision.chapter });
  return <Descriptions items={fieldRows(entries)} />;
}

function EvidenceList({ evidence }: { evidence?: Record<string, string> }) {
  const items = Object.entries(evidence ?? {});
  if (items.length === 0) return null;
  return (
    <Accordion variant="contained">
      <Accordion.Item value="evidence">
        <Accordion.Control>原文依据（{items.length} 条）</Accordion.Control>
        <Accordion.Panel>
          <Descriptions cols={1} items={items.map(([field, quote]) => ({ key: field, label: EVIDENCE_LABELS[field] ?? FIELD_LABELS[field] ?? field, children: `“${quote}”` }))} />
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  );
}

function ProblemList({ problems }: { problems: AIProblem[] }) {
  if (problems.length === 0) return <Text c="dimmed" size="sm">全部通过校验</Text>;
  return (
    <Stack gap="xs">
      {problems.map((problem, index) => (
        <Alert key={`${problem.code}-${index}`} color={problem.critical ? 'yellow' : 'blue'}>
          {problem.critical ? `需关注：${problem.message}` : `已剔除：${problem.message}`}
        </Alert>
      ))}
    </Stack>
  );
}
