import { useEffect, useState } from 'react';
import { Alert, Anchor, Button, Code, Group, Modal, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconPlus, IconSettings } from '@tabler/icons-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { ClusterNode, CreatedNode } from '../../shared/api/types';
import { formatBytes, formatRate } from '../../shared/lib/format';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { CopyButton } from '../../shared/ui/CopyButton';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { FormModal } from '../../shared/ui/FormModal';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { NodeStatusTag, Tag } from '../../shared/ui/StatusTags';
import { useOCRProviders } from '../ai/queries';
import { createNode, listNodes, patchNode, type NodeCreateBody, type NodePatchBody } from './api';
import { ClusterSettingsCard } from './ClusterSettingsCard';
import { CpuSparkline } from './MetricChart';
import { NodeRuntimeFields, validateRuntime } from './NodeRuntimeFields';
import {
  NODE_POLL_MS,
  formatAgo,
  isLive,
  orderedKinds,
  originNeedsTLS,
  runtimeBody,
  runtimeFormValues,
  totalSlots,
  workerEnv,
  type NodeRuntimeFormValues,
} from './nodeUtils';
import { href } from '../../app/routes';
import { qk } from '../../app/queryKeys';

interface NodeFormValues extends NodeRuntimeFormValues {
  name: string;
}

export function useNodes() {
  return useQuery({
    queryKey: qk.nodes.list,
    queryFn: ({ signal }) => listNodes(signal),
    refetchInterval: NODE_POLL_MS,
  });
}

export function useNodeUpdate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: NodePatchBody }) => patchNode(id, body),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: qk.nodes.all }),
    onError: (error) => {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    },
  });
}

