import { useEffect } from 'react';
import { App, Form, Input, InputNumber, Modal, Select } from 'antd';
import { describeError } from '../../shared/api/errors';
import type { Grade, Material, MaterialUpdate, Role, SubjectCode } from '../../shared/api/types';
import { GRADE_OPTIONS, ROLE_OPTIONS, SUBJECT_OPTIONS } from '../../shared/lib/options';
import { isValidAcademicYear, normalizeTags } from '../../shared/lib/metadata';
import { useUpdateMaterial } from './queries';

// 资料可编辑字段（PATCH 部分更新）：标题、描述、学科、年级、学年、标签、角色。
// 仅提交相对当前资料有变化的字段；不允许修改文件、SHA、上传路径与归档状态。

export interface MaterialFormValues {
  title: string;
  description: string;
  subject_codes: SubjectCode[];
  grade: Grade;
  academic_year: number | null;
  tags: string[];
  role: Role;
}

const GRADE_CHOICES = [{ value: '' as Grade, label: '未指定' }, ...GRADE_OPTIONS];

export function toFormValues(material: Material): MaterialFormValues {
  return {
    title: material.title,
    description: material.description,
    subject_codes: [...material.subject_codes],
    grade: material.grade,
    academic_year: material.academic_year,
    tags: [...material.tags],
    role: material.role,
  };
}

function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((item, index) => item === b[index]);
}

/** 计算相对原始资料的 PATCH 载荷；无变化时返回空对象。 */
export function diffMaterial(material: Material, values: MaterialFormValues): MaterialUpdate {
  const patch: MaterialUpdate = {};
  const title = values.title.trim();
  if (title !== material.title) patch.title = title;
  if (values.description !== material.description) patch.description = values.description;

  const subjects = [...new Set(values.subject_codes)];
  if (!sameList(subjects, material.subject_codes)) patch.subject_codes = subjects;

  if (values.grade !== material.grade) patch.grade = values.grade;

  const year = values.academic_year ?? 0;
  if (year !== material.academic_year) patch.academic_year = year;

  const tags = normalizeTags(values.tags);
  if (!sameList(tags, material.tags)) patch.tags = tags;

  if (values.role !== material.role) patch.role = values.role;
  return patch;
}

interface MaterialEditModalProps {
  material: Material;
  open: boolean;
  onClose: () => void;
}

export function MaterialEditModal({ material, open, onClose }: MaterialEditModalProps) {
  const [form] = Form.useForm<MaterialFormValues>();
  const { message } = App.useApp();
  const update = useUpdateMaterial(material.id);

  useEffect(() => {
    if (open) form.setFieldsValue(toFormValues(material));
  }, [open, material, form]);

  const handleOk = async () => {
    const values = await form.validateFields();
    const patch = diffMaterial(material, { ...values, academic_year: values.academic_year ?? 0 });
    if (Object.keys(patch).length === 0) {
      message.info('没有需要保存的修改');
      onClose();
      return;
    }
    try {
      await update.mutateAsync(patch);
      message.success('资料已保存');
      onClose();
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    }
  };

  return (
    <Modal
      title="编辑资料"
      open={open}
      onOk={() => void handleOk()}
      onCancel={onClose}
      okText="保存"
      cancelText="取消"
      confirmLoading={update.isPending}
      destroyOnHidden
      width={640}
    >
      <Form<MaterialFormValues> form={form} layout="vertical" requiredMark="optional" disabled={update.isPending}>
        <Form.Item
          name="title"
          label="资料名（同时作为文件名）"
          rules={[
            { required: true, whitespace: true, message: '请输入标题' },
            { max: 255, message: '标题不能超过 255 个字符' },
          ]}
        >
          <Input maxLength={255} />
        </Form.Item>
        <Form.Item name="description" label="描述" rules={[{ max: 4000, message: '描述不能超过 4000 个字符' }]}>
          <Input.TextArea rows={3} maxLength={4000} showCount />
        </Form.Item>
        <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <Form.Item
            name="subject_codes"
            label="学科"
            rules={[{ type: 'array', max: 12, message: '最多选择 12 个学科' }]}
          >
            <Select mode="multiple" options={SUBJECT_OPTIONS} placeholder="选择学科" />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true, message: '请选择角色' }]}>
            <Select options={ROLE_OPTIONS} placeholder="选择角色" />
          </Form.Item>
          <Form.Item name="grade" label="年级">
            <Select options={GRADE_CHOICES} />
          </Form.Item>
          <Form.Item
            name="academic_year"
            label="学年"
            rules={[
              {
                validator: (_rule, value: number | null) =>
                  isValidAcademicYear(value) ? Promise.resolve() : Promise.reject(new Error('学年须为 0 或 1900 至 2200 之间的整数')),
              },
            ]}
          >
            <InputNumber min={0} max={2200} precision={0} style={{ width: '100%' }} placeholder="未指定" />
          </Form.Item>
        </div>
        <Form.Item
          name="tags"
          label="标签"
          rules={[
            { type: 'array', max: 30, message: '最多 30 个标签' },
            {
              validator: (_rule, value: string[] | undefined) => {
                const bad = (value ?? []).some((tag) => tag.trim().length === 0 || tag.trim().length > 64);
                return bad ? Promise.reject(new Error('每个标签须为 1 至 64 个字符')) : Promise.resolve();
              },
            },
          ]}
        >
          <Select mode="tags" tokenSeparators={[',', '，']} placeholder="输入后回车添加" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
