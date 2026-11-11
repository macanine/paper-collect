import { Button, Checkbox, Group, MultiSelect, NumberInput, Paper, SimpleGrid, Stack, Select, TagsInput, Text, TextInput, ActionIcon } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconPlus, IconTrash } from '@tabler/icons-react';
import type { Drive, SyncRule, SyncTarget } from '../../shared/api/types';
import { FormModal } from '../../shared/ui/FormModal';
import { DOC_TYPE_OPTIONS, GRADE_OPTIONS, PART_OPTIONS, SUBJECT_OPTIONS, cohortOptions } from '../../shared/lib/options';
import { DEFAULT_PATH_TEMPLATE, TEMPLATE_VARIABLES, driveName } from './ruleUtils';

const MEDIA_TYPE_OPTIONS = [
  { value: 'application/pdf', label: 'PDF' },
  { value: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', label: 'Word（DOCX）' },
  { value: 'application/msword', label: 'Word（DOC）' },
  { value: 'image/jpeg', label: 'JPEG 图片' },
  { value: 'image/png', label: 'PNG 图片' },
  { value: 'audio/mpeg', label: 'MP3 音频' },
  { value: 'audio/mp4', label: 'M4A 音频' },
];

interface RuleFormValues {
  name: string;
  enabled: boolean;
  subjects: string[];
  doc_types: string[];
  grades: string[];
  cohorts: string[];
  regions: string[];
  tags: string[];
  parts: string[];
  media_types: string[];
  min_size_mb: number | string;
  max_size_mb: number | string;
  targets: SyncTarget[];
}

interface RuleFormModalProps {
  open: boolean;
  /** null 表示新建规则；编辑兜底规则时传入该规则。 */
  rule: SyncRule | null;
  drives: Drive[];
  onClose: () => void;
  onSave: (rule: SyncRule) => void;
}

function toFormValues(rule: SyncRule | null): RuleFormValues {
  const c = rule?.conditions ?? {};
  return {
    name: rule?.name ?? '',
    enabled: rule?.enabled ?? true,
    subjects: c.subjects ?? [],
    doc_types: c.doc_types ?? [],
    grades: c.grades ?? [],
    cohorts: (c.cohorts ?? []).map(String),
    regions: c.regions ?? [],
    tags: c.tags ?? [],
    parts: c.parts ?? [],
    media_types: c.media_types ?? [],
    min_size_mb: c.min_size ? Math.round(c.min_size / 1024 / 1024) : '',
    max_size_mb: c.max_size ? Math.round(c.max_size / 1024 / 1024) : '',
    targets: (rule?.targets ?? []).map((t) => ({ ...t })),
  };
}

function validateTemplate(value: string | undefined): string | null {
  if (!value || !value.trim()) return '请输入路径模板';
  if (value.startsWith('/') || value.includes('\\')) return '必须为相对路径，不能包含反斜杠';
  if (!value.trim().endsWith('{文件名}')) return '必须以 {文件名} 结尾';
  const known = new Set<string>(TEMPLATE_VARIABLES);
  for (const match of value.matchAll(/\{([^}]*)\}/g)) {
    const name = match[1] ?? '';
    if (!known.has(name)) return `未知变量 {${name}}`;
  }
  return null;
}

