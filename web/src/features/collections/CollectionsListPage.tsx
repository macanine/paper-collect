import { useState } from 'react';
import { Anchor, Button, Group, Modal, Pagination, Paper, Skeleton, Stack, Text, TextInput, Textarea } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconPlus, IconSearch } from '@tabler/icons-react';
import { Link, useNavigate } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { formatDateTime } from '../../shared/lib/format';
import { useListSearch } from '../../shared/lib/listSearch';
import { useDebouncedFilterInput } from '../../shared/lib/useDebouncedFilterInput';
import { ItemList, ItemRow } from '../../shared/ui/ItemList';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { useCollectionMutations, useCollectionsList } from './queries';
import { href } from '../../app/routes';

export function CollectionsListPage() {
  const { can } = useAuth();
  const navigate = useNavigate();
  const list = useListSearch(['q'] as const);
  const [qInput, setQInput] = useDebouncedFilterInput(list.filters.q, (q) => list.setFilters({ q }));
  const query = useCollectionsList({ page: list.page, page_size: list.pageSize, q: list.filters.q || undefined });
  const { create } = useCollectionMutations();
  const [open, setOpen] = useState(false);
  const form = useForm({
    initialValues: { title: '', description: '' },
    validate: { title: (v) => (v.trim() ? null : '请填写名称') },
  });

  const submit = form.onSubmit(async (values) => {
    try {
      const created = await create.mutateAsync({ title: values.title, description: values.description });
      message.success('专题已创建，可在资料库勾选套卷后加入');
      setOpen(false);
      form.reset();
      navigate(`/collections/${created.id}`);
    } catch (error) {
      message.error(errorMessage(error));
    }
  });

  const pages = query.data ? Math.ceil(query.data.total / list.pageSize) : 0;

  return (
    <Stack gap="lg">
      <PageHeader
        title="专题"
        description="把散落各处的套卷按复习主题整理在一起，如“导数压轴题”“一轮复习”。"
        extra={can('collections:write') ? <Button leftSection={<IconPlus size={16} />} onClick={() => setOpen(true)}>新建专题</Button> : null}
      />
      <TextInput maw={360} placeholder="搜索专题" leftSection={<IconSearch size={16} />} value={qInput} onChange={(e) => setQInput(e.currentTarget.value)} />
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : query.isPending ? (
        <Paper p="md" aria-label="正在加载专题"><Stack gap="sm"><Skeleton h={14} w="52%" /><Skeleton h={12} /><Skeleton h={12} w="70%" /></Stack></Paper>
      ) : query.data?.items.length ? (
        <Stack gap="md">
          <ItemList>
            {query.data.items.map((collection) => (
              <ItemRow
                key={collection.id}
                main={
                  <Stack gap={4}>
                    <Anchor component={Link} to={href.collection(collection.id)} fw={600} c="inherit" truncate>{collection.title}</Anchor>
                    <Text size="sm" c="dimmed" truncate>{collection.pack_count} 个套卷{collection.description ? ` · ${collection.description}` : ''}</Text>
                  </Stack>
                }
                aside={<Text size="xs" c="dimmed">{formatDateTime(collection.updated_at)}</Text>}
              />
            ))}
          </ItemList>
          {pages > 1 ? <Group justify="flex-end"><Pagination total={pages} value={list.page} onChange={(p) => list.setPage(p, list.pageSize)} /></Group> : null}
        </Stack>
      ) : (
        <Paper><EmptyState description="还没有专题" /></Paper>
      )}
      <Modal opened={open} onClose={() => setOpen(false)} title="新建专题">
        <form onSubmit={submit}>
          <Stack gap="md">
            <TextInput label="名称" withAsterisk maxLength={255} placeholder="如 导数压轴题汇编" data-autofocus {...form.getInputProps('title')} />
            <Textarea label="说明" autosize minRows={2} maxLength={4000} {...form.getInputProps('description')} />
            <Group justify="flex-end"><Button variant="default" onClick={() => setOpen(false)}>取消</Button><Button type="submit" loading={create.isPending}>创建</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  );
}
