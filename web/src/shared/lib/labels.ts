import type { SubjectCode, Part, DocType, Term, Stage, AIStep, Grade, MaterialStatus, SyncStatus, UserRole, ReplicaStatus, Verification, ShareStatus, JobKind, JobStatus, UploadStatus, Scope, AnalysisStatus, IndexStatus,
  NodeStatus,
} from '../api/types';

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
  processing: 'AI 整理中',
  waiting_ai: '等待 AI',
  attention: '需关注',
  ready: '已入库',
};

export const AI_STEP_LABELS: Record<AIStep, string> = {
  '': '',
  read: '阅读文件',
  group: '分组成套',
  decide: '确定编目',
};

/** 需关注原因（流程原因与校验代码，代码后的 :字段 部分单独显示）。 */
export const ATTENTION_REASON_LABELS: Record<string, string> = {
  ai_failed: 'AI 多次失败',
  group_unverified: '分组未能确认',
  metadata_only: '未读到正文',
  duplicate_role: '部件重复',
  missing_file: '编目漏掉文件',
  unknown_file: '编目含未知文件',
  duplicate_file: '文件重复列出',
  role_invalid: '部件无效',
  answer_page: '缺答案起始页',
  doc_type_missing: '缺资料类型',
  doc_type_invalid: '资料类型无效',
  subjects_missing: '缺学科',
  grade_missing: '缺年级',
  not_json: '模型输出无效',
};

/** 显示一条需关注原因，例如 duplicate_role:files → 部件重复。 */
export function attentionReasonLabel(reason: string): string {
  const code = reason.split(':')[0] ?? reason;
  return ATTENTION_REASON_LABELS[code] ?? code;
}

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
  archived: '已归档',
  missing: '原件缺失',
};

export const SYNC_STATUS_LABELS: Record<SyncStatus, string> = {
  waiting_catalog: '等待编目',
  not_required: '无需同步',
  syncing: '同步中',
  synced: '已同步',
  attention: '需处理',
};

export const USER_ROLE_LABELS: Record<UserRole, string> = {
  admin: '管理员',
  editor: '编辑',
  viewer: '查看者',
};

export const REPLICA_STATUS_LABELS: Record<ReplicaStatus, string> = {
  pending: '待处理',
  uploading: '上传中',
  moving: '移动中',
  ready: '已就绪',
  failed: '失败',
  skipped: '已跳过',
  orphaned: '规则外',
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
  analyze: 'AI 阅读',
  index: '题目索引',
  group: 'AI 分组',
  decide: 'AI 编目',
  entity: '名称去重',
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
  'nodes:manage': '节点管理',
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

export const NODE_STATUS_LABELS: Record<NodeStatus, string> = {
  online: '在线',
  stale: '失联',
  offline: '离线',
  draining: '排空中',
  disabled: '已停用',
};
