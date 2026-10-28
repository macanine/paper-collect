import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  AISettings,
  AITestResult,
  AIProvider,
  AIProbeResult,
  AnalysisEnqueueResult,
  MaterialDetail,
  MaterialAnalysis,
  MinerUTestResult,
  Role,
  SubjectCode,
  Grade,
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

export function testAIProvider(id: string, model: string): Promise<AITestResult> {
  return apiRequest<AITestResult>(`/ai/providers/${segment(id)}/test`, { method: 'POST', body: { model } });
}

export function getAISettings(): Promise<AISettings> {
  return apiRequest<AISettings>('/ai/settings');
}

export function putAISettings(input: {
  analysis_enabled: boolean;
  mineru_base_url: string;
  mineru_token?: string;
  max_pages: number;
  max_images: number;
}): Promise<AISettings> {
  return apiRequest<AISettings>('/ai/settings', { method: 'PUT', body: input });
}

export function testMinerU(): Promise<MinerUTestResult> {
  return apiRequest<MinerUTestResult>('/ai/mineru/test', { method: 'POST' });
}

export function getMaterialAnalysis(materialId: string): Promise<MaterialAnalysis> {
  return apiRequest<MaterialAnalysis>(`/materials/${segment(materialId)}/analysis`);
}

export function enqueueMaterialAnalysis(materialId: string, force: boolean): Promise<AnalysisEnqueueResult> {
  return apiRequest<AnalysisEnqueueResult>(`/materials/${segment(materialId)}/analysis`, {
    method: 'POST',
    body: { force },
  });
}

export interface AnalysisOverride {
  title?: string;
  subject_codes?: SubjectCode[];
  grade?: Grade;
  academic_year?: number;
  role?: Role;
  tags?: string[];
}

export function applyMaterialAnalysis(materialId: string, override?: AnalysisOverride): Promise<MaterialDetail> {
  return apiRequest<MaterialDetail>(`/materials/${segment(materialId)}/analysis/apply`, {
    method: 'POST',
    body: override ?? {},
  });
}

