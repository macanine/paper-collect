import { COLLECTION_KINDS, GRADES, MATERIAL_STATUSES, ROLES, SUBJECT_CODES, USER_ROLES, JOB_KINDS, JOB_STATUSES, type Grade, type MaterialStatus, type Role, type SubjectCode } from '../api/types';
import {
  COLLECTION_KIND_LABELS,
  GRADE_LABELS,
  JOB_KIND_LABELS,
  JOB_STATUS_LABELS,
  MATERIAL_STATUS_LABELS,
  ROLE_LABELS,
  SUBJECT_LABELS,
  USER_ROLE_LABELS,
  optionsFromLabels,
} from './labels';

// 选项列表来自 docs/API.md 数据字典（固定枚举），标签使用中文。

export const SUBJECT_OPTIONS = optionsFromLabels<SubjectCode>(SUBJECT_LABELS, SUBJECT_CODES);
export const ROLE_OPTIONS = optionsFromLabels<Role>(ROLE_LABELS, ROLES);
export const MATERIAL_STATUS_OPTIONS = optionsFromLabels<MaterialStatus>(MATERIAL_STATUS_LABELS, MATERIAL_STATUSES);
export const GRADE_OPTIONS = optionsFromLabels<Grade>(GRADE_LABELS, GRADES.filter((grade) => grade !== ''));
export const COLLECTION_KIND_OPTIONS = optionsFromLabels(COLLECTION_KIND_LABELS, COLLECTION_KINDS);
export const USER_ROLE_OPTIONS = optionsFromLabels(USER_ROLE_LABELS, USER_ROLES);
export const JOB_STATUS_OPTIONS = optionsFromLabels(JOB_STATUS_LABELS, JOB_STATUSES);
export const JOB_KIND_OPTIONS = optionsFromLabels(JOB_KIND_LABELS, JOB_KINDS);
