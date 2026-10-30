import { useCallback, useSyncExternalStore } from 'react';

const COMPACT_QUERY = '(max-width: 767px)';

function getSnapshot(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia(COMPACT_QUERY).matches
    : false;
}

/**
 * Table rows become card-like summaries below the tablet breakpoint.
 * The server snapshot stays expanded so tests and non-browser rendering are deterministic.
 */
export function useCompactViewport(): boolean {
  const subscribe = useCallback((onChange: () => void) => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => undefined;
    const media = window.matchMedia(COMPACT_QUERY);
    media.addEventListener('change', onChange);
    return () => media.removeEventListener('change', onChange);
  }, []);

  return useSyncExternalStore(subscribe, getSnapshot, () => false);
}
