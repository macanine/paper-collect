import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { Job } from '../../shared/api/types';
import { getJob, listJobs, retryJob, type JobListParams } from './api';

export const JOB_POLL_MS = 3000;

export const jobKeys = {
  all: ['jobs'] as const,
  list: (params: JobListParams) => ['jobs', 'list', params] as const,
  detail: (id: string) => ['jobs', 'detail', id] as const,
};

export function isTerminal(job: Pick<Job, 'status'> | undefined): boolean {
  return job?.status === 'succeeded' || job?.status === 'failed';
}

export function useJobsList(params: JobListParams, enabled = true) {
  return useQuery({
    queryKey: jobKeys.list(params),
    queryFn: ({ signal }) => listJobs(params, signal),
    enabled,
    placeholderData: keepPreviousData,
    // 列表中存在排队或执行中的任务时定时刷新，结束后自动停止。
    refetchInterval: (query) => {
      const items = query.state.data?.items ?? [];
      return items.some((job) => !isTerminal(job)) ? JOB_POLL_MS : false;
    },
  });
}

export function useJobDetail(id: string) {
  return useQuery({
    queryKey: jobKeys.detail(id),
    queryFn: () => getJob(id),
    enabled: id !== '',
    refetchInterval: (query) => (isTerminal(query.state.data) ? false : JOB_POLL_MS),
  });
}

export function useRetryJob() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => retryJob(id),
    onSuccess: (job) => {
      queryClient.setQueryData(jobKeys.detail(job.id), job);
      void queryClient.invalidateQueries({ queryKey: jobKeys.all });
      void queryClient.invalidateQueries({ queryKey: ['uploads'] });
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] });
    },
  });
}
