import { useQuery } from '@tanstack/react-query';
import type { JobKind, JobStatus, MaterialStatus } from '../../shared/api/types';
import { listJobs } from '../jobs/api';
import { listMaterials } from '../materials/api';

const REFRESH_MS = 30_000;

export const dashboardKeys = {
  all: ['dashboard'] as const,
  materialStatus: (status: MaterialStatus) => ['dashboard', 'materials', 'status', status] as const,
  recentMaterials: ['dashboard', 'materials', 'recent'] as const,
  jobStatus: (status: JobStatus) => ['dashboard', 'jobs', 'status', status] as const,
  recentJobs: (kind: JobKind | '') => ['dashboard', 'jobs', 'recent', kind] as const,
};

export function useMaterialStatusCount(status: MaterialStatus, enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.materialStatus(status),
    queryFn: () => listMaterials({ page: 1, page_size: 1, status }),
    enabled,
    staleTime: REFRESH_MS,
  });
}

export function useRecentMaterials(enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.recentMaterials,
    queryFn: () => listMaterials({ page: 1, page_size: 6 }),
    enabled,
    staleTime: REFRESH_MS,
  });
}

export function useJobStatusCount(status: JobStatus, enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.jobStatus(status),
    queryFn: () => listJobs({ page: 1, page_size: 1, status }),
    enabled,
    staleTime: REFRESH_MS,
  });
}

export function useRecentJobs(enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.recentJobs(''),
    queryFn: () => listJobs({ page: 1, page_size: 6 }),
    enabled,
    staleTime: REFRESH_MS,
  });
}
