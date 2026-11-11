// HTTP 契约：响应类型与枚举由后端 DTO 生成（contract.gen.ts，`make contract`），
// 这里只保留请求体等前端独有的类型。字段名为 snake_case 线上格式，不得在前端改写。

export * from './contract.gen';
import type { DocType, Grade, Part, SubjectCode, Term } from './contract.gen';

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
  /** 交还 AI 的人工锁定字段；AI 会重新编目这套资料。 */
  unlock_fields?: string[];
}

export interface MaterialUpdate {
  part?: Part;
  answer_start_page?: number;
  pack_id?: string;
  new_pack?: boolean;
}
