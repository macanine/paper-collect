// 与 docs/API.md 数据字典一致的 HTTP DTO（snake_case）。字段名不得在前端改写。

export const SUBJECT_CODES = [
  'chinese',
  'math',
  'english',
  'physics',
  'chemistry',
  'biology',
  'politics',
  'history',
  'geography',
  'information_technology',
  'general_technology',
  'comprehensive',
] as const;
export type SubjectCode = (typeof SUBJECT_CODES)[number];

/** 文件在套卷中的部件。 */
export const PARTS = ['questions', 'answers', 'full', 'teacher', 'answer_sheet', 'listening', 'main', 'other'] as const;
export type Part = (typeof PARTS)[number];

/** 套卷的资料类型。 */
export const DOC_TYPES = [
  'gaokao_real',
  'gaokao_mock',
  'zhongkao_real',
  'zhongkao_mock',
  'joint',
  'monthly',
  'midterm',
  'final',
  'opening',
  'unit',
  'weekly',
  'topic',
  'lecture',
  'study_guide',
  'courseware',
  'homework',
  'other',
] as const;
export type DocType = (typeof DOC_TYPES)[number];

export const GRADES = ['', 'grade7', 'grade8', 'grade9', 'grade10', 'grade11', 'grade12'] as const;
export type Grade = (typeof GRADES)[number];

export const TERMS = ['', 'first', 'second'] as const;
export type Term = (typeof TERMS)[number];

/** 用户唯一需要关心的整理状态。 */
export const STAGES = ['processing', 'review', 'ready', 'failed'] as const;
export type Stage = (typeof STAGES)[number];

export const REVIEW_REASONS = ['low_confidence', 'pack_suggestion', 'duplicate_suspect', 'analysis_failed', 'manual'] as const;
export type ReviewReason = (typeof REVIEW_REASONS)[number];

/** 归档状态：原件是否安全存放在本机 CAS。 */
export const MATERIAL_STATUSES = ['archived', 'missing'] as const;
export type MaterialStatus = (typeof MATERIAL_STATUSES)[number];

/** 同步状态：云端副本进度，独立于归档状态。 */
export const SYNC_STATUSES = ['waiting_catalog', 'not_required', 'syncing', 'synced', 'attention'] as const;
export type SyncStatus = (typeof SYNC_STATUSES)[number];

export const USER_ROLES = ['admin', 'editor', 'viewer'] as const;
export type UserRole = (typeof USER_ROLES)[number];

export const PROVIDER_KINDS = ['local', 'webdav', 'lanzou', '123pan', 'onedrive', 's3'] as const;
export type ProviderKind = (typeof PROVIDER_KINDS)[number];

const REPLICA_STATUSES = ['pending', 'uploading', 'moving', 'ready', 'failed', 'skipped', 'orphaned'] as const;
export type ReplicaStatus = (typeof REPLICA_STATUSES)[number];

const VERIFICATIONS = ['uploaded', 'size', 'sha256'] as const;
export type Verification = (typeof VERIFICATIONS)[number];

const SHARE_STATUSES = ['pending', 'ready', 'unsupported', 'failed'] as const;
export type ShareStatus = (typeof SHARE_STATUSES)[number];

export const JOB_KINDS = ['ingest', 'replicate', 'analyze', 'index', 'group'] as const;
export type JobKind = (typeof JOB_KINDS)[number];

export const JOB_STATUSES = ['pending', 'running', 'succeeded', 'failed'] as const;

const ANALYSIS_STATUSES = ['pending', 'running', 'succeeded', 'failed'] as const;
export type AnalysisStatus = (typeof ANALYSIS_STATUSES)[number];
export type JobStatus = (typeof JOB_STATUSES)[number];

const UPLOAD_STATUSES = ['pending', 'processing', 'completed', 'failed'] as const;
export type UploadStatus = (typeof UPLOAD_STATUSES)[number];

const UPLOAD_ENTRY_STATUSES = ['pending', 'received', 'existing'] as const;
type UploadEntryStatus = (typeof UPLOAD_ENTRY_STATUSES)[number];

