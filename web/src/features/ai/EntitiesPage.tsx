import { useState } from 'react';
import { App, Button, Card, Form, Input, Modal, Segmented, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { Entity, EntityKind } from '../../shared/api/types';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { invalidateLibrary } from '../materials/queries';
import { REGIONS } from '../packs/PackCatalogFields';
import { listEntities, mergeEntity, updateEntity } from './api';

const KIND_OPTIONS: { value: EntityKind; label: string }[] = [
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
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [kind, setKind] = useState<EntityKind>('organization');
  const [q, setQ] = useState('');
  const [page, setPage] = useState(1);
  const [editing, setEditing] = useState<Entity | null>(null);
  const [merging, setMerging] = useState<Entity | null>(null);
  const [mergeInto, setMergeInto] = useState<string>();
  const [mergeSearch, setMergeSearch] = useState('');
  const [form] = Form.useForm<{ name: string; region?: string; remove_aliases: string[] }>();
  const [saving, setSaving] = useState(false);

  const query = useQuery({
    queryKey: ['entities', kind, q, page],
    queryFn: ({ signal }) => listEntities({ kind, q: q || undefined, page, page_size: 50 }, signal),
    placeholderData: keepPreviousData,
  });
  const candidates = useQuery({
    queryKey: ['entities', kind, 'merge', mergeSearch],
    queryFn: ({ signal }) => listEntities({ kind, q: mergeSearch || undefined, page: 1, page_size: 50 }, signal),
    enabled: merging !== null,
  });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['entities'] });
    invalidateLibrary(queryClient);
  };

  const openEdit = (entity: Entity) => {
    setEditing(entity);
    form.setFieldsValue({ name: entity.name, region: entity.region || undefined, remove_aliases: [] });
  };

  const saveEdit = async () => {
    if (!editing) return;
    const values = await form.validateFields();
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
  };

  const saveMerge = async () => {
    if (!merging || !mergeInto) return;
    setSaving(true);
    try {
      await mergeEntity(merging.id, mergeInto);
      message.success('已合并，别名和套卷一起迁移');
      setMerging(null);
      setMergeInto(undefined);
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

  const columns: ColumnsType<Entity> = [
    {
      title: '规范名称',
      key: 'name',
      render: (_: unknown, e) => (
        <div className="cell-stack">
          <Typography.Text strong>{e.name}</Typography.Text>
          {e.aliases.filter((a) => a !== e.name).length > 0 ? (
            <Typography.Text type="secondary" className="text-xs">
              别名：{e.aliases.filter((a) => a !== e.name).join('、')}
            </Typography.Text>
          ) : null}
        </div>
      ),
    },
    ...(kind === 'organization' ? [{ title: '地区', key: 'region', width: 100, render: (_: unknown, e: Entity) => e.region || '—' }] : []),
    { title: '使用', key: 'usage', width: 80, align: 'right', render: (_: unknown, e) => e.usage },
    {
      title: '来源',
      key: 'source',
      width: 120,
      render: (_: unknown, e) => (
        <Space size={4}>
          <Tag>{e.source === 'human' ? '人工' : 'AI'}</Tag>
          {e.locked ? <Tag color="blue">已锁定</Tag> : null}
        </Space>
      ),
    },
    ...(canWrite
      ? [
          {
            title: '',
            key: 'actions',
            width: 200,
            render: (_: unknown, e: Entity) => (
              <Space size={0}>
                <Button type="link" size="small" onClick={() => openEdit(e)}>
                  编辑
                </Button>
                <Button type="link" size="small" onClick={() => setMerging(e)}>
                  合并到…
                </Button>
                <Button type="link" size="small" onClick={() => void toggleLock(e)}>
                  {e.locked ? '解锁' : '锁定'}
                </Button>
              </Space>
            ),
          },
        ]
      : []),
  ];

  return (
    <div className="page-stack">
      <PageHeader title="名称词典" description="同一所学校、联盟或同一个标签只保留一个规范名称；AI 负责选用、新建和去重，只有它弄错时才需要在这里修改。" />
      <div className="flex flex-wrap items-center gap-3">
        <Segmented
          value={kind}
          options={KIND_OPTIONS}
          onChange={(value) => {
            setKind(value as EntityKind);
            setPage(1);
          }}
        />
        <Input.Search allowClear placeholder="搜索名称或别名" onSearch={(value) => { setQ(value.trim()); setPage(1); }} style={{ width: 240 }} />
      </div>
      {query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<Entity>
            rowKey="id"
            loading={query.isFetching}
            dataSource={query.data?.items ?? []}
            columns={columns}
            locale={{ emptyText: <EmptyState description="还没有名称，AI 编目时会自动建立" /> }}
            pagination={{ current: page, pageSize: 50, total: query.data?.total ?? 0, onChange: setPage, hideOnSinglePage: true, showSizeChanger: false }}
            scroll={{ x: 640 }}
          />
        </Card>
      )}

      <Modal title="编辑名称" open={editing !== null} onCancel={() => setEditing(null)} onOk={() => void saveEdit()} confirmLoading={saving} okText="保存" destroyOnHidden>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="规范名称" rules={[{ required: true, whitespace: true, max: 128 }]} extra="改名会同步到所有使用它的套卷；保存后该条目锁定，AI 不再改动。">
            <Input maxLength={128} />
          </Form.Item>
          {kind === 'organization' ? (
            <Form.Item name="region" label="地区">
              <Select allowClear showSearch options={REGIONS.map((value) => ({ value, label: value }))} />
            </Form.Item>
          ) : null}
          <Form.Item name="remove_aliases" label="移除别名" extra="移除写错的别名，之后这种写法不会再被认作该名称。">
            <Select mode="multiple" allowClear options={(editing?.aliases ?? []).filter((a) => a !== editing?.name).map((a) => ({ value: a, label: a }))} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={merging ? `把「${merging.name}」合并到…` : ''}
        open={merging !== null}
        onCancel={() => setMerging(null)}
        onOk={() => void saveMerge()}
        okButtonProps={{ disabled: !mergeInto }}
        confirmLoading={saving}
        okText="合并"
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary">合并后「{merging?.name}」成为目标名称的别名，所有套卷改用目标名称。</Typography.Paragraph>
        <Select
          showSearch
          className="w-full"
          placeholder="搜索目标名称"
          filterOption={false}
          onSearch={setMergeSearch}
          value={mergeInto}
          onChange={setMergeInto}
          loading={candidates.isFetching}
          options={(candidates.data?.items ?? []).filter((e) => e.id !== merging?.id).map((e) => ({ value: e.id, label: `${e.name}（${e.usage}）` }))}
        />
      </Modal>
    </div>
  );
}
