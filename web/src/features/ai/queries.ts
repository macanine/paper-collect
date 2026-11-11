import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createAIProvider,
  createOCRProvider,
  getAISettings,
  getMaterialCard,
  getPackAI,
  listAIProviders,
  listOCRProviders,
  probeAIProviders,
  probeSavedProvider,
  putAISettings,
  redecidePack,
  rereadMaterial,
  testAIProvider,
  testOCRProvider,
  updateOCRProvider,
  updateAIProvider,
} from './api';
import type { AIProviderDraft, AIProviderPatch, OCRProviderDraft, OCRProviderPatch } from './api';
import { ApiError } from '../../shared/api/errors';
import { qk } from '../../app/queryKeys';

const aiKeys = { ...qk.ai, analysis: qk.materials.analysis, packAI: qk.packs.ai };

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

export function useOCRProviders(enabled = true) {
  return useQuery({ queryKey: aiKeys.ocrProviders, queryFn: listOCRProviders, enabled });
}

export function useCreateOCRProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (draft: OCRProviderDraft) => createOCRProvider(draft),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: aiKeys.ocrProviders }),
  });
}

export function useUpdateOCRProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: OCRProviderPatch }) => updateOCRProvider(id, patch),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: aiKeys.ocrProviders }),
  });
}

export function useTestOCRProvider() {
  return useMutation({ mutationFn: (id: string) => testOCRProvider(id) });
}

/** AI 对单个文件的阅读记录；处理中时轮询。 */
export function useMaterialCard(materialId: string, enabled = true) {
  return useQuery({
    queryKey: aiKeys.analysis(materialId),
    queryFn: () => getMaterialCard(materialId),
    enabled: enabled && materialId !== '',
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 2,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 3000 : false;
    },
  });
}

/** 套卷最近一次 AI 编目决定；处理中时轮询。 */
export function usePackAI(packId: string, enabled = true) {
  return useQuery({
    queryKey: aiKeys.packAI(packId),
    queryFn: () => getPackAI(packId),
    enabled: enabled && packId !== '',
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 3000 : false;
    },
  });
}

export function useRereadMaterial(materialId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => rereadMaterial(materialId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.materials.all });
      void queryClient.invalidateQueries({ queryKey: aiKeys.analysis(materialId) });
    },
  });
}

export function useRedecidePack(packId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => redecidePack(packId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.packs.all });
      void queryClient.invalidateQueries({ queryKey: qk.materials.all });
      void queryClient.invalidateQueries({ queryKey: aiKeys.packAI(packId) });
    },
  });
}
