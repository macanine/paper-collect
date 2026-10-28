import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { Job, Upload } from '../../shared/api/types';
import { finalizeUpload, getUpload } from './api';

export const POLL_INTERVAL_MS = 2000;

export const uploadKeys = {
  detail: (id: string) => ['uploads', 'detail', id] as const,
  job: (id: string) => ['jobs', 'detail', id] as const,
};

export function isTerminalJob(job: Job | undefined): boolean {
  return job?.status === 'succeeded' || job?.status === 'failed';
}

export function useUpload(id: string) {
  return useQuery({
    queryKey: uploadKeys.detail(id),
    queryFn: ({ signal }) => getUpload(id, signal),
    enabled: id !== '',
    refetchInterval: (query) => {
      const upload = query.state.data;
      if (!upload || !upload.job_id) return false;
      return upload.status === 'completed' || upload.status === 'failed' ? false : POLL_INTERVAL_MS;
    },
  });
}

export function useJob(jobId: string) {
  return useQuery({
    queryKey: uploadKeys.job(jobId),
    queryFn: () => apiRequest<Job>(`/jobs/${segment(jobId)}`),
    enabled: jobId !== '',
    refetchInterval: (query) => (isTerminalJob(query.state.data) ? false : POLL_INTERVAL_MS),
  });
}

export function useFinalizeUpload(uploadId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => finalizeUpload(uploadId),
    onSuccess: (result) => {
      queryClient.setQueryData<Upload>(uploadKeys.detail(uploadId), (current) =>
        current ? { ...current, job_id: result.job_id, status: 'processing' } : current,
      );
      void queryClient.invalidateQueries({ queryKey: uploadKeys.detail(uploadId) });
    },
  });
}
