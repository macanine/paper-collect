import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { Job, JobKind, JobStats, JobStatus, Page } from '../../shared/api/types';

export type JobListParams = {
  page: number;
  page_size: number;
  status?: JobStatus | '';
  kind?: JobKind | '';
  material_id?: string;
  upload_id?: string;
  replica_id?: string;
};

export function listJobs(params: JobListParams, signal?: AbortSignal): Promise<Page<Job>> {
  return apiRequest<Page<Job>>('/jobs', { query: params, signal });
}

export function getJobStats(signal?: AbortSignal): Promise<JobStats> {
  return apiRequest<JobStats>('/jobs/stats', { signal });
}

export function getJob(id: string): Promise<Job> {
  return apiRequest<Job>(`/jobs/${segment(id)}`);
}

export function retryJob(id: string): Promise<Job> {
  return apiRequest<Job>(`/jobs/${segment(id)}/retry`, { method: 'POST' });
}