export function NodesPage() {
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<ClusterNode | null>(null);
  const [created, setCreated] = useState<CreatedNode | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const form = useForm<NodeFormValues>({
    initialValues: { name: '', ...runtimeFormValues() },
    validate: (values) => ({
      ...(!values.name.trim() ? { name: '请输入节点名称' } : values.name.length > 64 ? { name: '不能超过 64 个字符' } : {}),
      ...validateRuntime(values),
    }),
  });
  const query = useNodes();
  const providers = useOCRProviders();
  const update = useNodeUpdate();
  const origin = window.location.origin;

  // 每次拉取后刷新“几秒前”，不需要单独的计时器。
  useEffect(() => setNow(Date.now()), [query.dataUpdatedAt]);

  const saveNode = useMutation({
    mutationFn: async (values: NodeFormValues) => {
      const body: NodeCreateBody = { name: values.name.trim(), ...runtimeBody(values) };
      if (editing) {
        await patchNode(editing.id, body);
        return null;
      }
      return createNode(body);
    },
    onSuccess: (result) => {
      if (result) setCreated(result);
      message.success(editing ? '节点配置已保存，将在下一次心跳应用' : '节点已创建');
      setCreating(false);
      setEditing(null);
      void queryClient.invalidateQueries({ queryKey: qk.nodes.all });
    },
    onError: (error) => {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    },
  });

  const masterProtocol = query.data?.find((node) => node.is_master)?.protocol;
  const openCreate = () => {
    setEditing(null);
    form.setValues({ name: '', ...runtimeFormValues() });
    form.resetDirty();
    setCreating(true);
  };
  const openEdit = (node: ClusterNode) => {
    if (node.is_master) return;
    form.setValues({ name: node.name, ...runtimeFormValues(node) });
    form.resetDirty();
    setEditing(node);
  };
  const closeForm = () => { setCreating(false); setEditing(null); };

  const columns: Column<ClusterNode>[] = [
    {
      key: 'name',
      title: '名称',
      render: (node) => (
        <Group gap="xs" wrap="nowrap">
          <Anchor component={Link} to={href.node(node.id)} fw={600} size="sm">{node.name}</Anchor>
          {node.is_master ? <Tag>主端</Tag> : null}
          {node.protocol && masterProtocol && node.protocol !== masterProtocol ? <Tag color="error">协议不一致</Tag> : null}
        </Group>
      ),
    },
    { key: 'status', title: '状态', render: (node) => <NodeStatusTag status={node.status} /> },
    { key: 'seen', title: '心跳', render: (node) => formatAgo(node.last_seen_at, now) },
    {
      key: 'cpu',
      title: 'CPU',
      render: (node) => isLive(node) ? (
        <Group gap="xs" wrap="nowrap"><Text size="sm" w={36}>{Math.round(node.cpu_percent)}%</Text><CpuSparkline nodeId={node.id} /></Group>
      ) : '—',
    },
    { key: 'memory', title: '内存', render: (node) => (isLive(node) ? `${formatBytes(node.mem_used)} / ${formatBytes(node.mem_total)}` : '—') },
    { key: 'network', title: '网络 ↓ / ↑', render: (node) => (isLive(node) ? `${formatRate(node.net_rx_bps)} / ${formatRate(node.net_tx_bps)}` : '—') },
    {
      key: 'jobs',
      title: '任务',
      render: (node) => {
        const running = Object.values(node.running_kinds).reduce<number>((sum, count) => sum + (count ?? 0), 0);
        return (
          <Stack gap={0}>
            <Anchor component={Link} to={`/jobs?node_id=${node.id}&status=running`} size="sm">{running} / {totalSlots(node.slots)}</Anchor>
            <Text size="xs" c="dimmed">{node.is_master ? `主端运行 ${totalSlots(node.slots)} 槽` : `配置 ${totalSlots(node.configured_slots)} 槽`}</Text>
          </Stack>
        );
      },
    },
    {
      key: 'revision',
      title: '配置修订',
      render: (node) => node.is_master ? <Tag>全局调度</Tag>
        : node.applied_config_revision === node.config_revision ? <Tag color="success">已应用 {node.config_revision}</Tag>
        : <Tag color="warning">等待应用 {node.applied_config_revision} / {node.config_revision}</Tag>,
    },
    {
      key: 'recent',
      title: '近 1 小时',
      render: (node) => <Anchor component={Link} to={href.jobs({ node_id: node.id })} size="sm">成功 {node.succeeded_1h}，失败 {node.failed_1h}</Anchor>,
    },
    {
      key: 'actions',
      title: '操作',
      render: (node) => (
        <Group gap="xs" wrap="nowrap">
          <Button size="compact-sm" variant="default" leftSection={<IconSettings size={14} />} onClick={() => openEdit(node)} disabled={node.is_master}>配置</Button>
          <Button size="compact-sm" variant="default" disabled={!node.enabled} onClick={() => update.mutate({ id: node.id, body: { draining: !node.draining } })}>{node.draining ? '恢复' : '排空'}</Button>
          {node.is_master ? null : node.enabled ? (
            <ConfirmButton size="compact-sm" variant="default" color="red" confirmTitle="停用该节点？" confirmDescription="令牌立即失效，正在执行的任务会停止，并在租约到期后交给其他节点。" okText="停用" danger onConfirm={() => update.mutate({ id: node.id, body: { enabled: false } })}>停用</ConfirmButton>
          ) : (
            <Button size="compact-sm" variant="default" onClick={() => update.mutate({ id: node.id, body: { enabled: true } })}>启用</Button>
          )}
        </Group>
      ),
    },
  ];

  return (
    <Stack gap="lg">
      <PageHeader
        title="节点"
        description="主端负责调度和存储状态，从端只领取并执行上传、AI 识别和索引任务。"
        extra={<Button leftSection={<IconPlus size={16} />} onClick={openCreate}>添加节点</Button>}
      />

      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} title="节点加载失败" /> : null}
      {query.isSuccess ? <DataTable rows={query.data} columns={columns} rowKey={(n) => n.id} empty={<EmptyState />} minWidth={1200} /> : null}
      <ClusterSettingsCard />

      <FormModal
        opened={creating || editing !== null}
        onClose={closeForm}
        title={editing ? `配置 ${editing.name}` : '添加节点'}
        size={760}
        okText={editing ? '保存配置' : '创建'}
        loading={saveNode.isPending}
        onSubmit={form.onSubmit((values) => saveNode.mutate(values))}
      >
        <TextInput label="节点名称" withAsterisk placeholder="例如：家里 NAS" data-autofocus {...form.getInputProps('name')} />
        <NodeRuntimeFields form={form} providers={providers.data ?? []} providersLoading={providers.isPending} providersError={providers.isError} />
      </FormModal>

      <Modal opened={created !== null} onClose={() => setCreated(null)} title="节点已创建" closeOnClickOutside={false} closeOnEscape={false} withCloseButton={false} size="lg">
        <Stack gap="md">
          <Text size="sm">令牌仅显示这一次。把下面的接入信息配置到从端后启动它；槽位、缓存上限和 OCR 服务已经由主端下发，不需要写进 env。</Text>
          {created ? (
            <>
              <Code block>{workerEnv(origin, created.token)}</Code>
              <Group><CopyButton value={workerEnv(origin, created.token)} label="复制配置" /></Group>
              <Text size="sm" c="dimmed">
                当前配置：{orderedKinds((['replicate', 'analyze', 'index'] as const).filter((kind) => (created.configured_slots[kind] ?? 0) > 0))
                  .map((kind) => `${JOB_KIND_LABELS[kind]} ${created.configured_slots[kind] ?? 0}`)
                  .join('，') || '暂不执行可下放任务'}；原件缓存上限 {created.cache_max_bytes > 0 ? formatBytes(created.cache_max_bytes) : '不限'}。
              </Text>
            </>
          ) : null}
          {originNeedsTLS(origin) ? (
            <Alert color="yellow" title="当前地址不是 HTTPS">任务会携带解密后的网盘和模型密钥，从端拒绝通过明文连接非本机主端。请为主端配置 HTTPS，并把 MASTER_URL 改成 HTTPS 地址。</Alert>
          ) : null}
          <Group justify="flex-end"><Button onClick={() => setCreated(null)}>我已保存</Button></Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
