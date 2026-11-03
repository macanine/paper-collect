import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { Collection, CollectionDetail, Page } from '../../shared/api/types';

export interface CollectionListParams {
  page: number;
  page_size: number;
  q?: string;
  pack_id?: string;
}

export interface CollectionCreateBody {
  title: string;
  description: string;
  pack_ids?: string[];
}

export type CollectionPatchBody = Partial<Pick<CollectionCreateBody, 'title' | 'description'>>;

export function listCollections(params: CollectionListParams, signal?: AbortSignal): Promise<Page<Collection>> {
  return apiRequest<Page<Collection>>('/collections', { query: { ...params }, signal });
}

export function getCollection(id: string): Promise<CollectionDetail> {
  return apiRequest<CollectionDetail>(`/collections/${segment(id)}`);
}

export function createCollection(body: CollectionCreateBody): Promise<CollectionDetail> {
  return apiRequest<CollectionDetail>('/collections', { method: 'POST', body });
}

export function updateCollection(id: string, body: CollectionPatchBody): Promise<CollectionDetail> {
  return apiRequest<CollectionDetail>(`/collections/${segment(id)}`, { method: 'PATCH', body });
}

export function addCollectionPacks(id: string, packIds: string[]): Promise<CollectionDetail> {
  return apiRequest<CollectionDetail>(`/collections/${segment(id)}/items`, { method: 'POST', body: { pack_ids: packIds } });
}

export function replaceCollectionPacks(id: string, packIds: string[]): Promise<CollectionDetail> {
  return apiRequest<CollectionDetail>(`/collections/${segment(id)}/items`, { method: 'PUT', body: { pack_ids: packIds } });
}

export function deleteCollection(id: string): Promise<void> {
  return apiRequest<void>(`/collections/${segment(id)}`, { method: 'DELETE' });
}
