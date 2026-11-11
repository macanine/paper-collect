import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { PackUpdate } from '../../shared/api/types';
import { invalidateLibrary } from '../materials/queries';
import {
  bulkUpdatePacks,
  getPack,
  getPackFacets,
  listPacks,
  mergePacks,
  purgePacks,
  reorderPack,
  restorePacks,
  trashPacks,
  updatePack,
  type PackListParams,
} from './api';
import { qk } from '../../app/queryKeys';

export const packKeys = qk.packs;

export function usePacksList(params: PackListParams, enabled = true) {
  return useQuery({
    queryKey: packKeys.list(params),
    queryFn: ({ signal }) => listPacks(params, signal),
    placeholderData: keepPreviousData,
    enabled,
  });
}

export function usePack(id: string) {
  return useQuery({
    queryKey: packKeys.detail(id),
    queryFn: () => getPack(id),
    enabled: id !== '',
    refetchInterval: (query) => (query.state.data?.stage === 'processing' ? 3000 : false),
    refetchIntervalInBackground: false,
  });
}

export function usePackFacets() {
  return useQuery({ queryKey: packKeys.facets, queryFn: ({ signal }) => getPackFacets(signal), staleTime: 30_000 });
}

export function useUpdatePack(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: PackUpdate) => updatePack(id, body),
    onSuccess: (pack) => {
      queryClient.setQueryData(packKeys.detail(id), pack);
      invalidateLibrary(queryClient);
    },
  });
}

export function usePackMutations() {
  const queryClient = useQueryClient();
  const done = () => invalidateLibrary(queryClient);
  return {
    bulkUpdate: useMutation({ mutationFn: ({ ids, patch }: { ids: string[]; patch: PackUpdate }) => bulkUpdatePacks(ids, patch), onSuccess: done }),
    merge: useMutation({ mutationFn: ({ target, sources }: { target: string; sources: string[] }) => mergePacks(target, sources), onSuccess: done }),
    reorder: useMutation({ mutationFn: ({ id, order }: { id: string; order: string[] }) => reorderPack(id, order), onSuccess: done }),
    trash: useMutation({ mutationFn: (ids: string[]) => trashPacks(ids), onSuccess: done }),
    restore: useMutation({ mutationFn: (ids: string[]) => restorePacks(ids), onSuccess: done }),
    purge: useMutation({ mutationFn: (ids: string[]) => purgePacks(ids), onSuccess: done }),
  };
}
