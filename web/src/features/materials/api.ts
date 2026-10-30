import { apiDownload, apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { Grade, MaterialDetail, MaterialStatus, Material, MaterialStats, MaterialUpdate, Page, Role, SubjectCode } from '../../shared/api/types';

export type MaterialListParams = {
  page: number;
  page_size: number;
  q?: string;
  subject?: SubjectCode | '';
  role?: Role | '';
  status?: MaterialStatus | '';
  grade?: Grade | '';
  sha256?: string;
};

export function listMaterials(params: MaterialListParams, signal?: AbortSignal): Promise<Page<Material>> {
  return apiRequest<Page<Material>>('/materials', { query: params, signal });
}

export function getMaterialStats(signal?: AbortSignal): Promise<MaterialStats> {
  return apiRequest<MaterialStats>('/materials/stats', { signal });
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

export function deleteMaterial(id: string, cascade: boolean): Promise<void> {
  return apiRequest<void>(`/materials/${segment(id)}?cascade=${cascade ? 'true' : 'false'}`, { method: 'DELETE' });
}
