import { useEffect, useMemo, useRef, useState } from 'react';
import { Alert, App, Button, Input, Space, Table, Tag, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  DeleteOutlined,
  PlusOutlined,
  SaveOutlined,
  SortAscendingOutlined,
} from '@ant-design/icons';
import { Link } from 'react-router';
import type { CollectionDetail, Material } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { validateRelativePath } from '../../shared/lib/paths';
import { RoleTag } from '../../shared/ui/StatusTags';
import { MaterialPickerModal } from './MaterialPickerModal';
import {
  MAX_COLLECTION_ITEMS,
  moveItem,
  sortNaturally,
  toItemPayload,
  validateItemRows,
  type EditableRow,
} from './itemsOrder';
import { useReplaceCollectionItems } from './queries';

interface CollectionItemsEditorProps {
  collection: CollectionDetail;
  canWrite: boolean;
}

function rowsFromDetail(detail: CollectionDetail): EditableRow[] {
  return [...detail.items]
    .sort((left, right) => left.position - right.position || left.id.localeCompare(right.id))
    .map((item) => ({
      key: item.id,
      material_id: item.material_id,
      relative_path: item.relative_path,
      role: item.role,
    }));
}

function materialsFromDetail(detail: CollectionDetail): Map<string, Material> {
  const map = new Map<string, Material>();
  for (const item of detail.items) {
    if (item.material) map.set(item.material.id, item.material);
  }
  return map;
}

function suggestedPath(material: Material): string {
  for (const candidate of [material.source_path, material.file_name]) {
    if (candidate && validateRelativePath(candidate) === null) return candidate;
  }
  return '';
}