export function RuleFormModal({ open, rule, drives, onClose, onSave }: RuleFormModalProps) {
  const isDefault = rule?.is_default ?? false;
  const driveOptions = drives.filter((d) => d.kind !== '123pan');
  const form = useForm<RuleFormValues>({
    initialValues: toFormValues(rule),
    validate: {
      name: (v) => (!v.trim() ? '请输入规则名称' : v.length > 128 ? '名称不能超过 128 个字符' : null),
      targets: {
        drive_id: (v) => (v ? null : '请选择网盘'),
        path_template: validateTemplate,
      },
    },
  });

  const handleSubmit = form.onSubmit((values) => {
    const conditions: SyncRule['conditions'] = {};
    if (values.subjects.length) conditions.subjects = values.subjects as SyncRule['conditions']['subjects'];
    if (values.doc_types.length) conditions.doc_types = values.doc_types as SyncRule['conditions']['doc_types'];
    if (values.grades.length) conditions.grades = values.grades as SyncRule['conditions']['grades'];
    if (values.cohorts.length) conditions.cohorts = values.cohorts.map(Number);
    if (values.regions.length) conditions.regions = values.regions.map((v) => v.trim()).filter(Boolean);
    if (values.tags.length) conditions.tags = values.tags.map((v) => v.trim()).filter(Boolean);
    if (values.parts.length) conditions.parts = values.parts as SyncRule['conditions']['parts'];
    if (values.media_types.length) conditions.media_types = values.media_types;
    if (values.min_size_mb) conditions.min_size = Math.round(Number(values.min_size_mb) * 1024 * 1024);
    if (values.max_size_mb) conditions.max_size = Math.round(Number(values.max_size_mb) * 1024 * 1024);
    onSave({
      id: rule?.id ?? '',
      name: values.name.trim(),
      enabled: isDefault ? true : values.enabled,
      is_default: isDefault,
      conditions: isDefault ? {} : conditions,
      targets: values.targets.map((t) => ({ ...t, path_template: t.path_template.trim() })),
    });
  });

  const addTarget = () => {
    const used = new Set(form.values.targets.map((t) => t.drive_id));
    const drive = driveOptions.find((d) => !used.has(d.id));
    if (!drive) return;
    form.insertListItem('targets', { drive_id: drive.id, path_template: DEFAULT_PATH_TEMPLATE, share: false, required: false });
  };

  const multi = (label: string, key: keyof RuleFormValues, data: { value: string; label: string }[]) => (
    <MultiSelect label={label} clearable placeholder="不限" data={data} {...form.getInputProps(key)} />
  );

  return (
    <FormModal opened={open} onClose={onClose} title={rule ? (isDefault ? '编辑兜底规则' : `编辑规则：${rule.name}`) : '新建规则'} okText="确定" size={760} onSubmit={handleSubmit}>
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
        <TextInput label="规则名称" withAsterisk maxLength={128} placeholder="例如：数学试卷" disabled={isDefault} {...form.getInputProps('name')} />
        <Checkbox label="启用这条规则" mt={{ base: 0, md: 28 }} disabled={isDefault} {...form.getInputProps('enabled', { type: 'checkbox' })} />
      </SimpleGrid>

      {!isDefault ? (
        <Stack gap="sm">
          <Text fw={600} size="sm">匹配条件（同一字段内为「或」，字段之间为「且」；全部留空表示匹配所有文件）</Text>
          <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
            {multi('学科', 'subjects', SUBJECT_OPTIONS)}
            {multi('资料类型', 'doc_types', DOC_TYPE_OPTIONS)}
            {multi('年级', 'grades', GRADE_OPTIONS)}
            {multi('届别', 'cohorts', cohortOptions().map((o) => ({ value: String(o.value), label: o.label })))}
            {multi('部件', 'parts', PART_OPTIONS)}
            {multi('文件格式', 'media_types', MEDIA_TYPE_OPTIONS)}
            <TagsInput label="地区" placeholder="输入后回车，留空不限" splitChars={[',', ' ', '；', ';']} {...form.getInputProps('regions')} />
            <TagsInput label="标签" placeholder="输入后回车，留空不限" splitChars={[',', ' ', '；', ';']} {...form.getInputProps('tags')} />
            <NumberInput label="大小下限（MB）" min={0} allowDecimal={false} placeholder="不限" {...form.getInputProps('min_size_mb')} />
            <NumberInput label="大小上限（MB）" min={0} allowDecimal={false} placeholder="不限" {...form.getInputProps('max_size_mb')} />
          </SimpleGrid>
        </Stack>
      ) : (
        <Text size="sm" c="dimmed">兜底规则固定匹配所有未被前面规则命中的文件，不能停用或设置条件。</Text>
      )}

      <Stack gap="sm">
        <Group justify="space-between">
          <Text fw={600} size="sm">同步目标</Text>
          <Button size="compact-sm" variant="default" leftSection={<IconPlus size={14} />} disabled={driveOptions.length === 0} onClick={addTarget}>添加目标</Button>
        </Group>
        {driveOptions.length === 0 ? <Text size="sm" c="dimmed">还没有可用的网盘，请先在「网盘」标签页添加。</Text> : null}
        {form.values.targets.map((target, index) => {
          const drive = drives.find((d) => d.id === target.drive_id);
          return (
            <Paper key={index} p="sm">
              <Stack gap="xs">
                <SimpleGrid cols={{ base: 1, md: 3 }} spacing="sm">
                  <Select label="网盘" placeholder="选择网盘" data={driveOptions.map((d) => ({ value: d.id, label: d.name + (d.enabled ? '' : '（已停用）') }))} {...form.getInputProps(`targets.${index}.drive_id`)} />
                  <TextInput label="路径模板" maxLength={2048} placeholder={DEFAULT_PATH_TEMPLATE} style={{ gridColumn: 'span 2' }} {...form.getInputProps(`targets.${index}.path_template`)} />
                </SimpleGrid>
                <Group justify="space-between">
                  <Group gap="md">
                    <Checkbox label="分享" {...form.getInputProps(`targets.${index}.share`, { type: 'checkbox' })} />
                    <Checkbox label="必需" {...form.getInputProps(`targets.${index}.required`, { type: 'checkbox' })} />
                  </Group>
                  <ActionIcon variant="subtle" color="red" onClick={() => form.removeListItem('targets', index)} aria-label="删除目标"><IconTrash size={16} /></ActionIcon>
                </Group>
                {drive ? (
                  <Group gap="sm">
                    <Text size="xs" c="dimmed">目标网盘：{driveName(drives, drive.id)}</Text>
                    {!drive.capabilities.folders ? <Text size="xs" c="orange">该网盘不支持子目录，仅使用文件名部分</Text> : null}
                    {target.share && !drive.capabilities.share ? <Text size="xs" c="orange">该网盘不支持分享</Text> : null}
                  </Group>
                ) : null}
              </Stack>
            </Paper>
          );
        })}
        <Text size="sm" c="dimmed">可用变量：{TEMPLATE_VARIABLES.map((v) => `{${v}}`).join(' ')}</Text>
      </Stack>
    </FormModal>
  );
}
