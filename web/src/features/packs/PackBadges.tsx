import { Tag, Tooltip } from 'antd';
import type { Pack } from '../../shared/api/types';
import { GRADE_LABELS, SUBJECT_LABELS } from '../../shared/lib/labels';
import { DocTypeTag, StageTag } from '../../shared/ui/StatusTags';

/** 试卷类套卷的齐全度：试题 / 答案 / 答题卡。 */
export function Completeness({ pack }: { pack: Pick<Pack, 'is_exam' | 'has_questions' | 'has_answers' | 'has_answer_sheet'> }) {
  if (!pack.is_exam) return null;
  return (
    <span className="completeness" aria-label="齐全度">
      <Tooltip title={pack.has_questions ? '有试题' : '缺试题'}>
        <Tag color={pack.has_questions ? 'success' : 'default'}>{pack.has_questions ? '✓' : '✗'} 试题</Tag>
      </Tooltip>
      <Tooltip title={pack.has_answers ? '有答案' : '缺答案'}>
        <Tag color={pack.has_answers ? 'success' : 'warning'}>{pack.has_answers ? '✓' : '✗'} 答案</Tag>
      </Tooltip>
      {pack.has_answer_sheet ? <Tag color="success">✓ 答题卡</Tag> : null}
    </span>
  );
}

/** 一行概括套卷的关键编目信息。 */
export function PackMeta({ pack }: { pack: Pack }) {
  const bits = [
    pack.cohort ? `${pack.cohort} 届` : '',
    GRADE_LABELS[pack.grade] !== '未指定' ? GRADE_LABELS[pack.grade] : '',
    pack.subject_codes.map((code) => SUBJECT_LABELS[code]).join('·'),
    pack.region,
    pack.organization,
    pack.exam_date,
  ].filter(Boolean);
  return (
    <span className="pack-meta">
      <DocTypeTag type={pack.doc_type} />
      {pack.stage !== 'ready' ? <StageTag stage={pack.stage} /> : null}
      <span className="pack-meta__text">{bits.join(' · ') || '未编目'}</span>
    </span>
  );
}
