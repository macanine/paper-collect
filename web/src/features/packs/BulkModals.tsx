import { useState } from 'react';
import { Radio, Select, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useDebouncedValue } from '@mantine/hooks';
import type { Pack } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { FormModal } from '../../shared/ui/FormModal';
import { message } from '../../shared/ui/feedback';
import { useCollectionMutations, useCollectionsList } from '../collections/queries';
import { PackCatalogFields, formToUpdate, validateCatalog, type CatalogField, type CatalogFormValues } from './PackCatalogFields';
import { usePackFacets, usePackMutations } from './queries';

const BULK_FIELDS: CatalogField[] = ['doc_type', 'subject_codes', 'grade', 'cohort', 'term', 'region', 'organization', 'exam_date', 'tags'];

/** 批量编目：只提交用户动过的字段，其余保持各自原值。 */
export function BulkEditModal({ ids, open, onClose }: { ids: string[]; open: boolean; onClose: () => void }) {
  const form = useForm<CatalogFormValues>({ initialValues: {}, validate: validateCatalog });
  const facets = usePackFacets();
  const { bulkUpdate } = usePackMutations();
  const submit = form.onSubmit(async (values) => {
    const touched = BULK_FIELDS.filter((field) => form.isDirty(field));
    if (touched.length === 0) {
      message.info('没有修改任何字段');
      return;
    }
    try {
      const result = await bulkUpdate.mutateAsync({ ids, patch: formToUpdate(values, touched) });
      message.success(`已更新 ${result.count} 个套卷`);
      form.reset();
      onClose();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
  return (
    <FormModal opened={open} onClose={onClose} title={`批量编目 ${ids.length} 个套卷`} size={720} okText="应用" loading={bulkUpdate.isPending} onSubmit={submit}>
      <Text size="sm" c="dimmed">只会修改你填写或清空过的字段，标题会按新字段自动重命名。</Text>
      <PackCatalogFields form={form} fields={BULK_FIELDS} facets={facets.data} />
    </FormModal>
  );
}

/** 加入专题：选已有专题或新建。 */
export function AddToCollectionModal({ ids, open, onClose }: { ids: string[]; open: boolean; onClose: () => void }) {
  const [mode, setMode] = useState<'existing' | 'new'>('existing');
  const [target, setTarget] = useState<string | null>(null);
  const [title, setTitle] = useState('');
  const [search, setSearch] = useState('');
  const [debounced] = useDebouncedValue(search, 250);
  const list = useCollectionsList({ page: 1, page_size: 50, q: debounced || undefined }, open);
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
    <FormModal opened={open} onClose={onClose} title={`把 ${ids.length} 个套卷加入专题`} okText="加入" loading={addPacks.isPending || create.isPending} onSubmit={() => void submit()}>
      <Radio.Group value={mode} onChange={(v) => setMode(v as 'existing' | 'new')}>
        <Stack gap="xs"><Radio value="existing" label="已有专题" /><Radio value="new" label="新建专题" /></Stack>
      </Radio.Group>
      {mode === 'existing' ? (
        <Select searchable placeholder="搜索专题" searchValue={search} onSearchChange={setSearch} value={target} onChange={setTarget} filter={({ options }) => options} data={(list.data?.items ?? []).map((c) => ({ value: c.id, label: `${c.title}（${c.pack_count}）` }))} />
      ) : (
        <TextInput placeholder="专题名称，如 导数压轴题汇编" value={title} onChange={(e) => setTitle(e.currentTarget.value)} maxLength={255} />
      )}
    </FormModal>
  );
}

/** 合并：选一个保留的套卷，其余套卷的文件并入它。 */
export function MergeModal({ packs, open, onClose, onMerged }: { packs: Pack[]; open: boolean; onClose: () => void; onMerged?: (id: string) => void }) {
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
    <FormModal opened={open} onClose={onClose} title={`合并 ${packs.length} 个套卷`} okText="合并" loading={merge.isPending} onSubmit={() => void submit()}>
      <Text size="sm" c="dimmed">选择保留哪一个套卷的编目信息，其余套卷的文件会并入它，空字段会自动补齐。</Text>
      <Radio.Group value={chosen} onChange={setTarget}>
        <Stack gap="xs">{packs.map((p) => <Radio key={p.id} value={p.id} label={`${p.title}（${p.members.length} 个文件）`} />)}</Stack>
      </Radio.Group>
    </FormModal>
  );
}
