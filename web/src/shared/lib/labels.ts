import type { SubjectCode, Part, DocType, Term, Stage, ReviewReason, Grade, MaterialStatus, UserRole, ProviderRole, StorageMode, ReplicaStatus, Verification, ShareStatus, JobKind, JobStatus, UploadStatus, Scope, AnalysisStatus, IndexStatus } from '../api/types';

export const SUBJECT_LABELS: Record<SubjectCode, string> = {
  chinese: '语文',
  math: '数学',
  english: '英语',
  physics: '物理',
  chemistry: '化学',
  biology: '生物',
  politics: '政治',
  history: '历史',
  geography: '地理',
  information_technology: '信息技术',
  general_technology: '通用技术',
  comprehensive: '综合',
};

export const PART_LABELS: Record<Part, string> = {
  questions: '试题卷',
  answers: '答案解析',
  full: '试题+答案',
  teacher: '教师版',
  answer_sheet: '答题卡',
  listening: '听力',
  main: '正文',
  other: '未分类',
};

export const DOC_TYPE_LABELS: Record<DocType, string> = {
  gaokao_real: '高考真题',
  gaokao_mock: '高考模拟',
  zhongkao_real: '中考真题',
  zhongkao_mock: '中考模拟',
  joint: '联考',
  monthly: '月考',
  midterm: '期中',
  final: '期末',
  opening: '开学考',
  unit: '单元测',
  weekly: '周测',
  topic: '专题练习',
  lecture: '讲义',
  study_guide: '学案',
  courseware: '课件',
  homework: '作业',
  other: '其他',
};

export const GRADE_LABELS: Record<Grade, string> = {
  '': '未指定',
  grade7: '初一',
  grade8: '初二',
  grade9: '初三',
  grade10: '高一',
  grade11: '高二',
  grade12: '高三',
};

export const TERM_LABELS: Record<Term, string> = {
  '': '未指定',
  first: '上学期',
  second: '下学期',
};

export const STAGE_LABELS: Record<Stage, string> = {
  processing: '识别中',
  review: '待整理',
  ready: '已入库',
  failed: '识别失败',
};

export const REVIEW_REASON_LABELS: Record<ReviewReason, string> = {
  low_confidence: 'AI 把握不足',
  pack_suggestion: '配套待确认',
  duplicate_suspect: '疑似重复',
  analysis_failed: '识别失败',
  manual: '需人工编目',
};

/** AI 返回的不确定字段名 → 中文。 */
export const FIELD_LABELS: Record<string, string> = {
  exam_name: '考试名称',
  doc_type: '资料类型',
  subject_codes: '学科',
  grade: '年级',
  term: '学期',
  cohort: '届别',
  region: '地区',
  organization: '来源机构',
  exam_date: '考试时间',
  chapter: '章节',
  tags: '标签',
  part: '部件',
};

export const MATERIAL_STATUS_LABELS: Record<MaterialStatus, string> = {
  pending_storage: '待存储',
  archived: '已归档',
  degraded: '降级',
};

export const USER_ROLE_LABELS: Record<UserRole, string> = {
  admin: '管理员',
  editor: '编辑',
  viewer: '查看者',
};

export const PROVIDER_ROLE_LABELS: Record<ProviderRole, string> = {
  primary: '主端',
  backup: '备端',
};

export const STORAGE_MODE_LABELS: Record<StorageMode, string> = {
  mirror_all: '全量镜像',
  primary_backup: '主备读写',
  balanced: '均衡分配',
};

export const REPLICA_STATUS_LABELS: Record<ReplicaStatus, string> = {
  pending: '待复制',
  uploading: '上传中',
  ready: '已就绪',
  failed: '失败',
};

export const VERIFICATION_LABELS: Record<Verification, string> = {
  uploaded: '仅上传成功',
  size: '字节数已校验',
  sha256: '内容完整性已校验',
};

export const SHARE_STATUS_LABELS: Record<ShareStatus, string> = {
  pending: '待生成',
  ready: '已分享',
  unsupported: '不支持分享',
  failed: '分享失败',
};

export const JOB_KIND_LABELS: Record<JobKind, string> = {
  ingest: '归档入库',
  replicate: '副本复制',
  analyze: 'AI 识别',
  index: '题目索引',
  group: '自动配套',
};

export const ANALYSIS_STATUS_LABELS: Record<AnalysisStatus, string> = {
  pending: '排队中',
  running: '识别中',
  succeeded: '已完成',
  failed: '失败',
};

export const JOB_STATUS_LABELS: Record<JobStatus, string> = {
  pending: '排队中',
  running: '执行中',
  succeeded: '已完成',
  failed: '失败',
};

export const UPLOAD_STATUS_LABELS: Record<UploadStatus, string> = {
  pending: '传输中',
  processing: '处理中',
  completed: '已完成',
  failed: '失败',
};

export const SCOPE_LABELS: Record<Scope, string> = {
  'materials:read': '资料读取',
  'materials:write': '资料编辑',
  'collections:read': '专题读取',
  'collections:write': '专题编辑',
  'uploads:write': '清单上传',
  'jobs:read': '任务查看',
  'jobs:write': '任务重试',
  'storage:read': '存储查看',
  'storage:write': '存储管理',
  'keys:manage': '密钥管理',
  'users:manage': '用户管理',
  'audit:read': '审计查看',
  'ai:read': 'AI 配置查看',
  'ai:write': 'AI 配置管理',
  'search:read': '搜索配置查看',
  'search:write': '搜索配置管理',
};

export const INDEX_STATUS_LABELS: Record<IndexStatus, string> = {
  pending: '排队中',
  running: '索引中',
  succeeded: '已索引',
  failed: '索引失败',
  not_applicable: '无需索引',
};

export function optionsFromLabels<T extends string>(labels: Record<T, string>, keys: readonly T[]): { value: T; label: string }[] {
  return keys.map((value) => ({ value, label: labels[value] }));
}
