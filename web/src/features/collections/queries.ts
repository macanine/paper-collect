import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import {
  createCollection,
  getCollection,
  listCollections,
  replaceCollectionItems,
  updateCollection,
  type CollectionCreateBody,
  type CollectionItemInput,
  type CollectionListParams,
  type CollectionPatchBody,
} from './api';

export const collectionKeys = {
  all: ['collections'] as const,
  list: (params: CollectionListParams) => ['collections', 'list', params] as const,
  detail: (id: string) => ['collections', 'detail', id] as const,
};

export function useCollectionsList(params: CollectionListParams, enabled = true) {
  return useQuery({
    queryKey: collectionKeys.list(params),
    queryFn: ({ signal }) => listCollections(params, signal),
    placeholderData: keepPreviousData,
    enabled,
  });
}

export function useCollection(id: string) {
  return useQuery({
    queryKey: collectionKeys.detail(id),
    queryFn: () => getCollection(id),
    enabled: id !== '',
  });
}

function invalidateCollections(queryClient: QueryClient, id?: string) {
  void queryClient.invalidateQueries({ queryKey: collectionKeys.all });
  if (id) void queryClient.invalidateQueries({ queryKey: collectionKeys.detail(id) });
  void queryClient.invalidateQueries({ queryKey: ['dashboard'] });
}

export function useCreateCollection() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CollectionCreateBody) => createCollection(body),
    onSuccess: () => {
      invalidateCollections(queryClient);
    },
  });
}

export function useUpdateCollection(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CollectionPatchBody) => updateCollection(id, body),
    onSuccess: () => {
      invalidateCollections(queryClient, id);
    },
  });
}

export function useReplaceCollectionItems(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (items: CollectionItemInput[]) => replaceCollectionItems(id, items),
    onSuccess: (detail) => {
      queryClient.setQueryData(collectionKeys.detail(id), detail);
      void queryClient.invalidateQueries({ queryKey: collectionKeys.all });
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}
