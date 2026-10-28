import { useEffect, useMemo, useState } from 'react';
import { Input, Modal, Table } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { TableRowSelection } from 'antd/es/table/interface';
import { Link } from 'react-router';
import type { Material, Role, SubjectCode } from '../../shared/api/types';
import { formatBytes } from '../../shared/lib/format';
import { RoleTag, SubjectTags } from '../../shared/ui/StatusTags';
import { useMaterialsList } from '../materials/queries';

interface MaterialPickerModalProps {
  open: boolean;
  /** 已在合集内的资料；禁用勾选，避免提交重复资料。 */
  existingIds: ReadonlySet<string>;
  onClose: () => void;
  onAdd: (materials: Material[]) => void;
}

/** 从资料库选择要加入合集的资料。按关键词搜索，支持跨页多选。 */
export function MaterialPickerModal({ open, existingIds, onClose, onAdd }: MaterialPickerModalProps) {
  const [selected, setSelected] = useState<Map<string, Material>>(new Map());
  const [qInput, setQInput] = useState('');
  const [q, setQ] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  useEffect(() => {
    if (!open) return;
    setSelected(new Map());
    setQInput('');
    setQ('');
    setPage(1);
  }, [open]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQ(qInput.trim());
      setPage(1);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [qInput]);

  const query = useMaterialsList({ page, page_size: pageSize, q: q || undefined });

  const columns: ColumnsType<Material> = [
    {
      title: '资料',
      key: 'title',
      render: (_: unknown, record) => (
        <div className="cell-stack">
          <Link to={`/materials/${record.id}`} target="_blank" rel="noreferrer" className="truncate font-medium">
            {record.title}
          </Link>
          <span className="table-subtext truncate">{record.file_name}</span>
        </div>
      ),
    },
    {
      title: '学科',
      dataIndex: 'subject_codes',
      key: 'subjects',
      width: 180,
      render: (codes: SubjectCode[]) => <SubjectTags codes={codes} />,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 96,
      render: (role: Role) => <RoleTag role={role} />,
    },
    {
      title: '大小',
      dataIndex: 'size_bytes',
      key: 'size',
      width: 96,
      align: 'right',
      render: (size: number) => formatBytes(size),
    },
  ];

  const rowSelection: TableRowSelection<Material> = {
    selectedRowKeys: [...selected.keys()],
    preserveSelectedRowKeys: true,
    getCheckboxProps: (record) => ({ disabled: existingIds.has(record.id) }),
    onChange: (keys, rows) => {
      const keySet = new Set(keys.map(String));
      setSelected((previous) => {
        const next = new Map(previous);
        for (const row of rows) {
          if (keySet.has(row.id)) next.set(row.id, row);
          else next.delete(row.id);
        }
        return next;
      });
    },
  };

  const selectedMaterials = useMemo(() => [...selected.values()], [selected]);

  return (
    <Modal
      title="添加资料到合集"
      open={open}
      onCancel={onClose}
      width={900}
      okText={selectedMaterials.length > 0 ? `添加所选（${selectedMaterials.length}）` : '添加所选'}
      cancelText="取消"
      okButtonProps={{ disabled: selectedMaterials.length === 0 }}
      onOk={() => {
        onAdd(selectedMaterials);
        onClose();
      }}
      destroyOnHidden
    >
      <div className="flex flex-col gap-3">
        <Input.Search
          allowClear
          placeholder="搜索资料标题或描述"
          value={qInput}
          onChange={(event) => setQInput(event.target.value)}
          maxLength={200}
          style={{ width: 320 }}
        />
        <Table<Material>
          rowKey="id"
          size="small"
          columns={columns}
          dataSource={query.data?.items ?? []}
          loading={query.isFetching}
          rowSelection={rowSelection}
          scroll={{ x: 720 }}
          locale={{ emptyText: query.isPending ? '正在加载…' : '暂无数据' }}
          pagination={{
            current: page,
            pageSize,
            total: query.data?.total ?? 0,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50],
            showTotal: (total) => `共 ${total} 条`,
            onChange: (nextPage, nextSize) => {
              setPage(nextPage);
              setPageSize(nextSize);
            },
          }}
        />
      </div>
    </Modal>
  );
}
