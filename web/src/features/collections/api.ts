import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  Collection,
  CollectionDetail,
  CollectionKind,
  Page,
  Role,
  SubjectCode,
} from '../../shared/api/types';

export interface CollectionListParams {
  page: number;
  page_size: number;
  q?: string;
  roots?: boolean;
  parent_id?: string;
  material_id?: string;
}

export interface CollectionCreateBody {
  title: string;
  description: string;
  kind: CollectionKind;
  parent_id: string;
  subject_codes: SubjectCode[];
}

export type CollectionPatchBody = Partial<CollectionCreateBody>;

export interface CollectionItemInput {
  material_id: string;
  position: number;
  role: Role;
  relative_path: string;
}

export function listCollections(params: CollectionListParams, signal?: AbortSignal): Promise<Page<Collection>> {
  return apiRequest<Page<Collection>>('/collections', {
    query: {
      page: params.page,
      page_size: params.page_size,
      q: params.q,
      roots: params.roots,
      parent_id: params.parent_id,
      material_id: params.material_id,
    },
    signal,
  });
}

export function getCollection(id: string): Promise<CollectionDetail> {
  return apiRequest<CollectionDetail>(`/collections/${segment(id)}`);
}

export function createCollection(body: CollectionCreateBody): Promise<Collection> {
  return apiRequest<Collection>('/collections', { method: 'POST', body });
}

export function updateCollection(id: string, body: CollectionPatchBody): Promise<Collection> {
  return apiRequest<Collection>(`/collections/${segment(id)}`, { method: 'PATCH', body });
}

export function replaceCollectionItems(id: string, items: CollectionItemInput[]): Promise<CollectionDetail> {
  return apiRequest<CollectionDetail>(`/collections/${segment(id)}/items`, {
    method: 'PUT',
    body: { items },
  });
}



export function deleteCollection(id: string, cascade: boolean): Promise<void> {
  return apiRequest<void>(`/collections/${segment(id)}?cascade=${cascade ? 'true' : 'false'}`, { method: 'DELETE' });
}