export const SCOPES = [
  'materials:read',
  'materials:write',
  'collections:read',
  'collections:write',
  'uploads:write',
  'jobs:read',
  'jobs:write',
  'storage:read',
  'storage:write',
  'keys:manage',
  'users:manage',
  'audit:read',
  'ai:read',
  'ai:write',
  'search:read',
  'search:write',
  'nodes:manage',
] as const;
export type Scope = (typeof SCOPES)[number];

export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface LibraryStats {
  packs: number;
  files: number;
  processing: number;
  review: number;
  ready: number;
  failed: number;
  missing_answers: number;
  trashed: number;
  waiting_catalog: number;
  syncing: number;
  synced: number;
  attention: number;
  not_required: number;
  archived: number;
  missing: number;
}

export interface JobStats {
  total: number;
  pending: number;
  running: number;
  succeeded: number;
  failed: number;
}

export interface User {
  id: string;
  username: string;
  role: UserRole;
  enabled: boolean;
  created_at: string;
}

export interface AuthSession {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user: User;
}

export interface PackBrief {
  id: string;
  title: string;
}

export interface PackMember {
  id: string;
  title: string;
  file_name: string;
  part: Part;
  answer_start_page: number;
  stage: Stage;
  status: MaterialStatus;
  sync_status: SyncStatus;
  media_type: string;
  size_bytes: number;
  position: number;
}

export interface Pack {
  id: string;
  title: string;
  title_locked: boolean;
  catalog_locked: boolean;
  doc_type: DocType | '';
  subject_codes: SubjectCode[];
  grade: Grade;
  term: Term;
  cohort: number;
  region: string;
  organization: string;
  exam_date: string;
  exam_name: string;
  chapter: string;
  tags: string[];
  description: string;
  stage: Stage;
  is_exam: boolean;
  has_questions: boolean;
  has_answers: boolean;
  has_answer_sheet: boolean;
  members: PackMember[];
  collections?: Collection[];
  deleted_at: string | null;
  created_at: string;
  updated_at: string;
}

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

export interface FacetValue {
  value: string;
  count: number;
}

export interface PackFacets {
  subjects: FacetValue[];
  grades: FacetValue[];
  doc_types: FacetValue[];
  cohorts: FacetValue[];
  regions: FacetValue[];
  organizations: FacetValue[];
  tags: FacetValue[];
}

export interface Material {
  id: string;
  pack_id: string;
  position: number;
  part: Part;
  answer_start_page: number;
  title: string;
  file_name: string;
  original_name: string;
  stage: Stage;
  review_reasons: ReviewReason[];
  suggested_pack: PackBrief | null;
  status: MaterialStatus;
  sync_status: SyncStatus;
  sync_planned_at: string | null;
  media_type: string;
  size_bytes: number;
  sha256: string;
  created_at: string;
  updated_at: string;
  pack?: Pack;
}

export interface MaterialSource {
  id: string;
  upload_id: string;
  path: string;
  duplicate: boolean;
  created_at: string;
}

