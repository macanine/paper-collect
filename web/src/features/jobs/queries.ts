import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { Job } from '../../shared/api/types';
import { cancelJob, getJob, listJobs, retryJob, type JobListParams } from './api';
import { invalidateLibrary, qk } from '../../app/queryKeys';

const JOB_POLL_MS = 3000;

const jobKeys = qk.jobs;

function isTerminal(job: Pick<Job, 'status'> | undefined): boolean {
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

/** 取消会让资料、套卷、副本或导入进入失败后的状态，因此一并刷新资料视图。 */
export function useCancelJob() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => cancelJob(id),
    onSuccess: (job) => {
      queryClient.setQueryData(jobKeys.detail(job.id), job);
      void queryClient.invalidateQueries({ queryKey: jobKeys.all });
      invalidateLibrary(queryClient);
    },
  });
}

export function useRetryJob() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => retryJob(id),
    onSuccess: (job) => {
      queryClient.setQueryData(jobKeys.detail(job.id), job);
      void queryClient.invalidateQueries({ queryKey: jobKeys.all });
      void queryClient.invalidateQueries({ queryKey: qk.uploads.all });
      void queryClient.invalidateQueries({ queryKey: qk.dashboard.all });
    },
  });
}
