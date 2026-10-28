import { apiRequest } from '../../shared/api/client';
import type { Page, User, UserRole } from '../../shared/api/types';

export interface UserListParams {
  page: number;
  page_size: number;
}

export interface UserCreateBody {
  username: string;
  password: string;
  role: UserRole;
}

export function listUsers(params: UserListParams, signal?: AbortSignal): Promise<Page<User>> {
  return apiRequest<Page<User>>('/users', { query: { page: params.page, page_size: params.page_size }, signal });
}

export function createUser(body: UserCreateBody): Promise<User> {
  return apiRequest<User>('/users', { method: 'POST', body });
}
