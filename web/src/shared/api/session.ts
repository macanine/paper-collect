import type { AuthSession, User } from './types';

// 会话存储：access token 仅保存在内存中（15 分钟有效）；refresh token 与用户摘要持久化，
// 以便刷新页面后通过 /auth/refresh 恢复。浏览器不保存 Agent 的 Secret Key。

export const SESSION_STORAGE_KEY = 'paper-collect.session.v1';

interface PersistedSession {
  refresh_token: string;
  user: User;
}

export interface SessionStore {
  getAccessToken(): string | null;
  getRefreshToken(): string | null;
  getUser(): User | null;
  setSession(session: AuthSession): void;
  /** 仅更新已持久化的用户摘要（例如 /auth/me 校验后的最新角色）。 */
  setUser(user: User): void;
  clear(): void;
  subscribe(listener: () => void): () => void;
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

function readPersisted(storage: StorageLike | null): PersistedSession | null {
  if (!storage) return null;
  try {
    const raw = storage.getItem(SESSION_STORAGE_KEY);
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (
      typeof parsed === 'object' &&
      parsed !== null &&
      'refresh_token' in parsed &&
      typeof parsed.refresh_token === 'string' &&
      'user' in parsed &&
      typeof parsed.user === 'object' &&
      parsed.user !== null
    ) {
      return { refresh_token: parsed.refresh_token, user: parsed.user as User };
    }
    return null;
  } catch {
    return null;
  }
}

export function createSessionStore(storage: StorageLike | null): SessionStore {
  let accessToken: string | null = null;
  let persisted = readPersisted(storage);
  const listeners = new Set<() => void>();

  const notify = () => {
    for (const listener of listeners) listener();
  };

  return {
    getAccessToken: () => accessToken,
    getRefreshToken: () => persisted?.refresh_token ?? null,
    getUser: () => persisted?.user ?? null,
    setSession(session) {
      accessToken = session.access_token;
      persisted = { refresh_token: session.refresh_token, user: session.user };
      try {
        storage?.setItem(SESSION_STORAGE_KEY, JSON.stringify(persisted));
      } catch {
        // 存储不可用（隐私模式等）时仅保留内存会话。
      }
      notify();
    },
    setUser(user) {
      if (!persisted) return;
      persisted = { ...persisted, user };
      try {
        storage?.setItem(SESSION_STORAGE_KEY, JSON.stringify(persisted));
      } catch {
        // 忽略存储错误
      }
      notify();
    },
    clear() {
      accessToken = null;
      persisted = null;
      try {
        storage?.removeItem(SESSION_STORAGE_KEY);
      } catch {
        // 忽略存储错误
      }
      notify();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}

export const sessionStore: SessionStore = createSessionStore(
  typeof window !== 'undefined' ? window.localStorage : null,
);
