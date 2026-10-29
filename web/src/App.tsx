import { lazy, Suspense, type ReactNode } from 'react';
import { Route, Routes } from 'react-router';
import { useAuth } from './features/auth/AuthContext';
import { LoginPage } from './features/auth/LoginPage';
import { hasScope } from './features/auth/permissions';
import type { Scope } from './shared/api/types';
import { ForbiddenState, FullPageLoading, NotFoundPage } from './shared/ui/States';

const ApiKeysPage = lazy(() => import('./features/apiKeys/ApiKeysPage').then((module) => ({ default: module.ApiKeysPage })));
const AuditLogsPage = lazy(() => import('./features/audit/AuditLogsPage').then((module) => ({ default: module.AuditLogsPage })));
const AiModelsPage = lazy(() => import('./features/ai/AiModelsPage').then((module) => ({ default: module.AiModelsPage })));
const SearchPage = lazy(() => import('./features/search/SearchPage').then((module) => ({ default: module.SearchPage })));
const AiSettingsPage = lazy(() => import('./features/ai/AiSettingsPage').then((module) => ({ default: module.AiSettingsPage })));
const CollectionDetailPage = lazy(() => import('./features/collections/CollectionDetailPage').then((module) => ({ default: module.CollectionDetailPage })));
const CollectionsListPage = lazy(() => import('./features/collections/CollectionsListPage').then((module) => ({ default: module.CollectionsListPage })));
const DashboardPage = lazy(() => import('./features/dashboard/DashboardPage').then((module) => ({ default: module.DashboardPage })));
const JobDetailPage = lazy(() => import('./features/jobs/JobDetailPage').then((module) => ({ default: module.JobDetailPage })));
const JobsListPage = lazy(() => import('./features/jobs/JobsListPage').then((module) => ({ default: module.JobsListPage })));
const MaterialDetailPage = lazy(() => import('./features/materials/MaterialDetailPage').then((module) => ({ default: module.MaterialDetailPage })));
const MaterialsListPage = lazy(() => import('./features/materials/MaterialsListPage').then((module) => ({ default: module.MaterialsListPage })));
const StoragePage = lazy(() => import('./features/storage/StoragePage').then((module) => ({ default: module.StoragePage })));
const NewUploadPage = lazy(() => import('./features/uploads/NewUploadPage').then((module) => ({ default: module.NewUploadPage })));
const UploadSessionPage = lazy(() => import('./features/uploads/UploadSessionPage').then((module) => ({ default: module.UploadSessionPage })));
const UsersPage = lazy(() => import('./features/users/UsersPage').then((module) => ({ default: module.UsersPage })));
const AuthenticatedLayout = lazy(() => import('./layouts/AppLayout').then((module) => ({ default: module.AuthenticatedLayout })));

/** 路由级权限镜像。服务端仍会逐请求重新检查角色与 scope。 */
function ScopeGuard({ scope, children }: { scope: Scope; children: ReactNode }) {
  const { user } = useAuth();
  if (!hasScope(user?.role, scope)) return <ForbiddenState />;
  return <>{children}</>;
}

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
        <Route
          path="search"
          element={
            <ScopeGuard scope="materials:read">
              <SearchPage />
            </ScopeGuard>
          }
        />
        <Route
          path="materials"
          element={
            <ScopeGuard scope="materials:read">
              <MaterialsListPage />
            </ScopeGuard>
          }
        />
        <Route
          path="materials/:materialId"
          element={
            <ScopeGuard scope="materials:read">
              <MaterialDetailPage />
            </ScopeGuard>
          }
        />
        <Route
          path="collections"
          element={
            <ScopeGuard scope="collections:read">
              <CollectionsListPage />
            </ScopeGuard>
          }
        />
        <Route
          path="collections/:collectionId"
          element={
            <ScopeGuard scope="collections:read">
              <CollectionDetailPage />
            </ScopeGuard>
          }
        />
        <Route
          path="uploads/new"
          element={
            <ScopeGuard scope="uploads:write">
              <NewUploadPage />
            </ScopeGuard>
          }
        />
        <Route
          path="uploads/:uploadId"
          element={
            <ScopeGuard scope="uploads:write">
              <UploadSessionPage />
            </ScopeGuard>
          }
        />
        <Route
          path="jobs"
          element={
            <ScopeGuard scope="jobs:read">
              <JobsListPage />
            </ScopeGuard>
          }
        />
        <Route
          path="jobs/:jobId"
          element={
            <ScopeGuard scope="jobs:read">
              <JobDetailPage />
            </ScopeGuard>
          }
        />
        <Route
          path="storage"
          element={
            <ScopeGuard scope="storage:read">
              <StoragePage />
            </ScopeGuard>
          }
        />
        <Route
          path="admin/ai-models"
          element={
            <ScopeGuard scope="ai:read">
              <AiModelsPage />
            </ScopeGuard>
          }
        />
        <Route
          path="admin/ai-settings"
          element={
            <ScopeGuard scope="ai:read">
              <AiSettingsPage />
            </ScopeGuard>
          }
        />
        <Route
          path="admin/api-keys"
          element={
            <ScopeGuard scope="keys:manage">
              <ApiKeysPage />
            </ScopeGuard>
          }
        />
        <Route
          path="admin/users"
          element={
            <ScopeGuard scope="users:manage">
              <UsersPage />
            </ScopeGuard>
          }
        />
        <Route
          path="admin/audit"
          element={
            <ScopeGuard scope="audit:read">
              <AuditLogsPage />
            </ScopeGuard>
          }
        />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
