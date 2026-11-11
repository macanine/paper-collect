import { useEffect, useState } from 'react';
import { ActionIcon, Alert, Anchor, Box, Button, Checkbox, Group, NumberInput, PasswordInput, Paper, Select, SimpleGrid, Stack, Switch, TagsInput, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconTrash } from '@tabler/icons-react';
import type { ClusterNode, Drive, DriveRouteMode, OnPathChange, ProviderKind } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { FormModal } from '../../shared/ui/FormModal';
import { message } from '../../shared/ui/feedback';
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

export function ProviderFormModal({ open, drive, onClose, onSaved, nodes = [], canRoute = false }: ProviderFormModalProps) {
  const form = useForm<ProviderFormValues>({ initialValues: initialProviderValues(drive ?? null) });
  const create = useCreateDrive();
  const update = useUpdateDrive();
  const editing = drive !== undefined;
  const [submitting, setSubmitting] = useState(false);

  const kind: ProviderKind = form.values.kind ?? drive?.kind ?? 'local';
  const spec = PROVIDER_FIELD_SPECS[kind];
  const clearSecret = Boolean(form.values.clearSecret);
  const config = form.values.config ?? (drive?.kind === kind ? initialProviderValues(drive).config : configDefaultsForKind(kind));
  const routes = form.values.routes ?? [];
  const oneDriveMode = oneDriveAuthMode(config);
  const canMove = kind !== 'lanzou' && kind !== '123pan';

  useEffect(() => {
    if (!open) return;
    const values = initialProviderValues(drive ?? null);
    form.setValues(values);
    form.resetDirty(values);
    form.clearErrors();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, drive]);

  const handleKindChange = (nextKind: ProviderKind) => {
    form.setValues({
      kind: nextKind,
      config: configDefaultsForKind(nextKind),
      secret: {},
      clearSecret: false,
      on_path_change: nextKind === 'lanzou' || nextKind === '123pan' ? 'keep' : 'move',
    });
  };

  const handleOk = async () => {
    const values = form.values;
    const normalized: ProviderFormValues = {
      ...values,
      config: values.config ?? {},
      secret: values.secret ?? {},
      allowed_exts: values.allowed_exts ?? [],
      clearSecret: Boolean(values.clearSecret),
    };
    const errors: Record<string, string> = {};
    if (!normalized.name.trim()) errors.name = '请输入网盘名称';
    else if (normalized.name.length > 128) errors.name = '名称不能超过 128 个字符';
    (normalized.routes ?? []).forEach((r, i) => {
      if (!r.node_id) errors[`routes.${i}.node_id`] = '请选择节点';
      if (typeof r.priority !== 'number' || r.priority < 1 || r.priority > 1000) errors[`routes.${i}.priority`] = '1-1000';
    });
    const validation = validateProviderForm(normalized, editing ? 'edit' : 'create');
    if (drive?.kind === 'onedrive' && oneDriveAuthMode(drive.config) !== oneDriveAuthMode(normalized.config) && !normalized.secret.refresh_token?.trim()) {
      validation.errors['secret.refresh_token'] = '切换授权方式时，请填写对应的刷新令牌';
      validation.valid = false;
    }
    Object.assign(errors, validation.errors);
    if (Object.keys(errors).length > 0) {
      form.setErrors(errors);
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
    <FormModal opened={open} onClose={onClose} title={drive ? `编辑网盘：${drive.name}` : '新建网盘'} size={720} loading={submitting} closeOnClickOutside={!submitting} onSubmit={(e) => { e?.preventDefault(); void handleOk(); }}>
      {kind === 'onedrive' ? <Alert color="blue" title="连接国际版 OneDrive">共享文件夹须对授权账号开放写入；不填链接时使用自己的 OneDrive。</Alert> : null}
      {kind === 's3' ? <Alert color="blue" title="分享链接在设定时间后失效，可在资料详情中重新生成。" /> : null}
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
        <TextInput label="名称" withAsterisk maxLength={128} placeholder="例如：学校 WebDAV" {...form.getInputProps('name')} />
        <Select label="类型" withAsterisk data={PROVIDER_KIND_OPTIONS} disabled={editing} value={kind} onChange={(v) => v && handleKindChange(v as ProviderKind)} />
        <Switch label="启用" {...form.getInputProps('enabled', { type: 'checkbox' })} />
        <NumberInput label="读取顺序" description="本机原件缺失时，按此顺序从各网盘拉取，数字越小越优先" min={0} max={1000} allowDecimal={false} {...form.getInputProps('read_order')} />
        <TextInput
          label={kind === 'lanzou' ? '根目录（文件夹 ID）' : '根目录'}
          description={kind === 'lanzou' ? '蓝奏云不支持子目录，填写文件夹 ID，-1 为根目录' : '本系统在该网盘中使用的根目录，规则中的路径模板都相对于它'}
          maxLength={512}
          placeholder={kind === 'lanzou' ? '-1' : '留空为网盘根目录，例如 paper-collect'}
          {...form.getInputProps('root_path')}
        />
        <NumberInput label="单文件上限（MiB）" description="0 表示不限；超出上限的文件对该网盘记为跳过，不算失败" min={0} max={1048576} allowDecimal={false} {...form.getInputProps('max_file_mib')} />
        <TagsInput label="允许的扩展名" description="留空表示不限；例如 .pdf" placeholder="留空表示不限" splitChars={[',', ' ', '；', ';']} {...form.getInputProps('allowed_exts')} />
        <Select label="路径变化时" description="改名或编目变化导致期望路径改变时的处理方式" allowDeselect={false} data={ON_PATH_CHANGE_OPTIONS.filter((o) => canMove || o.value === 'keep')} {...form.getInputProps('on_path_change')} />
      </SimpleGrid>

      {canRoute && kind !== 'local' ? (
        <Paper p="md" bg="gray.0">
          <Stack gap="sm">
            <Text fw={600} size="sm">操作节点路由</Text>
            <Text size="xs" c="dimmed">决定哪些从端上传到该网盘、直接从该网盘读取原件，并持有它的凭据。优先级数字越小越先使用；首选节点不可用时，优先策略自动切换到下一个节点，全部不可用时由任意节点处理；独占策略会保持等待。</Text>
            <Select
              label="分配策略"
              allowDeselect={false}
              data={[{ value: 'pool', label: '自动分配（所有可用节点）' }, { value: 'preferred', label: '按优先级，故障时自动切换' }, { value: 'exclusive', label: '仅使用指定节点' }]}
              value={form.values.route_mode ?? 'pool'}
              onChange={(v) => form.setFieldValue('route_mode', (v ?? 'pool') as DriveRouteMode)}
            />
            {routes.map((_, index) => (
              <Group key={index} gap="sm" align="flex-start" wrap="nowrap">
                <Select flex={1} searchable placeholder="选择主端或从端" data={nodes.map((n) => ({ value: n.id, label: `${n.name}${n.is_master ? '（主端）' : ''}`, disabled: !n.enabled }))} {...form.getInputProps(`routes.${index}.node_id`)} />
                <NumberInput w={110} min={1} max={1000} allowDecimal={false} placeholder="优先级" {...form.getInputProps(`routes.${index}.priority`)} />
                <ActionIcon mt={4} variant="subtle" color="red" aria-label="移除路由" onClick={() => form.removeListItem('routes', index)}><IconTrash size={16} /></ActionIcon>
              </Group>
            ))}
            <Box>
              <Button variant="default" disabled={routes.length >= nodes.length} onClick={() => form.insertListItem('routes', { node_id: '', priority: routes.length + 1, enabled: true })}>添加节点</Button>
            </Box>
          </Stack>
        </Paper>
      ) : null}

      {spec.config.length > 0 ? (
        <Stack gap="sm">
          <Text fw={600} size="sm">配置</Text>
          <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
            {visibleConfigFields(kind, config).map((field) =>
              field.input === 'select' ? (
                <Select key={field.key} label={field.label} withAsterisk={field.required} clearable={field.key !== 'auth_mode'} data={field.options ?? []} placeholder={field.placeholder ?? '默认'} {...form.getInputProps(`config.${field.key}`)} value={form.values.config?.[field.key] || null} onChange={(v) => form.setFieldValue(`config.${field.key}`, v ?? '')} />
              ) : (
                <TextInput key={field.key} label={field.label} withAsterisk={field.required} placeholder={field.placeholder} maxLength={2048} {...form.getInputProps(`config.${field.key}`)} />
              ),
            )}
          </SimpleGrid>
        </Stack>
      ) : null}

      {kind === 'onedrive' ? (
        <Alert color={oneDriveMode === 'openlist' ? 'yellow' : 'blue'} title={oneDriveMode === 'openlist' ? '使用现成应用获取刷新令牌' : '使用自己的微软应用'}>
          {oneDriveMode === 'openlist' ? (
            <Stack gap="xs">
              <Text size="sm">打开 OpenList 授权页面，选择 OneDrive 个人版或企业版，勾选“使用 OpenList 提供的参数”，点击“获取 Token”并登录微软账号。将页面中的刷新令牌粘贴到下方。</Text>
              <Anchor href="https://api.oplist.org/" target="_blank" rel="noopener noreferrer" size="sm">打开 OpenList 获取令牌</Anchor>
              <Text size="sm">此方式使用 OpenList 第三方应用，授权时请求访问你有权访问的文件。每次续期会通过 OpenList 服务传递刷新令牌，该服务接口将令牌放在 URL 参数中。</Text>
            </Stack>
          ) : (
            <Stack gap="xs">
              <Text size="sm">在微软注册应用，授予 Files.ReadWrite 和 offline_access；使用该应用完成授权后，把刷新令牌填入下方。</Text>
              <Anchor href="https://learn.microsoft.com/zh-cn/graph/auth-v2-user" target="_blank" rel="noopener noreferrer" size="sm">查看微软授权步骤</Anchor>
            </Stack>
          )}
        </Alert>
      ) : null}

      {spec.secret.length > 0 ? (
        <Stack gap="sm">
          <Text fw={600} size="sm">凭据</Text>
          <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
            {visibleSecretFields(kind, config).map((field) => (
              <PasswordInput key={field.key} label={field.label} withAsterisk={field.required} autoComplete="new-password" disabled={clearSecret} placeholder={editing && drive.secret_configured ? '留空保留' : field.required ? `请输入${field.label}` : '可选'} {...form.getInputProps(`secret.${field.key}`)} />
            ))}
          </SimpleGrid>
          {kind === 'onedrive' && editing && drive.secret_configured ? <Text size="xs" c="dimmed">更换凭据时，请同时重新填写仍需使用的应用密钥和共享文件夹链接；留空的旧凭据不会保留。</Text> : null}
          {editing && drive.secret_configured ? <Checkbox label="清空已保存凭据" {...form.getInputProps('clearSecret', { type: 'checkbox' })} /> : null}
        </Stack>
      ) : null}
    </FormModal>
  );
}
