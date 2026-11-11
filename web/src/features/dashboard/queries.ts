import { useQuery } from '@tanstack/react-query';
import { getLibraryStats } from '../materials/api';
import { listPacks } from '../packs/api';
import { listUploads } from '../uploads/api';
import { qk } from '../../app/queryKeys';

const REFRESH_MS = 30_000;

export function useLibraryStats(enabled: boolean) {
  return useQuery({
    queryKey: qk.dashboard.libraryStats,
    queryFn: ({ signal }) => getLibraryStats(signal),
    enabled,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}

export function useRecentPacks(enabled: boolean) {
  return useQuery({
    queryKey: qk.dashboard.recentPacks,
    queryFn: ({ signal }) => listPacks({ page: 1, page_size: 8 }, signal),
    enabled,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}

export function useRecentUploads(enabled: boolean) {
  return useQuery({
    queryKey: qk.dashboard.recentUploads,
    queryFn: ({ signal }) => listUploads({ page: 1, page_size: 6 }, signal),
    enabled,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}
