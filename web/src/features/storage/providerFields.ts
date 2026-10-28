import { PROVIDER_KINDS, type ProviderKind, type ProviderRole, type StorageProvider } from '../../shared/api/types';

// 存储端点 config / secret 字段表，严格对应 docs/API.md「POST /storage/providers」的适配器字段表。
// 不得增加合同之外的字段；secret 永不回显，编辑时留空表示保留原值。

export type FieldInput = 'text' | 'password' | 'select';

export interface FieldOption {
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

export function validateHttpUrl(value: string): string | null {
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

export function validateInteger(min: number, max: number): (value: string) => string | null {
  return (value) => {
    if (!/^-?\d+$/.test(value.trim())) return '请输入整数';
    const number = Number(value.trim());
    if (number < min || number > max) return `取值范围为 ${min} 至 ${max}`;
    return null;
  };
}

export function validateBooleanText(value: string): string | null {
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
        key: 'path_prefix',
        label: '路径前缀',
        required: false,
        placeholder: 'paper-collect',
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
        key: 'folder_id',
        label: '目录 ID',
        required: false,
        defaultValue: '-1',
        placeholder: '-1',
        validate: validateInteger(-1, Number.MAX_SAFE_INTEGER),
      },
      {
        key: 'verify_remote_hash',
        label: '远端 SHA 校验',
        required: false,
        input: 'select',
        options: [
          { value: '', label: '默认（false）' },
          { value: 'true', label: 'true（下载重算 SHA）' },
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
};

export const PROVIDER_KIND_OPTIONS = PROVIDER_KINDS.map((kind) => ({
  value: kind,
  label: PROVIDER_FIELD_SPECS[kind].label,
}));

export interface ProviderDraft {
  name: string;
  kind: ProviderKind;
  role: ProviderRole;
  enabled: boolean;
  priority: number;
  weight: number;
  config: Record<string, string>;
  secret?: Record<string, string>;
}

/** PATCH 不允许修改 kind。 */
export interface ProviderPatch {
  name: string;
  role: ProviderRole;
  enabled: boolean;
  priority: number;
  weight: number;
  config: Record<string, string>;
  secret?: Record<string, string>;
}

export interface ProviderFormValues {
  name: string;
  kind: ProviderKind;
  role: ProviderRole;
  enabled: boolean;
  priority: number;
  weight: number;
  config: Record<string, string>;
  secret: Record<string, string>;
  /** 编辑时是否显式清空已保存凭据（发送空对象 secret）。 */
  clearSecret?: boolean;
}

export interface ProviderValidation {
  errors: Record<string, string>;
  valid: boolean;
}

function isBlank(value: string | undefined): boolean {
  return value === undefined || value.trim() === '';
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
  const spec = PROVIDER_FIELD_SPECS[values.kind];
  const errors: Record<string, string> = {};

  if (isBlank(values.name)) errors.name = '请输入端点名称';
  else if (values.name.trim().length > 128) errors.name = '名称不能超过 128 个字符';

  if (!Number.isInteger(values.priority) || values.priority < 0 || values.priority > 1000) {
    errors.priority = '优先级取值为 0 至 1000，数字越小越优先';
  }
  if (!Number.isInteger(values.weight) || values.weight < 1 || values.weight > 1000) {
    errors.weight = '权重取值为 1 至 1000';
  }

  for (const field of spec.config) {
    const raw = values.config[field.key] ?? '';
    if (isBlank(raw)) {
      if (field.required) errors[`config.${field.key}`] = `请输入${field.label}`;
      continue;
    }
    const message = field.validate?.(raw.trim()) ?? null;
    if (message) errors[`config.${field.key}`] = message;
  }

  const secretTouched = spec.secret.some((field) => !isBlank(values.secret[field.key]));
  const requireSecret = mode === 'create' || secretTouched;
  if (requireSecret) {
    for (const field of spec.secret) {
      if (field.required && isBlank(values.secret[field.key])) {
        errors[`secret.${field.key}`] = mode === 'edit' ? `更换凭据时需重新填写${field.label}` : `请输入${field.label}`;
      }
    }
  }

  return { errors, valid: Object.keys(errors).length === 0 };
}

function collectSecret(spec: KindSpec, source: Record<string, string>): Record<string, string> {
  const secret: Record<string, string> = {};
  for (const field of spec.secret) {
    const raw = source[field.key] ?? '';
    // 凭据保留用户输入的原始值（包括可能的空格）；全空白视为未填写。
    if (!isBlank(raw)) secret[field.key] = raw;
  }
  return secret;
}

function collectConfig(spec: KindSpec, source: Record<string, string>): Record<string, string> {
  const config: Record<string, string> = {};
  for (const field of spec.config) {
    const raw = (source[field.key] ?? '').trim();
    if (raw !== '') config[field.key] = raw;
  }
  return config;
}

/**
 * 构造创建载荷。config 按字段表收集（留空的合同默认字段省略，由服务端取默认）；secret 必填项全部提交。
 */
export function buildCreatePayload(values: ProviderFormValues): ProviderDraft {
  const spec = PROVIDER_FIELD_SPECS[values.kind];
  return {
    name: values.name.trim(),
    kind: values.kind,
    role: values.role,
    enabled: values.enabled,
    priority: values.priority,
    weight: values.weight,
    config: collectConfig(spec, values.config),
    secret: collectSecret(spec, values.secret),
  };
}

/**
 * 构造 PATCH 载荷（不含 kind）。
 * - config 出现即整体替换，因此始终提交完整的字段表配置。
 * - secret：未填写则省略（保留原凭据）；需要更换时须整体重新填写；clearSecret 时提交空对象清空。
 */
export function buildPatchPayload(values: ProviderFormValues): ProviderPatch {
  const spec = PROVIDER_FIELD_SPECS[values.kind];
  const patch: ProviderPatch = {
    name: values.name.trim(),
    role: values.role,
    enabled: values.enabled,
    priority: values.priority,
    weight: values.weight,
    config: collectConfig(spec, values.config),
  };
  if (values.clearSecret) {
    patch.secret = {};
  } else if (spec.secret.some((field) => !isBlank(values.secret[field.key]))) {
    patch.secret = collectSecret(spec, values.secret);
  }
  return patch;
}

/** 编辑表单初始值：config 回填，secret 永远为空（服务端不返回）。 */
export function initialProviderValues(provider: StorageProvider | null): ProviderFormValues {
  const kind: ProviderKind = provider?.kind ?? 'local';
  const spec = PROVIDER_FIELD_SPECS[kind];
  const config: Record<string, string> = {};
  for (const field of spec.config) {
    config[field.key] = provider?.config[field.key] ?? field.defaultValue ?? '';
  }
  return {
    name: provider?.name ?? '',
    kind,
    role: provider?.role ?? 'backup',
    enabled: provider?.enabled ?? true,
    priority: provider?.priority ?? 10,
    weight: provider?.weight ?? 1,
    config,
    secret: {},
    clearSecret: false,
  };
}
