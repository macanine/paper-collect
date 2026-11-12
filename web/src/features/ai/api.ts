import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  AISettings,
  AIProvider,
  AIProbeResult,
  ProviderTestResult,
  Entity,
  EntityKind,
  MaterialCard,
  OCRProvider,
  OCRProviderKind,
  Page,
  PackAI,
  RerunResult,
} from '../../shared/api/types';

export interface AIProviderDraft {
  name: string;
  base_url: string;
  api_key?: string;
  models: string[];
  default_model: string;
  vision_model: string;
  enabled: boolean;
  priority: number;
}

export type AIProviderPatch = Partial<AIProviderDraft>;

export function listAIProviders(): Promise<AIProvider[]> {
  return apiRequest<AIProvider[]>('/ai/providers');
}

export function createAIProvider(draft: AIProviderDraft): Promise<AIProvider> {
  return apiRequest<AIProvider>('/ai/providers', { method: 'POST', body: draft });
}

export function updateAIProvider(id: string, patch: AIProviderPatch): Promise<AIProvider> {
  return apiRequest<AIProvider>(`/ai/providers/${segment(id)}`, { method: 'PATCH', body: patch });
}

export function probeAIProviders(input: { base_url?: string; api_key?: string; provider_id?: string }): Promise<AIProbeResult> {
  return apiRequest<AIProbeResult>('/ai/providers/probe', { method: 'POST', body: input });
}

export function probeSavedProvider(id: string): Promise<AIProbeResult> {
  return apiRequest<AIProbeResult>(`/ai/providers/${segment(id)}/probe`, { method: 'POST' });
}

export function testAIProvider(id: string, model: string): Promise<ProviderTestResult> {
  return apiRequest<ProviderTestResult>(`/ai/providers/${segment(id)}/test`, { method: 'POST', body: { model } });
}

export function getAISettings(): Promise<AISettings> {
  return apiRequest<AISettings>('/ai/settings');
}

export function putAISettings(input: { ocr_provider_id: string; max_pages: number; max_images: number }): Promise<AISettings> {
  return apiRequest<AISettings>('/ai/settings', { method: 'PUT', body: input });
}

export interface OCRProviderDraft {
  name: string;
  kind: OCRProviderKind;
  base_url: string;
  model: string;
  token?: string;
  enabled: boolean;
  priority: number;
}

export type OCRProviderPatch = Partial<Omit<OCRProviderDraft, 'kind'>>;

export function listOCRProviders(): Promise<OCRProvider[]> {
  return apiRequest<OCRProvider[]>('/ai/ocr-providers');
}

export function createOCRProvider(draft: OCRProviderDraft): Promise<OCRProvider> {
  return apiRequest<OCRProvider>('/ai/ocr-providers', { method: 'POST', body: draft });
}

export function updateOCRProvider(id: string, patch: OCRProviderPatch): Promise<OCRProvider> {
  return apiRequest<OCRProvider>(`/ai/ocr-providers/${segment(id)}`, { method: 'PATCH', body: patch });
}

export function testOCRProvider(id: string): Promise<ProviderTestResult> {
  return apiRequest<ProviderTestResult>(`/ai/ocr-providers/${segment(id)}/test`, { method: 'POST', body: {} });
}

/** AI 对单个文件的阅读记录。 */
export function getMaterialCard(materialId: string): Promise<MaterialCard> {
  return apiRequest<MaterialCard>(`/materials/${segment(materialId)}/analysis`);
}

/** 让 AI 重新阅读该文件，之后重新确定所在套卷的编目。 */
export function rereadMaterial(materialId: string): Promise<{ queued: boolean }> {
  return apiRequest<{ queued: boolean }>(`/materials/${segment(materialId)}/analysis`, { method: 'POST', body: {} });
}

/** 套卷最近一次 AI 编目决定。 */
export function getPackAI(packId: string): Promise<PackAI> {
  return apiRequest<PackAI>(`/packs/${segment(packId)}/ai`);
}

/** 让 AI 重新确定套卷编目；人工锁定的字段保持不变。force 为 false 时，成员与提示词都没变就直接跳过。 */
export function redecidePack(packId: string, force = true): Promise<{ queued: boolean }> {
  return apiRequest<{ queued: boolean }>(`/packs/${segment(packId)}/ai/redecide`, { method: 'POST', body: { force } });
}

/** 让已有文件重新经过 AI；dry_run 只返回影响范围。 */
export function rerunAI(input: { mode: 'decide' | 'full'; scope: 'attention' | 'all'; dry_run: boolean }): Promise<RerunResult> {
  return apiRequest<RerunResult>('/ai/rerun', { method: 'POST', body: input });
}

export function listEntities(params: { kind: EntityKind; q?: string; page: number; page_size: number }, signal?: AbortSignal): Promise<Page<Entity>> {
  return apiRequest<Page<Entity>>('/entities', { query: params, signal });
}

export function updateEntity(id: string, patch: { name?: string; region?: string; locked?: boolean; remove_aliases?: string[] }): Promise<Entity> {
  return apiRequest<Entity>(`/entities/${segment(id)}`, { method: 'PATCH', body: patch });
}

export function mergeEntity(id: string, into: string): Promise<Entity> {
  return apiRequest<Entity>(`/entities/${segment(id)}/merge`, { method: 'POST', body: { into } });
}
