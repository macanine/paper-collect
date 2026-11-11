import { useEffect, useMemo } from 'react';
import { Alert, Anchor, Button, Group, SimpleGrid, Stack, Text, TextInput } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { useOCRProviders } from '../ai/queries';
import { listJobs } from '../jobs/api';
import type { Job, NodePoint } from '../../shared/api/types';
import { formatBytes, formatDateTime, formatRate } from '../../shared/lib/format';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { BackLink } from '../../shared/ui/BackLink';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Descriptions } from '../../shared/ui/Descriptions';
import { PageHeader } from '../../shared/ui/PageHeader';
import { Section } from '../../shared/ui/Section';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { ErrorState, LoadingState, NotFoundPage } from '../../shared/ui/States';
import { NodeStatusTag, Tag } from '../../shared/ui/StatusTags';
import { getNodeMetrics } from './api';
import { useClusterSettings } from './ClusterSettingsCard';
import { MetricChart, type MetricSeries } from './MetricChart';
import { useNodes, useNodeUpdate } from './NodesPage';
import { NodeRuntimeFields, validateRuntime } from './NodeRuntimeFields';
import { NODE_POLL_MS, formatAgo, isLive, orderedKinds, runtimeBody, runtimeFormValues, type NodeRuntimeFormValues } from './nodeUtils';
import { href } from '../../app/routes';
import { qk } from '../../app/queryKeys';

const PRIMARY = 'var(--mantine-color-blue-6)';
const SECOND = 'var(--mantine-color-teal-6)';

const CPU: MetricSeries[] = [{ label: '整机', color: PRIMARY, value: (p: NodePoint) => p.cpu_percent }];
const MEMORY: MetricSeries[] = [{ label: '已用', color: PRIMARY, value: (p: NodePoint) => p.mem_used }];
const NETWORK: MetricSeries[] = [
  { label: '接收', color: PRIMARY, value: (p: NodePoint) => p.net_rx_bps },
  { label: '发送', color: SECOND, value: (p: NodePoint) => p.net_tx_bps },
];

const RUNNING_COLUMNS: Column<Job>[] = [
  { key: 'kind', title: '类型', render: (job) => <Anchor component={Link} to={href.job(job.id)} size="sm">{JOB_KIND_LABELS[job.kind]}</Anchor> },
  { key: 'progress', title: '进度', wrap: true, render: (job) => job.progress_message || '—' },
  { key: 'started', title: '开始时间', render: (job) => formatDateTime(job.started_at) },
];

