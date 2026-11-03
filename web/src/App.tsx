import { lazy, Suspense, type ComponentType, type ReactNode } from 'react';
import { Navigate, Route, Routes } from 'react-router';
import { useAuth } from './features/auth/AuthContext';
import { LoginPage } from './features/auth/LoginPage';
import { hasScope } from './features/auth/permissions';
import type { Scope } from './shared/api/types';
import { ForbiddenState, FullPageLoading, NotFoundPage } from './shared/ui/States';

function page<K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K) {
  return lazy(() => load().then((module) => ({ default: module[name] })));
}

const ApiKeysPage = page(() => import('./features/apiKeys/ApiKeysPage'), 'ApiKeysPage');
const AuditLogsPage = page(() => import('./features/audit/AuditLogsPage'), 'AuditLogsPage');
const AiModelsPage = page(() => import('./features/ai/AiModelsPage'), 'AiModelsPage');
const AiSettingsPage = page(() => import('./features/ai/AiSettingsPage'), 'AiSettingsPage');
const SearchPage = page(() => import('./features/search/SearchPage'), 'SearchPage');
const CollectionDetailPage = page(() => import('./features/collections/CollectionDetailPage'), 'CollectionDetailPage');
const CollectionsListPage = page(() => import('./features/collections/CollectionsListPage'), 'CollectionsListPage');
const DashboardPage = page(() => import('./features/dashboard/DashboardPage'), 'DashboardPage');
const InboxPage = page(() => import('./features/inbox/InboxPage'), 'InboxPage');
const JobDetailPage = page(() => import('./features/jobs/JobDetailPage'), 'JobDetailPage');
const JobsListPage = page(() => import('./features/jobs/JobsListPage'), 'JobsListPage');
const LibraryPage = page(() => import('./features/packs/LibraryPage'), 'LibraryPage');
const PackDetailPage = page(() => import('./features/packs/PackDetailPage'), 'PackDetailPage');
const MaterialDetailPage = page(() => import('./features/materials/MaterialDetailPage'), 'MaterialDetailPage');
const StoragePage = page(() => import('./features/storage/StoragePage'), 'StoragePage');
const TrashPage = page(() => import('./features/trash/TrashPage'), 'TrashPage');
const UploadsListPage = page(() => import('./features/uploads/UploadsListPage'), 'UploadsListPage');
const UploadSessionPage = page(() => import('./features/uploads/UploadSessionPage'), 'UploadSessionPage');
const UsersPage = page(() => import('./features/users/UsersPage'), 'UsersPage');
const AuthenticatedLayout = page(() => import('./layouts/AppLayout'), 'AuthenticatedLayout');

/** 路由级权限镜像。服务端仍会逐请求重新检查角色与 scope。 */
function ScopeGuard({ scope, children }: { scope: Scope; children: ReactNode }) {
  const { user } = useAuth();
  if (!hasScope(user?.role, scope)) return <ForbiddenState />;
  return <>{children}</>;
}

const ROUTES: { path: string; scope: Scope; element: ReactNode }[] = [
  { path: 'inbox', scope: 'materials:write', element: <InboxPage /> },
  { path: 'library', scope: 'materials:read', element: <LibraryPage /> },
  { path: 'packs/:packId', scope: 'materials:read', element: <PackDetailPage /> },
  { path: 'materials/:materialId', scope: 'materials:read', element: <MaterialDetailPage /> },
  { path: 'search', scope: 'materials:read', element: <SearchPage /> },
  { path: 'collections', scope: 'collections:read', element: <CollectionsListPage /> },
  { path: 'collections/:collectionId', scope: 'collections:read', element: <CollectionDetailPage /> },
  { path: 'uploads', scope: 'uploads:write', element: <UploadsListPage /> },
  { path: 'uploads/:uploadId', scope: 'uploads:write', element: <UploadSessionPage /> },
  { path: 'trash', scope: 'materials:write', element: <TrashPage /> },
  { path: 'jobs', scope: 'jobs:read', element: <JobsListPage /> },
  { path: 'jobs/:jobId', scope: 'jobs:read', element: <JobDetailPage /> },
  { path: 'storage', scope: 'storage:read', element: <StoragePage /> },
  { path: 'admin/ai-models', scope: 'ai:read', element: <AiModelsPage /> },
  { path: 'admin/ai-settings', scope: 'ai:read', element: <AiSettingsPage /> },
  { path: 'admin/api-keys', scope: 'keys:manage', element: <ApiKeysPage /> },
  { path: 'admin/users', scope: 'users:manage', element: <UsersPage /> },
  { path: 'admin/audit', scope: 'audit:read', element: <AuditLogsPage /> },
];

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        element={
          <Suspense fallback={<FullPageLoading />}>
            <AuthenticatedLayout />
          </Suspense>
        }
      >
        <Route index element={<DashboardPage />} />
        {ROUTES.map((route) => (
          <Route key={route.path} path={route.path} element={<ScopeGuard scope={route.scope}>{route.element}</ScopeGuard>} />
        ))}
        {/* 旧地址重定向到新的入口 */}
        <Route path="materials" element={<Navigate to="/library" replace />} />
        <Route path="uploads/new" element={<Navigate to="/uploads" replace />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
