import type { Drive, RuleConditions, SyncRule } from '../../shared/api/types';
import { DOC_TYPE_LABELS, GRADE_LABELS, PART_LABELS, SUBJECT_LABELS } from '../../shared/lib/labels';
import { formatBytes } from '../../shared/lib/format';
import type { TagColor } from '../../shared/ui/StatusTags';

/** 路径模板可用变量（与后端 templateKeys 一致）。 */
export const TEMPLATE_VARIABLES = ['学科', '年级', '学期', '届别', '地区', '机构', '类型', '考试年月', '套卷', '部件', '文件名', 'sha8'] as const;

export const DEFAULT_PATH_TEMPLATE = '{学科}/{届别}/{类型}/{套卷}/{文件名}';

function newRuleId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID().replace(/-/g, '').slice(0, 16);
  }
  return `r${Math.random().toString(36).slice(2, 18)}`;
}

export function makeRule(partial: Partial<SyncRule> = {}): SyncRule {
  return {
    id: partial.id ?? newRuleId(),
    name: partial.name ?? '',
    enabled: partial.enabled ?? true,
    is_default: partial.is_default ?? false,
    conditions: partial.conditions ?? {},
    targets: partial.targets ?? [],
  };
}

/** 条件摘要：例如「学科=数学 · 类型=试卷 · 大小 < 100 MB」。 */
export function describeConditions(conditions: RuleConditions): string {
  const parts: string[] = [];
  if (conditions.subjects?.length) parts.push(`学科=${conditions.subjects.map((s) => SUBJECT_LABELS[s] ?? s).join('/')}`);
  if (conditions.doc_types?.length) parts.push(`类型=${conditions.doc_types.map((t) => DOC_TYPE_LABELS[t] ?? t).join('/')}`);
  if (conditions.grades?.length) parts.push(`年级=${conditions.grades.map((g) => GRADE_LABELS[g] ?? g).join('/')}`);
  if (conditions.cohorts?.length) parts.push(`届别=${conditions.cohorts.map((c) => `${c}届`).join('/')}`);
  if (conditions.regions?.length) parts.push(`地区=${conditions.regions.join('/')}`);
  if (conditions.tags?.length) parts.push(`标签=${conditions.tags.join('/')}`);
  if (conditions.parts?.length) parts.push(`部件=${conditions.parts.map((p) => PART_LABELS[p] ?? p).join('/')}`);
  if (conditions.media_types?.length) parts.push(`格式=${conditions.media_types.map((m) => m.split('/')[1] ?? m).join('/')}`);
  if (conditions.min_size && conditions.max_size) parts.push(`大小 ${formatBytes(conditions.min_size)} ~ ${formatBytes(conditions.max_size)}`);
  else if (conditions.min_size) parts.push(`大小 ≥ ${formatBytes(conditions.min_size)}`);
  else if (conditions.max_size) parts.push(`大小 < ${formatBytes(conditions.max_size)}`);
  return parts.join(' · ');
}

export function driveName(drives: Drive[], id: string): string {
  return drives.find((d) => d.id === id)?.name ?? '未知网盘';
}

export const SYNC_ACTION_LABELS: Record<string, string> = {
  upload: '新增上传',
  move: '移动',
  share: '补分享',
  skip: '跳过',
  orphan: '规则外',
  cancel: '撤销',
  drift: '路径偏离',
  unchanged: '无变化',
  not_required: '无需同步',
  waiting_catalog: '等待编目',
};

export const SYNC_ACTION_COLORS: Record<string, TagColor> = {
  upload: 'processing',
  move: 'processing',
  share: 'processing',
  skip: 'warning',
  orphan: 'default',
  cancel: 'default',
  drift: 'warning',
  unchanged: 'success',
  not_required: 'default',
  waiting_catalog: 'default',
};
