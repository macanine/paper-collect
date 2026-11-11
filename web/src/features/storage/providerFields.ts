import { PROVIDER_KINDS, type Drive, type DriveRouteMode, type OnPathChange, type ProviderKind } from '../../shared/api/types';

// 网盘 config / secret 字段表，严格对应 docs/API.md「POST /storage/drives」的适配器字段表。
// 不得增加合同之外的字段；secret 永不回显，编辑时留空表示保留原值。

type FieldInput = 'text' | 'password' | 'select';

interface FieldOption {
  value: string;
  label: string;
}

export interface FieldSpec {
  key: string;
  label: string;
  required: boolean;
  /** 合同规定的默认值（留空时服务端使用默认）。 */
  defaultValue?: string;
  placeholder?: string;
  /** 输入控件类型；secret 字段固定使用密码框。 */
  input?: FieldInput;
  options?: FieldOption[];
  /** 是否为 secret 字段（输入框使用密码形式，且不回显）。 */
  secret?: boolean;
  /** 返回错误文案或 null。 */
  validate?: (value: string) => string | null;
}

export interface KindSpec {
  kind: ProviderKind;
  label: string;
  config: FieldSpec[];
  secret: FieldSpec[];
}

function validateHttpUrl(value: string): string | null {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return '请输入完整的 http 或 https 地址';
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    return '地址必须以 http:// 或 https:// 开头';
  }
  if (url.username !== '' || url.password !== '') {
    return '地址中不能包含内嵌用户名或密码，凭据请填写在凭据区域';
  }
  return null;
}

function validateInteger(min: number, max: number): (value: string) => string | null {
  return (value) => {
    if (!/^-?\d+$/.test(value.trim())) return '请输入整数';
    const number = Number(value.trim());
    if (number < min || number > max) return `取值范围为 ${min} 至 ${max}`;
    return null;
  };
}

function validateBooleanText(value: string): string | null {
  if (value === '' || value === 'true' || value === 'false') return null;
  return '只能填写 true 或 false';
}

export const PROVIDER_FIELD_SPECS: Record<ProviderKind, KindSpec> = {
  local: {
    kind: 'local',
    label: '本地存储',
    config: [],
    secret: [],
  },
  webdav: {
    kind: 'webdav',
    label: 'WebDAV',
    config: [
      {
        key: 'base_url',
        label: '基础地址',
        required: true,
        placeholder: 'https://dav.example/dav',
        validate: validateHttpUrl,
      },
      {
        key: 'public_base_url',
        label: '公开根地址',
        required: false,
        placeholder: 'https://share.example/dav',
        validate: validateHttpUrl,
      },
    ],
    secret: [
      { key: 'username', label: '用户名', required: true, secret: true },
      { key: 'password', label: '密码', required: true, secret: true },
    ],
  },
  lanzou: {
    kind: 'lanzou',
    label: '蓝奏云',
    config: [
      {
        key: 'base_url',
        label: '基础地址',
        required: false,
        defaultValue: 'https://pc.woozooo.com',
        placeholder: 'https://pc.woozooo.com',
        validate: validateHttpUrl,
      },
      {
        key: 'verify_remote_hash',
        label: '远端完整性校验',
        required: false,
        input: 'select',
        options: [
          { value: '', label: '默认（false）' },
          { value: 'true', label: '开启（需重新下载文件）' },
          { value: 'false', label: 'false' },
        ],
        validate: validateBooleanText,
      },
    ],
    secret: [
      {
        key: 'cookie',
        label: 'Cookie',
        required: true,
        secret: true,
      },
    ],
  },
  '123pan': {
    kind: '123pan',
    label: '123 云盘',
    config: [
      {
        key: 'base_url',
        label: '基础地址',
        required: false,
        defaultValue: 'https://open-api.123pan.com',
        placeholder: 'https://open-api.123pan.com',
        validate: validateHttpUrl,
      },
      {
        key: 'parent_file_id',
        label: '父目录 ID',
        required: false,
        defaultValue: '0',
        placeholder: '0',
        validate: validateInteger(0, Number.MAX_SAFE_INTEGER),
      },
    ],
    secret: [
      { key: 'client_id', label: 'Client ID', required: true, secret: true },
      { key: 'client_secret', label: 'Client Secret', required: true, secret: true },
    ],
  },
  onedrive: {
    kind: 'onedrive', label: 'OneDrive',
    config: [
      { key: 'auth_mode', label: '授权方式', required: false, defaultValue: 'openlist', input: 'select', options: [
        { value: 'openlist', label: '使用 OpenList 提供的应用' }, { value: 'direct', label: '使用自己的微软应用' },
      ] },
      { key: 'client_id', label: '应用程序 ID', required: true },
      { key: 'tenant', label: '租户', required: false, defaultValue: 'common', placeholder: 'common' },
      { key: 'share_scope', label: '分享范围', required: false, defaultValue: 'anonymous', input: 'select', options: [
        { value: 'anonymous', label: '持链接可查看' }, { value: 'organization', label: '仅组织内可查看' }, { value: 'disabled', label: '不创建分享链接' },
      ] },
    ],
    secret: [
      { key: 'refresh_token', label: '刷新令牌', required: true, secret: true },
      { key: 'client_secret', label: '应用程序密钥', required: false, secret: true },
      { key: 'share_url', label: '共享文件夹链接', required: false, secret: true },
    ],
  },
  s3: {
    kind: 's3', label: 'S3 / 兼容对象存储',
    config: [
      { key: 'bucket', label: '存储桶', required: true },
      { key: 'region', label: '区域', required: true, placeholder: 'us-east-1' },
      { key: 'endpoint', label: '服务地址', required: false, placeholder: '留空使用 AWS 默认地址', validate: validateHttpUrl },
      { key: 'path_style', label: '路径式访问', required: false, defaultValue: 'false', input: 'select', options: [{ value: 'false', label: '关闭' }, { value: 'true', label: '开启（兼容服务常用）' }] },
      { key: 'share_expires_seconds', label: '分享有效期（秒）', required: false, defaultValue: '3600', validate: validateInteger(60, 604800) },
    ],
    secret: [
      { key: 'access_key_id', label: '访问密钥', required: true, secret: true },
      { key: 'secret_access_key', label: '密钥密码', required: true, secret: true },
      { key: 'session_token', label: '会话令牌', required: false, secret: true },
    ],
  },
};

