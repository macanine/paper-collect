import { useState } from 'react';
import { App, Modal, Radio, Select, Space, Typography } from 'antd';
import { errorMessage } from '../../shared/api/errors';
import { updateMaterial } from '../materials/api';
import { invalidateLibrary } from '../materials/queries';
import { useQueryClient } from '@tanstack/react-query';
import { usePacksList } from './queries';

/** 把一个文件移到别的套卷，或拆成独立套卷。 */
export function MovePackModal({ materialId, currentPackId, open, onClose, onMoved }: { materialId: string; currentPackId: string; open: boolean; onClose: () => void; onMoved?: (packId: string) => void }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<'existing' | 'new'>('existing');
  const [search, setSearch] = useState('');
  const [target, setTarget] = useState<string>();
  const [saving, setSaving] = useState(false);
  const list = usePacksList({ page: 1, page_size: 30, q: search || undefined }, open && mode === 'existing');
  const submit = async () => {
    setSaving(true);
    try {
      const result = await updateMaterial(materialId, mode === 'new' ? { new_pack: true } : { pack_id: target });
      invalidateLibrary(queryClient);
      message.success(mode === 'new' ? '已拆成独立套卷' : '已移入目标套卷');
      onMoved?.(result.pack_id);
      onClose();
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal open={open} title="移动文件" okText="移动" okButtonProps={{ disabled: mode === 'existing' && !target }} confirmLoading={saving} onOk={() => void submit()} onCancel={onClose} destroyOnHidden>
      <Space direction="vertical" className="w-full">
        <Radio.Group value={mode} onChange={(e) => setMode(e.target.value as 'existing' | 'new')} options={[{ value: 'existing', label: '并入已有套卷' }, { value: 'new', label: '拆成独立套卷' }]} />
        {mode === 'existing' ? (
          <Select
            showSearch
            className="w-full"
            placeholder="搜索套卷名称、考试、机构"
            filterOption={false}
            onSearch={setSearch}
            value={target}
            onChange={setTarget}
            loading={list.isFetching}
            options={(list.data?.items ?? []).filter((p) => p.id !== currentPackId).map((p) => ({ value: p.id, label: `${p.title}（${p.members.length} 个文件）` }))}
          />
        ) : (
          <Typography.Text type="secondary">新套卷会沿用当前套卷的学科、年级等编目信息，可随后修改。</Typography.Text>
        )}
      </Space>
    </Modal>
  );
}
