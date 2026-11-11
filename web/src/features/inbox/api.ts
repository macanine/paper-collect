import { apiRequest } from '../../shared/api/client';
import type { InboxPack, Page, Stage } from '../../shared/api/types';

export interface InboxParams {
  page: number;
  page_size: number;
  stage?: Stage;
  reason?: string;
}

export function listInbox(params: InboxParams, signal?: AbortSignal): Promise<Page<InboxPack>> {
  return apiRequest<Page<InboxPack>>('/inbox', { query: { ...params }, signal });
}

/** 人工确认需关注的文件：按当前编目入库并开始同步。 */
export function acceptMaterials(ids: string[]): Promise<{ count: number }> {
  return apiRequest<{ count: number }>('/materials/accept', { method: 'POST', body: { ids } });
}
