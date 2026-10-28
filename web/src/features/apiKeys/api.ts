import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { APIKey, CreatedAPIKey, Page, Scope } from '../../shared/api/types';

export interface APIKeyListParams {
  page: number;
  page_size: number;
}

export interface APIKeyCreateBody {
  name: string;
  scopes: Scope[];
  expires_at?: string;
}

export function listKeys(params: APIKeyListParams, signal?: AbortSignal): Promise<Page<APIKey>> {
  return apiRequest<Page<APIKey>>('/api-keys', { query: { page: params.page, page_size: params.page_size }, signal });
}

export function createKey(body: APIKeyCreateBody): Promise<CreatedAPIKey> {
  return apiRequest<CreatedAPIKey>('/api-keys', { method: 'POST', body });
}

export function revokeKey(id: string): Promise<void> {
  return apiRequest<void>(`/api-keys/${segment(id)}`, { method: 'DELETE' });
}
