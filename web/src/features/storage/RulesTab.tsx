import { useEffect, useMemo, useState } from 'react';
import { App, Button, Card, Modal, Space, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import { describeError } from '../../shared/api/errors';
import type { Drive, SyncAction, SyncPreview, SyncRule } from '../../shared/api/types';
import { EmptyState } from '../../shared/ui/States';
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
  const { message } = App.useApp();
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

  const ruleColumns: ColumnsType<SyncRule> = [
    {
      title: '顺序',
      key: 'order',
      width: 90,
      render: (_: unknown, _record, index) => (
        <Space size={0}>
          <Button size="small" type="text" icon={<ArrowUpOutlined />} disabled={!canWrite || index === 0 || Boolean(draft[index]?.is_default)} onClick={() => moveRule(index, -1)} aria-label="上移" />
          <Button
            size="small"
            type="text"
            icon={<ArrowDownOutlined />}
            disabled={!canWrite || index >= draft.length - 1 || draft[index + 1]?.is_default === true}
            onClick={() => moveRule(index, 1)}
            aria-label="下移"
          />
        </Space>
      ),
    },
    {
      title: '规则',
      key: 'rule',
      width: 240,
      render: (_: unknown, record) => {
        const conditions = describeConditions(record.conditions);
        return (
          <div className="flex flex-col gap-1">
            <span className="font-medium">
              {record.name}
              {!record.enabled ? <Tag className="ml-2">已停用</Tag> : null}
              {record.is_default ? <Tag className="ml-2" color="blue">兜底</Tag> : null}
            </span>
            {record.is_default ? <span className="text-xs text-slate-500">匹配所有未被前面规则命中的文件</span> : conditions ? <span className="text-xs text-slate-500">{conditions}</span> : <span className="text-xs text-slate-500">无条件（匹配所有文件）</span>}
          </div>
        );
      },
    },
    {
      title: '目标',
      key: 'targets',
      width: 420,
      render: (_: unknown, record) =>
        record.targets.length === 0 ? (
          <span className="text-slate-500">不上传</span>
        ) : (
          <div className="flex flex-col gap-1">
            {record.targets.map((target) => (
              <div key={target.drive_id} className="text-sm">
                <span className="font-medium">{driveName(drives, target.drive_id)}</span>
                <span className="mx-1 text-slate-400">→</span>
                <span className="break-all font-mono text-xs">{target.path_template}</span>
                {target.share ? <Tag className="ml-2">分享</Tag> : null}
                {target.required ? <Tag className="ml-1" color="blue">必需</Tag> : null}
              </div>
            ))}
          </div>
        ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 130,
      fixed: 'right' as const,
      render: (_: unknown, record, index) => (
        <Space size={4}>
          <Button
            size="small"
            icon={<EditOutlined />}
            disabled={!canWrite}
            onClick={() => {
              setEditing(record);
              setEditingIndex(index);
              setCreating(false);
            }}
          >
            编辑
          </Button>
          {!record.is_default ? (
            <Button size="small" danger icon={<DeleteOutlined />} disabled={!canWrite} onClick={() => removeRule(index)} aria-label="删除规则" />
          ) : null}
        </Space>
      ),
    },
  ];

  const previewColumns: ColumnsType<SyncAction> = [
    {
      title: '动作',
      dataIndex: 'action',
      key: 'action',
      width: 100,
      render: (action: string) => <Tag color={SYNC_ACTION_COLORS[action]}>{SYNC_ACTION_LABELS[action] ?? action}</Tag>,
    },
    { title: '文件', dataIndex: 'file_name', key: 'file_name', render: (name: string) => <span className="break-all">{name}</span> },
    { title: '网盘', dataIndex: 'drive_id', key: 'drive_id', width: 140, render: (id: string) => driveName(drives, id) },
    { title: '规则', dataIndex: 'rule_name', key: 'rule_name', width: 120, render: (name: string) => name || '—' },
    {
      title: '路径',
      key: 'path',
      render: (_: unknown, record) => (
        <div className="flex flex-col text-xs">
          {record.desired_path ? <span className="break-all">期望：{record.desired_path}</span> : null}
          {record.remote_path && record.remote_path !== record.desired_path ? <span className="break-all text-slate-500">当前：{record.remote_path}</span> : null}
        </div>
      ),
    },
    { title: '原因', dataIndex: 'reason', key: 'reason', width: 140, render: (reason: string) => reason || '—' },
  ];

  return (
    <>
      <Card
        title="同步规则"
        styles={{ body: { padding: 0 } }}
        extra={
          <Space size={8}>
            <Button
              icon={<PlusOutlined />}
              aria-label="新建规则"
              disabled={!canWrite}
              onClick={() => {
                setCreating(true);
                setEditing(null);
                setEditingIndex(-1);
              }}
            >
              <span className="max-sm:hidden">新建规则</span>
            </Button>
            <Button type="primary" disabled={!canWrite || !dirty} loading={previewMutation.isPending} onClick={() => runPreview(1)}>
              预览并保存
            </Button>
          </Space>
        }
      >
        <Table<SyncRule>
          rowKey="id"
          size="middle"
          dataSource={draft}
          columns={ruleColumns}
          locale={{ emptyText: <EmptyState description="还没有同步规则" /> }}
          pagination={false}
          scroll={{ x: 880 }}
        />
        <div className="border-t border-solid border-0 border-slate-100 px-6 py-3 text-xs text-slate-500">
          规则自上而下匹配，命中第一条为止；修改后需要「预览并保存」才会生效。
        </div>
      </Card>

      {editing !== null || creating ? (
        <RuleFormModal
          open
          rule={creating ? null : editing}
          drives={drives}
          onClose={() => {
            setCreating(false);
            setEditing(null);
            setEditingIndex(-1);
          }}
          onSave={saveRule}
        />
      ) : null}

      <Modal
        title="规则变更预览"
        open={preview !== null}
        onCancel={() => setPreview(null)}
        width={960}
        footer={
          <Space>
            <Button onClick={() => setPreview(null)}>取消</Button>
            <Button type="primary" loading={putRules.isPending} onClick={applyRules}>
              应用规则
            </Button>
          </Space>
        }
      >
        {preview ? (
          <div className="flex flex-col gap-3">
            <div className="text-sm">{previewSummary(preview) || '没有需要变更的内容'}</div>
            <Table<SyncAction>
              rowKey={(record) => `${record.replica_id}:${record.action}`}
              size="small"
              columns={previewColumns}
              dataSource={preview.items}
              locale={{ emptyText: '没有明细（所有文件保持现状或等待编目）' }}
              loading={previewMutation.isPending}
              pagination={{
                current: previewPage,
                pageSize: PREVIEW_PAGE_SIZE,
                total: preview.total,
                showSizeChanger: false,
                onChange: (page) => runPreview(page),
              }}
            />
            <div className="text-xs text-slate-500">预览 15 分钟内有效；期间若网盘或规则被其他人修改，应用时会要求重新预览。</div>
          </div>
        ) : null}
      </Modal>
    </>
  );
}
