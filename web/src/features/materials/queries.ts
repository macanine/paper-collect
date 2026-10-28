import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { MaterialUpdate } from '../../shared/api/types';
import { getMaterial, listMaterials, updateMaterial, type MaterialListParams } from './api';

export const materialKeys = {
  all: ['materials'] as const,
  list: (params: MaterialListParams) => ['materials', 'list', params] as const,
  detail: (id: string) => ['materials', 'detail', id] as const,
};

export function useMaterialsList(params: MaterialListParams) {
  return useQuery({
    queryKey: materialKeys.list(params),
    queryFn: ({ signal }) => listMaterials(params, signal),
    placeholderData: keepPreviousData,
  });
}

export function useMaterial(id: string) {
  return useQuery({
    queryKey: materialKeys.detail(id),
    queryFn: () => getMaterial(id),
    enabled: id !== '',
  });
}

export function useUpdateMaterial(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: MaterialUpdate) => updateMaterial(id, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: materialKeys.all });
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}
