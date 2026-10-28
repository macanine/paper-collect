import { apiRequest } from '../../shared/api/client';
import type { AuthSession, User } from '../../shared/api/types';

export function loginRequest(username: string, password: string): Promise<AuthSession> {
  return apiRequest<AuthSession>('/auth/login', {
    method: 'POST',
    body: { username, password },
    public: true,
  });
}

export function logoutRequest(refreshToken: string): Promise<void> {
  return apiRequest<void>('/auth/logout', {
    method: 'POST',
    body: { refresh_token: refreshToken },
  });
}

export function fetchCurrentUser(): Promise<User> {
  return apiRequest<User>('/auth/me');
}
