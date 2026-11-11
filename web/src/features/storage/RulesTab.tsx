import { useEffect, useMemo, useState } from 'react';
import { ActionIcon, Button, Group, Modal, Stack, Text } from '@mantine/core';
import { IconArrowDown, IconArrowUp, IconEdit, IconPlus, IconTrash } from '@tabler/icons-react';
import { describeError } from '../../shared/api/errors';
import type { Drive, SyncAction, SyncPreview, SyncRule } from '../../shared/api/types';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Section } from '../../shared/ui/Section';
import { message } from '../../shared/ui/feedback';
import { EmptyState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import { RuleFormModal } from './RuleFormModal';
import { SYNC_ACTION_COLORS, SYNC_ACTION_LABELS, describeConditions, driveName, makeRule } from './ruleUtils';
import { usePreviewRules, usePutRules } from './queries';

const PREVIEW_PAGE_SIZE = 50;

const ACTION_ORDER = ['upload', 'move', 'share', 'orphan', 'skip', 'cancel', 'drift', 'unchanged', 'waiting_catalog'];

function previewSummary(preview: SyncPreview): string {
  return ACTION_ORDER.filter((key) => (preview.counts[key] ?? 0) > 0)
    .map((key) => `${SYNC_ACTION_LABELS[key]} ${preview.counts[key]}`)
    .join(' · ');
}

interface RulesTabProps {
  canWrite: boolean;
  rules: SyncRule[];
  drives: Drive[];
}

export function RulesTab({ canWrite, rules, drives }: RulesTabProps) {
  const [draft, setDraft] = useState<SyncRule[]>(rules);
  const [editing, setEditing] = useState<SyncRule | null>(null);
  const [editingIndex, setEditingIndex] = useState<number>(-1);
  const [creating, setCreating] = useState(false);
  const [preview, setPreview] = useState<SyncPreview | null>(null);
  const [previewPage, setPreviewPage] = useState(1);

  const previewMutation = usePreviewRules();
  const putRules = usePutRules();

  useEffect(() => {
    setDraft(rules);
  }, [rules]);

  const dirty = useMemo(() => JSON.stringify(draft) !== JSON.stringify(rules), [draft, rules]);

  const moveRule = (index: number, delta: number) => {
    const next = [...draft];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target]!, next[index]!];
    setDraft(next);
    setPreview(null);
  };

  const removeRule = (index: number) => {
    setDraft(draft.filter((_, i) => i !== index));
    setPreview(null);
  };

  const saveRule = (rule: SyncRule) => {
    if (creating) {
      const inserted = makeRule({ ...rule, id: rule.id || undefined });
      const defaultIndex = draft.findIndex((r) => r.is_default);
      const next = [...draft];
      next.splice(defaultIndex < 0 ? next.length : defaultIndex, 0, inserted);
      setDraft(next);
      setCreating(false);
    } else if (editingIndex >= 0) {
      const next = [...draft];
      next[editingIndex] = rule;
      setDraft(next);
      setEditing(null);
      setEditingIndex(-1);
    }
    setPreview(null);
  };

  const runPreview = (page: number) => {
    previewMutation.mutate(
      { rules: draft, page, pageSize: PREVIEW_PAGE_SIZE },
      {
        onSuccess: (result) => {
          setPreview(result);
          setPreviewPage(page);
        },
        onError: (error) => {
          const { title, detail } = describeError(error);
          message.error(detail ? `${title}：${detail}` : title);
        },
      },
    );
  };

  const applyRules = () => {
    if (!preview) return;
    putRules.mutate(
      { rules: draft, previewToken: preview.preview_token },
      {
        onSuccess: (result) => {
          message.success(`规则已保存，已生成 ${result.queued} 个同步任务`);
          setPreview(null);
        },
        onError: (error) => {
          const { title, detail } = describeError(error);
          message.error(detail ? `${title}：${detail}` : title);
          setPreview(null);
        },
      },
    );
  };

  const ruleColumns: Column<SyncRule>[] = [
    {
      key: 'order', title: '顺序', width: 90,
      render: (record) => {
        const index = draft.findIndex((r) => r.id === record.id);
        return (
          <Group gap={0} wrap="nowrap">
            <ActionIcon variant="subtle" color="gray" disabled={!canWrite || index === 0 || Boolean(draft[index]?.is_default)} onClick={() => moveRule(index, -1)} aria-label="上移"><IconArrowUp size={16} /></ActionIcon>
            <ActionIcon variant="subtle" color="gray" disabled={!canWrite || index >= draft.length - 1 || draft[index + 1]?.is_default === true} onClick={() => moveRule(index, 1)} aria-label="下移"><IconArrowDown size={16} /></ActionIcon>
          </Group>
        );
      },
    },
    {
      key: 'rule', wrap: true, title: '规则', width: 260,
      render: (record) => {
        const conditions = describeConditions(record.conditions);
        return (
          <Stack gap={4}>
            <Group gap={6} wrap="nowrap">
              <Text fw={600} size="sm">{record.name}</Text>
              {!record.enabled ? <Tag>已停用</Tag> : null}
              {record.is_default ? <Tag color="blue">兜底</Tag> : null}
            </Group>
            <Text size="xs" c="dimmed">{record.is_default ? '匹配所有未被前面规则命中的文件' : conditions || '无条件（匹配所有文件）'}</Text>
          </Stack>
        );
      },
    },
    {
      key: 'targets', wrap: true, title: '目标',
      render: (record) => record.targets.length === 0 ? <Text size="sm" c="dimmed">不上传</Text> : (
        <Stack gap={4}>
          {record.targets.map((target) => (
            <Group key={target.drive_id} gap={6} wrap="wrap">
              <Text fw={600} size="sm">{driveName(drives, target.drive_id)}</Text>
              <Text c="dimmed" size="sm">→</Text>
              <Text ff="monospace" size="xs" style={{ wordBreak: 'break-all' }}>{target.path_template}</Text>
              {target.share ? <Tag>分享</Tag> : null}
              {target.required ? <Tag color="blue">必需</Tag> : null}
            </Group>
          ))}
        </Stack>
      ),
    },
    {
      key: 'actions', title: '操作', width: 150,
      render: (record) => {
        const index = draft.findIndex((r) => r.id === record.id);
        return (
          <Group gap={6} wrap="nowrap">
            <Button size="compact-sm" variant="default" leftSection={<IconEdit size={14} />} disabled={!canWrite} onClick={() => { setEditing(record); setEditingIndex(index); setCreating(false); }}>编辑</Button>
            {!record.is_default ? <ActionIcon variant="default" color="red" disabled={!canWrite} onClick={() => removeRule(index)} aria-label="删除规则"><IconTrash size={14} /></ActionIcon> : null}
          </Group>
        );
      },
    },
  ];

  const previewColumns: Column<SyncAction>[] = [
    { key: 'action', title: '动作', width: 100, render: (r) => <Tag color={SYNC_ACTION_COLORS[r.action]}>{SYNC_ACTION_LABELS[r.action] ?? r.action}</Tag> },
    { key: 'file_name', wrap: true, title: '文件', render: (r) => <Text size="sm" style={{ wordBreak: 'break-all' }}>{r.file_name}</Text> },
    { key: 'drive', title: '网盘', width: 140, render: (r) => driveName(drives, r.drive_id) },
    { key: 'rule', wrap: true, title: '规则', width: 120, render: (r) => r.rule_name || '—' },
    {
      key: 'path', wrap: true, title: '路径',
      render: (r) => (
        <Stack gap={0}>
          {r.desired_path ? <Text size="xs" style={{ wordBreak: 'break-all' }}>期望：{r.desired_path}</Text> : null}
          {r.remote_path && r.remote_path !== r.desired_path ? <Text size="xs" c="dimmed" style={{ wordBreak: 'break-all' }}>当前：{r.remote_path}</Text> : null}
        </Stack>
      ),
    },
    { key: 'reason', title: '原因', width: 140, render: (r) => r.reason || '—' },
  ];

  return (
    <>
      <Section
        title="同步规则"
        flush
        extra={
          <Group gap="xs">
            <Button variant="default" leftSection={<IconPlus size={16} />} disabled={!canWrite} onClick={() => { setCreating(true); setEditing(null); setEditingIndex(-1); }}>新建规则</Button>
            <Button disabled={!canWrite || !dirty} loading={previewMutation.isPending} onClick={() => runPreview(1)}>预览并保存</Button>
          </Group>
        }
      >
        <DataTable bare rows={draft} columns={ruleColumns} rowKey={(r) => r.id} empty={<EmptyState description="还没有同步规则" />} minWidth={880} />
        <Text size="xs" c="dimmed" px="lg" py="sm" style={{ borderTop: '1px solid var(--mantine-color-gray-2)' }}>规则自上而下匹配，命中第一条为止；修改后需要「预览并保存」才会生效。</Text>
      </Section>

      {editing !== null || creating ? (
        <RuleFormModal
          open
          rule={creating ? null : editing}
          drives={drives}
          onClose={() => { setCreating(false); setEditing(null); setEditingIndex(-1); }}
          onSave={saveRule}
        />
      ) : null}

      <Modal opened={preview !== null} onClose={() => setPreview(null)} title="规则变更预览" size={960}>
        {preview ? (
          <Stack gap="md">
            <Text size="sm">{previewSummary(preview) || '没有需要变更的内容'}</Text>
            <DataTable
              rows={preview.items}
              columns={previewColumns}
              rowKey={(r) => `${r.replica_id}:${r.action}`}
              loading={previewMutation.isPending}
              minWidth={760}
              empty={<Text size="sm" c="dimmed">没有明细（所有文件保持现状或等待编目）</Text>}
              pagination={{ page: previewPage, pageSize: PREVIEW_PAGE_SIZE, total: preview.total, pageSizes: [PREVIEW_PAGE_SIZE], onChange: (page) => runPreview(page) }}
            />
            <Text size="xs" c="dimmed">预览 15 分钟内有效；期间若网盘或规则被其他人修改，应用时会要求重新预览。</Text>
            <Group justify="flex-end"><Button variant="default" onClick={() => setPreview(null)}>取消</Button><Button loading={putRules.isPending} onClick={applyRules}>应用规则</Button></Group>
          </Stack>
        ) : null}
      </Modal>
    </>
  );
}
