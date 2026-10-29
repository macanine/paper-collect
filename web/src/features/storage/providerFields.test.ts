import { describe, expect, it } from 'vitest';
import { PROVIDER_FIELD_SPECS, buildCreatePayload, configDefaultsForKind, initialProviderValues, validateProviderForm } from './providerFields';
import type { StorageProvider } from '../../shared/api/types';
import { POLICY_MODE_COPY, describePolicy } from './policyCopy';

describe('存储端点字段表', () => {
  it('与合同字段一致', () => {
    expect(PROVIDER_FIELD_SPECS.webdav.config.map((field) => field.key)).toEqual(['base_url', 'path_prefix', 'public_base_url']);
    expect(PROVIDER_FIELD_SPECS.webdav.secret.map((field) => field.key)).toEqual(['username', 'password']);
    expect(PROVIDER_FIELD_SPECS.lanzou.config.map((field) => field.key)).toEqual(['base_url', 'folder_id', 'verify_remote_hash']);
    expect(PROVIDER_FIELD_SPECS.lanzou.secret.map((field) => field.key)).toEqual(['cookie']);
    expect(PROVIDER_FIELD_SPECS['123pan'].secret.map((field) => field.key)).toEqual(['client_id', 'client_secret']);
    expect(PROVIDER_FIELD_SPECS.onedrive.secret.map((field) => field.key)).toEqual(['refresh_token', 'client_secret', 'share_url']);
    expect(PROVIDER_FIELD_SPECS.s3.secret.map((field) => field.key)).toEqual(['access_key_id', 'secret_access_key', 'session_token']);
  });

  it('验证 S3 有效期和 OneDrive 授权字段', () => {
    const common = { name: '云存储', role: 'backup' as const, enabled: true, priority: 10, weight: 1 };
    const s3 = { ...common, kind: 's3' as const, config: { bucket: 'bucket', region: 'us-east-1', share_expires_seconds: '604801' }, secret: { access_key_id: 'a', secret_access_key: 'b' } };
    expect(validateProviderForm(s3, 'create').errors['config.share_expires_seconds']).toBeDefined();
    const onedrive = { ...common, kind: 'onedrive' as const, config: { client_id: 'app' }, secret: {} };
    expect(validateProviderForm(onedrive, 'create').errors['secret.refresh_token']).toBeDefined();
    expect(validateProviderForm({ ...onedrive, secret: { refresh_token: 'token' } }, 'create').valid).toBe(true);
  });

  it('新 OneDrive 可使用 OpenList 令牌，旧端点继续使用自己的应用', () => {
    const values = {
      name: 'OneDrive', kind: 'onedrive' as const, role: 'backup' as const, enabled: true,
      priority: 10, weight: 1,
      config: { auth_mode: 'openlist', client_id: 'previous', tenant: 'common' },
      secret: { refresh_token: 'token', client_secret: 'previous' },
    };
    expect(configDefaultsForKind('onedrive').auth_mode).toBe('openlist');
    expect(validateProviderForm(values, 'create').valid).toBe(true);
    expect(buildCreatePayload(values)).toMatchObject({ config: { auth_mode: 'openlist' }, secret: { refresh_token: 'token' } });
    expect(buildCreatePayload(values).config.client_id).toBeUndefined();
    expect(buildCreatePayload(values).secret?.client_secret).toBeUndefined();
    expect(validateProviderForm({ ...values, config: { auth_mode: 'direct' } }, 'create').errors['config.client_id']).toBeDefined();
    const previous: StorageProvider = {
      id: 'old', name: '旧端点', kind: 'onedrive', role: 'backup', enabled: true,
      priority: 1, weight: 1, config: { client_id: 'old' }, secret_configured: true, created_at: '', updated_at: '',
    };
    expect(initialProviderValues(previous).config.auth_mode).toBe('direct');
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
