import { ROLE_SCOPES, type Scope, type UserRole } from '../../shared/api/types';

// 前端权限镜像，仅用于决定界面可见性与路由守卫；真正的授权由服务端每次请求检查。
// 角色权限由后端 auth.RoleScopes 生成（contract.gen.ts）。

function scopesForRole(role: UserRole | null | undefined): readonly Scope[] {
  if (!role) return [];
  return ROLE_SCOPES[role] ?? [];
}

export function hasScope(role: UserRole | null | undefined, scope: Scope): boolean {
  return scopesForRole(role).includes(scope);
}

