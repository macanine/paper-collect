import { DOC_TYPES, GRADES, MATERIAL_STATUSES, PARTS, STAGES, SUBJECT_CODES, TERMS, USER_ROLES, JOB_KINDS, JOB_STATUSES, type DocType, type Grade, type MaterialStatus, type Part, type Stage, type SubjectCode, type Term } from '../api/types';
import {
  DOC_TYPE_LABELS,
  GRADE_LABELS,
  JOB_KIND_LABELS,
  JOB_STATUS_LABELS,
  MATERIAL_STATUS_LABELS,
  PART_LABELS,
  STAGE_LABELS,
  SUBJECT_LABELS,
  TERM_LABELS,
  USER_ROLE_LABELS,
  optionsFromLabels,
} from './labels';

// 选项列表来自 docs/API.md 数据字典（固定枚举），标签使用中文。

export const SUBJECT_OPTIONS = optionsFromLabels<SubjectCode>(SUBJECT_LABELS, SUBJECT_CODES);
export const PART_OPTIONS = optionsFromLabels<Part>(PART_LABELS, PARTS);
export const DOC_TYPE_OPTIONS = optionsFromLabels<DocType>(DOC_TYPE_LABELS, DOC_TYPES);
export const STAGE_OPTIONS = optionsFromLabels<Stage>(STAGE_LABELS, STAGES);
export const MATERIAL_STATUS_OPTIONS = optionsFromLabels<MaterialStatus>(MATERIAL_STATUS_LABELS, MATERIAL_STATUSES);
export const GRADE_OPTIONS = optionsFromLabels<Grade>(GRADE_LABELS, GRADES.filter((grade) => grade !== ''));
export const TERM_OPTIONS = optionsFromLabels<Term>(TERM_LABELS, TERMS.filter((term) => term !== ''));
export const USER_ROLE_OPTIONS = optionsFromLabels(USER_ROLE_LABELS, USER_ROLES);
export const JOB_STATUS_OPTIONS = optionsFromLabels(JOB_STATUS_LABELS, JOB_STATUSES);
export const JOB_KIND_OPTIONS = optionsFromLabels(JOB_KIND_LABELS, JOB_KINDS);

/** 届别选项：当前年份前后若干届。 */
export function cohortOptions(now = new Date()): { value: number; label: string }[] {
  const base = now.getFullYear() + 3;
  return Array.from({ length: 12 }, (_, i) => base - i).map((year) => ({ value: year, label: `${year} 届` }));
}
