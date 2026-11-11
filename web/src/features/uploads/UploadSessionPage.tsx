import { useEffect, useRef, useState } from 'react';
import { Alert, Anchor, Button, Group, Progress, SimpleGrid, Stack, Text, Title } from '@mantine/core';
import { IconCircleCheck } from '@tabler/icons-react';
import { Link, useParams } from 'react-router';
import { describeError, errorMessage } from '../../shared/api/errors';
import type { JobStatus, Upload } from '../../shared/api/types';
import { formatBytes, formatDuration, formatRate } from '../../shared/lib/format';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Metric } from '../../shared/ui/Metric';
import { PageHeader } from '../../shared/ui/PageHeader';
import { Section } from '../../shared/ui/Section';
import { message } from '../../shared/ui/feedback';
import { BackLink } from '../../shared/ui/BackLink';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { LinkButton } from '../../shared/ui/LinkButton';
import { StageTag, Tag, UploadStatusTag, type TagColor } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { retryJob } from '../jobs/api';
import { FileSelector } from './FileSelector';
import type { SourceResult } from './fileSources';
import { useFinalizeUpload, useJob, useUpload, uploadKeys } from './queries';
import { type RowStatus, type TransferRow, useUploadTransfer } from './useUploadTransfer';
import { useQueryClient } from '@tanstack/react-query';
import { href } from '../../app/routes';

export function UploadSessionPage() {
  const { uploadId = '' } = useParams();
  const query = useUpload(uploadId);

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <Stack gap="lg">
        <BackLink to="/imports">返回导入中心</BackLink>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="清单加载失败" />
      </Stack>
    );
  }
  return <UploadSessionPanel upload={query.data} />;
}

const ROW_STATUS: Record<RowStatus, { color: TagColor; text: string }> = {
  received: { color: 'success', text: '已接收' },
  queued: { color: 'default', text: '待传输' },
  uploading: { color: 'processing', text: '传输中' },
  failed: { color: 'error', text: '失败' },
  'missing-file': { color: 'warning', text: '需重新选择' },
};

