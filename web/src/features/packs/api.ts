import { apiDownload, apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { DocType, Grade, Pack, PackFacets, PackUpdate, Page, Stage, SubjectCode, Term } from '../../shared/api/types';

export type PackSort = 'updated' | 'cohort' | 'title';

export interface PackListParams {
  page: number;
  page_size: number;
  q?: string;
  subject?: SubjectCode | '';
  grade?: Grade | '';
  doc_type?: DocType | '';
  term?: Term | '';
  cohort?: number;
  region?: string;
  organization?: string;
  tag?: string;
  stage?: Stage | '';
  missing?: 'answers' | '';
  collection_id?: string;
  material_id?: string;
  content?: boolean;
  trashed?: boolean;
  sort?: PackSort | '';
}

export function listPacks(params: PackListParams, signal?: AbortSignal): Promise<Page<Pack>> {
  return apiRequest<Page<Pack>>('/packs', { query: { ...params }, signal });
}

export function getPackFacets(signal?: AbortSignal): Promise<PackFacets> {
  return apiRequest<PackFacets>('/packs/facets', { signal });
}

export function getPack(id: string): Promise<Pack> {
  return apiRequest<Pack>(`/packs/${segment(id)}`);
}

export function updatePack(id: string, body: PackUpdate): Promise<Pack> {
  return apiRequest<Pack>(`/packs/${segment(id)}`, { method: 'PATCH', body });
}

export function bulkUpdatePacks(ids: string[], patch: PackUpdate): Promise<{ count: number }> {
  return apiRequest<{ count: number }>('/packs/bulk', { method: 'POST', body: { ids, patch } });
}

export function mergePacks(targetId: string, sourceIds: string[]): Promise<Pack> {
  return apiRequest<Pack>(`/packs/${segment(targetId)}/merge`, { method: 'POST', body: { source_ids: sourceIds } });
}

export function reorderPack(id: string, materialIds: string[]): Promise<Pack> {
  return apiRequest<Pack>(`/packs/${segment(id)}/order`, { method: 'PUT', body: { material_ids: materialIds } });
}

export function trashPacks(ids: string[]): Promise<{ count: number }> {
  return apiRequest<{ count: number }>('/packs/trash', { method: 'POST', body: { ids } });
}

export function restorePacks(ids: string[]): Promise<{ count: number }> {
  return apiRequest<{ count: number }>('/packs/restore', { method: 'POST', body: { ids } });
}

export function purgePacks(ids: string[]): Promise<{ count: number }> {
  return apiRequest<{ count: number }>('/packs/purge', { method: 'POST', body: { ids } });
}

export function downloadPack(id: string) {
  return apiDownload(`/packs/${segment(id)}/download`);
}
