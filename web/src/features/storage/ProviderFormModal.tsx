import { useEffect, useState } from 'react';
import type { FormInstance } from 'antd';
import { App, Checkbox, Form, Input, InputNumber, Modal, Select, Switch } from 'antd';
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
  validateProviderForm,
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
              {spec.config.map((field) => (
                <Form.Item
                  key={field.key}
                  name={fieldName(field)}
                  label={field.label}
                  rules={field.required ? [{ required: true, whitespace: true, message: `请输入${field.label}` }] : undefined}
                >
                  {field.input === 'select' ? (
                    <Select allowClear options={field.options} placeholder={field.placeholder ?? '默认'} />
                  ) : (
                    <Input placeholder={field.placeholder} maxLength={2048} />
                  )}
                </Form.Item>
              ))}
            </div>
          </>
        ) : null}

        {spec.secret.length > 0 ? (
          <>
            <div className="mb-3 text-sm font-medium">凭据</div>
            <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
              {spec.secret.map((field) => (
                <Form.Item key={field.key} name={['secret', field.key]} label={field.label} required={field.required}>
                  <Input.Password
                    autoComplete="new-password"
                    disabled={clearSecret}
                    placeholder={editing && provider.secret_configured ? '留空保留' : field.required ? `请输入${field.label}` : '可选'}
                  />
                </Form.Item>
              ))}
            </div>
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
