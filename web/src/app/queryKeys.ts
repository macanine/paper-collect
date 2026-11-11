import type { QueryClient } from '@tanstack/react-query';

// 所有 TanStack Query 键的唯一来源。第一段是资源名，按前缀失效即可刷新该资源的全部视图；
// 文件级数据（AI 阅读、题目索引）挂在 materials 下，写入资料后会一并刷新。

export const qk = {
  dashboard: {
    all: ['dashboard'] as const,
    libraryStats: ['dashboard', 'library-stats'] as const,
    recentPacks: ['dashboard', 'recent-packs'] as const,
    recentUploads: ['dashboard', 'recent-uploads'] as const,
  },
  materials: {
    all: ['materials'] as const,
    inbox: (params: object) => ['materials', 'inbox', params] as const,
    detail: (id: string) => ['materials', 'detail', id] as const,
    analysis: (id: string) => ['materials', id, 'analysis'] as const,
    index: (id: string) => ['materials', id, 'index'] as const,
    questions: (id: string, page?: number) => (page === undefined ? (['materials', id, 'questions'] as const) : (['materials', id, 'questions', page] as const)),
  },
  packs: {
    all: ['packs'] as const,
    list: (params: object) => ['packs', 'list', params] as const,
    detail: (id: string) => ['packs', 'detail', id] as const,
    facets: ['packs', 'facets'] as const,
    ai: (id: string) => ['packs', id, 'ai'] as const,
    lookup: (q: string) => ['packs', 'lookup', q] as const,
  },
  collections: {
    all: ['collections'] as const,
    list: (params: object) => ['collections', 'list', params] as const,
    detail: (id: string) => ['collections', 'detail', id] as const,
  },
  uploads: {
    all: ['uploads'] as const,
    list: (page: number, pageSize: number) => ['uploads', 'list', page, pageSize] as const,
    detail: (id: string) => ['uploads', 'detail', id] as const,
  },
  jobs: {
    all: ['jobs'] as const,
    list: (params: object) => ['jobs', 'list', params] as const,
    detail: (id: string) => ['jobs', 'detail', id] as const,
  },
  search: {
    questions: (params: object | null) => ['search', 'questions', params] as const,
  },
  entities: {
    all: ['entities'] as const,
    list: (kind: string, q: string, page: number) => ['entities', kind, q, page] as const,
    mergeCandidates: (kind: string, q: string) => ['entities', kind, 'merge', q] as const,
  },
  ai: {
    providers: ['ai', 'providers'] as const,
    ocrProviders: ['ai', 'ocr-providers'] as const,
    settings: ['ai', 'settings'] as const,
  },
  storage: {
    drives: ['storage', 'drives'] as const,
    rules: ['storage', 'rules'] as const,
    replicas: (status: string, driveId: string, page: number) => ['storage', 'replicas', status, driveId, page] as const,
    allReplicas: ['storage', 'replicas'] as const,
    explain: (materialId: string) => ['storage', 'explain', materialId] as const,
  },
  nodes: {
    all: ['nodes'] as const,
    list: ['nodes', 'list'] as const,
    metrics: (id: string) => ['nodes', id, 'metrics'] as const,
    runningJobs: (id: string) => ['nodes', id, 'running-jobs'] as const,
    clusterSettings: ['nodes', 'cluster-settings'] as const,
  },
  users: {
    all: ['users'] as const,
    list: (page: number) => ['users', page] as const,
  },
  apiKeys: {
    all: ['api-keys'] as const,
    list: (page: number) => ['api-keys', page] as const,
  },
  audit: {
    list: (page: number) => ['audit', page] as const,
  },
};

/** 资料、套卷、专题、导入和仪表盘共享同一批数据，任一资料写操作后一起刷新。 */
export function invalidateLibrary(queryClient: QueryClient) {
  for (const key of [qk.materials.all, qk.packs.all, qk.collections.all, qk.dashboard.all, qk.uploads.all]) {
    void queryClient.invalidateQueries({ queryKey: key });
  }
}
