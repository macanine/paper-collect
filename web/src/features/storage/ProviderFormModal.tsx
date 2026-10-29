import { useEffect, useState } from 'react';
import type { FormInstance } from 'antd';
import { Alert, App, Checkbox, Form, Input, InputNumber, Modal, Select, Switch } from 'antd';
import type { ProviderKind, ProviderRole, StorageProvider } from '../../shared/api/types';
import { PROVIDER_ROLE_LABELS } from '../../shared/lib/labels';
import { errorMessage } from '../../shared/api/errors';
import {
  PROVIDER_FIELD_SPECS,
  PROVIDER_KIND_OPTIONS,
  buildCreatePayload,
  buildPatchPayload,
  configDefaultsForKind,
  initialProviderValues,
  oneDriveAuthMode,
  validateProviderForm,
  visibleConfigFields,
  visibleSecretFields,
  type FieldSpec,
  type ProviderFormValues,
} from './providerFields';
import { useCreateProvider, useUpdateProvider } from './queries';

interface ProviderFormModalProps {
  open: boolean;
  provider?: StorageProvider;
  onClose: () => void;
  onSaved?: (provider: StorageProvider) => void;
}

const ROLE_OPTIONS = (Object.keys(PROVIDER_ROLE_LABELS) as ProviderRole[]).map((role) => ({
  value: role,
  label: PROVIDER_ROLE_LABELS[role],
}));

function fieldName(field: FieldSpec): (string | number)[] {
  return ['config', field.key];
}

function applyFieldErrors(form: FormInstance<ProviderFormValues>, errors: Record<string, string>) {
  const entries = Object.entries(errors);
  if (entries.length === 0) return;
  form.setFields(
    entries.map(([name, message]) => ({
      name: name.split('.'),
      errors: [message],
    })) as unknown as Parameters<typeof form.setFields>[0],
  );
}

