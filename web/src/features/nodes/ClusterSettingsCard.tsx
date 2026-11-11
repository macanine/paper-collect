import { useEffect } from 'react';
import { Button, Checkbox, Group, Loader, NumberInput, SimpleGrid, Stack, Text } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { ClusterSettings, NodeKind } from '../../shared/api/types';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { Section } from '../../shared/ui/Section';
import { message } from '../../shared/ui/feedback';
import { Tag } from '../../shared/ui/StatusTags';
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

const range = (min: number, max: number) => (v: number) => (typeof v === 'number' && v >= min && v <= max ? null : `请输入 ${min} 到 ${max}`);
const seconds = (label: string) => `${label}`;

export function ClusterSettingsCard() {
  const queryClient = useQueryClient();
  const query = useClusterSettings();
  const form = useForm<ClusterSettingsFormValues>({
    initialValues: { master_kinds: [], master_ingest_concurrency: 1, master_replicate_concurrency: 1, master_analyze_concurrency: 1, master_index_concurrency: 1, lease_seconds: 120, heartbeat_seconds: 15, claim_wait_seconds: 10, poll_seconds: 5 },
    validate: {
      master_ingest_concurrency: range(1, 64),
      master_replicate_concurrency: range(1, 64),
      master_analyze_concurrency: range(1, 64),
      master_index_concurrency: range(1, 64),
      lease_seconds: range(30, 3600),
      heartbeat_seconds: (v, all) => (typeof v !== 'number' || v < 1 ? '请输入大于 0 的值' : v >= all.lease_seconds ? '心跳必须小于租约' : null),
      claim_wait_seconds: (v, all) => (typeof v !== 'number' || v < 1 || v > 25 ? '请输入 1 到 25' : v >= all.lease_seconds ? '领取等待必须小于租约' : null),
      poll_seconds: range(1, 60),
    },
  });
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
    if (query.data) form.setValues(settingValues(query.data));
    form.resetDirty();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data]);

  return (
    <Section title="集群调度" extra={query.data ? <Tag color="blue">修订 {query.data.revision}</Tag> : null}>
      {query.isPending ? <Loader size="sm" /> : null}
      {query.isError ? (
        <Stack gap="xs" align="flex-start">
          <Text c="red" size="sm">调度设置加载失败</Text>
          <Button variant="default" onClick={() => void query.refetch()}>重试</Button>
        </Stack>
      ) : null}
      {query.isSuccess ? (
        <form onSubmit={form.onSubmit((values) => save.mutate(values))}>
          <Stack gap="lg">
            <Text size="sm" c="dimmed">这些参数保存在主端数据库，保存后主端立即重配；从端在下一次心跳获取并应用，节点详情会显示配置修订状态。</Text>

            <Checkbox.Group
              label="主端执行的可下放任务"
              description="归档、配套和 local 网盘复制始终留在主端；这里决定主端是否也执行复制、AI 分析或索引。"
              {...form.getInputProps('master_kinds')}
            >
              <Group mt="xs" gap="lg">
                {(['replicate', 'analyze', 'index'] as const).map((kind) => <Checkbox key={kind} value={kind} label={JOB_KIND_LABELS[kind]} />)}
              </Group>
            </Checkbox.Group>

            <Stack gap="xs">
              <Text fw={600} size="sm">主端并发</Text>
              <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="sm">
                <NumberInput label="归档" min={1} max={64} allowDecimal={false} {...form.getInputProps('master_ingest_concurrency')} />
                <NumberInput label="复制上传" min={1} max={64} allowDecimal={false} {...form.getInputProps('master_replicate_concurrency')} />
                <NumberInput label="AI 分析" min={1} max={64} allowDecimal={false} {...form.getInputProps('master_analyze_concurrency')} />
                <NumberInput label="题目索引" min={1} max={64} allowDecimal={false} {...form.getInputProps('master_index_concurrency')} />
              </SimpleGrid>
            </Stack>

            <Stack gap="xs">
              <Text fw={600} size="sm">租约与调度周期</Text>
              <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="sm">
                <NumberInput label="任务租约" description="30–3600 秒" min={30} max={3600} allowDecimal={false} suffix={seconds(' 秒')} {...form.getInputProps('lease_seconds')} />
                <NumberInput label="从端心跳" description="必须小于租约" min={1} allowDecimal={false} suffix=" 秒" {...form.getInputProps('heartbeat_seconds')} />
                <NumberInput label="从端领取等待" description="1–25 秒" min={1} max={25} allowDecimal={false} suffix=" 秒" {...form.getInputProps('claim_wait_seconds')} />
                <NumberInput label="主端空闲轮询" description="1–60 秒" min={1} max={60} allowDecimal={false} suffix=" 秒" {...form.getInputProps('poll_seconds')} />
              </SimpleGrid>
            </Stack>

            <Group>
              <Button type="submit" loading={save.isPending}>保存调度设置</Button>
              {query.data ? <Text size="sm" c="dimmed">最近更新 {new Date(query.data.updated_at).toLocaleString()}</Text> : null}
            </Group>
          </Stack>
        </form>
      ) : null}
    </Section>
  );
}
