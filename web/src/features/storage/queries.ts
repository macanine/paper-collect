import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createProvider, getPolicy, listProviders, reconcileStorage, refreshReplicaShare, testProvider, updatePolicy, updateProvider } from './api';
import type { ProviderDraft, ProviderPatch } from './providerFields';
import type { StoragePolicy } from '../../shared/api/types';

export const storageKeys = {
  providers: ['storage', 'providers'] as const,
  policy: ['storage', 'policy'] as const,
};

export function useProviders(enabled = true) {
  return useQuery({
    queryKey: storageKeys.providers,
    queryFn: listProviders,
    enabled,
  });
}

export function usePolicy(enabled = true) {
  return useQuery({
    queryKey: storageKeys.policy,
    queryFn: getPolicy,
    enabled,
  });
}

export function useCreateProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (draft: ProviderDraft) => createProvider(draft),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: storageKeys.providers });
    },
  });
}

export function useUpdateProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: ProviderPatch }) => updateProvider(id, patch),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: storageKeys.providers });
    },
  });
}

export function useTestProvider() {
  return useMutation({
    mutationFn: (id: string) => testProvider(id),
  });
}

export function useUpdatePolicy() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (policy: StoragePolicy) => updatePolicy(policy),
    onSuccess: (saved) => {
      queryClient.setQueryData(storageKeys.policy, saved);
    },
  });
}

export function useReconcile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: reconcileStorage,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['jobs'] });
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}

export function useRefreshShare() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (replicaId: string) => refreshReplicaShare(replicaId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['materials'] });
    },
  });
}
