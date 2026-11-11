import { lazy, type ComponentType, type LazyExoticComponent, type ReactNode } from 'react';
import { matchPath } from 'react-router';
import {
  IconAdjustments,
  IconApps,
  IconChecklist,
  IconCloudUpload,
  IconDashboard,
  IconFolders,
  IconInbox,
  IconKey,
  IconListDetails,
  IconRobot,
  IconSearch,
  IconServer,
  IconSettings,
  IconSitemap,
  IconTags,
  IconTrash,
  IconUsers,
} from '@tabler/icons-react';
import type { Scope } from '../shared/api/types';

// 路由清单是路由、导航、权限、标题与链接的唯一来源。App 据此注册路由，
// 布局据此生成侧栏、面包屑与命令面板；服务端仍会逐请求重新检查 scope。

export type LazyPage = LazyExoticComponent<ComponentType> & { preload: () => Promise<unknown> };

function page<K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K): LazyPage {
  let pending: Promise<Record<K, ComponentType>> | null = null;
  const loadOnce = () => (pending ??= load());
  return Object.assign(
    lazy(() => loadOnce().then((module) => ({ default: module[name] }))),
    { preload: loadOnce },
  );
}

/** 类型化的站内链接。路径只在这里出现一次。 */
export const href = {
  home: () => '/',
  imports: () => '/imports',
  upload: (id: string) => `/imports/${encodeURIComponent(id)}`,
  inbox: () => '/inbox',
  library: () => '/library',
  pack: (id: string) => `/packs/${encodeURIComponent(id)}`,
  material: (id: string) => `/materials/${encodeURIComponent(id)}`,
  search: (q?: string) => (q ? `/search?q=${encodeURIComponent(q)}` : '/search'),
  collections: () => '/collections',
  collection: (id: string) => `/collections/${encodeURIComponent(id)}`,
  trash: () => '/trash',
  jobs: (query?: Record<string, string>) => (query ? `/jobs?${new URLSearchParams(query)}` : '/jobs'),
  job: (id: string) => `/jobs/${encodeURIComponent(id)}`,
  storage: () => '/storage',
  nodes: () => '/nodes',
  node: (id: string) => `/nodes/${encodeURIComponent(id)}`,
  settings: (section?: SettingsSection) => (section ? `/settings/${section}` : '/settings'),
} as const;

export type NavSection = 'library' | 'operations' | 'system';

export const NAV_SECTION_LABELS: Record<NavSection, string> = {
  library: '资料',
  operations: '运维',
  system: '系统',
};

export interface AppRoute {
  path: string;
  title: string;
  page: LazyPage;
  scope?: Scope;
  /** 出现在侧栏中的入口。 */
  nav?: { section: NavSection; icon: ReactNode; badge?: 'attention' };
  /** 详情页归属的导航入口路径。 */
  parent?: string;
  /** 只在命令面板中搜索的别名。 */
  keywords?: string;
}

export const ROUTES: AppRoute[] = [
  { path: '/', title: '工作台', page: page(() => import('../features/dashboard/DashboardPage'), 'DashboardPage'), nav: { section: 'library', icon: <IconDashboard size={18} stroke={1.6} /> }, keywords: 'dashboard home' },
  { path: '/imports', title: '导入中心', scope: 'uploads:write', page: page(() => import('../features/uploads/ImportCenterPage'), 'ImportCenterPage'), nav: { section: 'library', icon: <IconCloudUpload size={18} stroke={1.6} /> }, keywords: 'upload 上传' },
  { path: '/imports/:uploadId', title: '导入详情', scope: 'uploads:write', parent: '/imports', page: page(() => import('../features/uploads/UploadSessionPage'), 'UploadSessionPage') },
  { path: '/inbox', title: '需关注', scope: 'materials:write', page: page(() => import('../features/inbox/InboxPage'), 'InboxPage'), nav: { section: 'library', icon: <IconInbox size={18} stroke={1.6} />, badge: 'attention' }, keywords: 'review inbox 审核' },
  { path: '/library', title: '资料库', scope: 'materials:read', page: page(() => import('../features/packs/LibraryPage'), 'LibraryPage'), nav: { section: 'library', icon: <IconFolders size={18} stroke={1.6} /> }, keywords: 'packs 套卷' },
  { path: '/packs/:packId', title: '套卷详情', scope: 'materials:read', parent: '/library', page: page(() => import('../features/packs/PackDetailPage'), 'PackDetailPage') },
  { path: '/materials/:materialId', title: '文件详情', scope: 'materials:read', parent: '/library', page: page(() => import('../features/materials/MaterialDetailPage'), 'MaterialDetailPage') },
  { path: '/search', title: '搜题', scope: 'materials:read', page: page(() => import('../features/search/SearchPage'), 'SearchPage'), nav: { section: 'library', icon: <IconSearch size={18} stroke={1.6} /> }, keywords: 'search questions 题目' },
  { path: '/collections', title: '专题', scope: 'collections:read', page: page(() => import('../features/collections/CollectionsListPage'), 'CollectionsListPage'), nav: { section: 'library', icon: <IconApps size={18} stroke={1.6} /> }, keywords: 'collections' },
  { path: '/collections/:collectionId', title: '专题详情', scope: 'collections:read', parent: '/collections', page: page(() => import('../features/collections/CollectionDetailPage'), 'CollectionDetailPage') },
  { path: '/trash', title: '回收站', scope: 'materials:write', page: page(() => import('../features/trash/TrashPage'), 'TrashPage'), nav: { section: 'library', icon: <IconTrash size={18} stroke={1.6} /> }, keywords: 'trash 删除' },
  { path: '/jobs', title: '处理日志', scope: 'jobs:read', page: page(() => import('../features/jobs/JobsListPage'), 'JobsListPage'), nav: { section: 'operations', icon: <IconListDetails size={18} stroke={1.6} /> }, keywords: 'jobs 任务' },
  { path: '/jobs/:jobId', title: '处理详情', scope: 'jobs:read', parent: '/jobs', page: page(() => import('../features/jobs/JobDetailPage'), 'JobDetailPage') },
  { path: '/storage', title: '存储', scope: 'storage:read', page: page(() => import('../features/storage/StoragePage'), 'StoragePage'), nav: { section: 'operations', icon: <IconServer size={18} stroke={1.6} /> }, keywords: 'storage drives 网盘 同步' },
  { path: '/nodes', title: '节点', scope: 'nodes:manage', page: page(() => import('../features/nodes/NodesPage'), 'NodesPage'), nav: { section: 'operations', icon: <IconSitemap size={18} stroke={1.6} /> }, keywords: 'nodes cluster 集群' },
  { path: '/nodes/:nodeId', title: '节点详情', scope: 'nodes:manage', parent: '/nodes', page: page(() => import('../features/nodes/NodeDetailPage'), 'NodeDetailPage') },
];

