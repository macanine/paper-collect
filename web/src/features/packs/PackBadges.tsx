import { Group, Text } from '@mantine/core';
import type { Pack } from '../../shared/api/types';
import { DOC_TYPE_LABELS, GRADE_LABELS, SUBJECT_LABELS } from '../../shared/lib/labels';
import { StageTag, Tag } from '../../shared/ui/StatusTags';

/** 试卷类套卷的齐全度：齐全时只是一行说明，缺什么才用警告标签提示。 */
export function Completeness({ pack }: { pack: Pick<Pack, 'is_exam' | 'has_questions' | 'has_answers' | 'has_answer_sheet'> }) {
  if (!pack.is_exam) return null;
  if (pack.has_questions && pack.has_answers) {
    return <Text size="sm" c="dimmed" aria-label="齐全度" style={{ whiteSpace: 'nowrap' }}>{pack.has_answer_sheet ? '含试题、答案、答题卡' : '含试题、答案'}</Text>;
  }
  return (
    <Group gap="xs" wrap="nowrap" aria-label="齐全度">
      {pack.has_questions ? null : <Tag color="warning">缺试题</Tag>}
      {pack.has_answers ? null : <Tag color="warning">缺答案</Tag>}
    </Group>
  );
}

/** 一行概括套卷的关键编目信息；只有未完成整理时才带状态标签。 */
export function PackMeta({ pack }: { pack: Pack }) {
  const bits = [
    pack.doc_type ? DOC_TYPE_LABELS[pack.doc_type] : '',
    pack.cohort ? `${pack.cohort} 届` : '',
    GRADE_LABELS[pack.grade] !== '未指定' ? GRADE_LABELS[pack.grade] : '',
    pack.subject_codes.map((code) => SUBJECT_LABELS[code]).join('·'),
    pack.region,
    pack.organization,
    pack.exam_date,
  ].filter(Boolean);
  return (
    <Group gap="xs" wrap="nowrap" align="flex-start" miw={0}>
      {pack.stage !== 'ready' ? <StageTag stage={pack.stage} /> : null}
      <Text size="sm" c="dimmed" lineClamp={2} miw={0} style={{ whiteSpace: 'normal' }}>{bits.join(' · ') || '未编目'}</Text>
    </Group>
  );
}
