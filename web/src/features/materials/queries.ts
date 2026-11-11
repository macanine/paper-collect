import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { MaterialUpdate, Stage } from '../../shared/api/types';
import { acceptMaterials, getMaterial, listInbox, updateMaterial } from './api';

export const materialKeys = {
  all: ['materials'] as const,
  detail: (id: string) => ['materials', 'detail', id] as const,
};

/** 资料、套卷、需关注和仪表盘共享同一批数据，任一写操作后一起刷新。 */
export function invalidateLibrary(queryClient: QueryClient) {
  for (const key of ['materials', 'packs', 'collections', 'dashboard', 'uploads']) {
    void queryClient.invalidateQueries({ queryKey: [key] });
  }
}

export function useReviewPacks(params: { page: number; page_size: number; stage?: Stage; reason?: string }, options: { poll?: boolean | number } = {}) {
  const refetchInterval = options.poll === true ? 5000 : typeof options.poll === 'number' ? options.poll : false;
  return useQuery({
    queryKey: ['materials', 'inbox', params],
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
