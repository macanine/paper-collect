import { useCallback, useMemo, useState, type Key } from 'react';
import { Anchor, Button, Checkbox, Group, Menu, Paper, Select, SimpleGrid, Stack, Text, TextInput } from '@mantine/core';
import { IconChevronDown, IconFilter, IconSearch } from '@tabler/icons-react';
import { Link, useNavigate } from 'react-router';
import type { DocType, Grade, Pack, Stage, SubjectCode } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { saveBlob } from '../../shared/lib/download';
import { formatDateTime } from '../../shared/lib/format';
import { DOC_TYPE_LABELS, GRADE_LABELS, SUBJECT_LABELS } from '../../shared/lib/labels';
import { useListSearch } from '../../shared/lib/listSearch';
import { STAGE_OPTIONS } from '../../shared/lib/options';
import { useDebouncedFilterInput } from '../../shared/lib/useDebouncedFilterInput';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message, modal } from '../../shared/ui/feedback';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { downloadPack, type PackSort } from './api';
import { AddToCollectionModal, BulkEditModal, MergeModal } from './BulkModals';
import { Completeness, PackMeta } from './PackBadges';
import { usePackFacets, usePackMutations, usePacksList } from './queries';
import { href } from '../../app/routes';

const FILTER_KEYS = ['q', 'content', 'subject', 'grade', 'doc_type', 'cohort', 'region', 'organization', 'tag', 'stage', 'missing', 'collection_id', 'sort'] as const;

function facetOptions<T extends string>(values: { value: string; count: number }[] | undefined, label: (v: T) => string) {
  return (values ?? []).map((f) => ({ value: f.value, label: `${label(f.value as T)}（${f.count}）` }));
}

