import { useEffect, useState } from 'react';
import { App, Form, Input, Modal, Select } from 'antd';
import type { Collection, CollectionKind, SubjectCode } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import type { CollectionCreateBody, CollectionPatchBody } from './api';
import { COLLECTION_KIND_OPTIONS, SUBJECT_OPTIONS } from '../../shared/lib/options';
import { useCollectionsList, useCreateCollection, useUpdateCollection } from './queries';

// 合集元数据表单（创建与编辑共用）。条目顺序与路径在详情页单独编辑（PUT items）。

export interface CollectionFormValues {
  title: string;
  description: string;
  kind: CollectionKind;
  parent_id: string;
  subject_codes: SubjectCode[];
}

export function collectionToForm(collection: Collection): CollectionFormValues {
  return {
    title: collection.title,
    description: collection.description,
    kind: collection.kind,
    parent_id: collection.parent_id,
    subject_codes: [...collection.subject_codes],
  };
}

function sameStringList(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

/** 只提交相对原合集有变化的字段；PATCH 省略即表示不修改。 */
export function diffCollection(collection: Collection, values: CollectionFormValues): CollectionPatchBody {
  const patch: CollectionPatchBody = {};
  const title = values.title.trim();
  if (title !== collection.title) patch.title = title;
  if (values.description !== collection.description) patch.description = values.description;
  if (values.kind !== collection.kind) patch.kind = values.kind;
  if ((values.parent_id || '') !== collection.parent_id) patch.parent_id = values.parent_id || '';
  const subjects = [...new Set(values.subject_codes)];
  if (!sameStringList(subjects, collection.subject_codes)) patch.subject_codes = subjects;
  return patch;
}

export function buildCreateBody(values: CollectionFormValues): CollectionCreateBody {
  return {
    title: values.title.trim(),
    description: values.description ?? '',
    kind: values.kind,
    parent_id: values.parent_id || '',
    subject_codes: [...new Set(values.subject_codes ?? [])],
  };
}

interface CollectionParentSelectProps {
  value?: string;
  onChange?: (value: string) => void;
  excludeId?: string;
}

/**
 * 父合集选择：按关键词远程搜索（列表接口分页）。当前值若不在搜索结果中，保留一个占位选项，
 * 避免编辑时看不到已设置的父级。父级循环由服务端最终校验，这里只排除自身。
 */
function CollectionParentSelect({ value, onChange, excludeId }: CollectionParentSelectProps) {
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');
  const [known, setKnown] = useState<Map<string, string>>(new Map());
  const query = useCollectionsList({ page: 1, page_size: 50, q: debounced || undefined });

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(search.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [search]);

  useEffect(() => {
    if (!query.data) return;
    setKnown((previous) => {
      const next = new Map(previous);
      for (const item of query.data.items) next.set(item.id, item.title);
      return next;
    });
  }, [query.data]);

  const options = (query.data?.items ?? [])
    .filter((item) => item.id !== excludeId)
    .map((item) => ({ value: item.id, label: item.title }));

  if (value && !options.some((option) => option.value === value)) {
    options.unshift({ value, label: known.get(value) ?? `当前父合集（${value.slice(0, 8)}…）` });
  }

  return (
    <Select
      allowClear
      showSearch
      filterOption={false}
      value={value || undefined}
      onChange={(next) => onChange?.(next ?? '')}
      onSearch={setSearch}
      options={options}
      loading={query.isFetching}
      placeholder="不设父合集（作为顶层合集）"
      notFoundContent={query.isFetching ? '搜索中…' : query.isError ? '合集列表加载失败' : '没有匹配的合集'}
      onSelect={(selected) => {
        const item = query.data?.items.find((candidate) => candidate.id === selected);
        if (item) setKnown((previous) => new Map(previous).set(item.id, item.title));
      }}
    />
  );
}

interface CollectionFormModalProps {
  open: boolean;
  /** 提供时为编辑，否则为创建。 */
  collection?: Collection;
  onClose: () => void;
  onSaved?: (collection: Collection) => void;
}

export function CollectionFormModal({ open, collection, onClose, onSaved }: CollectionFormModalProps) {
  const [form] = Form.useForm<CollectionFormValues>();
  const { message } = App.useApp();
  const create = useCreateCollection();
  const update = useUpdateCollection(collection?.id ?? '');
  const submitting = create.isPending || update.isPending;
  const editing = collection !== undefined;

  useEffect(() => {
    if (!open) return;
    if (collection) {
      form.setFieldsValue(collectionToForm(collection));
    } else {
      form.resetFields();
      form.setFieldsValue({ title: '', description: '', kind: 'custom', parent_id: '', subject_codes: [] });
    }
  }, [open, collection, form]);

  const handleOk = async () => {
    const values = await form.validateFields();
    try {
      if (collection) {
        const patch = diffCollection(collection, values);
        if (Object.keys(patch).length === 0) {
          message.info('没有需要保存的修改');
          onClose();
          return;
        }
        const saved = await update.mutateAsync(patch);
        message.success('合集信息已保存');
        onSaved?.({ ...collection, ...saved });
        onClose();
      } else {
        const saved = await create.mutateAsync(buildCreateBody(values));
        message.success('合集已创建');
        onSaved?.(saved);
        onClose();
      }
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  return (
    <Modal
      title={editing ? '编辑合集' : '新建合集'}
      open={open}
      onOk={() => void handleOk()}
      onCancel={onClose}
      okText="保存"
      cancelText="取消"
      confirmLoading={submitting}
      destroyOnHidden
      width={680}
      maskClosable={!submitting}
    >
      <Form<CollectionFormValues> form={form} layout="vertical" requiredMark="optional" disabled={submitting}>
        <Form.Item
          name="title"
          label="标题"
          rules={[
            { required: true, whitespace: true, message: '请输入合集标题' },
            { max: 255, message: '标题不能超过 255 个字符' },
          ]}
        >
          <Input maxLength={255} placeholder="例如：高三数学一轮复习" />
        </Form.Item>
        <Form.Item name="description" label="描述" rules={[{ max: 4000, message: '描述不能超过 4000 个字符' }]}>
          <Input.TextArea rows={3} maxLength={4000} showCount placeholder="可选" />
        </Form.Item>
        <div className="grid grid-cols-1 gap-x-4 md:grid-cols-2">
          <Form.Item name="kind" label="类型" rules={[{ required: true, message: '请选择合集类型' }]}>
            <Select options={COLLECTION_KIND_OPTIONS} />
          </Form.Item>
          <Form.Item name="parent_id" label="父合集">
            <CollectionParentSelect excludeId={collection?.id} />
          </Form.Item>
        </div>
        <Form.Item
          name="subject_codes"
          label="学科"
          rules={[{ type: 'array', max: 12, message: '最多选择 12 个学科' }]}
        >
          <Select mode="multiple" options={SUBJECT_OPTIONS} placeholder="可选" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
