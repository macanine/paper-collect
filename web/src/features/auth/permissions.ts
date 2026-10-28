import { SCOPES, type Scope, type UserRole } from '../../shared/api/types';

// 前端权限镜像，仅用于决定界面可见性与路由守卫；真正的授权由服务端每次请求检查。
// 依据 docs/API.md 与 docs/ARCHITECTURE.md 的角色定义：admin 全部；editor 负责资料/合集编辑、上传与任务读取；
// viewer 只读资料、合集与任务。编辑角色包含对应的读取权限（与服务端 auth.RoleScopes 一致）。

export const ROLE_SCOPES: Record<UserRole, readonly Scope[]> = {
  admin: SCOPES,
  editor: [
    'materials:read',
    'materials:write',
    'collections:read',
    'collections:write',
    'uploads:write',
    'jobs:read',
  ],
  viewer: ['materials:read', 'collections:read', 'jobs:read'],
};

export function scopesForRole(role: UserRole | null | undefined): readonly Scope[] {
  if (!role) return [];
  return ROLE_SCOPES[role] ?? [];
}

export function hasScope(role: UserRole | null | undefined, scope: Scope): boolean {
  return scopesForRole(role).includes(scope);
}

