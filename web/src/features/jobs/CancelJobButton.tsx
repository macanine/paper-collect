import { App, Button, Popconfirm } from 'antd';
import { StopOutlined } from '@ant-design/icons';
import type { Job } from '../../shared/api/types';
import { describeError } from '../../shared/api/errors';
import { useCancelJob } from './queries';

/** 取消排队中或运行中的任务。任务按失败结束，可再重试。 */
export function CancelJobButton({ job, size }: { job: Pick<Job, 'id' | 'status'>; size?: 'small' }) {
  const { message } = App.useApp();
  const cancel = useCancelJob();
  if (job.status !== 'pending' && job.status !== 'running') return null;
  return (
    <Popconfirm
      title="取消该任务？"
      description="任务会按失败结束，相关文件进入失败后的状态；之后可以重试。"
      okText="取消任务"
      okButtonProps={{ danger: true }}
      cancelText="返回"
      onConfirm={async () => {
        try {
          await cancel.mutateAsync(job.id);
          message.success('任务已取消');
        } catch (error) {
          const { title, detail } = describeError(error);
          message.error(detail ? `${title}：${detail}` : title);
        }
      }}
    >
      <Button size={size} icon={size ? undefined : <StopOutlined />} loading={cancel.isPending}>
        取消
      </Button>
    </Popconfirm>
  );
}