export const PROVIDER_KIND_OPTIONS = PROVIDER_KINDS.map((kind) => ({
  value: kind,
  label: PROVIDER_FIELD_SPECS[kind].label,
}));

export interface ProviderDraft {
  name: string;
  kind: ProviderKind;
  enabled: boolean;
  root_path: string;
  max_file_bytes: number;
  allowed_exts: string[];
  read_order: number;
  on_path_change: OnPathChange;
  route_mode?: DriveRouteMode;
  routes?: DriveRouteDraft[];
  config: Record<string, string>;
  secret?: Record<string, string>;
}

/** PATCH 不允许修改 kind。 */
export interface ProviderPatch {
  name?: string;
  enabled?: boolean;
  root_path?: string;
  max_file_bytes?: number;
  allowed_exts?: string[];
  read_order?: number;
  on_path_change?: OnPathChange;
  route_mode?: DriveRouteMode;
  routes?: DriveRouteDraft[];
  config?: Record<string, string>;
  secret?: Record<string, string>;
}

export interface ProviderFormValues {
  name: string;
  kind: ProviderKind;
  enabled: boolean;
  root_path: string;
  /** 单位 MiB；0 表示不限。 */
  max_file_mib: number;
  allowed_exts: string[];
  read_order: number;
  on_path_change: OnPathChange;
  route_mode?: DriveRouteMode;
  routes?: DriveRouteDraft[];
  config: Record<string, string>;
  secret: Record<string, string>;
  /** 编辑时是否显式清空已保存凭据（发送空对象 secret）。 */
  clearSecret?: boolean;
}

export interface DriveRouteDraft {
  node_id: string;
  priority: number;
  enabled: boolean;
}

export interface ProviderValidation {
  errors: Record<string, string>;
  valid: boolean;
}

function isBlank(value: string | undefined): boolean {
  return value === undefined || value.trim() === '';
}

export function oneDriveAuthMode(config: Record<string, string>): 'openlist' | 'direct' {
  return config.auth_mode === 'openlist' ? 'openlist' : 'direct';
}

