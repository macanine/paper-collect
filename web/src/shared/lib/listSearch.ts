import { useCallback } from 'react';
import { useSearchParams } from 'react-router';

// 列表状态同步到 URL（page、page_size 与过滤条件），刷新与前进后退都能保持视图。
// 参数名与 API 查询参数保持一致；过滤条件变化时回到第一页。

export const DEFAULT_PAGE_SIZE = 20;
export const MAX_PAGE_SIZE = 100;

export function parseIntParam(raw: string | null, fallback: number, min: number, max: number): number {
  if (raw === null || raw.trim() === '') return fallback;
  const value = Number(raw);
  if (!Number.isInteger(value)) return fallback;
  return Math.min(max, Math.max(min, value));
}

export interface ListSearchState<K extends string> {
  page: number;
  pageSize: number;
  filters: Record<K, string>;
  setPage: (page: number, pageSize: number) => void;
  setFilters: (patch: Partial<Record<K, string>>) => void;
}

export function useListSearch<K extends string>(filterKeys: readonly K[], defaultPageSize = DEFAULT_PAGE_SIZE): ListSearchState<K> {
  const [searchParams, setSearchParams] = useSearchParams();

  const page = parseIntParam(searchParams.get('page'), 1, 1, 1_000_000);
  const pageSize = parseIntParam(searchParams.get('page_size'), defaultPageSize, 1, MAX_PAGE_SIZE);

  const filters = {} as Record<K, string>;
  for (const key of filterKeys) {
    filters[key] = searchParams.get(key) ?? '';
  }

  const setPage = useCallback(
    (nextPage: number, nextPageSize: number) => {
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current);
          next.set('page', String(nextPage));
          next.set('page_size', String(nextPageSize));
          return next;
        },
        { replace: false },
      );
    },
    [setSearchParams],
  );

  const setFilters = useCallback(
    (patch: Partial<Record<K, string>>) => {
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current);
          for (const [key, value] of Object.entries(patch) as [K, string | undefined][]) {
            if (value === undefined || value === '') next.delete(key);
            else next.set(key, value);
          }
          next.set('page', '1');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return { page, pageSize, filters, setPage, setFilters };
}