export function LibraryPage() {
  const { can } = useAuth();
  const canWrite = can('materials:write');
  const navigate = useNavigate();
  const [selectMode, setSelectMode] = useState(false);
  const list = useListSearch(FILTER_KEYS);
  const { filters, setFilters } = list;
  const [qInput, setQInput] = useDebouncedFilterInput(filters.q, (q) => setFilters({ q }));
  const [selected, setSelected] = useState<Pack[]>([]);
  const [dialog, setDialog] = useState<'' | 'edit' | 'topic' | 'merge'>('');
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const facets = usePackFacets();
  const { trash } = usePackMutations();

  const query = usePacksList({
    page: list.page,
    page_size: list.pageSize,
    q: filters.q || undefined,
    content: filters.content === '1' || undefined,
    subject: (filters.subject as SubjectCode) || undefined,
    grade: (filters.grade as Grade) || undefined,
    doc_type: (filters.doc_type as DocType) || undefined,
    cohort: filters.cohort ? Number(filters.cohort) : undefined,
    region: filters.region || undefined,
    organization: filters.organization || undefined,
    tag: filters.tag || undefined,
    stage: (filters.stage as Stage) || undefined,
    missing: filters.missing === 'answers' ? 'answers' : undefined,
    collection_id: filters.collection_id || undefined,
    sort: (filters.sort as PackSort) || undefined,
  });
  const hasFilters = FILTER_KEYS.some((key) => key !== 'sort' && filters[key] !== '');
  const activeFilterCount = FILTER_KEYS.filter((key) => key !== 'sort' && filters[key] !== '').length;
  const ids = useMemo(() => selected.map((p) => p.id), [selected]);

  const handleTrash = useCallback(async () => {
    try {
      const result = await trash.mutateAsync(ids);
      message.success(`已移入回收站 ${result.count} 个套卷，30 天内可恢复`);
      setSelected([]);
    } catch (error) {
      message.error(errorMessage(error));
    }
  }, [ids, trash]);

  const handleDownload = useCallback(async () => {
    for (const pack of selected) {
      try {
        const { blob, filename } = await downloadPack(pack.id);
        saveBlob(blob, filename ?? `${pack.title}.zip`);
      } catch (error) {
        message.error(`${pack.title}：${errorMessage(error)}`);
      }
    }
  }, [selected]);

  const columns = useMemo<Column<Pack>[]>(() => [
    {
      key: 'title', wrap: true,
      title: '套卷',
      render: (pack) => (
        <Stack gap={4} miw={0}>
          <Anchor component={Link} to={href.pack(pack.id)} fw={600} c="inherit">{pack.title}</Anchor>
          <PackMeta pack={pack} />
        </Stack>
      ),
    },
    { key: 'complete', title: '齐全度', width: 200, hideOnCompact: true, render: (pack) => <Completeness pack={pack} /> },
    { key: 'files', title: '文件', width: 70, align: 'right', hideOnCompact: true, render: (pack) => pack.members.length },
    { key: 'updated_at', title: '更新时间', width: 170, hideOnCompact: true, render: (pack) => formatDateTime(pack.updated_at) },
  ], []);

  const clear = useCallback(() => {
    setQInput('');
    setFilters(Object.fromEntries(FILTER_KEYS.filter((k) => k !== 'sort').map((k) => [k, ''])));
  }, [setFilters, setQInput]);

  const onSelectedChange = (keys: Key[]) => {
    const pool = new Map<string, Pack>([...selected, ...(query.data?.items ?? [])].map((p) => [p.id, p]));
    setSelected(keys.map((k) => pool.get(String(k))).filter((p): p is Pack => !!p));
  };

  const confirmTrash = () =>
    modal.confirm({ title: `把 ${selected.length} 个套卷移入回收站？`, content: '30 天内可以恢复。', okText: '移入', danger: true, onOk: handleTrash });

  const exitSelect = () => { setSelectMode(false); setSelected([]); };

  const select = (label: string, key: (typeof FILTER_KEYS)[number], data: { value: string; label: string }[], searchable = false) => (
    <Select label={label} placeholder="全部" clearable searchable={searchable} data={data} value={filters[key] || null} onChange={(v) => setFilters({ [key]: v ?? '' })} />
  );

  return (
    <Stack gap="lg">
      <PageHeader title="资料库" />

      <Paper p="md">
        <Stack gap="md">
          <Group gap="sm" wrap="wrap">
            <TextInput flex={1} miw={220} placeholder="搜索名称、考试、机构、地区或标签" leftSection={<IconSearch size={16} />} value={qInput} onChange={(e) => setQInput(e.currentTarget.value)} />
            <Group gap="sm" wrap="nowrap">
              <Button variant={advancedOpen || activeFilterCount > 0 ? 'light' : 'default'} leftSection={<IconFilter size={16} />} onClick={() => setAdvancedOpen((o) => !o)} aria-expanded={advancedOpen}>
                筛选{activeFilterCount > 0 ? `（${activeFilterCount}）` : ''}
              </Button>
              {canWrite ? <Button variant={selectMode ? 'light' : 'default'} onClick={() => (selectMode ? exitSelect() : setSelectMode(true))}>{selectMode ? '完成' : '选择'}</Button> : null}
            </Group>
          </Group>
          {advancedOpen ? (
            <Stack gap="md" aria-label="高级筛选">
              <SimpleGrid cols={{ base: 1, xs: 2, md: 3, lg: 4 }} spacing="md">
                {select('学科', 'subject', facetOptions<SubjectCode>(facets.data?.subjects, (v) => SUBJECT_LABELS[v] ?? v))}
                {select('年级', 'grade', facetOptions<Grade>(facets.data?.grades, (v) => GRADE_LABELS[v] ?? v))}
                {select('资料类型', 'doc_type', facetOptions<DocType>(facets.data?.doc_types, (v) => DOC_TYPE_LABELS[v] ?? v))}
                {select('届别', 'cohort', facetOptions<string>(facets.data?.cohorts, (v) => `${v} 届`))}
                {select('地区', 'region', facetOptions<string>(facets.data?.regions, (v) => v), true)}
                {select('来源机构', 'organization', facetOptions<string>(facets.data?.organizations, (v) => v), true)}
                {select('标签', 'tag', facetOptions<string>(facets.data?.tags, (v) => v), true)}
                {select('整理状态', 'stage', STAGE_OPTIONS)}
                <Select
                  label="排序"
                  allowDeselect={false}
                  value={filters.sort || 'updated'}
                  onChange={(sort) => setFilters({ sort: sort === 'updated' ? '' : (sort ?? '') })}
                  data={[{ value: 'updated', label: '最近更新' }, { value: 'cohort', label: '按届别与考试时间' }, { value: 'title', label: '按名称' }]}
                />
              </SimpleGrid>
              <Group gap="lg">
                <Checkbox label="只看缺答案" checked={filters.missing === 'answers'} onChange={(e) => setFilters({ missing: e.currentTarget.checked ? 'answers' : '' })} />
                <Checkbox label="同时搜索正文" checked={filters.content === '1'} onChange={(e) => setFilters({ content: e.currentTarget.checked ? '1' : '' })} />
                {hasFilters ? <Button variant="subtle" size="compact-sm" onClick={clear}>清除全部</Button> : null}
              </Group>
            </Stack>
          ) : null}
        </Stack>
      </Paper>

      {canWrite && selectMode && selected.length > 0 ? (
        <Paper p="sm" px="md">
          <Group gap="sm">
            <Text fw={600} size="sm">已选 {selected.length} 个</Text>
            <Button variant="default" onClick={() => setDialog('topic')}>加入专题</Button>
            <Button variant="default" onClick={() => void handleDownload()}>下载</Button>
            <Menu position="bottom-start">
              <Menu.Target><Button variant="default" rightSection={<IconChevronDown size={14} />}>更多</Button></Menu.Target>
              <Menu.Dropdown>
                <Menu.Item onClick={() => setDialog('edit')}>批量编目</Menu.Item>
                <Menu.Item disabled={selected.length < 2} onClick={() => setDialog('merge')}>合并为一套</Menu.Item>
                <Menu.Divider />
                <Menu.Item color="red" onClick={confirmTrash}>移入回收站</Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Paper>
      ) : null}

      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <DataTable
          rows={query.data?.items ?? []}
          columns={columns}
          rowKey={(p) => p.id}
          minWidth={320}
          loading={query.isFetching}
          selected={canWrite && selectMode ? ids : undefined}
          onSelectedChange={canWrite && selectMode ? onSelectedChange : undefined}
          onRowClick={(pack) => navigate(href.pack(pack.id))}
          empty={<EmptyState description={hasFilters ? '没有符合条件的套卷' : '资料库还是空的，把文件拖到页面任意位置即可上传'} />}
          pagination={{ page: list.page, pageSize: list.pageSize, total: query.data?.total ?? 0, onChange: list.setPage }}
        />
      )}

      <BulkEditModal ids={ids} open={dialog === 'edit'} onClose={() => setDialog('')} />
      <AddToCollectionModal ids={ids} open={dialog === 'topic'} onClose={() => setDialog('')} />
      <MergeModal packs={selected} open={dialog === 'merge'} onClose={() => setDialog('')} onMerged={() => setSelected([])} />
    </Stack>
  );
}