function UploadSessionPanel({ upload }: { upload: Upload }) {
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const transfer = useUploadTransfer(upload);
  const finalize = useFinalizeUpload(upload.id);
  const job = useJob(upload.job_id);
  const [resumeRequested, setResumeRequested] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [transferSpeed, setTransferSpeed] = useState(0);
  const autoSubmitted = useRef(false);
  const speedSample = useRef({ bytes: 0, at: Date.now() });

  const isSubmitted = upload.job_id !== '';
  const canTransfer = upload.status === 'pending' && !isSubmitted;
  const { stats, rows, running } = transfer;

  useEffect(() => {
    const now = Date.now();
    const previous = speedSample.current;
    const deltaBytes = stats.doneBytes - previous.bytes;
    const elapsed = now - previous.at;
    if (running && deltaBytes > 0 && elapsed > 0) {
      setTransferSpeed(deltaBytes / (elapsed / 1000));
    } else if (!running) {
      setTransferSpeed(0);
    }
    speedSample.current = { bytes: stats.doneBytes, at: now };
  }, [running, stats.doneBytes]);

  useEffect(() => {
    if (resumeRequested && !running) {
      setResumeRequested(false);
      transfer.start();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resumeRequested, running]);

  const handleAttach = (result: SourceResult) => {
    const added = transfer.attachFiles(result.files);
    if (added === 0) {
      message.warning('所选文件与未完成的条目不匹配');
      return;
    }
    message.success(`已匹配 ${added} 个文件`);
    setResumeRequested(true);
  };

  const handleFinalize = async () => {
    try {
      await finalize.mutateAsync();
      message.success('已提交归档');
    } catch (error) {
      autoSubmitted.current = false;
      message.error(errorMessage(error));
    }
  };

  useEffect(() => {
    if (!canTransfer || !stats.allReceived || autoSubmitted.current) return;
    autoSubmitted.current = true;
    void handleFinalize();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canTransfer, stats.allReceived]);

  const handleRetryJob = async () => {
    setRetrying(true);
    try {
      await retryJob(upload.job_id);
      message.success('已重新排队');
      void queryClient.invalidateQueries({ queryKey: uploadKeys.job(upload.job_id) });
      void queryClient.invalidateQueries({ queryKey: uploadKeys.detail(upload.id) });
    } catch (error) {
      message.error(describeError(error).title);
    } finally {
      setRetrying(false);
    }
  };

  const percent = stats.totalBytes > 0 ? Math.round((stats.doneBytes / stats.totalBytes) * 100) : 0;
  const remainingBytes = Math.max(0, stats.totalBytes - stats.doneBytes);
  const etaMs = transferSpeed > 0 ? (remainingBytes / transferSpeed) * 1000 : undefined;

  const retryRow = (row: TransferRow) =>
    row.status === 'failed' && !running && canTransfer ? (
      <Button size="compact-sm" variant="default" onClick={() => transfer.start([row.entry.id])}>重试</Button>
    ) : null;

  const columns: Column<TransferRow>[] = [
    {
      key: 'path', wrap: true, title: '相对路径',
      render: (row) => (
        <Stack gap={4}>
          <Text size="sm" style={{ wordBreak: 'break-all' }}>{row.entry.relative_path}</Text>
          {row.error ? <Text size="xs" c="red">{row.error}</Text> : null}
        </Stack>
      ),
    },
    { key: 'size', title: '大小', width: 100, align: 'right', render: (row) => formatBytes(row.entry.size_bytes) },
    { key: 'status', title: '状态', width: 120, render: (row) => <Tag color={ROW_STATUS[row.status].color}>{ROW_STATUS[row.status].text}</Tag> },
    {
      key: 'progress', title: '进度', width: 200,
      render: (row) => {
        const size = row.entry.size_bytes;
        const value = size > 0 ? Math.min(100, Math.round((row.loaded / size) * 100)) : 0;
        return row.status === 'missing-file' ? '—' : <Progress size="sm" value={row.status === 'received' ? 100 : value} />;
      },
    },
    ...(stats.failedCount > 0 ? [{ key: 'actions', width: 80, render: (row: TransferRow) => retryRow(row) }] : []),
  ];

  const [filePage, setFilePage] = useState(1);

  return (
    <Stack gap="lg">
      <BackLink to="/imports">返回导入中心</BackLink>
      <PageHeader
        title={upload.title}
        extra={
          <>
            <UploadStatusTag status={upload.status} />
            {canTransfer && running ? <Button variant="default" color="red" onClick={() => transfer.cancel()}>暂停</Button> : null}
            {canTransfer ? (
              <Button variant="default" disabled={running || stats.failedCount === 0} onClick={() => transfer.start(rows.filter((row) => row.status === 'failed').map((row) => row.entry.id))}>重试失败项</Button>
            ) : null}
          </>
        }
      />

      {upload.status === 'completed' ? null : (
        <Section>
          <SimpleGrid cols={{ base: 2, sm: 3, xl: 6 }} spacing="md">
            <Metric label="已接收" value={`${stats.receivedCount} / ${stats.total}`} />
            <Metric label="已传输" value={formatBytes(stats.doneBytes)} />
            <Metric label="实时速度" value={formatRate(transferSpeed)} />
            <Metric label="预计剩余" value={etaMs === undefined ? '—' : formatDuration(etaMs)} />
            <Metric label="失败" value={stats.failedCount} />
            <Metric label="待重新选择" value={stats.missingCount} />
          </SimpleGrid>
          <Progress value={percent} color={stats.failedCount > 0 ? 'red' : 'blue'} />
        </Section>
      )}

      {canTransfer && stats.missingCount > 0 ? (
        <Alert color="yellow" title={`有 ${stats.missingCount} 个文件需要重新选择`}>
          <FileSelector inputId="resume-upload" compact onSelected={handleAttach} />
        </Alert>
      ) : null}

      {isSubmitted ? (
        <SubmittedCard
          upload={upload}
          jobPending={job.isPending}
          jobQueryError={job.error}
          jobStatus={job.data?.status}
          jobFailure={job.data?.error ?? ''}
          jobProgress={job.data?.progress_message ?? ''}
          canRetryJob={can('jobs:write')}
          retrying={retrying}
          onRetry={() => void handleRetryJob()}
        />
      ) : null}

      {upload.status === 'completed' ? null : (
        <Section title="文件清单" flush>
          <DataTable
            bare
            rows={rows.slice((filePage - 1) * 50, filePage * 50)}
            columns={columns}
            rowKey={(row) => row.entry.id}
            minWidth={720}
            pagination={{ page: filePage, pageSize: 50, total: rows.length, pageSizes: [50], onChange: (p) => setFilePage(p) }}
          />
        </Section>
      )}
    </Stack>
  );
}

interface SubmittedCardProps {
  upload: Upload;
  jobPending: boolean;
  jobQueryError: unknown;
  jobStatus: JobStatus | undefined;
  jobFailure: string;
  jobProgress: string;
  canRetryJob: boolean;
  retrying: boolean;
  onRetry: () => void;
}

function SubmittedCard({ upload, jobPending, jobQueryError, jobStatus, jobFailure, jobProgress, canRetryJob, retrying, onRetry }: SubmittedCardProps) {
  const [page, setPage] = useState(1);
  if (upload.status === 'completed') {
    const columns: Column<Upload['results'][number]>[] = [
      { key: 'path', wrap: true, title: '来源路径', render: (row) => <Text size="sm" style={{ wordBreak: 'break-all' }}>{row.path}</Text> },
      { key: 'result', title: '结果', width: 120, render: (row) => (row.duplicate ? <Tag>已存在</Tag> : <Tag color="success">新文件</Tag>) },
      { key: 'stage', title: '整理状态', width: 110, render: (row) => <StageTag stage={row.stage} /> },
      { key: 'pack', title: '归入', render: (row) => (row.pack_id ? <Anchor component={Link} to={href.pack(row.pack_id)} size="sm">{row.title || '查看套卷'}</Anchor> : '—') },
    ];
    return (
      <Section title="处理结果">
        <Stack align="center" gap="xs" py="md">
          <IconCircleCheck size={44} color="var(--mantine-color-green-6)" />
          <Title order={4}>{`新增 ${upload.new_count} 个文件，${upload.duplicate_count} 个已存在`}</Title>
          <Text size="sm" c="dimmed" ta="center">{upload.skipped.length > 0 ? `${upload.skipped.length} 个文件格式不支持，已跳过` : 'AI 正在阅读、分组并编目新文件；没能通过校验的会出现在“需关注”里'}</Text>
          <Group mt="xs"><LinkButton to="/inbox">查看需关注</LinkButton><LinkButton variant="default" to="/library">打开资料库</LinkButton></Group>
        </Stack>
        <DataTable
          rows={upload.results.slice((page - 1) * 50, page * 50)}
          columns={columns}
          rowKey={(row) => `${row.material_id}-${row.path}`}
          minWidth={560}
          pagination={upload.results.length > 50 ? { page, pageSize: 50, total: upload.results.length, pageSizes: [50], onChange: (p) => setPage(p) } : undefined}
        />
        {upload.skipped.length > 0 ? <Alert color="yellow" title="以下文件格式不支持，未归档">{upload.skipped.join('、')}</Alert> : null}
      </Section>
    );
  }

  return (
    <Section title="正在处理">
      {jobQueryError ? <Alert color="red">导入状态加载失败</Alert> : null}
      <Text size="sm" c="dimmed">{jobPending ? '正在安排导入…' : jobProgress || (jobStatus === 'failed' ? '导入失败' : '文件正在整理，请稍候')}</Text>
      {jobStatus === 'failed' ? (
        <Alert color="red" title="归档失败">
          <Group justify="space-between" wrap="nowrap">
            <Text size="sm">{jobFailure}</Text>
            {canRetryJob ? <Button size="compact-sm" variant="default" color="red" loading={retrying} onClick={onRetry}>重新排队</Button> : null}
          </Group>
        </Alert>
      ) : null}
    </Section>
  );
}
