import { apiRequest } from '../../shared/api/client';
import type { Audit, Page } from '../../shared/api/types';

export interface AuditListParams {
  page: number;
  page_size: number;
}

export function listAuditLogs(params: AuditListParams, signal?: AbortSignal): Promise<Page<Audit>> {
  return apiRequest<Page<Audit>>('/audit-logs', { query: { page: params.page, page_size: params.page_size }, signal });
}