export function NodeDetailPage() {
  const { nodeId = '' } = useParams();
  const nodes = useNodes();
  const providers = useOCRProviders();
  const clusterSettings = useClusterSettings();
  const update = useNodeUpdate();
  const runtimeForm = useForm<NodeRuntimeFormValues & { name?: string }>({
    initialValues: runtimeFormValues(),
    validate: (values) => validateRuntime(values),
  });
  const rename = useForm({ initialValues: { name: '' } });
  const metrics = useQuery({
    queryKey: qk.nodes.metrics(nodeId),
    queryFn: ({ signal }) => getNodeMetrics(nodeId, signal),
    refetchInterval: NODE_POLL_MS,
  });
  const running = useQuery({
    queryKey: qk.nodes.runningJobs(nodeId),
    queryFn: ({ signal }) => listJobs({ node_id: nodeId, status: 'running', page: 1, page_size: 50 }, signal),
    refetchInterval: NODE_POLL_MS,
  });
  const points = useMemo(() => metrics.data ?? [], [metrics.data]);
  const node = nodes.data?.find((item) => item.id === nodeId);

  useEffect(() => {
    if (node && !node.is_master) runtimeForm.setValues(runtimeFormValues(node));
    if (node) rename.setValues({ name: node.name });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node?.id, node?.config_revision, node?.name]);

  if (nodes.isPending) return <LoadingState />;
  if (nodes.isError) return <ErrorState error={nodes.error} onRetry={() => void nodes.refetch()} title="节点加载失败" />;
  if (!node) return <NotFoundPage />;

  const memTotal = points.reduce((peak, point) => Math.max(peak, point.mem_total), node.mem_total);
  const saveRuntime = runtimeForm.onSubmit(async (values) => {
    try {
      await update.mutateAsync({ id: node.id, body: runtimeBody(values) });
      message.success('节点运行配置已保存，将在下一次心跳应用');
    } catch {
      // useNodeUpdate 已显示接口错误。
    }
  });
  const commitName = () => {
    const name = rename.values.name.trim();
    if (name && name !== node.name) update.mutate({ id: node.id, body: { name } });
    else rename.setValues({ name: node.name });
  };
  const configTag = node.applied_config_revision === node.config_revision ? <Tag color="success">已应用修订 {node.config_revision}</Tag>
    : node.applied_config_revision === 0 ? <Tag color="warning">等待首次应用 · 最新 {node.config_revision}</Tag>
    : <Tag color="warning">已应用 {node.applied_config_revision} / 最新 {node.config_revision}</Tag>;

  return (
    <Stack gap="lg">
      <BackLink to={href.nodes()}>返回节点列表</BackLink>
      <PageHeader
        title={
          node.is_master ? node.name : (
            <TextInput
              variant="unstyled"
              aria-label="节点名称（失焦保存）"
              size="xl"
              fw={600}
              value={rename.values.name}
              onChange={(e) => rename.setFieldValue('name', e.currentTarget.value)}
              onBlur={commitName}
              onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur(); }}
              styles={{ input: { fontSize: 24, fontWeight: 600, height: 36, padding: 0 } }}
            />
          )
        }
        extra={
          <>
            <NodeStatusTag status={node.status} />
            <Button variant="default" disabled={!node.enabled} onClick={() => update.mutate({ id: node.id, body: { draining: !node.draining } })}>{node.draining ? '恢复' : '排空'}</Button>
            {node.is_master ? null : node.enabled ? (
              <ConfirmButton variant="default" color="red" confirmTitle="停用该节点？" confirmDescription="令牌立即失效，正在执行的任务会停止，并在租约到期后交给其他节点。" okText="停用" danger onConfirm={() => update.mutate({ id: node.id, body: { enabled: false } })}>停用</ConfirmButton>
            ) : (
              <Button variant="default" onClick={() => update.mutate({ id: node.id, body: { enabled: true } })}>启用</Button>
            )}
          </>
        }
      />

      {node.is_master && node.draining ? (
        <Alert color="blue" title="主端排空中">主端不再执行可下放的任务，但仍会处理归档、配套和本地目录网盘的复制，这些任务只能在主端运行。</Alert>
      ) : null}

      <Section title="资源趋势（最近 1 小时）">
        <SimpleGrid cols={{ base: 1, md: 3 }} spacing="lg">
          <MetricChart title="CPU" points={points} series={CPU} format={(v) => `${Math.round(v)}%`} max={100} />
          <MetricChart title="内存" points={points} series={MEMORY} format={formatBytes} max={memTotal || undefined} />
          <MetricChart title="网络" points={points} series={NETWORK} format={formatRate} />
        </SimpleGrid>
        <Text size="sm" c="dimmed">在容器中运行时，CPU 和内存是宿主机的数值，网络是容器网络命名空间的流量。</Text>
      </Section>

      <Section title="节点信息">
        <Descriptions
          cols={3}
          items={[
            { key: 'seen', label: '最近心跳', children: formatAgo(node.last_seen_at, Date.now()) },
            { key: 'started', label: '启动时间', children: formatDateTime(node.started_at) },
            { key: 'ip', label: '来源地址', children: node.last_ip || '—' },
            { key: 'version', label: '版本', children: node.version || '—' },
            { key: 'protocol', label: '协议', children: node.protocol || '—' },
            { key: 'cores', label: 'CPU 核数', children: node.cpu_cores || '—' },
            { key: 'proc', label: '进程', children: isLive(node) ? `CPU ${Math.round(node.proc_cpu_percent)}%，内存 ${formatBytes(node.proc_rss)}` : '—' },
            !node.is_master && { key: 'cache', label: '原件缓存', children: `${formatBytes(node.cache_used)} / ${node.cache_max > 0 ? formatBytes(node.cache_max) : '不限'}` },
            { key: 'recent', label: '近 1 小时', children: `成功 ${node.succeeded_1h}，失败 ${node.failed_1h}` },
            {
              key: 'slots', label: '任务槽位', full: true,
              children: node.kinds.length === 0 ? '未执行可下放的任务' : (
                <Group gap={4}>
                  {orderedKinds(node.kinds).map((kind) => <Tag key={kind}>{JOB_KIND_LABELS[kind]} {node.running_kinds[kind] ?? 0}/{node.slots[kind] ?? 0}</Tag>)}
                </Group>
              ),
            },
          ]}
        />
      </Section>

      {node.is_master ? (
        <Section title="运行配置" extra={<Tag>由集群调度统一控制</Tag>}>
          <Descriptions
            cols={3}
            items={[
              { key: 'kinds', label: '执行任务', children: clusterSettings.data?.master_kinds.length ? clusterSettings.data.master_kinds.map((kind) => JOB_KIND_LABELS[kind]).join('、') : '仅主端专属任务' },
              { key: 'ingest', label: '归档并发', children: clusterSettings.data?.master_ingest_concurrency ?? '—' },
              { key: 'poll', label: '空闲轮询', children: clusterSettings.data ? `${clusterSettings.data.poll_seconds} 秒` : '—' },
            ]}
          />
          <Text size="sm" c="dimmed">主端并发、任务类型、租约和心跳均在节点列表顶部统一配置。</Text>
          <Anchor component={Link} to={href.nodes()} size="sm">前往集群调度设置</Anchor>
        </Section>
      ) : (
        <Section title="运行配置" extra={configTag}>
          {node.applied_config_revision !== node.config_revision ? (
            <Alert color="yellow" title="配置等待从端应用">{`从端下一次心跳会获取修订 ${node.config_revision}。如果长时间不更新，请检查节点是否在线和网络连接。`}</Alert>
          ) : null}
          <form onSubmit={saveRuntime}>
            <Stack gap="lg">
              <NodeRuntimeFields form={runtimeForm} providers={providers.data ?? []} providersLoading={providers.isPending} providersError={providers.isError} />
              <Group>
                <Button type="submit" loading={update.isPending} disabled={providers.isError}>保存运行配置</Button>
                <Text size="sm" c="dimmed">已用缓存 {formatBytes(node.cache_used)} / {node.cache_max_bytes > 0 ? formatBytes(node.cache_max_bytes) : '不限'}</Text>
              </Group>
            </Stack>
          </form>
        </Section>
      )}

      <Section title="正在执行" flush>
        {running.isError ? <ErrorState error={running.error} onRetry={() => void running.refetch()} title="任务加载失败" /> : (
          <DataTable bare rows={running.data?.items ?? []} columns={RUNNING_COLUMNS} rowKey={(j) => j.id} loading={running.isPending} empty={<Text c="dimmed" size="sm">当前没有执行中的任务</Text>} minWidth={480} />
        )}
      </Section>
    </Stack>
  );
}
