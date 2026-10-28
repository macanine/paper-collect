import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  applyMaterialAnalysis,
  createAIProvider,
  getAISettings,
  getMaterialAnalysis,
  enqueueMaterialAnalysis,
  listAIProviders,
  probeAIProviders,
  probeSavedProvider,
  putAISettings,
  testAIProvider,
  updateAIProvider,
} from './api';
import type { AIProviderDraft, AIProviderPatch } from './api';
import { ApiError } from '../../shared/api/errors';

export const aiKeys = {
  providers: ['ai', 'providers'] as const,
  settings: ['ai', 'settings'] as const,
  analysis: (materialId: string) => ['materials', materialId, 'analysis'] as const,
};

export function useAIProviders(enabled = true) {
  return useQuery({ queryKey: aiKeys.providers, queryFn: listAIProviders, enabled });
}

export function useAISettings(enabled = true) {
  return useQuery({ queryKey: aiKeys.settings, queryFn: getAISettings, enabled });
}

export function useCreateAIProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (draft: AIProviderDraft) => createAIProvider(draft),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: aiKeys.providers }),
  });
}

export function useUpdateAIProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: AIProviderPatch }) => updateAIProvider(id, patch),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: aiKeys.providers }),
  });
}

export function useProbeModels() {
  return useMutation({
    mutationFn: (input: { base_url?: string; api_key?: string; provider_id?: string }) => probeAIProviders(input),
  });
}

export function useProbeSavedModels() {
  return useMutation({ mutationFn: (id: string) => probeSavedProvider(id) });
}

export function useTestAIProvider() {
  return useMutation({ mutationFn: ({ id, model }: { id: string; model: string }) => testAIProvider(id, model) });
}

export function useUpdateAISettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: putAISettings,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: aiKeys.settings }),
  });
}

export function useMaterialAnalysis(materialId: string, enabled = true) {
  return useQuery({
    queryKey: aiKeys.analysis(materialId),
    queryFn: () => getMaterialAnalysis(materialId),
    enabled: enabled && materialId !== '',
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 2,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 3000 : false;
    },
  });
}

export function useEnqueueAnalysis(materialId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (force: boolean) => enqueueMaterialAnalysis(materialId, force),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: aiKeys.analysis(materialId) }),
  });
}

export function useApplyAnalysis(materialId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => applyMaterialAnalysis(materialId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['materials'] });
      void queryClient.invalidateQueries({ queryKey: aiKeys.analysis(materialId) });
    },
  });
}
