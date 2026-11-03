import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { invalidateLibrary } from '../materials/queries';
import {
  addCollectionPacks,
  createCollection,
  deleteCollection,
  getCollection,
  listCollections,
  replaceCollectionPacks,
  updateCollection,
  type CollectionCreateBody,
  type CollectionListParams,
  type CollectionPatchBody,
} from './api';

const collectionKeys = {
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
  return useQuery({ queryKey: collectionKeys.detail(id), queryFn: () => getCollection(id), enabled: id !== '' });
}

export function useCollectionMutations() {
  const queryClient = useQueryClient();
  const done = () => invalidateLibrary(queryClient);
  return {
    create: useMutation({ mutationFn: (body: CollectionCreateBody) => createCollection(body), onSuccess: done }),
    update: useMutation({ mutationFn: ({ id, body }: { id: string; body: CollectionPatchBody }) => updateCollection(id, body), onSuccess: done }),
    addPacks: useMutation({ mutationFn: ({ id, packIds }: { id: string; packIds: string[] }) => addCollectionPacks(id, packIds), onSuccess: done }),
    replacePacks: useMutation({ mutationFn: ({ id, packIds }: { id: string; packIds: string[] }) => replaceCollectionPacks(id, packIds), onSuccess: done }),
    remove: useMutation({ mutationFn: (id: string) => deleteCollection(id), onSuccess: done }),
  };
}
