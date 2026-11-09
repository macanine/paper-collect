import { useQuery } from '@tanstack/react-query';
import { getLibraryStats } from '../materials/api';
import { listPacks } from '../packs/api';
import { listUploads } from '../uploads/api';

const REFRESH_MS = 30_000;

export function useLibraryStats(enabled: boolean) {
  return useQuery({
    queryKey: ['dashboard', 'library-stats'],
    queryFn: ({ signal }) => getLibraryStats(signal),
    enabled,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}

export function useRecentPacks(enabled: boolean) {
  return useQuery({
    queryKey: ['dashboard', 'recent-packs'],
    queryFn: ({ signal }) => listPacks({ page: 1, page_size: 8 }, signal),
    enabled,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}

export function useRecentUploads(enabled: boolean) {
  return useQuery({
    queryKey: ['dashboard', 'recent-uploads'],
    queryFn: ({ signal }) => listUploads({ page: 1, page_size: 6 }, signal),
    enabled,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}
