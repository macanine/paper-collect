import { QueryClient } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { invalidateLibrary, qk } from './queryKeys';

describe('query keys', () => {
  it('refreshes file-level data together with the library', async () => {
    const client = new QueryClient();
    const keys = [qk.materials.index('m1'), qk.materials.questions('m1', 2), qk.materials.analysis('m1'), qk.packs.ai('p1'), qk.dashboard.libraryStats];
    for (const key of keys) client.setQueryData(key, 1);
    invalidateLibrary(client);
    for (const key of keys) expect(client.getQueryState(key)?.isInvalidated, JSON.stringify(key)).toBe(true);
    expect(client.getQueryState(qk.materials.index('m1'))?.isInvalidated).toBe(true);
  });

  it('nests questions pages under the prefix used to invalidate them', () => {
    expect(qk.materials.questions('m1', 3).slice(0, 3)).toEqual([...qk.materials.questions('m1')]);
  });
});
