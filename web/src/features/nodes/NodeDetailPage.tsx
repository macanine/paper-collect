import { useEffect, useMemo } from 'react';
import { Alert, App, Button, Card, Descriptions, Form, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { useOCRProviders } from '../ai/queries';
import { listJobs } from '../jobs/api';
import type { Job, NodePoint } from '../../shared/api/types';
import { formatBytes, formatDateTime, formatRate } from '../../shared/lib/format';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { BackLink } from '../../shared/ui/BackLink';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState, NotFoundPage } from '../../shared/ui/States';
import { NodeStatusTag } from '../../shared/ui/StatusTags';
import { getNodeMetrics } from './api';
import { useClusterSettings } from './ClusterSettingsCard';
import { MetricChart, type MetricSeries } from './MetricChart';
import { useNodes, useNodeUpdate } from './NodesPage';
import { NodeRuntimeFields } from './NodeRuntimeFields';
import {
  NODE_POLL_MS,
  formatAgo,
  isLive,
  orderedKinds,
  runtimeBody,
  runtimeFormValues,
  type NodeRuntimeFormValues,
} from './nodeUtils';
import { href } from '../../app/routes';

const PRIMARY = 'var(--ant-primary)';
const SECOND = 'var(--chart-series-2)';

const CPU: MetricSeries[] = [{ label: '整机', color: PRIMARY, value: (p: NodePoint) => p.cpu_percent }];
const MEMORY: MetricSeries[] = [{ label: '已用', color: PRIMARY, value: (p: NodePoint) => p.mem_used }];
const NETWORK: MetricSeries[] = [
  { label: '接收', color: PRIMARY, value: (p: NodePoint) => p.net_rx_bps },
  { label: '发送', color: SECOND, value: (p: NodePoint) => p.net_tx_bps },
];

