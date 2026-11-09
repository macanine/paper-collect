import { useEffect, useState } from 'react';
import type { FormInstance } from 'antd';
import { Alert, App, Button, Checkbox, Form, Input, InputNumber, Modal, Select, Space, Switch } from 'antd';
import type { ClusterNode, Drive, DriveRouteMode, OnPathChange, ProviderKind } from '../../shared/api/types';
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
  type DriveRouteDraft,
  type ProviderFormValues,
} from './providerFields';
import { useCreateDrive, useUpdateDrive } from './queries';

interface ProviderFormModalProps {
  open: boolean;
  drive?: Drive;
  onClose: () => void;
  onSaved?: (drive: Drive) => void;
  nodes?: ClusterNode[];
  canRoute?: boolean;
}

const ON_PATH_CHANGE_OPTIONS: { value: OnPathChange; label: string }[] = [
  { value: 'move', label: '随之移动（远端文件跟着改名/改目录）' },
  { value: 'keep', label: '保持原位（只更新期望路径，不移动远端文件）' },
];

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

export function ProviderFormModal({ open, drive, onClose, onSaved, nodes = [], canRoute = false }: ProviderFormModalProps) {
  const [form] = Form.useForm<ProviderFormValues>();
  const { message } = App.useApp();
  const create = useCreateDrive();
  const update = useUpdateDrive();
  const editing = drive !== undefined;
  const [submitting, setSubmitting] = useState(false);

  const watchedKind = Form.useWatch('kind', form) as ProviderKind | undefined;
  const kind = watchedKind ?? drive?.kind ?? 'local';
  const spec = PROVIDER_FIELD_SPECS[kind];
  const clearSecret = Boolean(Form.useWatch('clearSecret', form));
  const watchedConfig = Form.useWatch('config', form) as Record<string, string> | undefined;
  const config = watchedConfig ?? (drive?.kind === kind ? initialProviderValues(drive).config : configDefaultsForKind(kind));
  const watchedRoutes = Form.useWatch('routes', form) as DriveRouteDraft[] | undefined;
  const oneDriveMode = oneDriveAuthMode(config);
  const canMove = kind !== 'lanzou' && kind !== '123pan';
  const routes = watchedRoutes ?? [];

  useEffect(() => {
    if (!open) return;
    form.resetFields();
    form.setFieldsValue(initialProviderValues(drive ?? null));
  }, [open, drive, form]);

  const handleKindChange = (nextKind: ProviderKind) => {
    form.setFieldsValue({
      config: configDefaultsForKind(nextKind),
      secret: {},
      clearSecret: false,
      on_path_change: nextKind === 'lanzou' || nextKind === '123pan' ? 'keep' : 'move',
    });
  };

  const handleOk = async () => {
    const values = await form.validateFields();
    const normalized: ProviderFormValues = {
      ...values,
      config: values.config ?? {},
      secret: values.secret ?? {},
      allowed_exts: values.allowed_exts ?? [],
      clearSecret: Boolean(values.clearSecret),
    };
    const validation = validateProviderForm(normalized, editing ? 'edit' : 'create');
    if (drive?.kind === 'onedrive' && oneDriveAuthMode(drive.config) !== oneDriveAuthMode(normalized.config) && !normalized.secret.refresh_token?.trim()) {
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
      if (drive) {
        const saved = await update.mutateAsync({ id: drive.id, patch: buildPatchPayload(normalized) });
        message.success('网盘已保存');
        onSaved?.(saved);
      } else {
        const saved = await create.mutateAsync(buildCreatePayload(normalized));
        message.success('网盘已创建');
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
      title={drive ? `编辑网盘：${drive.name}` : '新建网盘'}
      open={open}
      onOk={() => void handleOk()}
      onCancel={onClose}
      okText="保存"
      cancelText="取消"
      confirmLoading={submitting}
      destroyOnHidden
      width={720}
      mask={{ closable: !submitting }}
    >
      <Form<ProviderFormValues> form={form} layout="vertical" requiredMark="optional" disabled={submitting}>
        {kind === 'onedrive' ? <Alert className="mb-4" type="info" showIcon title="连接国际版 OneDrive" description="共享文件夹须对授权账号开放写入；不填链接时使用自己的 OneDrive。" /> : null}
        {kind === 's3' ? <Alert className="mb-4" type="info" showIcon title="分享链接在设定时间后失效，可在资料详情中重新生成。" /> : null}
        <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <Form.Item
            name="name"
            label="名称"
            rules={[
              { required: true, whitespace: true, message: '请输入网盘名称' },
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
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="停用" />
          </Form.Item>
          <Form.Item name="read_order" label="读取顺序" tooltip="本机原件缺失时，按此顺序从各网盘拉取，数字越小越优先">
            <InputNumber min={0} max={1000} precision={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="root_path"
            label={kind === 'lanzou' ? '根目录（文件夹 ID）' : '根目录'}
            tooltip={kind === 'lanzou' ? '蓝奏云不支持子目录，填写文件夹 ID，-1 为根目录' : '本系统在该网盘中使用的根目录，规则中的路径模板都相对于它'}
          >
            <Input maxLength={512} placeholder={kind === 'lanzou' ? '-1' : '留空为网盘根目录，例如 paper-collect'} />
          </Form.Item>
          <Form.Item name="max_file_mib" label="单文件上限（MiB）" tooltip="0 表示不限；超出上限的文件对该网盘记为跳过，不算失败">
            <InputNumber min={0} max={1048576} precision={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="allowed_exts" label="允许的扩展名" tooltip="留空表示不限；例如 .pdf">
            <Select mode="tags" open={false} suffixIcon={null} placeholder="留空表示不限" tokenSeparators={[',', ' ', '；', ';']} />
          </Form.Item>
          <Form.Item name="on_path_change" label="路径变化时" tooltip="改名或编目变化导致期望路径改变时的处理方式">
            <Select options={ON_PATH_CHANGE_OPTIONS.filter((option) => canMove || option.value === 'keep')} />
          </Form.Item>
        </div>

        {canRoute && kind !== 'local' ? (
          <div className="mb-5 rounded-md border border-solid border-slate-200 bg-gray-50 p-4">
            <div className="mb-3 text-sm font-semibold">上传节点路由</div>
            <div className="mb-3 text-xs text-slate-500">优先级数字越小越先上传。首选节点不可用时，优先策略会自动切换到下一个节点；独占策略会保持等待。</div>
            <Form.Item name="route_mode" label="分配策略">
              <Select<DriveRouteMode>
                options={[
                  { value: 'pool', label: '自动分配（所有可用节点）' },
                  { value: 'preferred', label: '按优先级，故障时自动切换' },
                  { value: 'exclusive', label: '仅使用指定节点' },
                ]}
              />
            </Form.Item>
            <Form.List name="routes">
              {(fields, { add, remove }) => (
                <div className="flex flex-col gap-2">
                  {fields.map((field) => {
                    const selected = routes[field.name]?.node_id;
                    return (
                      <Space key={field.key} align="start" className="w-full">
                        <Form.Item
                          {...field}
                          name={[field.name, 'node_id']}
                          rules={[{ required: true, message: '请选择节点' }]}
                          className="mb-0 min-w-0 flex-1"
                        >
                          <Select
                            placeholder="选择主端或从端"
                            options={nodes.map((node) => ({
                              value: node.id,
                              label: `${node.name}${node.is_master ? '（主端）' : ''}`,
                              disabled: !node.enabled,
                            }))}
                            optionFilterProp="label"
                            showSearch
                          />
                        </Form.Item>
                        <Form.Item
                          {...field}
                          name={[field.name, 'priority']}
                          rules={[{ required: true, type: 'number', min: 1, max: 1000, message: '1-1000' }]}
                          className="mb-0 w-28"
                        >
                          <InputNumber min={1} max={1000} precision={0} className="w-full" placeholder="优先级" />
                        </Form.Item>
                        <Button type="text" danger onClick={() => remove(field.name)} aria-label={`移除 ${selected || '节点'}路由`}>移除</Button>
                      </Space>
                    );
                  })}
                  <Button type="dashed" onClick={() => add({ node_id: '', priority: fields.length + 1, enabled: true })} disabled={fields.length >= nodes.length}>
                    添加节点
                  </Button>
                </div>
              )}
            </Form.List>
          </div>
        ) : null}

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
            title={oneDriveMode === 'openlist' ? '使用现成应用获取刷新令牌' : '使用自己的微软应用'}
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
                    placeholder={editing && drive.secret_configured ? '留空保留' : field.required ? `请输入${field.label}` : '可选'}
                  />
                </Form.Item>
              ))}
            </div>
            {kind === 'onedrive' && editing && drive.secret_configured ? <p className="text-xs text-slate-500">更换凭据时，请同时重新填写仍需使用的应用密钥和共享文件夹链接；留空的旧凭据不会保留。</p> : null}
            {editing && drive.secret_configured ? (
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
