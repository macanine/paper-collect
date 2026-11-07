import { App, Alert, Button, Card, Tabs } from 'antd';
import { describeError } from '../../shared/api/errors';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { useAuth } from '../auth/AuthContext';
import { DrivesTab } from './DrivesTab';
import { RulesTab } from './RulesTab';
import { ReplicasTab } from './ReplicasTab';
import { useDrives, useReconcile, useRules } from './queries';

export function StoragePage() {
  const { can } = useAuth();
  const { message } = App.useApp();
  const canWrite = can('storage:write');
  const canRead = can('storage:read');

  const drives = useDrives(canRead);
  const rules = useRules(canRead);
  const reconcile = useReconcile();

  if (drives.isPending || rules.isPending) {
    return (
      <div className="page-stack">
        <PageHeader title="存储" />
        <LoadingState />
      </div>
    );
  }
  if (drives.isError || rules.isError) {
    return (
      <div className="page-stack">
        <PageHeader title="存储" />
        <ErrorState
          error={drives.error ?? rules.error}
          onRetry={() => {
            void drives.refetch();
            void rules.refetch();
          }}
        />
      </div>
    );
  }

  const driveList = drives.data ?? [];
  const ruleList = rules.data ?? [];
  const attention = driveList.reduce((sum, d) => sum + (d.stats?.failed ?? 0), 0);

  return (
    <div className="page-stack">
      <PageHeader
        title="存储"
        extra={
          <Button
            onClick={() =>
              reconcile.mutate(undefined, {
                onSuccess: (result) => message.success(`已补建 ${result.queued} 个复制任务`),
                onError: (error) => message.error(describeError(error).title),
              })
            }
            loading={reconcile.isPending}
            disabled={!canWrite}
          >
            补齐副本
          </Button>
        }
      />

      {attention > 0 ? (
        <Alert type="warning" showIcon message={`有 ${attention} 个副本同步失败，可在「副本」标签页筛选「失败」查看并重试。`} />
      ) : null}

      <Card styles={{ body: { padding: 0 } }}>
        <Tabs
          className="px-6"
          items={[
            { key: 'drives', label: '网盘', children: <div className="pb-6"><DrivesTab canWrite={canWrite} /></div> },
            { key: 'rules', label: '同步规则', children: <div className="pb-6"><RulesTab canWrite={canWrite} rules={ruleList} drives={driveList} /></div> },
            { key: 'replicas', label: '副本', children: <div className="pb-6"><ReplicasTab canWrite={canWrite} /></div> },
          ]}
        />
      </Card>
    </div>
  );
}
