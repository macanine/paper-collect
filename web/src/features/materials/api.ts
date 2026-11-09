import { apiDownload, apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  LibraryStats,
  Material,
  MaterialDetail,
  MaterialPullCache,
  MaterialStatus,
  MaterialUpdate,
  Page,
  Part,
  ReviewReason,
  Stage,
} from '../../shared/api/types';

export type MaterialListParams = {
  page: number;
  page_size: number;
  q?: string;
  stage?: Stage | '';
  part?: Part | '';
  status?: MaterialStatus | '';
  reason?: ReviewReason | '';
  pack_id?: string;
  sha256?: string;
};

export function listMaterials(params: MaterialListParams, signal?: AbortSignal): Promise<Page<Material>> {
  return apiRequest<Page<Material>>('/materials', { query: params, signal });
}

export function getLibraryStats(signal?: AbortSignal): Promise<LibraryStats> {
  return apiRequest<LibraryStats>('/materials/stats', { signal });
}

export function getMaterial(id: string): Promise<MaterialDetail> {
  return apiRequest<MaterialDetail>(`/materials/${segment(id)}`);
}

export function updateMaterial(id: string, body: MaterialUpdate): Promise<MaterialDetail> {
  return apiRequest<MaterialDetail>(`/materials/${segment(id)}`, { method: 'PATCH', body });
}

/** 确认待确认文件；acceptSuggestion 同时并入建议的套卷。 */
export function acceptMaterials(ids: string[], acceptSuggestion = false): Promise<{ count: number }> {
  return apiRequest<{ count: number }>('/materials/accept', { method: 'POST', body: { ids, accept_suggestion: acceptSuggestion } });
}

export function downloadMaterial(id: string) {
  return apiDownload(`/materials/${segment(id)}/download`);
}

export function pullMaterialCache(id: string): Promise<MaterialPullCache> {
  return apiRequest<MaterialPullCache>(`/materials/${segment(id)}/pull-cache`, { method: 'POST' });
}

export function downloadMaterialPullCache(id: string) {
  return apiDownload(`/materials/${segment(id)}/pull-cache`);
}

export function previewMaterial(id: string, signal?: AbortSignal) {
  return apiDownload(`/materials/${segment(id)}/preview`, { signal });
}

/** 单个文件移入回收站。 */
export function trashMaterial(id: string): Promise<void> {
  return apiRequest<void>(`/materials/${segment(id)}`, { method: 'DELETE' });
}
