import { IconPlayerStop } from '@tabler/icons-react';
import type { Job } from '../../shared/api/types';
import { describeError } from '../../shared/api/errors';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { useCancelJob } from './queries';

/** 取消排队中或运行中的任务。任务按失败结束，可再重试。 */
export function CancelJobButton({ job, size }: { job: Pick<Job, 'id' | 'status'>; size?: 'small' }) {
  const cancel = useCancelJob();
  if (job.status !== 'pending' && job.status !== 'running') return null;
  return (
    <ConfirmButton
      variant="default"
      size={size ? 'compact-sm' : 'sm'}
      leftSection={size ? undefined : <IconPlayerStop size={16} />}
      loading={cancel.isPending}
      confirmTitle="取消该任务？"
      confirmDescription="任务会按失败结束，相关文件进入失败后的状态；之后可以重试。"
      okText="取消任务"
      danger
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
      取消
    </ConfirmButton>
  );
}
