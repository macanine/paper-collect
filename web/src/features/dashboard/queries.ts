import { useQuery } from '@tanstack/react-query';
import { getJobStats, listJobs } from '../jobs/api';
import { getMaterialStats, listMaterials } from '../materials/api';

const REFRESH_MS = 30_000;

export const dashboardKeys = {
  all: ['dashboard'] as const,
  materialStats: ['dashboard', 'materials', 'stats'] as const,
  recentMaterials: ['dashboard', 'materials', 'recent'] as const,
  jobStats: ['dashboard', 'jobs', 'stats'] as const,
  recentJobs: ['dashboard', 'jobs', 'recent'] as const,
};

export function useMaterialStats(enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.materialStats,
    queryFn: ({ signal }) => getMaterialStats(signal),
    enabled,
    staleTime: REFRESH_MS,
    refetchInterval: REFRESH_MS,
  });
}

export function useRecentMaterials(enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.recentMaterials,
    queryFn: ({ signal }) => listMaterials({ page: 1, page_size: 6 }, signal),
    enabled,
    staleTime: REFRESH_MS,
    refetchInterval: REFRESH_MS,
  });
}

export function useJobStats(enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.jobStats,
    queryFn: ({ signal }) => getJobStats(signal),
    enabled,
    staleTime: REFRESH_MS,
    refetchInterval: REFRESH_MS,
  });
}

export function useRecentJobs(enabled: boolean) {
  return useQuery({
    queryKey: dashboardKeys.recentJobs,
    queryFn: ({ signal }) => listJobs({ page: 1, page_size: 6 }, signal),
    enabled,
    staleTime: REFRESH_MS,
    refetchInterval: REFRESH_MS,
  });
}