export function visibleConfigFields(kind: ProviderKind, config: Record<string, string>): FieldSpec[] {
  return PROVIDER_FIELD_SPECS[kind].config.filter((field) =>
    kind !== 'onedrive' || oneDriveAuthMode(config) !== 'openlist' || !['client_id', 'tenant'].includes(field.key),
  );
}

export function visibleSecretFields(kind: ProviderKind, config: Record<string, string>): FieldSpec[] {
  return PROVIDER_FIELD_SPECS[kind].secret.filter((field) =>
    kind !== 'onedrive' || oneDriveAuthMode(config) !== 'openlist' || field.key !== 'client_secret',
  );
}

/** 某类型端点的 config 初始值（合同默认值；留空表示由服务端取默认）。 */
export function configDefaultsForKind(kind: ProviderKind): Record<string, string> {
  const config: Record<string, string> = {};
  for (const field of PROVIDER_FIELD_SPECS[kind].config) {
    config[field.key] = field.defaultValue ?? '';
  }
  return config;
}

/** 校验表单。mode=edit 时，secret 全部留空代表保留原凭据。 */
export function validateProviderForm(values: ProviderFormValues, mode: 'create' | 'edit'): ProviderValidation {
  const errors: Record<string, string> = {};
  const routeMode = values.route_mode ?? 'pool';
  const routes = values.routes ?? [];

  if (isBlank(values.name)) errors.name = '请输入网盘名称';
  else if (values.name.trim().length > 128) errors.name = '名称不能超过 128 个字符';

  if (!Number.isInteger(values.max_file_mib) || values.max_file_mib < 0) {
    errors.max_file_mib = '单文件上限必须为不小于 0 的整数（0 表示不限）';
  }
  if (!Number.isInteger(values.read_order) || values.read_order < 0 || values.read_order > 1000) {
    errors.read_order = '读取顺序取值为 0 至 1000，数字越小越优先';
  }
  if ((values.kind === 'lanzou' || values.kind === '123pan') && values.on_path_change !== 'keep') {
    errors.on_path_change = '该网盘不支持移动，请选择保持原位';
  }
  if (values.kind === 'local' && (routeMode !== 'pool' || routes.length > 0)) {
    errors.routes = '本地网盘只能由主端执行';
  }
  if (values.kind !== 'local' && routeMode !== 'pool') {
    if (routes.length === 0) errors.routes = '请至少选择一个操作节点';
    const nodeIDs = routes.map((route) => route.node_id).filter(Boolean);
    if (new Set(nodeIDs).size !== nodeIDs.length) errors.routes = '同一个节点只能出现一次';
    const priorities = routes.map((route) => route.priority);
    if (priorities.some((priority) => !Number.isInteger(priority) || priority < 1 || priority > 1000) || new Set(priorities).size !== priorities.length) {
      errors.routes = '优先级必须是 1 到 1000 的不重复整数';
    }
  }
  if (values.kind === 'lanzou') {
    if (/[/\\]/.test(values.root_path.trim())) {
      errors.root_path = '蓝奏云根目录应填写文件夹 ID，不能包含斜杠';
    }
  } else if (values.root_path.trim()) {
    for (const segment of values.root_path.trim().split('/')) {
      if (!segment || segment === '.' || segment === '..' || /[/\\:*?"<>|]/.test(segment)) {
        errors.root_path = '根目录必须为安全的相对路径';
        break;
      }
    }
  }

  for (const field of visibleConfigFields(values.kind, values.config)) {
    const raw = values.config[field.key] ?? '';
    if (isBlank(raw)) {
      if (field.required) errors[`config.${field.key}`] = `请输入${field.label}`;
      continue;
    }
    const message = field.validate?.(raw.trim()) ?? null;
    if (message) errors[`config.${field.key}`] = message;
  }

  const secretFields = visibleSecretFields(values.kind, values.config);
  const secretTouched = secretFields.some((field) => !isBlank(values.secret[field.key]));
  const requireSecret = mode === 'create' || secretTouched;
  if (requireSecret) {
    for (const field of secretFields) {
      if (field.required && isBlank(values.secret[field.key])) {
        errors[`secret.${field.key}`] = mode === 'edit' ? `更换凭据时需重新填写${field.label}` : `请输入${field.label}`;
      }
    }
  }

  return { errors, valid: Object.keys(errors).length === 0 };
}

function collectSecret(kind: ProviderKind, config: Record<string, string>, source: Record<string, string>): Record<string, string> {
  const secret: Record<string, string> = {};
  for (const field of visibleSecretFields(kind, config)) {
    const raw = source[field.key] ?? '';
    // 凭据保留用户输入的原始值（包括可能的空格）；全空白视为未填写。
    if (!isBlank(raw)) secret[field.key] = raw;
  }
  return secret;
}

function collectConfig(kind: ProviderKind, source: Record<string, string>): Record<string, string> {
  const config: Record<string, string> = {};
  for (const field of visibleConfigFields(kind, source)) {
    const raw = (source[field.key] ?? '').trim();
    if (raw !== '') config[field.key] = raw;
  }
  return config;
}

/**
 * 构造创建载荷。config 按字段表收集（留空的合同默认字段省略，由服务端取默认）；secret 必填项全部提交。
 */
export function buildCreatePayload(values: ProviderFormValues): ProviderDraft {
  return {
    name: values.name.trim(),
    kind: values.kind,
    enabled: values.enabled,
    root_path: values.root_path.trim(),
    max_file_bytes: Math.round(values.max_file_mib * 1024 * 1024),
    allowed_exts: values.allowed_exts.map((ext) => ext.trim().toLowerCase()).filter((ext) => ext !== ''),
    read_order: values.read_order,
    on_path_change: values.on_path_change,
    route_mode: values.route_mode ?? 'pool',
    routes: values.routes ?? [],
    config: collectConfig(values.kind, values.config),
    secret: collectSecret(values.kind, values.config, values.secret),
  };
}

/**
 * 构造 PATCH 载荷（不含 kind）。
 * - config 出现即整体替换，因此始终提交完整的字段表配置。
 * - secret：未填写则省略（保留原凭据）；需要更换时须整体重新填写；clearSecret 时提交空对象清空。
 */
export function buildPatchPayload(values: ProviderFormValues): ProviderPatch {
  const patch: ProviderPatch = {
    name: values.name.trim(),
    enabled: values.enabled,
    root_path: values.root_path.trim(),
    max_file_bytes: Math.round(values.max_file_mib * 1024 * 1024),
    allowed_exts: values.allowed_exts.map((ext) => ext.trim().toLowerCase()).filter((ext) => ext !== ''),
    read_order: values.read_order,
    on_path_change: values.on_path_change,
    route_mode: values.route_mode ?? 'pool',
    routes: values.routes ?? [],
    config: collectConfig(values.kind, values.config),
  };
  if (values.clearSecret) {
    patch.secret = {};
  } else if (visibleSecretFields(values.kind, values.config).some((field) => !isBlank(values.secret[field.key]))) {
    patch.secret = collectSecret(values.kind, values.config, values.secret);
  }
  return patch;
}

/** 编辑表单初始值：config 回填，secret 永远为空（服务端不返回）。 */
export function initialProviderValues(drive: Drive | null): ProviderFormValues {
  const kind: ProviderKind = drive?.kind ?? 'local';
  const spec = PROVIDER_FIELD_SPECS[kind];
  const config: Record<string, string> = {};
  for (const field of spec.config) {
    config[field.key] = drive?.config[field.key] ?? field.defaultValue ?? '';
  }
  if (drive?.kind === 'onedrive' && !drive.config.auth_mode) config.auth_mode = 'direct';
  return {
    name: drive?.name ?? '',
    kind,
    enabled: drive?.enabled ?? true,
    root_path: drive?.root_path ?? '',
    max_file_mib: drive ? Math.round(drive.max_file_bytes / 1024 / 1024) : 0,
    allowed_exts: drive?.allowed_exts ?? [],
    read_order: drive?.read_order ?? 100,
    on_path_change: drive?.on_path_change ?? (kind === 'lanzou' || kind === '123pan' ? 'keep' : 'move'),
    route_mode: drive?.route_mode ?? 'pool',
    routes: drive?.routes?.map((route) => ({ node_id: route.node_id, priority: route.priority, enabled: route.enabled })) ?? [],
    config,
    secret: {},
    clearSecret: false,
  };
}
