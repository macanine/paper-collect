import { useState } from 'react';
import { App, Form, Input, Modal, Radio, Select, Space, Typography } from 'antd';
import type { Pack } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { useCollectionMutations, useCollectionsList } from '../collections/queries';
import { PackCatalogFields, formToUpdate, type CatalogField, type CatalogFormValues } from './PackCatalogFields';
import { usePackFacets, usePackMutations } from './queries';

const BULK_FIELDS: CatalogField[] = ['doc_type', 'subject_codes', 'grade', 'cohort', 'term', 'region', 'organization', 'exam_date', 'tags'];

/** 批量编目：只提交用户动过的字段，其余保持各自原值。 */
export function BulkEditModal({ ids, open, onClose }: { ids: string[]; open: boolean; onClose: () => void }) {
  const { message } = App.useApp();
  const [form] = Form.useForm<CatalogFormValues>();
  const facets = usePackFacets();
  const { bulkUpdate } = usePackMutations();
  const submit = async () => {
    const values = await form.validateFields();
    const touched = BULK_FIELDS.filter((field) => form.isFieldTouched(field));
    if (touched.length === 0) {
      message.info('没有修改任何字段');
      return;
    }
    try {
      const result = await bulkUpdate.mutateAsync({ ids, patch: formToUpdate(values, touched) });
      message.success(`已更新 ${result.count} 个套卷`);
      form.resetFields();
      onClose();
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  return (
    <Modal open={open} title={`批量编目 ${ids.length} 个套卷`} width={720} okText="应用" onOk={() => void submit()} onCancel={onClose} confirmLoading={bulkUpdate.isPending} destroyOnHidden>
      <Typography.Paragraph type="secondary">只会修改你填写或清空过的字段，标题会按新字段自动重命名。</Typography.Paragraph>
      <Form form={form} layout="vertical">
        <PackCatalogFields fields={BULK_FIELDS} facets={facets.data} />
      </Form>
    </Modal>
  );
}

/** 加入专题：选已有专题或新建。 */
export function AddToCollectionModal({ ids, open, onClose }: { ids: string[]; open: boolean; onClose: () => void }) {
  const { message } = App.useApp();
  const [mode, setMode] = useState<'existing' | 'new'>('existing');
  const [target, setTarget] = useState<string>();
  const [title, setTitle] = useState('');
  const [search, setSearch] = useState('');
  const list = useCollectionsList({ page: 1, page_size: 50, q: search || undefined }, open);
  const { addPacks, create } = useCollectionMutations();
  const submit = async () => {
    try {
      if (mode === 'existing') {
        if (!target) return;
        await addPacks.mutateAsync({ id: target, packIds: ids });
      } else {
        if (!title.trim()) return;
        await create.mutateAsync({ title: title.trim(), description: '', pack_ids: ids });
      }
      message.success('已加入专题');
      onClose();
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  return (
    <Modal open={open} title={`把 ${ids.length} 个套卷加入专题`} okText="加入" onOk={() => void submit()} onCancel={onClose} confirmLoading={addPacks.isPending || create.isPending} destroyOnHidden>
      <Space direction="vertical" className="w-full">
        <Radio.Group value={mode} onChange={(e) => setMode(e.target.value as 'existing' | 'new')} options={[{ value: 'existing', label: '已有专题' }, { value: 'new', label: '新建专题' }]} />
        {mode === 'existing' ? (
          <Select
            showSearch
            className="w-full"
            placeholder="搜索专题"
            filterOption={false}
            onSearch={setSearch}
            value={target}
            onChange={setTarget}
            options={(list.data?.items ?? []).map((c) => ({ value: c.id, label: `${c.title}（${c.pack_count}）` }))}
            loading={list.isFetching}
          />
        ) : (
          <Input placeholder="专题名称，如 导数压轴题汇编" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={255} />
        )}
      </Space>
    </Modal>
  );
}

/** 合并：选一个保留的套卷，其余套卷的文件并入它。 */
export function MergeModal({ packs, open, onClose, onMerged }: { packs: Pack[]; open: boolean; onClose: () => void; onMerged?: (id: string) => void }) {
  const { message } = App.useApp();
  const [target, setTarget] = useState<string>();
  const { merge } = usePackMutations();
  const chosen = target ?? packs[0]?.id;
  const submit = async () => {
    if (!chosen) return;
    try {
      await merge.mutateAsync({ target: chosen, sources: packs.map((p) => p.id).filter((id) => id !== chosen) });
      message.success('已合并');
      onMerged?.(chosen);
      onClose();
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  return (
    <Modal open={open} title={`合并 ${packs.length} 个套卷`} okText="合并" onOk={() => void submit()} onCancel={onClose} confirmLoading={merge.isPending} destroyOnHidden>
      <Typography.Paragraph type="secondary">选择保留哪一个套卷的编目信息，其余套卷的文件会并入它，空字段会自动补齐。</Typography.Paragraph>
      <Radio.Group value={chosen} onChange={(e) => setTarget(e.target.value as string)} className="flex flex-col gap-2">
        {packs.map((p) => (
          <Radio key={p.id} value={p.id}>
            {p.title}（{p.members.length} 个文件）
          </Radio>
        ))}
      </Radio.Group>
    </Modal>
  );
}
