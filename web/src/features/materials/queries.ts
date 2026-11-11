import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { MaterialUpdate, Stage } from '../../shared/api/types';
import { acceptMaterials, getMaterial, listInbox, updateMaterial } from './api';
import { invalidateLibrary, qk } from '../../app/queryKeys';

export const materialKeys = qk.materials;
export { invalidateLibrary };

export function useInbox(params: { page: number; page_size: number; stage?: Stage; reason?: string }, options: { poll?: boolean | number } = {}) {
  const refetchInterval = options.poll === true ? 5000 : typeof options.poll === 'number' ? options.poll : false;
  return useQuery({
    queryKey: qk.materials.inbox(params),
    queryFn: ({ signal }) => listInbox(params, signal),
    placeholderData: keepPreviousData,
    refetchInterval,
    refetchIntervalInBackground: false,
  });
}

export function useMaterial(id: string) {
  return useQuery({
    queryKey: materialKeys.detail(id),
    queryFn: () => getMaterial(id),
    enabled: id !== '',
    refetchInterval: (query) => (query.state.data?.stage === 'processing' ? 3000 : false),
    refetchIntervalInBackground: false,
  });
}

export function useUpdateMaterial(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: MaterialUpdate) => updateMaterial(id, body),
    onSuccess: () => invalidateLibrary(queryClient),
  });
}

export function useAcceptMaterials() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ ids }: { ids: string[] }) => acceptMaterials(ids),
    onSuccess: () => invalidateLibrary(queryClient),
  });
}
