import { useEffect, useState } from 'react';
import { Alert, App, Button, Card, Form, Input, Modal, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router';
import { describeError } from '../../shared/api/errors';
import type { ClusterNode, CreatedNode } from '../../shared/api/types';
import { formatBytes, formatRate } from '../../shared/lib/format';
import { CopyButton } from '../../shared/ui/CopyButton';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { NodeStatusTag } from '../../shared/ui/StatusTags';
import { createNode, listNodes, patchNode, type NodePatchBody } from './api';
import { CpuSparkline } from './MetricChart';
import { NODE_POLL_MS, formatAgo, isLive, originNeedsTLS, totalSlots, workerEnv } from './nodeUtils';

export function useNodes() {
  return useQuery({
    queryKey: ['nodes'],
    queryFn: ({ signal }) => listNodes(signal),
    refetchInterval: NODE_POLL_MS,
  });
}

export function useNodeUpdate() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: NodePatchBody }) => patchNode(id, body),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['nodes'] }),
    onError: (error) => {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    },
  });
}

export function NodesPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedNode | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [form] = Form.useForm<{ name: string }>();
  const query = useNodes();
  const update = useNodeUpdate();
  const origin = window.location.origin;

  // 每次拉取后刷新“几秒前”，不需要单独的计时器。
  useEffect(() => setNow(Date.now()), [query.dataUpdatedAt]);

  const create = useMutation({
    mutationFn: createNode,
    onSuccess: (result) => {
      setCreated(result);
      setCreating(false);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ['nodes'] });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const masterProtocol = query.data?.find((node) => node.is_master)?.protocol;

  const columns: ColumnsType<ClusterNode> = [
    {
      title: '名称',
      key: 'name',
      fixed: 'left',
      render: (_, node) => (
        <Space size={4} wrap>
          <Link to={`/admin/nodes/${node.id}`}>{node.name}</Link>
          {node.is_master ? <Tag>主端</Tag> : null}
          {node.protocol && masterProtocol && node.protocol !== masterProtocol ? <Tag color="error">协议不一致</Tag> : null}
        </Space>
      ),
    },
    { title: '状态', key: 'status', render: (_, node) => <NodeStatusTag status={node.status} /> },
    { title: '心跳', key: 'seen', render: (_, node) => formatAgo(node.last_seen_at, now) },
    {
      title: 'CPU',
      key: 'cpu',
      render: (_, node) =>
        isLive(node) ? (
          <Space size={8}>
            <span>{Math.round(node.cpu_percent)}%</span>
            <CpuSparkline nodeId={node.id} />
          </Space>
        ) : '—',
    },
    {
      title: '内存',
      key: 'memory',
      render: (_, node) => (isLive(node) ? `${formatBytes(node.mem_used)} / ${formatBytes(node.mem_total)}` : '—'),
    },
    {
      title: '网络 ↓ / ↑',
      key: 'network',
      render: (_, node) => (isLive(node) ? `${formatRate(node.net_rx_bps)} / ${formatRate(node.net_tx_bps)}` : '—'),
    },
    {
      title: '任务',
      key: 'jobs',
      render: (_, node) => {
        const running = Object.values(node.running_kinds).reduce<number>((sum, count) => sum + (count ?? 0), 0);
        return <Link to={`/jobs?node_id=${node.id}&status=running`}>{running} / {totalSlots(node.slots)}</Link>;
      },
    },
    {
      title: '近 1 小时',
      key: 'recent',
      render: (_, node) => (
        <Link to={`/jobs?node_id=${node.id}`}>
          成功 {node.succeeded_1h}，失败 {node.failed_1h}
        </Link>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, node) => (
        <Space size={4}>
          <Button
            size="small"
            disabled={!node.enabled}
            onClick={() => update.mutate({ id: node.id, body: { draining: !node.draining } })}
          >
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
              <Button size="small" danger>停用</Button>
            </Popconfirm>
          ) : (
            <Button size="small" onClick={() => update.mutate({ id: node.id, body: { enabled: true } })}>启用</Button>
          )}
        </Space>
      ),
    },
  ];

  const handleCreate = async () => {
    const { name } = await form.validateFields();
    create.mutate(name.trim());
  };

  return (
    <div className="page-stack">
      <PageHeader
        title="节点"
        description="主端负责调度和存储状态，从端只领取并执行上传、AI 识别和索引任务。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
            添加节点
          </Button>
        }
      />

      {query.isPending ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} title="节点加载失败" /> : null}
      {query.isSuccess ? (
        <Card styles={{ body: { padding: 0 } }}>
          <Table<ClusterNode>
            rowKey="id"
            size="middle"
            dataSource={query.data}
            columns={columns}
            pagination={false}
            scroll={{ x: 'max-content' }}
            locale={{ emptyText: <EmptyState /> }}
          />
        </Card>
      ) : null}

      <Modal
        title="添加节点"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={() => void handleCreate()}
        okText="创建"
        cancelText="取消"
        confirmLoading={create.isPending}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="name"
            label="节点名称"
            rules={[{ required: true, whitespace: true, message: '请输入节点名称' }, { max: 64, message: '不能超过 64 个字符' }]}
          >
            <Input placeholder="例如：家里 NAS" autoFocus />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="节点已创建"
        open={created !== null}
        onCancel={() => setCreated(null)}
        onOk={() => setCreated(null)}
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
        maskClosable={false}
      >
        <Typography.Paragraph>令牌仅显示这一次。把下面的环境变量配置到从端后启动它，节点会在几秒内上线。</Typography.Paragraph>
        {created ? (
          <>
            <pre className="node-token">{workerEnv(origin, created.token)}</pre>
            <CopyButton value={workerEnv(origin, created.token)} label="复制配置" />
          </>
        ) : null}
        {originNeedsTLS(origin) ? (
          <Alert
            style={{ marginTop: 12 }}
            type="warning"
            showIcon
            message="当前地址不是 HTTPS"
            description="任务会携带解密后的网盘和模型密钥，从端拒绝通过明文连接非本机主端。请为主端配置 HTTPS，并把 MASTER_URL 改成 HTTPS 地址。"
          />
        ) : null}
      </Modal>
    </div>
  );
}
