import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { ProviderTestResult, Replica, ReconcileResult, StoragePolicy, StorageProvider } from '../../shared/api/types';
import type { ProviderDraft, ProviderPatch } from './providerFields';

export function listProviders(): Promise<StorageProvider[]> {
  return apiRequest<StorageProvider[]>('/storage/providers');
}

export function createProvider(draft: ProviderDraft): Promise<StorageProvider> {
  return apiRequest<StorageProvider>('/storage/providers', { method: 'POST', body: draft });
}

export function updateProvider(id: string, patch: ProviderPatch): Promise<StorageProvider> {
  return apiRequest<StorageProvider>(`/storage/providers/${segment(id)}`, { method: 'PATCH', body: patch });
}

export function testProvider(id: string): Promise<ProviderTestResult> {
  return apiRequest<ProviderTestResult>(`/storage/providers/${segment(id)}/test`, { method: 'POST' });
}

export function getPolicy(): Promise<StoragePolicy> {
  return apiRequest<StoragePolicy>('/storage/policy');
}

export function updatePolicy(policy: StoragePolicy): Promise<StoragePolicy> {
  return apiRequest<StoragePolicy>('/storage/policy', { method: 'PUT', body: policy });
}

export function reconcileStorage(): Promise<ReconcileResult> {
  return apiRequest<ReconcileResult>('/storage/reconcile', { method: 'POST' });
}

export function refreshReplicaShare(replicaId: string): Promise<Replica> {
  return apiRequest<Replica>(`/storage/replicas/${segment(replicaId)}/share`, { method: 'POST' });
}
