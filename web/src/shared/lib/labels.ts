import type { SubjectCode, Role, Grade, MaterialStatus, CollectionKind, UserRole, ProviderKind, ProviderRole, StorageMode, ReplicaStatus, Verification, ShareStatus, JobKind, JobStatus, UploadStatus, Scope, AnalysisStatus, IndexStatus } from '../api/types';

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

export const ROLE_LABELS: Record<Role, string> = {
  exam: '试卷',
  answer: '答案',
  questions_answers: '试卷+答案',
  teacher_edition: '教师版',
  lecture: '讲义',
  other: '其他',
};

export const GRADE_LABELS: Record<Grade, string> = {
  '': '未指定',
  grade10: '高一',
  grade11: '高二',
  grade12: '高三',
};

export const MATERIAL_STATUS_LABELS: Record<MaterialStatus, string> = {
  pending_storage: '待存储',
  archived: '已归档',
  degraded: '降级',
};

export const COLLECTION_KIND_LABELS: Record<CollectionKind, string> = {
  series: '系列',
  exam: '整套试卷',
  folder: '文件夹',
  zip: 'ZIP 包',
  custom: '自定义',
};

export const USER_ROLE_LABELS: Record<UserRole, string> = {
  admin: '管理员',
  editor: '编辑',
  viewer: '查看者',
};

export const PROVIDER_KIND_LABELS: Record<ProviderKind, string> = {
  local: '本地存储',
  webdav: 'WebDAV',
  lanzou: '蓝奏云',
  '123pan': '123 云盘',
  onedrive: 'OneDrive',
  s3: 'S3 / 兼容对象存储',
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
  group: '自动归类',
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
  pending: '待传输',
  processing: '归档处理中',
  completed: '已完成',
  failed: '失败',
};

export const SCOPE_LABELS: Record<Scope, string> = {
  'materials:read': '资料读取',
  'materials:write': '资料编辑',
  'collections:read': '合集读取',
  'collections:write': '合集编辑',
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
