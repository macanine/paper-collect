import { Alert, Button, Stack, Tabs } from '@mantine/core';
import { describeError } from '../../shared/api/errors';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message } from '../../shared/ui/feedback';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { DrivesTab } from './DrivesTab';
import { RulesTab } from './RulesTab';
import { ReplicasTab } from './ReplicasTab';
import { useDrives, useReconcile, useRules } from './queries';

export function StoragePage() {
  const { can } = useAuth();
  const canWrite = can('storage:write');
  const canRead = can('storage:read');

  const drives = useDrives(canRead);
  const rules = useRules(canRead);
  const reconcile = useReconcile();

  if (drives.isPending || rules.isPending) {
    return (
      <Stack gap="lg">
        <PageHeader title="存储" />
        <LoadingState />
      </Stack>
    );
  }
  if (drives.isError || rules.isError) {
    return (
      <Stack gap="lg">
        <PageHeader title="存储" />
        <ErrorState error={drives.error ?? rules.error} onRetry={() => { void drives.refetch(); void rules.refetch(); }} />
      </Stack>
    );
  }

  const driveList = drives.data ?? [];
  const ruleList = rules.data ?? [];
  const attention = driveList.reduce((sum, d) => sum + (d.stats?.failed ?? 0), 0);

  return (
    <Stack gap="lg">
      <PageHeader
        title="存储"
        extra={
          <Button
            variant="default"
            onClick={() => reconcile.mutate(undefined, {
              onSuccess: (result) => message.success(`已补建 ${result.queued} 个复制任务`),
              onError: (error) => message.error(describeError(error).title),
            })}
            loading={reconcile.isPending}
            disabled={!canWrite}
          >
            补齐副本
          </Button>
        }
      />

      {attention > 0 ? <Alert color="yellow">{`有 ${attention} 个副本同步失败，可在「副本」标签页筛选「失败」查看并重试。`}</Alert> : null}

      <Tabs defaultValue="drives" keepMounted={false}>
        <Tabs.List mb="md">
          <Tabs.Tab value="drives">网盘</Tabs.Tab>
          <Tabs.Tab value="rules">同步规则</Tabs.Tab>
          <Tabs.Tab value="replicas">副本</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="drives"><DrivesTab canWrite={canWrite} canRoute={can('nodes:manage')} /></Tabs.Panel>
        <Tabs.Panel value="rules"><RulesTab canWrite={canWrite} rules={ruleList} drives={driveList} /></Tabs.Panel>
        <Tabs.Panel value="replicas"><ReplicasTab canWrite={canWrite} /></Tabs.Panel>
      </Tabs>
    </Stack>
  );
}
