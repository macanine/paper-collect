import { Component, lazy, Suspense, type ErrorInfo, type ReactNode } from 'react';
import { ROUTES } from './app/routes';
import { Route, Routes } from 'react-router';
import { useAuth } from './features/auth/AuthContext';
import { LoginPage } from './features/auth/LoginPage';
import { hasScope } from './features/auth/permissions';
import type { Scope } from './shared/api/types';
import { ErrorState, ForbiddenState, FullPageLoading, NotFoundPage } from './shared/ui/States';

const AuthenticatedLayout = lazy(() => import('./layouts/AppLayout').then((m) => ({ default: m.AuthenticatedLayout })));
const SettingsPage = lazy(() => import('./features/settings/SettingsPage').then((m) => ({ default: m.SettingsPage })));

/** 路由级权限镜像。服务端仍会逐请求重新检查角色与 scope。 */
function ScopeGuard({ scope, children }: { scope?: Scope; children: ReactNode }) {
  const { user } = useAuth();
  if (scope && !hasScope(user?.role, scope)) return <ForbiddenState />;
  return <>{children}</>;
}

export function App() {
  return (
    <AppErrorBoundary>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          element={
            <Suspense fallback={<FullPageLoading />}>
              <AuthenticatedLayout />
            </Suspense>
          }
        >
          {ROUTES.map(({ path, scope, page: Page }) => (
            <Route key={path} path={path} element={<ScopeGuard scope={scope}><Page /></ScopeGuard>} />
          ))}
          <Route path="/settings/:section?" element={<SettingsPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </AppErrorBoundary>
  );
}

interface AppErrorBoundaryState {
  error: Error | null;
}

class AppErrorBoundary extends Component<{ children: ReactNode }, AppErrorBoundaryState> {
  override state: AppErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): AppErrorBoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('页面渲染失败', error, info.componentStack);
  }

  override render() {
    if (this.state.error) {
      return (
        <div style={{ minHeight: "100vh", display: "grid", placeItems: "center" }}>
          <ErrorState error={this.state.error} title="页面加载失败" onRetry={() => window.location.reload()} />
        </div>
      );
    }
    return this.props.children;
  }
}
