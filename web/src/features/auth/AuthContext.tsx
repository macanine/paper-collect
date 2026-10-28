import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { sessionStore } from '../../shared/api/session';
import type { AuthSession, Scope, User } from '../../shared/api/types';
import { hasScope } from './permissions';
import { fetchCurrentUser, logoutRequest } from './api';

export type AuthStatus = 'loading' | 'authenticated' | 'anonymous';

export interface AuthContextValue {
  status: AuthStatus;
  user: User | null;
  can: (scope: Scope) => boolean;
  signIn: (session: AuthSession) => void;
  signOut: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [, rerender] = useReducer((count: number) => count + 1, 0);
  // 刷新页面后 access token 不在内存中，但仍有持久化的 refresh token：需先通过 /auth/me 校验再放行。
  const [verifying, setVerifying] = useState<boolean>(() => sessionStore.getRefreshToken() !== null);

  useEffect(() => sessionStore.subscribe(rerender), []);

  useEffect(() => {
    if (!verifying) return;
    let active = true;
    fetchCurrentUser()
      .then((user) => {
        if (active) sessionStore.setUser(user);
      })
      .catch(() => {
        // 401 时会话已被清理；网络错误保留会话，由页面层的错误态与重试处理。
      })
      .finally(() => {
        if (active) setVerifying(false);
      });
    return () => {
      active = false;
    };
  }, [verifying]);

  const hasRefresh = sessionStore.getRefreshToken() !== null;
  const user = sessionStore.getUser();
  const status: AuthStatus = verifying ? 'loading' : hasRefresh ? 'authenticated' : 'anonymous';

  const signIn = useCallback(
    (session: AuthSession) => {
      queryClient.clear();
      sessionStore.setSession(session);
    },
    [queryClient],
  );

  const signOut = useCallback(async () => {
    const refreshToken = sessionStore.getRefreshToken();
    try {
      if (refreshToken) await logoutRequest(refreshToken);
    } catch {
      // 注销接口失败时仍在本地清理会话。
    } finally {
      sessionStore.clear();
      queryClient.clear();
    }
  }, [queryClient]);

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      user,
      can: (scope: Scope) => hasScope(user?.role, scope),
      signIn,
      signOut,
    }),
    [status, user, signIn, signOut],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error('useAuth 必须在 AuthProvider 内使用');
  return value;
}
