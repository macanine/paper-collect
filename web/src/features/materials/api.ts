import { apiDownload, apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  LibraryStats,
  MaterialDetail,
  MaterialUpdate,
} from '../../shared/api/types';

export function getLibraryStats(signal?: AbortSignal): Promise<LibraryStats> {
  return apiRequest<LibraryStats>('/materials/stats', { signal });
}

export function getMaterial(id: string): Promise<MaterialDetail> {
  return apiRequest<MaterialDetail>(`/materials/${segment(id)}`);
}

export function updateMaterial(id: string, body: MaterialUpdate): Promise<MaterialDetail> {
  return apiRequest<MaterialDetail>(`/materials/${segment(id)}`, { method: 'PATCH', body });
}

export function downloadMaterial(id: string) {
  return apiDownload(`/materials/${segment(id)}/download`);
}

export function previewMaterial(id: string, signal?: AbortSignal) {
  return apiDownload(`/materials/${segment(id)}/preview`, { signal });
}

/** 单个文件移入回收站。 */
export function trashMaterial(id: string): Promise<void> {
  return apiRequest<void>(`/materials/${segment(id)}`, { method: 'DELETE' });
}
