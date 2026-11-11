import { useEffect } from 'react';
import { App, Button, Card, Checkbox, Form, InputNumber, Space, Spin, Tag, Typography } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { ClusterSettings, NodeKind } from '../../shared/api/types';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { getClusterSettings, patchClusterSettings, type ClusterSettingsPatch } from './api';
import { NODE_POLL_MS } from './nodeUtils';
import { qk } from '../../app/queryKeys';

interface ClusterSettingsFormValues {
  master_kinds: NodeKind[];
  master_ingest_concurrency: number;
  master_replicate_concurrency: number;
  master_analyze_concurrency: number;
  master_index_concurrency: number;
  lease_seconds: number;
  heartbeat_seconds: number;
  claim_wait_seconds: number;
  poll_seconds: number;
}

const settingValues = (settings: ClusterSettings): ClusterSettingsFormValues => ({
  master_kinds: settings.master_kinds,
  master_ingest_concurrency: settings.master_ingest_concurrency,
  master_replicate_concurrency: settings.master_replicate_concurrency,
  master_analyze_concurrency: settings.master_analyze_concurrency,
  master_index_concurrency: settings.master_index_concurrency,
  lease_seconds: settings.lease_seconds,
  heartbeat_seconds: settings.heartbeat_seconds,
  claim_wait_seconds: settings.claim_wait_seconds,
  poll_seconds: settings.poll_seconds,
});

export function useClusterSettings() {
  return useQuery({
    queryKey: qk.nodes.clusterSettings,
    queryFn: ({ signal }) => getClusterSettings(signal),
    refetchInterval: NODE_POLL_MS,
  });
}

export function ClusterSettingsCard() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<ClusterSettingsFormValues>();
  const query = useClusterSettings();
  const save = useMutation({
    mutationFn: (body: ClusterSettingsPatch) => patchClusterSettings(body),
    onSuccess: (settings) => {
      queryClient.setQueryData(['cluster-settings'], settings);
      void queryClient.invalidateQueries({ queryKey: qk.nodes.all });
      message.success('集群调度设置已保存');
    },
    onError: (error) => {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    },
  });

  useEffect(() => {
    if (query.data) form.setFieldsValue(settingValues(query.data));
  }, [form, query.data]);

  const submit = async () => {
    const values = await form.validateFields();
    save.mutate(values);
  };

  return (
    <Card
      title="集群调度"
      extra={query.data ? <Tag color="blue">修订 {query.data.revision}</Tag> : null}
    >
      {query.isPending ? <Spin /> : null}
      {query.isError ? (
        <Space direction="vertical">
          <Typography.Text type="danger">调度设置加载失败</Typography.Text>
          <Button onClick={() => void query.refetch()}>重试</Button>
        </Space>
      ) : null}
      {query.isSuccess ? (
        <Form form={form} layout="vertical" onFinish={() => void submit()}>
          <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
            这些参数保存在主端数据库，保存后主端立即重配；从端在下一次心跳获取并应用，节点详情会显示配置修订状态。
          </Typography.Paragraph>

          <Form.Item
            name="master_kinds"
            label="主端执行的可下放任务"
            extra="归档、配套和 local 网盘复制始终留在主端；这里决定主端是否也执行复制、AI 分析或索引。"
          >
            <Checkbox.Group
              options={(['replicate', 'analyze', 'index'] as const).map((kind) => ({
                value: kind,
                label: JOB_KIND_LABELS[kind],
              }))}
            />
          </Form.Item>

          <Typography.Text strong>主端并发</Typography.Text>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4" style={{ marginTop: 12 }}>
            {[
              ['master_ingest_concurrency', '归档'],
              ['master_replicate_concurrency', '复制上传'],
              ['master_analyze_concurrency', 'AI 分析'],
              ['master_index_concurrency', '题目索引'],
            ].map(([name, label]) => (
              <Form.Item
                key={name}
                name={name}
                label={label}
                rules={[{ required: true, type: 'number', min: 1, max: 64, message: '请输入 1 到 64' }]}
                style={{ marginBottom: 0 }}
              >
                <InputNumber min={1} max={64} precision={0} className="w-full" />
              </Form.Item>
            ))}
          </div>

          <Typography.Text strong style={{ display: 'block', marginTop: 20 }}>租约与调度周期</Typography.Text>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4" style={{ marginTop: 12 }}>
            <Form.Item
              name="lease_seconds"
              label="任务租约"
              extra="30–3600 秒"
              rules={[{ required: true, type: 'number', min: 30, max: 3600 }]}
            >
              <InputNumber min={30} max={3600} precision={0} addonAfter="秒" className="w-full" />
            </Form.Item>
            <Form.Item
              name="heartbeat_seconds"
              label="从端心跳"
              extra="必须小于租约"
              dependencies={['lease_seconds']}
              rules={[
                { required: true, type: 'number', min: 1 },
                ({ getFieldValue }) => ({
                  validator: async (_rule, value: number) => {
                    if (value >= getFieldValue('lease_seconds')) throw new Error('心跳必须小于租约');
                  },
                }),
              ]}
            >
              <InputNumber min={1} precision={0} addonAfter="秒" className="w-full" />
            </Form.Item>
            <Form.Item
              name="claim_wait_seconds"
              label="从端领取等待"
              extra="1–25 秒"
              dependencies={['lease_seconds']}
              rules={[
                { required: true, type: 'number', min: 1, max: 25 },
                ({ getFieldValue }) => ({
                  validator: async (_rule, value: number) => {
                    if (value >= getFieldValue('lease_seconds')) throw new Error('领取等待必须小于租约');
                  },
                }),
              ]}
            >
              <InputNumber min={1} max={25} precision={0} addonAfter="秒" className="w-full" />
            </Form.Item>
            <Form.Item
              name="poll_seconds"
              label="主端空闲轮询"
              rules={[{ required: true, type: 'number', min: 1, max: 60 }]}
            >
              <InputNumber min={1} max={60} precision={0} addonAfter="秒" className="w-full" />
            </Form.Item>
          </div>

          <Space style={{ marginTop: 20 }} wrap>
            <Button type="primary" htmlType="submit" loading={save.isPending}>保存调度设置</Button>
            <Typography.Text type="secondary">
              {query.data ? `最近更新 ${new Date(query.data.updated_at).toLocaleString()}` : null}
            </Typography.Text>
          </Space>
        </Form>
      ) : null}
    </Card>
  );
}
