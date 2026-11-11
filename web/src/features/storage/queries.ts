import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  bulkReplicas,
  createDrive,
  listDrives,
  listReplicas,
  listRules,
  previewRules,
  putRules,
  reconcileStorage,
  refreshReplicaShare,
  syncMaterial,
  testDrive,
  updateDrive,
  type ReplicaBulkAction,
} from './api';
import type { ProviderDraft, ProviderPatch } from './providerFields';
import type { SyncRule } from '../../shared/api/types';
import { qk } from '../../app/queryKeys';

export const storageKeys = qk.storage;

export function useDrives(enabled = true) {
  return useQuery({
    queryKey: storageKeys.drives,
    queryFn: listDrives,
    enabled,
  });
}

export function useRules(enabled = true) {
  return useQuery({
    queryKey: storageKeys.rules,
    queryFn: listRules,
    enabled,
  });
}

export function useReplicas(status: string, driveId: string, page: number, pageSize: number, enabled = true) {
  return useQuery({
    queryKey: storageKeys.replicas(status, driveId, page),
    queryFn: () => listReplicas({ status: status || undefined, drive_id: driveId || undefined, page, page_size: pageSize }),
    enabled,
  });
}

export function useCreateDrive() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (draft: ProviderDraft) => createDrive(draft),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: storageKeys.drives });
    },
  });
}

export function useUpdateDrive() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: ProviderPatch }) => updateDrive(id, patch),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: storageKeys.drives });
    },
  });
}

export function useTestDrive() {
  return useMutation({
    mutationFn: (id: string) => testDrive(id),
  });
}

export function usePreviewRules() {
  return useMutation({
    mutationFn: ({ rules, page, pageSize }: { rules: SyncRule[]; page: number; pageSize: number }) => previewRules(rules, page, pageSize),
  });
}

export function usePutRules() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ rules, previewToken }: { rules: SyncRule[]; previewToken: string }) => putRules(rules, previewToken),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: storageKeys.rules });
      void queryClient.invalidateQueries({ queryKey: qk.storage.allReplicas });
      void queryClient.invalidateQueries({ queryKey: qk.materials.all });
      void queryClient.invalidateQueries({ queryKey: qk.dashboard.all });
      void queryClient.invalidateQueries({ queryKey: qk.jobs.all });
    },
  });
}

export function useReconcile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: reconcileStorage,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.jobs.all });
      void queryClient.invalidateQueries({ queryKey: qk.dashboard.all });
    },
  });
}

export function useBulkReplicas() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ ids, action }: { ids: string[]; action: ReplicaBulkAction }) => bulkReplicas(ids, action),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.storage.allReplicas });
      void queryClient.invalidateQueries({ queryKey: storageKeys.drives });
      void queryClient.invalidateQueries({ queryKey: qk.materials.all });
      void queryClient.invalidateQueries({ queryKey: qk.dashboard.all });
      void queryClient.invalidateQueries({ queryKey: qk.jobs.all });
    },
  });
}

export function useSyncMaterial() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (materialId: string) => syncMaterial(materialId),
    onSuccess: (_result, materialId) => {
      void queryClient.invalidateQueries({ queryKey: storageKeys.explain(materialId) });
      void queryClient.invalidateQueries({ queryKey: qk.materials.all });
      void queryClient.invalidateQueries({ queryKey: qk.jobs.all });
      void queryClient.invalidateQueries({ queryKey: qk.dashboard.all });
    },
  });
}

export function useRefreshShare() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (replicaId: string) => refreshReplicaShare(replicaId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.materials.all });
    },
  });
}
