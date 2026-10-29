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

export const ROLES = ['exam', 'answer', 'questions_answers', 'teacher_edition', 'lecture', 'other'] as const;
export type Role = (typeof ROLES)[number];

export const GRADES = ['', 'grade10', 'grade11', 'grade12'] as const;
export type Grade = (typeof GRADES)[number];

export const MATERIAL_STATUSES = ['pending_storage', 'archived', 'degraded'] as const;
export type MaterialStatus = (typeof MATERIAL_STATUSES)[number];

export const COLLECTION_KINDS = ['series', 'exam', 'folder', 'zip', 'custom'] as const;
export type CollectionKind = (typeof COLLECTION_KINDS)[number];

export const USER_ROLES = ['admin', 'editor', 'viewer'] as const;
export type UserRole = (typeof USER_ROLES)[number];

export const PROVIDER_KINDS = ['local', 'webdav', 'lanzou', '123pan', 'onedrive', 's3'] as const;
export type ProviderKind = (typeof PROVIDER_KINDS)[number];

export const PROVIDER_ROLES = ['primary', 'backup'] as const;
export type ProviderRole = (typeof PROVIDER_ROLES)[number];

export const STORAGE_MODES = ['mirror_all', 'primary_backup', 'balanced'] as const;
export type StorageMode = (typeof STORAGE_MODES)[number];

export const REPLICA_STATUSES = ['pending', 'uploading', 'ready', 'failed'] as const;
export type ReplicaStatus = (typeof REPLICA_STATUSES)[number];

export const VERIFICATIONS = ['uploaded', 'size', 'sha256'] as const;
export type Verification = (typeof VERIFICATIONS)[number];

export const SHARE_STATUSES = ['pending', 'ready', 'unsupported', 'failed'] as const;
export type ShareStatus = (typeof SHARE_STATUSES)[number];

export const JOB_KINDS = ['ingest', 'replicate', 'analyze', 'index'] as const;
export type JobKind = (typeof JOB_KINDS)[number];

export const JOB_STATUSES = ['pending', 'running', 'succeeded', 'failed'] as const;

export const ANALYSIS_STATUSES = ['pending', 'running', 'succeeded', 'failed'] as const;
export type AnalysisStatus = (typeof ANALYSIS_STATUSES)[number];
export type JobStatus = (typeof JOB_STATUSES)[number];

export const UPLOAD_STATUSES = ['pending', 'processing', 'completed', 'failed'] as const;
export type UploadStatus = (typeof UPLOAD_STATUSES)[number];

export const UPLOAD_ENTRY_STATUSES = ['pending', 'received'] as const;
export type UploadEntryStatus = (typeof UPLOAD_ENTRY_STATUSES)[number];

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
] as const;
export type Scope = (typeof SCOPES)[number];

export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface MaterialStats {
  total: number;
  pending_storage: number;
  archived: number;
  degraded: number;
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

export interface Material {
  id: string;
  title: string;
  description: string;
  subject_codes: SubjectCode[];
  grade: Grade;
  academic_year: number;
  tags: string[];
  role: Role;
  blob_id: string;
  file_name: string;
  media_type: string;
  size_bytes: number;
  sha256: string;
  source_path: string;
  parent_material_id: string;
  status: MaterialStatus;
  created_at: string;
  updated_at: string;
}

export interface Replica {
  id: string;
  blob_id: string;
  provider_id: string;
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
  replicas: Replica[];
}

export interface MaterialUpdate {
  title?: string;
  description?: string;
  subject_codes?: SubjectCode[];
  grade?: Grade;
  academic_year?: number;
  tags?: string[];
  role?: Role;
}

export interface Collection {
  id: string;
  title: string;
  description: string;
  kind: CollectionKind;
  parent_id: string;
  subject_codes: SubjectCode[];
  created_at: string;
  updated_at: string;
}

export interface CollectionItem {
  id: string;
  collection_id: string;
  material_id: string;
  position: number;
  role: Role;
  relative_path: string;
  material?: Material;
}

export interface CollectionDetail extends Collection {
  items: CollectionItem[];
}

export interface UploadEntry {
  id: string;
  relative_path: string;
  size_bytes: number;
  sha256: string;
  status: UploadEntryStatus;
}

export interface Upload {
  id: string;
  title: string;
  collection_kind: CollectionKind;
  subject_codes: SubjectCode[];
  grade: Grade;
  academic_year: number;
  tags: string[];
  status: UploadStatus;
  collection_id: string;
  material_id: string;
  job_id: string;
  created_at: string;
  entries: UploadEntry[];
}

export interface Job {
  id: string;
  kind: JobKind;
  status: JobStatus;
  upload_id: string;
  replica_id: string;
  material_id: string;
  attempts: number;
  error: string;
  progress_message: string;
  progress_current: number;
  progress_total: number;
  logs?: { time: string; level: 'info' | 'error'; message: string }[];
  created_at: string;
  updated_at: string;
}

export interface StorageProvider {
  id: string;
  name: string;
  kind: ProviderKind;
  role: ProviderRole;
  enabled: boolean;
  priority: number;
  weight: number;
  config: Record<string, string>;
  secret_configured: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProviderCapabilities {
  upload: boolean;
  download: boolean;
  share: boolean;
}

export interface ProviderTestResult {
  healthy: boolean;
  message: string;
  capabilities: ProviderCapabilities;
}

export interface StoragePolicy {
  mode: StorageMode;
  minimum_replicas: number;
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
  title?: string;
  subject_codes?: SubjectCode[];
  grade?: Grade;
  academic_year?: number;
  role?: Role;
  tags?: string[];
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


export interface SearchMaterialRef {
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
  subject_codes: SubjectCode[];
  collection_id: string;
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

export const INDEX_STATUSES = ['pending', 'running', 'succeeded', 'failed', 'not_applicable'] as const;
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