export interface Replica {
  id: string;
  blob_id: string;
  drive_id: string;
  rule_id: string;
  desired_path: string;
  remote_path: string;
  skip_reason: string;
  share: boolean;
  required: boolean;
  status: ReplicaStatus;
  size_bytes: number;
  verification: Verification;
  error: string;
  share_status: ShareStatus;
  share_url: string;
  share_password: string;
  share_expires_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface MaterialDetail extends Material {
  sources: MaterialSource[];
  replicas: Replica[];
}

export interface MaterialPullCache {
  download_url: string;
  file_name: string;
  media_type: string;
  size_bytes: number;
  sha256: string;
  expires_at: string;
  expires_in: number;
}

export interface MaterialUpdate {
  part?: Part;
  answer_start_page?: number;
  pack_id?: string;
  new_pack?: boolean;
}

/** 专题：人工整理的套卷集合。 */
export interface Collection {
  id: string;
  title: string;
  description: string;
  pack_count: number;
  created_at: string;
  updated_at: string;
}

export interface CollectionDetail extends Collection {
  packs: Pack[];
}

export interface UploadEntry {
  id: string;
  relative_path: string;
  size_bytes: number;
  sha256: string;
  status: UploadEntryStatus;
}

export interface UploadHints {
  subject_codes: SubjectCode[];
  grade: Grade;
  doc_type: DocType | '';
  cohort: number;
  region: string;
  organization: string;
  tags: string[];
}

export interface UploadResult {
  material_id: string;
  pack_id: string;
  title: string;
  path: string;
  duplicate: boolean;
  stage: Stage;
  part: Part;
}

export interface Upload {
  id: string;
  title: string;
  hints: UploadHints;
  status: UploadStatus;
  job_id: string;
  new_count: number;
  duplicate_count: number;
  skipped: string[];
  created_at: string;
  entries: UploadEntry[];
  results: UploadResult[];
}

export interface Job {
  id: string;
  kind: JobKind;
  status: JobStatus;
  upload_id: string;
  replica_id: string;
  material_id: string;
  node_id: string;
  node_name: string;
  attempts: number;
  error: string;
  progress_message: string;
  progress_current: number;
  progress_total: number;
  logs?: {
    time: string;
    level: 'info' | 'error';
    message: string;
    elapsed_ms?: number;
    duration_ms?: number;
    metrics?: Record<string, string | number | boolean>;
  }[];
  started_at: string | null;
  finished_at: string | null;
  duration_ms: number;
  created_at: string;
  updated_at: string;
}

export interface DriveCapabilities {
  upload: boolean;
  download: boolean;
  share: boolean;
  folders: boolean;
  move: boolean;
  delete: boolean;
}

export type OnPathChange = 'move' | 'keep';

export interface Drive {
  id: string;
  name: string;
  kind: ProviderKind;
  enabled: boolean;
  root_path: string;
  max_file_bytes: number;
  allowed_exts: string[];
  read_order: number;
  on_path_change: OnPathChange;
  config: Record<string, string>;
  secret_configured: boolean;
  capabilities: DriveCapabilities;
  /** 按副本状态统计的数量，size_bytes 为 ready + orphaned 副本字节数。 */
  stats: Record<string, number>;
  created_at: string;
  updated_at: string;
}

export interface DriveTestResult {
  healthy: boolean;
  message: string;
  capabilities: DriveCapabilities;
}

export interface RuleConditions {
  subjects?: SubjectCode[];
  doc_types?: DocType[];
  grades?: Grade[];
  cohorts?: number[];
  regions?: string[];
  tags?: string[];
  parts?: Part[];
  media_types?: string[];
  min_size?: number;
  max_size?: number;
}

export interface SyncTarget {
  drive_id: string;
  path_template: string;
  share: boolean;
  required: boolean;
}

export interface SyncRule {
  id: string;
  name: string;
  enabled: boolean;
  is_default: boolean;
  conditions: RuleConditions;
  targets: SyncTarget[];
}

export type SyncActionKind = 'upload' | 'move' | 'share' | 'skip' | 'orphan' | 'cancel' | 'drift' | 'unchanged' | 'not_required';

export interface SyncAction {
  material_id: string;
  file_name: string;
  rule_id: string;
  rule_name: string;
  drive_id: string;
  replica_id: string;
  action: SyncActionKind;
  desired_path: string;
  remote_path: string;
  reason: string;
  status: ReplicaStatus | '';
  error: string;
  share_status: ShareStatus | '';
  share: boolean;
  required: boolean;
}

export interface SyncPreview {
  preview_token: string;
  counts: Record<string, number>;
  items: SyncAction[];
  total: number;
}

export interface ReplicaBulkResult {
  id: string;
  success: boolean;
  error?: string;
}

export interface ReconcileResult {
  queued: number;
}

export interface APIKey {
  id: string;
  name: string;
  scopes: Scope[];
  expires_at: string | null;
  revoked: boolean;
  created_at: string;
}

export interface CreatedAPIKey {
  key: APIKey;
  secret: string;
}

export interface Audit {
  id: string;
  user_id: string;
  username: string;
  action: string;
  target_id: string;
  created_at: string;
}

export interface FinalizeResult {
  job_id: string;
  upload_id: string;
}


export interface AISuggestions {
  exam_name?: string;
  doc_type?: DocType;
  subject_codes?: SubjectCode[];
  grade?: Grade;
  term?: Term;
  cohort?: number;
  region?: string;
  organization?: string;
  exam_date?: string;
  chapter?: string;
  tags?: string[];
  part?: Part;
  answer_start_page?: number;
  uncertain?: string[];
}

export interface AIProvider {
  id: string;
  name: string;
  base_url: string;
  models: string[];
  default_model: string;
  vision_model: string;
  enabled: boolean;
  priority: number;
  api_key_configured: boolean;
  created_at: string;
  updated_at: string;
}

export interface AIProbeResult {
  models: string[];
  count: number;
}

export interface AITestResult {
  healthy: boolean;
  model: string;
  message: string;
}

export interface AISettings {
  analysis_enabled: boolean;
  ocr_provider_id: string;
  mineru_base_url: string;
  max_pages: number;
  max_images: number;
  auto_accept_confidence: number;
  mineru_configured: boolean;
  ocr_configured: boolean;
  updated_at: string;
}

export interface MinerUTestResult {
  healthy: boolean;
  message: string;
}

export type OCRProviderKind = 'mineru' | 'paddleocr' | 'rapidocr';

export interface OCRProvider {
  id: string;
  name: string;
  kind: OCRProviderKind;
  base_url: string;
  model: string;
  enabled: boolean;
  priority: number;
  token_configured: boolean;
  created_at: string;
  updated_at: string;
}

export interface MaterialAnalysis {
  id: string;
  material_id: string;
  status: AnalysisStatus;
  provider_id: string;
  provider_name: string;
  model: string;
  page_count: number;
  image_count: number;
  confidence: number;
  suggestions: AISuggestions;
  markdown_excerpt: string;
  error: string;
  applied_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface AnalysisEnqueueResult {
  job_id: string;
  analysis: MaterialAnalysis;
}


interface SearchMaterialRef {
  id: string;
  title: string;
  file_name: string;
}

export interface SearchHit {
  question_id: string;
  question_no: string;
  text: string;
  snippet: string;
  highlights: [number, number][];
  score: number;
  page_no: number;
  material: SearchMaterialRef;
  pack: PackBrief;
  subject_codes: SubjectCode[];
  also_in: PackBrief[];
}

export interface SearchQuestionsResult {
  items: SearchHit[];
  total: number;
  took_ms: number;
}

export interface QuestionRow {
  id: string;
  material_id: string;
  page_no: number;
  seq: number;
  question_no: string;
  text: string;
  confidence: number;
  created_at: string;
}

const INDEX_STATUSES = ['pending', 'running', 'succeeded', 'failed', 'not_applicable'] as const;
export type IndexStatus = (typeof INDEX_STATUSES)[number];

export interface MaterialIndexState {
  status: IndexStatus;
  job_id: string;
  page_count: number;
  question_count: number;
  llm_calls: number;
  llm_failed_batches: number;
  error: string;
  indexed_at: string | null;
}

export type NodeStatus = 'online' | 'stale' | 'offline' | 'draining' | 'disabled';
export type NodeKind = 'replicate' | 'analyze' | 'index';

/** 执行任务的节点。主端自身也是一个节点；令牌只在创建响应中出现。 */
export interface ClusterNode {
  id: string;
  name: string;
  is_master: boolean;
  enabled: boolean;
  draining: boolean;
  status: NodeStatus;
  kinds: NodeKind[];
  slots: Partial<Record<NodeKind, number>>;
  running: number;
  running_kinds: Partial<Record<JobKind, number>>;
  succeeded_1h: number;
  failed_1h: number;
  version: string;
  protocol: number;
  last_ip: string;
  last_seen_at: string | null;
  started_at: string | null;
  cpu_percent: number;
  cpu_cores: number;
  proc_cpu_percent: number;
  mem_used: number;
  mem_total: number;
  proc_rss: number;
  net_rx_bps: number;
  net_tx_bps: number;
  cache_used: number;
  cache_max: number;
  created_at: string;
}

export interface CreatedNode extends ClusterNode {
  token: string;
}

export interface NodePoint {
  time: string;
  cpu_percent: number;
  mem_used: number;
  mem_total: number;
  net_rx_bps: number;
  net_tx_bps: number;
  running: number;
}