export type SettingsSection = 'ai-models' | 'ai-parsing' | 'entities' | 'api-keys' | 'users' | 'audit';

export interface SettingsRoute {
  key: SettingsSection;
  title: string;
  description: string;
  scope: Scope;
  icon: ReactNode;
  page: LazyPage;
}

/** 设置中心的分区；只显示当前角色有权访问的分区。 */
export const SETTINGS: SettingsRoute[] = [
  { key: 'ai-models', title: 'AI 模型', description: '模型端点、密钥与优先级', scope: 'ai:read', icon: <IconRobot size={18} stroke={1.6} />, page: page(() => import('../features/ai/AiModelsPage'), 'AiModelsPage') },
  { key: 'ai-parsing', title: 'AI 解析', description: 'OCR 服务与阅读限制', scope: 'ai:read', icon: <IconAdjustments size={18} stroke={1.6} />, page: page(() => import('../features/ai/AiSettingsPage'), 'AiSettingsPage') },
  { key: 'entities', title: '名称词典', description: '学校、联盟与标签的规范名称', scope: 'materials:read', icon: <IconTags size={18} stroke={1.6} />, page: page(() => import('../features/ai/EntitiesPage'), 'EntitiesPage') },
  { key: 'api-keys', title: 'API 密钥', description: 'Agent 与脚本的访问凭据', scope: 'keys:manage', icon: <IconKey size={18} stroke={1.6} />, page: page(() => import('../features/apiKeys/ApiKeysPage'), 'ApiKeysPage') },
  { key: 'users', title: '用户', description: '账户与角色', scope: 'users:manage', icon: <IconUsers size={18} stroke={1.6} />, page: page(() => import('../features/users/UsersPage'), 'UsersPage') },
  { key: 'audit', title: '审计日志', description: '管理操作记录', scope: 'audit:read', icon: <IconChecklist size={18} stroke={1.6} />, page: page(() => import('../features/audit/AuditLogsPage'), 'AuditLogsPage') },
];

export const SETTINGS_NAV = { path: '/settings', title: '设置', icon: <IconSettings size={18} stroke={1.6} /> } as const;

/** 当前位置对应的路由与导航高亮键。 */
export function resolveLocation(pathname: string): { route?: AppRoute; navKey: string; title: string; detail?: string } {
  if (pathname === '/settings' || pathname.startsWith('/settings/')) {
    const section = SETTINGS.find((s) => pathname === href.settings(s.key));
    return { navKey: SETTINGS_NAV.path, title: SETTINGS_NAV.title, detail: section?.title };
  }
  const route = ROUTES.find((r) => matchPath({ path: r.path, end: true }, pathname));
  if (!route) return { navKey: '', title: '管理台' };
  if (route.parent) {
    const parent = ROUTES.find((r) => r.path === route.parent);
    return { route, navKey: route.parent, title: parent?.title ?? route.title, detail: route.title };
  }
  return { route, navKey: route.path, title: route.title };
}
