import { useState } from 'react';
import { Button, Group, MultiSelect, SegmentedControl, Select, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useDebouncedValue } from '@mantine/hooks';
import { IconSearch } from '@tabler/icons-react';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { Entity, EntityKind } from '../../shared/api/types';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { FormModal } from '../../shared/ui/FormModal';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { invalidateLibrary } from '../materials/queries';
import { REGIONS } from '../packs/PackCatalogFields';
import { listEntities, mergeEntity, updateEntity } from './api';
import { qk } from '../../app/queryKeys';

const KIND_OPTIONS = [
  { value: 'organization', label: '学校与联盟' },
  { value: 'tag', label: '标签' },
];

/**
 * 名称词典：每所学校、每个联盟、每个标签只有一个规范名称，文中的各种写法都是它的别名。
 * AI 负责选用、新建和去重；只有 AI 弄错时才需要在这里改名、合并或移除别名。
 */
export function EntitiesPage() {
  const { can } = useAuth();
  const canWrite = can('materials:write');
  const queryClient = useQueryClient();
  const [kind, setKind] = useState<EntityKind>('organization');
  const [qInput, setQInput] = useState('');
  const [q] = useDebouncedValue(qInput.trim(), 300);
  const [page, setPage] = useState(1);
  const [editing, setEditing] = useState<Entity | null>(null);
  const [merging, setMerging] = useState<Entity | null>(null);
  const [mergeInto, setMergeInto] = useState<string | null>(null);
  const [mergeSearch, setMergeSearch] = useState('');
  const [debouncedMergeSearch] = useDebouncedValue(mergeSearch, 250);
  const form = useForm<{ name: string; region: string | null; remove_aliases: string[] }>({
    initialValues: { name: '', region: null, remove_aliases: [] },
    validate: { name: (v) => (!v.trim() ? '请填写名称' : v.length > 128 ? '最多 128 个字符' : null) },
  });
  const [saving, setSaving] = useState(false);

  const query = useQuery({
    queryKey: qk.entities.list(kind, q, page),
    queryFn: ({ signal }) => listEntities({ kind, q: q || undefined, page, page_size: 50 }, signal),
    placeholderData: keepPreviousData,
  });
  const candidates = useQuery({
    queryKey: qk.entities.mergeCandidates(kind, debouncedMergeSearch),
    queryFn: ({ signal }) => listEntities({ kind, q: debouncedMergeSearch || undefined, page: 1, page_size: 50 }, signal),
    enabled: merging !== null,
  });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: qk.entities.all });
    invalidateLibrary(queryClient);
  };

  const openEdit = (entity: Entity) => {
    form.setValues({ name: entity.name, region: entity.region || null, remove_aliases: [] });
    form.resetDirty();
    setEditing(entity);
  };

  const saveEdit = form.onSubmit(async (values) => {
    if (!editing) return;
    setSaving(true);
    try {
      await updateEntity(editing.id, {
        name: values.name.trim() !== editing.name ? values.name.trim() : undefined,
        region: kind === 'organization' ? (values.region ?? '') : undefined,
        remove_aliases: values.remove_aliases,
      });
      message.success('已保存，使用该名称的套卷已同步更新');
      setEditing(null);
      refresh();
    } catch (error) {
      message.error(describeError(error).title);
    } finally {
      setSaving(false);
    }
  });

  const saveMerge = async () => {
    if (!merging || !mergeInto) return;
    setSaving(true);
    try {
      await mergeEntity(merging.id, mergeInto);
      message.success('已合并，别名和套卷一起迁移');
      setMerging(null);
      setMergeInto(null);
      refresh();
    } catch (error) {
      message.error(describeError(error).title);
    } finally {
      setSaving(false);
    }
  };

  const toggleLock = async (entity: Entity) => {
    try {
      await updateEntity(entity.id, { locked: !entity.locked });
      refresh();
    } catch (error) {
      message.error(describeError(error).title);
    }
  };

  const columns: Column<Entity>[] = [
    {
      key: 'name', wrap: true,
      title: '规范名称',
      render: (e) => (
        <Stack gap={4}>
          <Text fw={600} size="sm">{e.name}</Text>
          {e.aliases.filter((a) => a !== e.name).length > 0 ? <Text size="xs" c="dimmed">别名：{e.aliases.filter((a) => a !== e.name).join('、')}</Text> : null}
        </Stack>
      ),
    },
    ...(kind === 'organization' ? [{ key: 'region', title: '地区', width: 100, render: (e: Entity) => e.region || '—' }] : []),
    { key: 'usage', title: '使用', width: 80, align: 'right' as const, render: (e: Entity) => e.usage },
    {
      key: 'source',
      title: '来源',
      width: 130,
      render: (e) => <Group gap={4} wrap="nowrap"><Tag>{e.source === 'human' ? '人工' : 'AI'}</Tag>{e.locked ? <Tag>已锁定</Tag> : null}</Group>,
    },
    ...(canWrite ? [{
      key: 'actions',
      width: 220,
      render: (e: Entity) => (
        <Group gap={4} wrap="nowrap">
          <Button variant="subtle" size="compact-sm" onClick={() => openEdit(e)}>编辑</Button>
          <Button variant="subtle" size="compact-sm" onClick={() => setMerging(e)}>合并到…</Button>
          <Button variant="subtle" size="compact-sm" onClick={() => void toggleLock(e)}>{e.locked ? '解锁' : '锁定'}</Button>
        </Group>
      ),
    }] : []),
  ];

  return (
    <Stack gap="lg">
      <PageHeader title="名称词典" description="同一所学校、联盟或同一个标签只保留一个规范名称；AI 负责选用、新建和去重，只有它弄错时才需要在这里修改。" />
      <Group gap="sm">
        <SegmentedControl value={kind} data={KIND_OPTIONS} onChange={(value) => { setKind(value as EntityKind); setPage(1); }} />
        <TextInput w={240} placeholder="搜索名称或别名" leftSection={<IconSearch size={16} />} value={qInput} onChange={(e) => { setQInput(e.currentTarget.value); setPage(1); }} />
      </Group>
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <DataTable
          rows={query.data?.items ?? []}
          columns={columns}
          rowKey={(e) => e.id}
          loading={query.isFetching}
          minWidth={640}
          empty={<EmptyState description="还没有名称，AI 编目时会自动建立" />}
          pagination={{ page, pageSize: 50, total: query.data?.total ?? 0, pageSizes: [50], onChange: (p) => setPage(p) }}
        />
      )}

      <FormModal opened={editing !== null} onClose={() => setEditing(null)} title="编辑名称" loading={saving} onSubmit={saveEdit}>
        <TextInput label="规范名称" withAsterisk maxLength={128} description="改名会同步到所有使用它的套卷；保存后该条目锁定，AI 不再改动。" data-autofocus {...form.getInputProps('name')} />
        {kind === 'organization' ? <Select label="地区" clearable searchable data={REGIONS} {...form.getInputProps('region')} /> : null}
        <MultiSelect label="移除别名" description="移除写错的别名，之后这种写法不会再被认作该名称。" clearable data={(editing?.aliases ?? []).filter((a) => a !== editing?.name)} {...form.getInputProps('remove_aliases')} />
      </FormModal>

      <FormModal opened={merging !== null} onClose={() => setMerging(null)} title={merging ? `把「${merging.name}」合并到…` : ''} okText="合并" loading={saving} onSubmit={() => void saveMerge()}>
        <Text size="sm" c="dimmed">合并后「{merging?.name}」成为目标名称的别名，所有套卷改用目标名称。</Text>
        <Select
          searchable
          placeholder="搜索目标名称"
          searchValue={mergeSearch}
          onSearchChange={setMergeSearch}
          value={mergeInto}
          onChange={setMergeInto}
          filter={({ options }) => options}
          nothingFoundMessage={candidates.isFetching ? '搜索中…' : '没有匹配的名称'}
          data={(candidates.data?.items ?? []).filter((e) => e.id !== merging?.id).map((e) => ({ value: e.id, label: `${e.name}（${e.usage}）` }))}
        />
      </FormModal>
    </Stack>
  );
}