export function ProviderFormModal({ open, provider, onClose, onSaved }: ProviderFormModalProps) {
  const [form] = Form.useForm<ProviderFormValues>();
  const { message } = App.useApp();
  const create = useCreateProvider();
  const update = useUpdateProvider();
  const editing = provider !== undefined;
  const [submitting, setSubmitting] = useState(false);

  const watchedKind = Form.useWatch('kind', form) as ProviderKind | undefined;
  const kind = watchedKind ?? provider?.kind ?? 'local';
  const spec = PROVIDER_FIELD_SPECS[kind];
  const clearSecret = Boolean(Form.useWatch('clearSecret', form));
  const watchedConfig = Form.useWatch('config', form) as Record<string, string> | undefined;
  const config = watchedConfig ?? (provider?.kind === kind ? initialProviderValues(provider).config : configDefaultsForKind(kind));
  const oneDriveMode = oneDriveAuthMode(config);

  useEffect(() => {
    if (!open) return;
    form.resetFields();
    form.setFieldsValue(initialProviderValues(provider ?? null));
  }, [open, provider, form]);

  const handleKindChange = (nextKind: ProviderKind) => {
    form.setFieldsValue({
      config: configDefaultsForKind(nextKind),
      secret: {},
      clearSecret: false,
    });
  };

  const handleOk = async () => {
    const values = await form.validateFields();
    const normalized: ProviderFormValues = {
      ...values,
      config: values.config ?? {},
      secret: values.secret ?? {},
      clearSecret: Boolean(values.clearSecret),
    };
    const validation = validateProviderForm(normalized, editing ? 'edit' : 'create');
    if (provider?.kind === 'onedrive' && oneDriveAuthMode(provider.config) !== oneDriveAuthMode(normalized.config) && !normalized.secret.refresh_token?.trim()) {
      validation.errors['secret.refresh_token'] = '切换授权方式时，请填写对应的刷新令牌';
      validation.valid = false;
    }
    if (!validation.valid) {
      applyFieldErrors(form, validation.errors);
      message.error('请修正表单中标红的问题');
      return;
    }

    setSubmitting(true);
    try {
      if (provider) {
        const saved = await update.mutateAsync({ id: provider.id, patch: buildPatchPayload(normalized) });
        message.success('存储端点已保存');
        onSaved?.(saved);
      } else {
        const saved = await create.mutateAsync(buildCreatePayload(normalized));
        message.success('存储端点已创建');
        onSaved?.(saved);
      }
      onClose();
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title={provider ? `编辑存储端点：${provider.name}` : '新建存储端点'}
      open={open}
      onOk={() => void handleOk()}
      onCancel={onClose}
      okText="保存"
      cancelText="取消"
      confirmLoading={submitting}
      destroyOnHidden
      width={720}
      maskClosable={!submitting}
    >
      <Form<ProviderFormValues> form={form} layout="vertical" requiredMark="optional" disabled={submitting}>
        {kind === 'onedrive' ? <Alert className="mb-4" type="info" showIcon message="连接国际版 OneDrive" description="共享文件夹须对授权账号开放写入；不填链接时使用自己的 OneDrive。" /> : null}
        {kind === 's3' ? <Alert className="mb-4" type="info" showIcon message="分享链接在设定时间后失效，可在资料详情中重新生成。" /> : null}
        <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <Form.Item
            name="name"
            label="名称"
            rules={[
              { required: true, whitespace: true, message: '请输入端点名称' },
              { max: 128, message: '名称不能超过 128 个字符' },
            ]}
          >
            <Input maxLength={128} placeholder="例如：学校 WebDAV" />
          </Form.Item>
          <Form.Item name="kind" label="类型" rules={[{ required: true, message: '请选择类型' }]}>
            <Select
              options={PROVIDER_KIND_OPTIONS}
              disabled={editing}
              onChange={(nextKind: ProviderKind) => handleKindChange(nextKind)}
            />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true, message: '请选择角色' }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="停用" />
          </Form.Item>
          <Form.Item name="priority" label="优先级">
            <InputNumber min={0} max={1000} precision={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="weight" label="权重">
            <InputNumber min={1} max={1000} precision={0} style={{ width: '100%' }} />
          </Form.Item>
        </div>

        {spec.config.length > 0 ? (
          <>
            <div className="mb-3 text-sm font-medium">配置</div>
            <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
              {visibleConfigFields(kind, config).map((field) => (
                <Form.Item
                  key={field.key}
                  name={fieldName(field)}
                  label={field.label}
                  rules={field.required ? [{ required: true, whitespace: true, message: `请输入${field.label}` }] : undefined}
                >
                  {field.input === 'select' ? (
                    <Select allowClear={field.key !== 'auth_mode'} options={field.options} placeholder={field.placeholder ?? '默认'} />
                  ) : (
                    <Input placeholder={field.placeholder} maxLength={2048} />
                  )}
                </Form.Item>
              ))}
            </div>
          </>
        ) : null}

        {kind === 'onedrive' ? (
          <Alert
            className="mb-4"
            type={oneDriveMode === 'openlist' ? 'warning' : 'info'}
            showIcon
            message={oneDriveMode === 'openlist' ? '使用现成应用获取刷新令牌' : '使用自己的微软应用'}
            description={oneDriveMode === 'openlist' ? (
              <div className="flex flex-col gap-2">
                <span>打开 OpenList 授权页面，选择 OneDrive 个人版或企业版，勾选“使用 OpenList 提供的参数”，点击“获取 Token”并登录微软账号。将页面中的刷新令牌粘贴到下方。</span>
                <a href="https://api.oplist.org/" target="_blank" rel="noopener noreferrer">打开 OpenList 获取令牌</a>
                <span>此方式使用 OpenList 第三方应用，授权时请求访问你有权访问的文件。每次续期会通过 OpenList 服务传递刷新令牌，该服务接口将令牌放在 URL 参数中。</span>
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                <span>在微软注册应用，授予 Files.ReadWrite 和 offline_access；使用该应用完成授权后，把刷新令牌填入下方。</span>
                <a href="https://learn.microsoft.com/zh-cn/graph/auth-v2-user" target="_blank" rel="noopener noreferrer">查看微软授权步骤</a>
              </div>
            )}
          />
        ) : null}

        {spec.secret.length > 0 ? (
          <>
            <div className="mb-3 text-sm font-medium">凭据</div>
            <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
              {visibleSecretFields(kind, config).map((field) => (
                <Form.Item key={field.key} name={['secret', field.key]} label={field.label} required={field.required}>
                  <Input.Password
                    autoComplete="new-password"
                    disabled={clearSecret}
                    placeholder={editing && provider.secret_configured ? '留空保留' : field.required ? `请输入${field.label}` : '可选'}
                  />
                </Form.Item>
              ))}
            </div>
            {kind === 'onedrive' && editing && provider.secret_configured ? <p className="text-xs text-slate-500">更换凭据时，请同时重新填写仍需使用的应用密钥和共享文件夹链接；留空的旧凭据不会保留。</p> : null}
            {editing && provider.secret_configured ? (
              <Form.Item name="clearSecret" valuePropName="checked">
                <Checkbox>清空已保存凭据</Checkbox>
              </Form.Item>
            ) : null}
          </>
        ) : null}
      </Form>
    </Modal>
  );
}
