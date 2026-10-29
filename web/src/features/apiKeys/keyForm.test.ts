import { describe, expect, it } from 'vitest';
import { buildCreateKeyBody } from './keyForm';

describe('API 密钥创建', () => {
  it('未填写过期时间时创建长期有效的密钥', () => {
    expect(buildCreateKeyBody({ name: ' 检索 ', scopes: ['materials:read'] })).toEqual({ name: '检索', scopes: ['materials:read'] });
  });

  it('将指定过期时间转为服务端接受的标准时间', () => {
    expect(buildCreateKeyBody({ name: '检索', scopes: ['materials:read'], expires_at: '2027-01-02T08:00:00+08:00' }).expires_at)
      .toBe('2027-01-02T00:00:00.000Z');
  });
});
