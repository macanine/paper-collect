import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { MaterialUpdate } from '../../shared/api/types';
import { getMaterial, updateMaterial } from './api';
import { invalidateLibrary, qk } from '../../app/queryKeys';

export const materialKeys = qk.materials;
export { invalidateLibrary };

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
