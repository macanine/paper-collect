import { describe, expect, it } from 'vitest';
import { PROVIDER_FIELD_SPECS, configDefaultsForKind, validateProviderForm } from './providerFields';
import { POLICY_MODE_COPY, describePolicy } from './policyCopy';

describe('存储端点字段表', () => {
  it('与合同字段一致', () => {
    expect(PROVIDER_FIELD_SPECS.webdav.config.map((field) => field.key)).toEqual(['base_url', 'path_prefix', 'public_base_url']);
    expect(PROVIDER_FIELD_SPECS.webdav.secret.map((field) => field.key)).toEqual(['username', 'password']);
    expect(PROVIDER_FIELD_SPECS.lanzou.config.map((field) => field.key)).toEqual(['base_url', 'folder_id', 'verify_remote_hash']);
    expect(PROVIDER_FIELD_SPECS.lanzou.secret.map((field) => field.key)).toEqual(['cookie']);
    expect(PROVIDER_FIELD_SPECS['123pan'].secret.map((field) => field.key)).toEqual(['client_id', 'client_secret']);
  });

  it('为可选字段填入合同默认值', () => {
    expect(configDefaultsForKind('lanzou')).toMatchObject({ base_url: 'https://pc.woozooo.com', folder_id: '-1' });
    expect(configDefaultsForKind('123pan')).toMatchObject({ parent_file_id: '0' });
  });

  it('创建时校验必填凭据与 URL 规则', () => {
    const base = {
      name: '学校 WebDAV',
      kind: 'webdav' as const,
      role: 'backup' as const,
      enabled: true,
      priority: 20,
      weight: 1,
      config: { base_url: 'https://dav.example/dav', path_prefix: '', public_base_url: '' },
      secret: {},
    };
    const invalid = validateProviderForm({ ...base, secret: {} }, 'create');
    expect(invalid.valid).toBe(false);
    expect(invalid.errors['secret.username']).toBeDefined();

    const valid = validateProviderForm({ ...base, secret: { username: 'u', password: 'p' } }, 'create');
    expect(valid.valid).toBe(true);

    const embedded = validateProviderForm(
      { ...base, config: { base_url: 'https://u:p@dav.example/dav', path_prefix: '', public_base_url: '' }, secret: { username: 'u', password: 'p' } },
      'create',
    );
    expect(embedded.valid).toBe(false);
  });
});

describe('存储策略文案', () => {
  it('三种模式都有标签', () => {
    expect(Object.keys(POLICY_MODE_COPY)).toEqual(['mirror_all', 'primary_backup', 'balanced']);
    expect(describePolicy('balanced', 2)).toContain('最低副本数 2');
  });
});
