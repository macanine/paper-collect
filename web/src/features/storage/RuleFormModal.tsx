import { Button, Checkbox, Form, Input, InputNumber, Modal, Select } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import type { Drive, SyncRule, SyncTarget } from '../../shared/api/types';
import {
  DOC_TYPE_OPTIONS,
  GRADE_OPTIONS,
  PART_OPTIONS,
  SUBJECT_OPTIONS,
  cohortOptions,
} from '../../shared/lib/options';
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
  cohorts: number[];
  regions: string[];
  tags: string[];
  parts: string[];
  media_types: string[];
  min_size_mb?: number | null;
  max_size_mb?: number | null;
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
    cohorts: c.cohorts ?? [],
    regions: c.regions ?? [],
    tags: c.tags ?? [],
    parts: c.parts ?? [],
    media_types: c.media_types ?? [],
    min_size_mb: c.min_size ? Math.round(c.min_size / 1024 / 1024) : null,
    max_size_mb: c.max_size ? Math.round(c.max_size / 1024 / 1024) : null,
    targets: (rule?.targets ?? []).map((t) => ({ ...t })),
  };
}

export function RuleFormModal({ open, rule, drives, onClose, onSave }: RuleFormModalProps) {
  const [form] = Form.useForm<RuleFormValues>();
  const isDefault = rule?.is_default ?? false;
  const driveOptions = drives.filter((d) => d.kind !== '123pan');

  const handleOk = async () => {
    const values = await form.validateFields();
    const conditions: SyncRule['conditions'] = {};
    if (values.subjects?.length) conditions.subjects = values.subjects as SyncRule['conditions']['subjects'];
    if (values.doc_types?.length) conditions.doc_types = values.doc_types as SyncRule['conditions']['doc_types'];
    if (values.grades?.length) conditions.grades = values.grades as SyncRule['conditions']['grades'];
    if (values.cohorts?.length) conditions.cohorts = values.cohorts;
    if (values.regions?.length) conditions.regions = values.regions.map((v) => v.trim()).filter(Boolean);
    if (values.tags?.length) conditions.tags = values.tags.map((v) => v.trim()).filter(Boolean);
    if (values.parts?.length) conditions.parts = values.parts as SyncRule['conditions']['parts'];
    if (values.media_types?.length) conditions.media_types = values.media_types;
    if (values.min_size_mb) conditions.min_size = Math.round(values.min_size_mb * 1024 * 1024);
    if (values.max_size_mb) conditions.max_size = Math.round(values.max_size_mb * 1024 * 1024);
    const saved: SyncRule = {
      id: rule?.id ?? '',
      name: values.name.trim(),
      enabled: isDefault ? true : values.enabled,
      is_default: isDefault,
      conditions: isDefault ? {} : conditions,
      targets: (values.targets ?? []).map((t) => ({ ...t, path_template: t.path_template.trim() })),
    };
    onSave(saved);
  };

  return (
    <Modal
      title={rule ? (isDefault ? '编辑兜底规则' : `编辑规则：${rule.name}`) : '新建规则'}
      open={open}
      onOk={() => void handleOk()}
      onCancel={onClose}
      okText="确定"
      cancelText="取消"
      destroyOnHidden
      width={760}
      afterOpenChange={(visible) => {
        if (visible) {
          form.resetFields();
          form.setFieldsValue(toFormValues(rule));
        }
      }}
    >
      <Form<RuleFormValues> form={form} layout="vertical" requiredMark="optional" initialValues={toFormValues(rule)}>
        <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <Form.Item name="name" label="规则名称" rules={[{ required: true, whitespace: true, message: '请输入规则名称' }, { max: 128, message: '名称不能超过 128 个字符' }]}>
            <Input maxLength={128} placeholder="例如：数学试卷" disabled={isDefault} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Checkbox disabled={isDefault}>启用这条规则</Checkbox>
          </Form.Item>
        </div>

        {!isDefault ? (
          <>
            <div className="mb-2 text-sm font-medium">匹配条件（同一字段内为「或」，字段之间为「且」；全部留空表示匹配所有文件）</div>
            <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
              <Form.Item name="subjects" label="学科">
                <Select mode="multiple" allowClear options={SUBJECT_OPTIONS} placeholder="不限" />
              </Form.Item>
              <Form.Item name="doc_types" label="资料类型">
                <Select mode="multiple" allowClear options={DOC_TYPE_OPTIONS} placeholder="不限" />
              </Form.Item>
              <Form.Item name="grades" label="年级">
                <Select mode="multiple" allowClear options={GRADE_OPTIONS} placeholder="不限" />
              </Form.Item>
              <Form.Item name="cohorts" label="届别">
                <Select mode="multiple" allowClear options={cohortOptions()} placeholder="不限" />
              </Form.Item>
              <Form.Item name="parts" label="部件">
                <Select mode="multiple" allowClear options={PART_OPTIONS} placeholder="不限" />
              </Form.Item>
              <Form.Item name="media_types" label="文件格式">
                <Select mode="multiple" allowClear options={MEDIA_TYPE_OPTIONS} placeholder="不限" />
              </Form.Item>
              <Form.Item name="regions" label="地区">
                <Select mode="tags" open={false} suffixIcon={null} placeholder="输入后回车，留空不限" tokenSeparators={[',', ' ', '；', ';']} />
              </Form.Item>
              <Form.Item name="tags" label="标签">
                <Select mode="tags" open={false} suffixIcon={null} placeholder="输入后回车，留空不限" tokenSeparators={[',', ' ', '；', ';']} />
              </Form.Item>
              <Form.Item name="min_size_mb" label="大小下限（MB）">
                <InputNumber min={0} precision={0} style={{ width: '100%' }} placeholder="不限" />
              </Form.Item>
              <Form.Item name="max_size_mb" label="大小上限（MB）">
                <InputNumber min={0} precision={0} style={{ width: '100%' }} placeholder="不限" />
              </Form.Item>
            </div>
          </>
        ) : (
          <p className="mb-4 text-sm text-slate-500">兜底规则固定匹配所有未被前面规则命中的文件，不能停用或设置条件。</p>
        )}

        <div className="mb-2 flex items-center justify-between">
          <span className="text-sm font-medium">同步目标</span>
          <Form.Item noStyle shouldUpdate>
            {({ getFieldValue, setFieldValue }) => (
              <Button
                size="small"
                icon={<PlusOutlined />}
                disabled={driveOptions.length === 0}
                onClick={() => {
                  const targets: SyncTarget[] = getFieldValue('targets') ?? [];
                  const used = new Set(targets.map((t) => t.drive_id));
                  const drive = driveOptions.find((d) => !used.has(d.id));
                  if (!drive) return;
                  setFieldValue('targets', [...targets, { drive_id: drive.id, path_template: DEFAULT_PATH_TEMPLATE, share: false, required: false }]);
                }}
              >
                添加目标
              </Button>
            )}
          </Form.Item>
        </div>
        {driveOptions.length === 0 ? <p className="mb-3 text-sm text-slate-500">还没有可用的网盘，请先在「网盘」标签页添加。</p> : null}
        <Form.List name="targets">
          {(fields, { remove }) => (
            <div className="flex flex-col gap-3">
              {fields.map((field) => (
                <div key={field.key} className="rounded-md border border-solid border-slate-200 p-3">
                  <div className="grid grid-cols-1 items-end gap-x-3 gap-y-1 md:grid-cols-[180px_1fr_auto]">
                    <Form.Item
                      name={[field.name, 'drive_id']}
                      label="网盘"
                      rules={[{ required: true, message: '请选择网盘' }]}
                      className="mb-2"
                    >
                      <Select
                        options={driveOptions.map((d) => ({ value: d.id, label: d.name + (d.enabled ? '' : '（已停用）') }))}
                        placeholder="选择网盘"
                      />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'path_template']}
                      label="路径模板"
                      className="mb-2"
                      rules={[
                        { required: true, whitespace: true, message: '请输入路径模板' },
                        {
                          validator: (_rule, value: string | undefined) => {
                            if (!value) return Promise.resolve();
                            if (value.startsWith('/') || value.includes('\\')) return Promise.reject(new Error('必须为相对路径，不能包含反斜杠'));
                            if (!value.trim().endsWith('{文件名}')) return Promise.reject(new Error('必须以 {文件名} 结尾'));
                            const known = new Set<string>(TEMPLATE_VARIABLES);
                            for (const match of value.matchAll(/\{([^}]*)\}/g)) {
                              const name = match[1] ?? '';
                              if (!known.has(name)) return Promise.reject(new Error(`未知变量 {${name}}`));
                            }
                            return Promise.resolve();
                          },
                        },
                      ]}
                    >
                      <Input maxLength={2048} placeholder={DEFAULT_PATH_TEMPLATE} />
                    </Form.Item>
                    <div className="mb-2 flex items-center gap-3">
                      <Form.Item name={[field.name, 'share']} valuePropName="checked" noStyle>
                        <Checkbox>分享</Checkbox>
                      </Form.Item>
                      <Form.Item name={[field.name, 'required']} valuePropName="checked" noStyle>
                        <Checkbox>必需</Checkbox>
                      </Form.Item>
                      <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)} aria-label="删除目标" />
                    </div>
                  </div>
                  <Form.Item noStyle shouldUpdate>
                    {({ getFieldValue }) => {
                      const targets: SyncTarget[] = getFieldValue('targets') ?? [];
                      const target = targets[field.name];
                      if (!target) return null;
                      const drive = drives.find((d) => d.id === target.drive_id);
                      if (!drive) return null;
                      return (
                        <div className="mt-1 flex flex-wrap gap-2 text-xs text-slate-500">
                          <span>目标网盘：{driveName(drives, drive.id)}</span>
                          {!drive.capabilities.folders ? <span className="text-orange-600">该网盘不支持子目录，仅使用文件名部分</span> : null}
                          {target.share && !drive.capabilities.share ? <span className="text-orange-600">该网盘不支持分享</span> : null}
                        </div>
                      );
                    }}
                  </Form.Item>
                </div>
              ))}
            </div>
          )}
        </Form.List>
        <p className="mt-2 text-xs text-slate-500">可用变量：{TEMPLATE_VARIABLES.map((v) => `{${v}}`).join(' ')}</p>
      </Form>
    </Modal>
  );
}