/** 合集条目编辑：顺序、角色、相对路径，整体 PUT 替换。 */
export function CollectionItemsEditor({ collection, canWrite }: CollectionItemsEditorProps) {
  const { message } = App.useApp();
  const replace = useReplaceCollectionItems(collection.id);
  const [rows, setRows] = useState<EditableRow[]>(() => rowsFromDetail(collection));
  const [materials, setMaterials] = useState<Map<string, Material>>(() => materialsFromDetail(collection));
  const [dirty, setDirty] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const sequence = useRef(0);

  useEffect(() => {
    setRows(rowsFromDetail(collection));
    setMaterials(materialsFromDetail(collection));
    setDirty(false);
  }, [collection]);

  const validation = useMemo(() => validateItemRows(rows), [rows]);
  const existingIds = useMemo(() => new Set(rows.map((row) => row.material_id)), [rows]);

  const updateRow = (key: string, patch: Partial<EditableRow>) => {
    setRows((previous) => previous.map((row) => (row.key === key ? { ...row, ...patch } : row)));
    setDirty(true);
  };

  const removeRow = (key: string) => {
    setRows((previous) => previous.filter((row) => row.key !== key));
    setDirty(true);
  };

  const moveRow = (index: number, delta: -1 | 1) => {
    setRows((previous) => moveItem(previous, index, delta));
    setDirty(true);
  };

  const handleAdd = (selected: Material[]) => {
    const currentIds = new Set(rows.map((row) => row.material_id));
    const additions = selected.filter((material) => !currentIds.has(material.id));
    if (additions.length === 0) {
      message.info('所选资料已在合集中，未添加');
      return;
    }
    setMaterials((previous) => {
      const next = new Map(previous);
      for (const material of additions) next.set(material.id, material);
      return next;
    });
    setRows((previous) => [
      ...previous,
      ...additions.map((material) => {
        sequence.current += 1;
        return {
          key: `new-${sequence.current}`,
          material_id: material.id,
          relative_path: suggestedPath(material),
          role: material.role,
        } satisfies EditableRow;
      }),
    ]);
    setDirty(true);
    message.success(`已添加 ${additions.length} 个资料，保存后生效`);
  };

  const handleSave = async () => {
    if (rows.length > MAX_COLLECTION_ITEMS) {
      message.error(`一个合集最多 ${MAX_COLLECTION_ITEMS} 个条目`);
      return;
    }
    if (!validation.valid) {
      message.error('请先修正标红的条目再保存');
      return;
    }
    try {
      await replace.mutateAsync(toItemPayload(rows));
      message.success('合集条目已保存');
      setDirty(false);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  if (!canWrite) {
    return <ReadOnlyItems collection={collection} />;
  }

  const columns: ColumnsType<EditableRow> = [
    {
      title: '顺序',
      key: 'position',
      width: 64,
      align: 'center',
      render: (_: unknown, _row, index) => index + 1,
    },
    {
      title: '资料',
      key: 'material',
      render: (_: unknown, row) => {
        const material = materials.get(row.material_id);
        if (!material) {
          return <span className="text-slate-500">资料暂不可用</span>;
        }
        return (
          <Link to={`/materials/${material.id}`} target="_blank" rel="noreferrer" className="truncate font-medium">
            {material.title}
          </Link>
        );
      },
    },
    {
      title: '角色',
      key: 'role',
      width: 120,
      render: (_: unknown, row) => <RoleTag role={row.role} />,
    },
    {
      title: '相对路径',
      key: 'relative_path',
      width: 300,
      render: (_: unknown, row) => {
        const error = validation.rowErrors[row.key];
        return (
          <div className="cell-stack">
            <Input
              size="small"
              value={row.relative_path}
              placeholder="相对路径"
              status={error ? 'error' : undefined}
              aria-label="条目相对路径"
              onChange={(event) => updateRow(row.key, { relative_path: event.target.value })}
            />
            {error ? <span className="text-xs text-red-600">{error}</span> : null}
          </div>
        );
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 156,
      render: (_: unknown, row, index) => (
        <Space size={2}>
          <Tooltip title="上移">
            <Button
              type="text"
              size="small"
              icon={<ArrowUpOutlined />}
              aria-label="上移"
              disabled={index === 0}
              onClick={() => moveRow(index, -1)}
            />
          </Tooltip>
          <Tooltip title="下移">
            <Button
              type="text"
              size="small"
              icon={<ArrowDownOutlined />}
              aria-label="下移"
              disabled={index === rows.length - 1}
              onClick={() => moveRow(index, 1)}
            />
          </Tooltip>
          <Tooltip title="移除（保存后生效）">
            <Button
              type="text"
              danger
              size="small"
              icon={<DeleteOutlined />}
              aria-label="移除条目"
              onClick={() => removeRow(row.key)}
            />
          </Tooltip>
        </Space>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="toolbar-row">
        <Space wrap>
          <Button icon={<PlusOutlined />} onClick={() => setPickerOpen(true)}>
            添加资料
          </Button>
          <Button
            icon={<SortAscendingOutlined />}
            disabled={rows.length < 2}
            onClick={() => {
              setRows((previous) => sortNaturally(previous, (row) => row.relative_path));
              setDirty(true);
            }}
          >
            按路径自然排序
          </Button>
          {dirty ? <Tag color="orange">未保存</Tag> : null}
        </Space>
        <Button
          type="primary"
          icon={<SaveOutlined />}
          loading={replace.isPending}
          disabled={!dirty}
          onClick={() => void handleSave()}
        >
          保存条目
        </Button>
      </div>

      {validation.globalErrors.map((text) => (
        <Alert key={text} type="error" showIcon title={text} />
      ))}

      <Table<EditableRow>
        rowKey="key"
        size="small"
        columns={columns}
        dataSource={rows}
        pagination={rows.length > 50 ? { pageSize: 50, showSizeChanger: false, showTotal: (total) => `共 ${total} 条` } : false}
        scroll={{ x: 860 }}
        rowClassName={(row) => (validation.rowErrors[row.key] ? 'bg-red-50' : '')}
        locale={{ emptyText: '暂无条目' }}
      />

      <MaterialPickerModal
        open={pickerOpen}
        existingIds={existingIds}
        onClose={() => setPickerOpen(false)}
        onAdd={handleAdd}
      />
    </div>
  );
}

function ReadOnlyItems({ collection }: { collection: CollectionDetail }) {
  const rows = rowsFromDetail(collection);
  const materials = materialsFromDetail(collection);
  const columns: ColumnsType<EditableRow> = [
    { title: '顺序', key: 'position', width: 64, align: 'center', render: (_: unknown, _row, index) => index + 1 },
    {
      title: '资料',
      key: 'material',
      render: (_: unknown, row) => {
        const material = materials.get(row.material_id);
        return material ? (
          <Link to={`/materials/${material.id}`}>{material.title}</Link>
        ) : (
          <span>资料暂不可用</span>
        );
      },
    },
    {
      title: '角色',
      key: 'role',
      width: 110,
      render: (_: unknown, row) => <RoleTag role={row.role} />,
    },
    {
      title: '相对路径',
      dataIndex: 'relative_path',
      key: 'relative_path',
      render: (value: string) => value || <span className="text-slate-400">—</span>,
    },
  ];
  return (
    <Table<EditableRow>
      rowKey="key"
      size="small"
      columns={columns}
      dataSource={rows}
      pagination={false}
      scroll={{ x: 640 }}
      locale={{ emptyText: '合集暂无条目' }}
    />
  );
}