export function NodeDetailPage() {
  const { nodeId = '' } = useParams();
  const { message } = App.useApp();
  const nodes = useNodes();
  const providers = useOCRProviders();
  const clusterSettings = useClusterSettings();
  const update = useNodeUpdate();
  const [runtimeForm] = Form.useForm<NodeRuntimeFormValues>();
  const metrics = useQuery({
    queryKey: ['nodes', nodeId, 'metrics'],
    queryFn: ({ signal }) => getNodeMetrics(nodeId, signal),
    refetchInterval: NODE_POLL_MS,
  });
  const running = useQuery({
    queryKey: ['nodes', nodeId, 'running-jobs'],
    queryFn: ({ signal }) => listJobs({ node_id: nodeId, status: 'running', page: 1, page_size: 50 }, signal),
    refetchInterval: NODE_POLL_MS,
  });
  const points = useMemo(() => metrics.data ?? [], [metrics.data]);
  const node = nodes.data?.find((item) => item.id === nodeId);

  useEffect(() => {
    if (node && !node.is_master) runtimeForm.setFieldsValue(runtimeFormValues(node));
  }, [node?.id, node?.config_revision, runtimeForm]);

  if (nodes.isPending) return <LoadingState />;
  if (nodes.isError) return <ErrorState error={nodes.error} onRetry={() => void nodes.refetch()} title="节点加载失败" />;
  if (!node) return <NotFoundPage />;

  const memTotal = points.reduce((peak, point) => Math.max(peak, point.mem_total), node.mem_total);
  const saveRuntime = async () => {
    const values = await runtimeForm.validateFields();
    try {
      await update.mutateAsync({ id: node.id, body: runtimeBody(values) });
      message.success('节点运行配置已保存，将在下一次心跳应用');
    } catch {
      // useNodeUpdate 已显示接口错误。
    }
  };
  return (
    <div className="page-stack">
      <BackLink to={href.nodes()}>返回节点列表</BackLink>
      <PageHeader
        title={
          <Typography.Text
            editable={{
              tooltip: '重命名',
              onChange: (name) => {
                if (name.trim() && name !== node.name) update.mutate({ id: node.id, body: { name: name.trim() } });
              },
            }}
          >
            {node.name}
          </Typography.Text>
        }
        extra={
          <Space wrap>
            <NodeStatusTag status={node.status} />
            <Button disabled={!node.enabled} onClick={() => update.mutate({ id: node.id, body: { draining: !node.draining } })}>
              {node.draining ? '恢复' : '排空'}
            </Button>
            {node.is_master ? null : node.enabled ? (
              <Popconfirm
                title="停用该节点？"
                description="令牌立即失效，正在执行的任务会停止，并在租约到期后交给其他节点。"
                okText="停用"
                okButtonProps={{ danger: true }}
                cancelText="取消"
                onConfirm={() => update.mutate({ id: node.id, body: { enabled: false } })}
              >
                <Button danger>停用</Button>
              </Popconfirm>
            ) : (
              <Button onClick={() => update.mutate({ id: node.id, body: { enabled: true } })}>启用</Button>
            )}
          </Space>
        }
      />

      {node.is_master && node.draining ? (
        <Alert type="info" showIcon title="主端排空中" description="主端不再执行可下放的任务，但仍会处理归档、配套和本地目录网盘的复制，这些任务只能在主端运行。" />
      ) : null}

      <Card title="资源趋势（最近 1 小时）">
        <div className="node-chart-grid">
          <MetricChart title="CPU" points={points} series={CPU} format={(v) => `${Math.round(v)}%`} max={100} />
          <MetricChart title="内存" points={points} series={MEMORY} format={formatBytes} max={memTotal || undefined} />
          <MetricChart title="网络" points={points} series={NETWORK} format={formatRate} />
        </div>
        <Typography.Paragraph type="secondary" style={{ margin: '12px 0 0' }}>
          在容器中运行时，CPU 和内存是宿主机的数值，网络是容器网络命名空间的流量。
        </Typography.Paragraph>
      </Card>

      <Card title="节点信息">
        <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }}>
          <Descriptions.Item label="最近心跳">{formatAgo(node.last_seen_at, Date.now())}</Descriptions.Item>
          <Descriptions.Item label="启动时间">{formatDateTime(node.started_at)}</Descriptions.Item>
          <Descriptions.Item label="来源地址">{node.last_ip || '—'}</Descriptions.Item>
          <Descriptions.Item label="版本">{node.version || '—'}</Descriptions.Item>
          <Descriptions.Item label="协议">{node.protocol || '—'}</Descriptions.Item>
          <Descriptions.Item label="CPU 核数">{node.cpu_cores || '—'}</Descriptions.Item>
          <Descriptions.Item label="进程">
            {isLive(node) ? `CPU ${Math.round(node.proc_cpu_percent)}%，内存 ${formatBytes(node.proc_rss)}` : '—'}
          </Descriptions.Item>
          {node.is_master ? null : (
            <Descriptions.Item label="原件缓存">
              {formatBytes(node.cache_used)} / {node.cache_max > 0 ? formatBytes(node.cache_max) : '不限'}
            </Descriptions.Item>
          )}
          <Descriptions.Item label="近 1 小时">成功 {node.succeeded_1h}，失败 {node.failed_1h}</Descriptions.Item>
          <Descriptions.Item label="任务槽位" span={3}>
            {node.kinds.length === 0 ? '未执行可下放的任务' : (
              <Space size={4} wrap>
                {orderedKinds(node.kinds).map((kind) => (
                  <Tag key={kind}>
                    {JOB_KIND_LABELS[kind]} {node.running_kinds[kind] ?? 0}/{node.slots[kind] ?? 0}
                  </Tag>
                ))}
              </Space>
            )}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {node.is_master ? (
        <Card
          title="运行配置"
          extra={<Tag color="blue">由集群调度统一控制</Tag>}
        >
          <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }}>
            <Descriptions.Item label="执行任务">
              {clusterSettings.data?.master_kinds.length
                ? clusterSettings.data.master_kinds.map((kind) => JOB_KIND_LABELS[kind]).join('、')
                : '仅主端专属任务'}
            </Descriptions.Item>
            <Descriptions.Item label="归档并发">{clusterSettings.data?.master_ingest_concurrency ?? '—'}</Descriptions.Item>
            <Descriptions.Item label="空闲轮询">
              {clusterSettings.data ? `${clusterSettings.data.poll_seconds} 秒` : '—'}
            </Descriptions.Item>
          </Descriptions>
          <Typography.Paragraph type="secondary" style={{ margin: '12px 0 0' }}>
            主端并发、任务类型、租约和心跳均在节点列表顶部统一配置。
          </Typography.Paragraph>
          <Link to={href.nodes()}>前往集群调度设置</Link>
        </Card>
      ) : (
        <Card
          title="运行配置"
          extra={
            node.applied_config_revision === node.config_revision ? (
              <Tag color="green">已应用修订 {node.config_revision}</Tag>
            ) : node.applied_config_revision === 0 ? (
              <Tag color="orange">等待首次应用 · 最新 {node.config_revision}</Tag>
            ) : (
              <Tag color="orange">已应用 {node.applied_config_revision} / 最新 {node.config_revision}</Tag>
            )
          }
        >
          {node.applied_config_revision !== node.config_revision ? (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 20 }}
              title="配置等待从端应用"
              description={`从端下一次心跳会获取修订 ${node.config_revision}。如果长时间不更新，请检查节点是否在线和网络连接。`}
            />
          ) : null}
          <Form form={runtimeForm} layout="vertical">
            <NodeRuntimeFields
              providers={providers.data ?? []}
              providersLoading={providers.isPending}
              providersError={providers.isError}
            />
            <Space style={{ marginTop: 20 }} wrap>
              <Button
                type="primary"
                loading={update.isPending}
                onClick={() => void saveRuntime()}
                disabled={providers.isError}
              >
                保存运行配置
              </Button>
              <Typography.Text type="secondary">
                已用缓存 {formatBytes(node.cache_used)} / {node.cache_max_bytes > 0 ? formatBytes(node.cache_max_bytes) : '不限'}
              </Typography.Text>
            </Space>
          </Form>
        </Card>
      )}

      <Card title="正在执行" styles={{ body: { padding: 0 } }}>
        {running.isError ? <ErrorState error={running.error} onRetry={() => void running.refetch()} title="任务加载失败" /> : (
          <Table<Job>
            rowKey="id"
            size="middle"
            loading={running.isPending}
            dataSource={running.data?.items ?? []}
            pagination={false}
            scroll={{ x: 'max-content' }}
            locale={{ emptyText: '当前没有执行中的任务' }}
            columns={[
              { title: '类型', key: 'kind', render: (_, job) => <Link to={href.job(job.id)}>{JOB_KIND_LABELS[job.kind]}</Link> },
              { title: '进度', key: 'progress', render: (_, job) => job.progress_message || '—' },
              { title: '开始时间', key: 'started', render: (_, job) => formatDateTime(job.started_at) },
            ]}
          />
        )}
      </Card>
    </div>
  );
}
