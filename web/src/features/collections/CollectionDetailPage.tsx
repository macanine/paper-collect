import { useState } from 'react';
import { Anchor, Button, Group, Modal, Stack, TextInput, Textarea, ActionIcon } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconArrowDown, IconArrowUp } from '@tabler/icons-react';
import { Link, useNavigate, useParams } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { BackLink } from '../../shared/ui/BackLink';
import { ItemList, ItemRow } from '../../shared/ui/ItemList';
import { LinkButton } from '../../shared/ui/LinkButton';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message, modal } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { Completeness, PackMeta } from '../packs/PackBadges';
import { useCollection, useCollectionMutations } from './queries';
import { href } from '../../app/routes';

export function CollectionDetailPage() {
  const { collectionId = '' } = useParams();
  const { can } = useAuth();
  const canWrite = can('collections:write');
  const navigate = useNavigate();
  const query = useCollection(collectionId);
  const { update, replacePacks, remove } = useCollectionMutations();
  const [editing, setEditing] = useState(false);
  const form = useForm({
    initialValues: { title: '', description: '' },
    validate: { title: (v) => (v.trim() ? null : '请填写名称') },
  });

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <Stack gap="lg">
        <BackLink to="/collections">返回专题</BackLink>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="专题加载失败" />
      </Stack>
    );
  }
  const collection = query.data;
  const order = collection.packs.map((p) => p.id);

  const save = form.onSubmit(async (values) => {
    try {
      await update.mutateAsync({ id: collection.id, body: { title: values.title, description: values.description } });
      setEditing(false);
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
  const setOrder = async (next: string[]) => {
    try {
      await replacePacks.mutateAsync({ id: collection.id, packIds: next });
    } catch (error) {
      message.error(errorMessage(error));
    }
  };
  const move = (index: number, delta: number) => {
    const next = [...order];
    const [id] = next.splice(index, 1);
    next.splice(index + delta, 0, id as string);
    void setOrder(next);
  };

  return (
    <Stack gap="lg">
      <BackLink to="/collections">返回专题</BackLink>
      <PageHeader
        title={collection.title}
        description={collection.description || `${collection.packs.length} 个套卷`}
        extra={
          <>
            <LinkButton to={`/library?collection_id=${collection.id}`}>在资料库中查看</LinkButton>
            {canWrite ? (
              <>
                <Button variant="default" onClick={() => { form.setValues({ title: collection.title, description: collection.description ?? '' }); setEditing(true); }}>编辑</Button>
                <Button
                  variant="default"
                  color="red"
                  onClick={() => modal.confirm({
                    title: '删除专题？',
                    content: '其中的套卷不会被删除',
                    okText: '删除',
                    danger: true,
                    onOk: () => remove.mutateAsync(collection.id).then(() => navigate('/collections', { replace: true })),
                  })}
                >
                  删除专题
                </Button>
              </>
            ) : null}
          </>
        }
      />
      {collection.packs.length === 0 ? (
        <EmptyState description="专题还是空的：在资料库勾选套卷，点“加入专题”" />
      ) : (
        <ItemList>
          {collection.packs.map((pack, index) => (
            <ItemRow
              key={pack.id}
              main={
                <Stack gap={4}>
                  <Anchor component={Link} to={href.pack(pack.id)} fw={600} c="inherit" truncate>{pack.title}</Anchor>
                  <Group gap="sm"><PackMeta pack={pack} /><Completeness pack={pack} /></Group>
                </Stack>
              }
              aside={canWrite ? (
                <>
                  <ActionIcon variant="subtle" color="gray" aria-label="上移" disabled={index === 0} onClick={() => move(index, -1)}><IconArrowUp size={16} /></ActionIcon>
                  <ActionIcon variant="subtle" color="gray" aria-label="下移" disabled={index === order.length - 1} onClick={() => move(index, 1)}><IconArrowDown size={16} /></ActionIcon>
                  <Button variant="subtle" color="red" size="compact-sm" onClick={() => void setOrder(order.filter((id) => id !== pack.id))}>移出</Button>
                </>
              ) : null}
            />
          ))}
        </ItemList>
      )}
      <Modal opened={editing} onClose={() => setEditing(false)} title="编辑专题">
        <form onSubmit={save}>
          <Stack gap="md">
            <TextInput label="名称" withAsterisk maxLength={255} {...form.getInputProps('title')} />
            <Textarea label="说明" autosize minRows={2} maxLength={4000} {...form.getInputProps('description')} />
            <Group justify="flex-end"><Button variant="default" onClick={() => setEditing(false)}>取消</Button><Button type="submit" loading={update.isPending}>保存</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  );
}
