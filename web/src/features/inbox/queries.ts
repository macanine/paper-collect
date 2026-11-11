import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { invalidateLibrary, qk } from '../../app/queryKeys';
import { acceptMaterials, listInbox, type InboxParams } from './api';

/** 需关注列表；poll 为刷新间隔（毫秒），false 不轮询。 */
export function useInbox(params: InboxParams, options: { poll?: number | false } = {}) {
  return useQuery({
    queryKey: qk.materials.inbox(params),
    queryFn: ({ signal }) => listInbox(params, signal),
    placeholderData: keepPreviousData,
    refetchInterval: options.poll ?? false,
    refetchIntervalInBackground: false,
  });
}

export function useAcceptMaterials() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ ids }: { ids: string[] }) => acceptMaterials(ids),
    onSuccess: () => invalidateLibrary(queryClient),
  });
}
