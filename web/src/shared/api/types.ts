// HTTP 契约：响应类型与枚举由后端 DTO 生成（contract.gen.ts，`make contract`），
// 这里只保留请求体等前端独有的类型。字段名为 snake_case 线上格式，不得在前端改写。

export * from './contract.gen';
import type { DocType, Grade, Part, SubjectCode, Term } from './contract.gen';

/** 需关注原因：流程原因或校验代码（如 duplicate_role:files）。 */
export const ATTENTION_REASONS = [
  'ai_failed',
  'group_unverified',
  'metadata_only',
  'duplicate_role',
  'missing_file',
  'unknown_file',
  'duplicate_file',
  'role_invalid',
  'answer_page',
  'doc_type_missing',
  'doc_type_invalid',
  'subjects_missing',
  'grade_missing',
  'not_json',
] as const;
export type AttentionReason = string;

export interface PackUpdate {
  title?: string;
  doc_type?: DocType | '';
  subject_codes?: SubjectCode[];
  grade?: Grade;
  term?: Term;
  cohort?: number;
  region?: string;
  organization?: string;
  exam_date?: string;
  exam_name?: string;
  chapter?: string;
  tags?: string[];
  description?: string;
}

export interface MaterialUpdate {
  part?: Part;
  answer_start_page?: number;
  pack_id?: string;
  new_pack?: boolean;
}
