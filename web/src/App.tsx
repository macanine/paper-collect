import type { ReactNode } from 'react';
import { Route, Routes } from 'react-router';
import { useAuth } from './features/auth/AuthContext';
import { LoginPage } from './features/auth/LoginPage';
import { ApiKeysPage } from './features/apiKeys/ApiKeysPage';
import { AuditLogsPage } from './features/audit/AuditLogsPage';
import { AiModelsPage } from './features/ai/AiModelsPage';
import { SearchPage } from './features/search/SearchPage';
import { AiSettingsPage } from './features/ai/AiSettingsPage';
import { CollectionDetailPage } from './features/collections/CollectionDetailPage';
import { CollectionsListPage } from './features/collections/CollectionsListPage';
import { DashboardPage } from './features/dashboard/DashboardPage';
import { JobDetailPage } from './features/jobs/JobDetailPage';
import { JobsListPage } from './features/jobs/JobsListPage';
import { MaterialDetailPage } from './features/materials/MaterialDetailPage';
import { MaterialsListPage } from './features/materials/MaterialsListPage';
import { StoragePage } from './features/storage/StoragePage';
import { NewUploadPage } from './features/uploads/NewUploadPage';
import { UploadSessionPage } from './features/uploads/UploadSessionPage';
import { UsersPage } from './features/users/UsersPage';
import { AuthenticatedLayout } from './layouts/AppLayout';
import { hasScope } from './features/auth/permissions';
import type { Scope } from './shared/api/types';
import { ForbiddenState, NotFoundPage } from './shared/ui/States';

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
      <Route element={<AuthenticatedLayout />}>
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
