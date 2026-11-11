import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  Drive,
  DriveTestResult,
  Page,
  ReconcileResult,
  Replica,
  ReplicaBulkResult,
  SyncPreview,
  SyncRule,
} from '../../shared/api/types';
import type { ProviderDraft, ProviderPatch } from './providerFields';

export function listDrives(): Promise<Drive[]> {
  return apiRequest<Drive[]>('/storage/drives');
}

export function createDrive(draft: ProviderDraft): Promise<Drive> {
  return apiRequest<Drive>('/storage/drives', { method: 'POST', body: draft });
}

export function updateDrive(id: string, patch: ProviderPatch): Promise<Drive> {
  return apiRequest<Drive>(`/storage/drives/${segment(id)}`, { method: 'PATCH', body: patch });
}

export function testDrive(id: string): Promise<DriveTestResult> {
  return apiRequest<DriveTestResult>(`/storage/drives/${segment(id)}/test`, { method: 'POST' });
}

export function listRules(): Promise<SyncRule[]> {
  return apiRequest<SyncRule[]>('/storage/rules');
}

export function previewRules(rules: SyncRule[], page: number, pageSize: number): Promise<SyncPreview> {
  return apiRequest<SyncPreview>('/storage/rules/preview', {
    method: 'POST',
    query: { page, page_size: pageSize },
    body: { rules },
  });
}

export function putRules(rules: SyncRule[], previewToken: string): Promise<ReconcileResult> {
  return apiRequest<ReconcileResult>('/storage/rules', { method: 'PUT', body: { rules, preview_token: previewToken } });
}

export function listReplicas(filter: { status?: string; drive_id?: string; page?: number; page_size?: number }): Promise<Page<Replica>> {
  return apiRequest<Page<Replica>>('/storage/replicas', {
    query: { status: filter.status, drive_id: filter.drive_id, page: filter.page, page_size: filter.page_size },
  });
}

export type ReplicaBulkAction = 'retry' | 'delete_remote' | 'forget';

export function bulkReplicas(ids: string[], action: ReplicaBulkAction): Promise<ReplicaBulkResult[]> {
  return apiRequest<ReplicaBulkResult[]>('/storage/replicas/bulk', { method: 'POST', body: { ids, action } });
}

export function reconcileStorage(): Promise<ReconcileResult> {
  return apiRequest<ReconcileResult>('/storage/reconcile', { method: 'POST' });
}

export function syncMaterial(materialId: string): Promise<ReconcileResult> {
  return apiRequest<ReconcileResult>(`/materials/${segment(materialId)}/sync`, { method: 'POST' });
}

export function refreshReplicaShare(replicaId: string): Promise<Replica> {
  return apiRequest<Replica>(`/storage/replicas/${segment(replicaId)}/share`, { method: 'POST' });
}
