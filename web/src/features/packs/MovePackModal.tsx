import { useState } from 'react';
import { Radio, Select, Stack, Text } from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import { useQueryClient } from '@tanstack/react-query';
import { errorMessage } from '../../shared/api/errors';
import { FormModal } from '../../shared/ui/FormModal';
import { message } from '../../shared/ui/feedback';
import { updateMaterial } from '../materials/api';
import { invalidateLibrary } from '../materials/queries';
import { usePacksList } from './queries';

/** 把一个文件移到别的套卷，或拆成独立套卷。 */
export function MovePackModal({ materialId, currentPackId, open, onClose, onMoved }: { materialId: string; currentPackId: string; open: boolean; onClose: () => void; onMoved?: (packId: string) => void }) {
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<'existing' | 'new'>('existing');
  const [search, setSearch] = useState('');
  const [debounced] = useDebouncedValue(search, 250);
  const [target, setTarget] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const list = usePacksList({ page: 1, page_size: 30, q: debounced || undefined }, open && mode === 'existing');
  const submit = async () => {
    if (mode === 'existing' && !target) return;
    setSaving(true);
    try {
      const result = await updateMaterial(materialId, mode === 'new' ? { new_pack: true } : { pack_id: target! });
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
    <FormModal opened={open} onClose={onClose} title="移动文件" okText="移动" loading={saving} onSubmit={() => void submit()}>
      <Radio.Group value={mode} onChange={(v) => setMode(v as 'existing' | 'new')}>
        <Stack gap="xs"><Radio value="existing" label="并入已有套卷" /><Radio value="new" label="拆成独立套卷" /></Stack>
      </Radio.Group>
      {mode === 'existing' ? (
        <Select
          searchable
          placeholder="搜索套卷名称、考试、机构"
          searchValue={search}
          onSearchChange={setSearch}
          value={target}
          onChange={setTarget}
          filter={({ options }) => options}
          nothingFoundMessage={list.isFetching ? '搜索中…' : '没有匹配的套卷'}
          data={(list.data?.items ?? []).filter((p) => p.id !== currentPackId).map((p) => ({ value: p.id, label: `${p.title}（${p.members.length} 个文件）` }))}
        />
      ) : (
        <Text size="sm" c="dimmed">新套卷会沿用当前套卷的学科、年级等编目信息，可随后修改。</Text>
      )}
    </FormModal>
  );
}
