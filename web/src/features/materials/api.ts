import { apiDownload, apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  LibraryStats,
  MaterialDetail,
  MaterialUpdate,
  Page,
  InboxPack,
  Stage,
} from '../../shared/api/types';

export function listInbox(params: { page: number; page_size: number; stage?: Stage; reason?: string }, signal?: AbortSignal): Promise<Page<InboxPack>> {
  return apiRequest<Page<InboxPack>>('/inbox', { query: params, signal });
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

/** 人工确认需关注的文件：按当前编目入库并开始同步。 */
export function acceptMaterials(ids: string[]): Promise<{ count: number }> {
  return apiRequest<{ count: number }>('/materials/accept', { method: 'POST', body: { ids } });
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
