import { describe, expect, it } from 'vitest';
import { hasScope } from '../auth/permissions';
import { SCOPES } from '../../shared/api/types';

describe('AI 配置权限镜像', () => {
  it('仅管理员拥有 AI 配置读写', () => {
    expect(hasScope('admin', 'ai:read')).toBe(true);
    expect(hasScope('admin', 'ai:write')).toBe(true);
    expect(hasScope('editor', 'ai:read')).toBe(false);
    expect(hasScope('viewer', 'ai:write')).toBe(false);
  });

  it('AI scope 已加入完整 scope 列表', () => {
    expect(SCOPES).toContain('ai:read');
    expect(SCOPES).toContain('ai:write');
  });
});
