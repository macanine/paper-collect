import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Card, Descriptions, Progress, Result, Space, Statistic, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Link, useParams } from 'react-router';
import { describeError, errorMessage } from '../../shared/api/errors';
import type { Job, JobStatus, Upload } from '../../shared/api/types';
import { formatBytes, formatDuration, formatRate } from '../../shared/lib/format';
import { UPLOAD_STATUS_LABELS } from '../../shared/lib/labels';
import { useCompactViewport } from '../../shared/lib/useCompactViewport';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { JobStatusTag, UploadStatusTag } from '../../shared/ui/StatusTags';
import { useAuth } from '../auth/AuthContext';
import { retryJob } from '../jobs/api';
import { FileSelector } from './FileSelector';
import type { SourceResult } from './fileSources';
import { useFinalizeUpload, useJob, useUpload, uploadKeys } from './queries';
import { type RowStatus, type TransferRow, useUploadTransfer } from './useUploadTransfer';
import { useQueryClient } from '@tanstack/react-query';

export function UploadSessionPage() {
  const { uploadId = '' } = useParams();
  const query = useUpload(uploadId);

  if (query.isPending) return <LoadingState />;
  if (query.isError) {
    return (
      <div className="page-stack">
        <BackLink />
        <ErrorState error={query.error} onRetry={() => void query.refetch()} title="清单加载失败" />
      </div>
    );
  }
  return <UploadSessionPanel upload={query.data} />;
}

const ROW_STATUS: Record<RowStatus, { color: string; text: string }> = {
  received: { color: 'success', text: '已接收' },
  queued: { color: 'default', text: '待传输' },
  uploading: { color: 'processing', text: '传输中' },
  failed: { color: 'error', text: '失败' },
  'missing-file': { color: 'warning', text: '需重新选择' },
};

function UploadSessionPanel({ upload }: { upload: Upload }) {
  const { can } = useAuth();
  const compact = useCompactViewport();
  const { message } = App.useApp();
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
      <Button size="small" onClick={() => transfer.start([row.entry.id])}>
        重试
      </Button>
    ) : null;

  const desktopColumns: ColumnsType<TransferRow> = [
    {
      title: '相对路径',
      key: 'path',
      render: (_: unknown, row) => (
        <div className="flex flex-col gap-1">
          <span className="break-all text-sm">{row.entry.relative_path}</span>
          {row.error ? <span className="text-xs text-red-600">{row.error}</span> : null}
        </div>
      ),
    },
    {
      title: '大小',
      key: 'size',
      width: 100,
      align: 'right',
      render: (_: unknown, row) => formatBytes(row.entry.size_bytes),
    },
    {
      title: '状态',
      key: 'status',
      width: 120,
      render: (_: unknown, row) => <Tag color={ROW_STATUS[row.status].color}>{ROW_STATUS[row.status].text}</Tag>,
    },
    {
      title: '进度',
      key: 'progress',
      width: 200,
      render: (_: unknown, row) => {
        const size = row.entry.size_bytes;
        const value = size > 0 ? Math.min(100, Math.round((row.loaded / size) * 100)) : 0;
        return row.status === 'missing-file' ? '—' : <Progress percent={row.status === 'received' ? 100 : value} size="small" />;
      },
    },
    ...(stats.failedCount > 0
      ? [
          {
            title: '',
            key: 'actions',
            width: 80,
            render: (_: unknown, row: TransferRow) => retryRow(row),
          },
        ]
      : []),
  ];

  const compactColumns: ColumnsType<TransferRow> = [
    {
      title: '文件',
      key: 'summary',
      render: (_: unknown, row) => {
        const size = row.entry.size_bytes;
        const value = size > 0 ? Math.min(100, Math.round((row.loaded / size) * 100)) : 0;
        return (
          <div className="compact-entity">
            <span className="compact-entity__title break-all">{row.entry.relative_path}</span>
            <div className="compact-entity__tags">
              <Tag color={ROW_STATUS[row.status].color}>{ROW_STATUS[row.status].text}</Tag>
              <span className="table-subtext">{formatBytes(size)}</span>
              {retryRow(row)}
            </div>
            {row.status === 'missing-file' ? (
              <span className="compact-entity__error">需要重新选择原文件后才能继续</span>
            ) : (
              <Progress percent={row.status === 'received' ? 100 : value} size="small" />
            )}
            {row.error ? <span className="compact-entity__error">{row.error}</span> : null}
          </div>
        );
      },
    },
  ];

  return (
    <div className="page-stack">
      <BackLink />
      <PageHeader
        title={upload.title}
        extra={
          <Space wrap>
            <UploadStatusTag status={upload.status} />
            {canTransfer ? (
              running ? (
                <Button danger onClick={() => transfer.cancel()}>
                  暂停
                </Button>
              ) : (
                <Button type="primary" disabled={!stats.readyToStart} onClick={() => transfer.start()}>
                  {stats.receivedCount > 0 ? '继续传输' : '开始传输'}
                </Button>
              )
            ) : null}
            {canTransfer ? (
              <Button
                disabled={running || stats.failedCount === 0}
                onClick={() => transfer.start(rows.filter((row) => row.status === 'failed').map((row) => row.entry.id))}
              >
                重试失败项
              </Button>
            ) : null}
            {canTransfer ? (
              <Button type="primary" ghost disabled={running || !stats.allReceived} loading={finalize.isPending} onClick={() => void handleFinalize()}>
                提交归档
              </Button>
            ) : null}
          </Space>
        }
      />

      <Card>
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-6">
          <Statistic title="已接收" value={stats.receivedCount} suffix={`/ ${stats.total}`} />
          <Statistic title="已传输" value={formatBytes(stats.doneBytes)} />
          <Statistic title="实时速度" value={formatRate(transferSpeed)} />
          <Statistic title="预计剩余" value={etaMs === undefined ? '—' : formatDuration(etaMs)} />
          <Statistic title="失败" value={stats.failedCount} />
          <Statistic title="待重新选择" value={stats.missingCount} />
        </div>
        <Progress className="mt-4" percent={percent} status={stats.failedCount > 0 ? 'exception' : undefined} />
      </Card>

      {canTransfer && stats.missingCount > 0 ? (
        <Alert
          type="warning"
          showIcon
          message={`有 ${stats.missingCount} 个文件需要重新选择`}
          description={<FileSelector inputId="resume-upload" compact onSelected={handleAttach} />}
        />
      ) : null}

      {isSubmitted ? (
        <SubmittedCard
          upload={upload}
          jobPending={job.isPending}
          jobQueryError={job.error}
          jobStatus={job.data?.status}
          jobFailure={job.data?.error ?? ''}
          jobProgress={job.data?.progress_message ?? ''}
          jobLogs={job.data?.logs}
          jobId={upload.job_id}
          attempts={job.data?.attempts ?? 0}
          canRetryJob={can('jobs:write')}
          retrying={retrying}
          onRetry={() => void handleRetryJob()}
        />
      ) : null}

      <Card title="文件清单">
        <Table<TransferRow>
          rowKey={(row) => row.entry.id}
          size="small"
          className={compact ? 'compact-list-table' : undefined}
          columns={compact ? compactColumns : desktopColumns}
          dataSource={rows}
          showHeader={!compact}
          pagination={{
            pageSize: 50,
            showSizeChanger: false,
            simple: compact,
            showTotal: (total) => `共 ${total} 个文件`,
          }}
          scroll={compact ? undefined : { x: 900 }}
        />
      </Card>
    </div>
  );
}

interface SubmittedCardProps {
  upload: Upload;
  jobId: string;
  jobPending: boolean;
  jobQueryError: unknown;
  jobStatus: JobStatus | undefined;
  jobFailure: string;
  jobProgress: string;
  jobLogs: Job['logs'];
  attempts: number;
  canRetryJob: boolean;
  retrying: boolean;
  onRetry: () => void;
}

function SubmittedCard({ upload, jobId, jobPending, jobQueryError, jobStatus, jobFailure, jobProgress, jobLogs, attempts, canRetryJob, retrying, onRetry }: SubmittedCardProps) {
  if (upload.status === 'completed') {
    const skipped = jobLogs?.filter((entry) => entry.message.includes('个不支持的文件，已跳过，未归档')) ?? [];
    return (
      <Card>
        {skipped.map((entry) => <Alert key={`${entry.time}-${entry.message}`} type="warning" showIcon message={entry.message} className="mb-3" />)}
        <Result
          status="success"
          title="归档完成"
          extra={[
            upload.material_id ? (
              <Link key="material" to={`/materials/${upload.material_id}`}>
                <Button type="primary">查看资料</Button>
              </Link>
            ) : null,
            upload.collection_id ? (
              <Link key="collection" to={`/collections/${upload.collection_id}`}>
                <Button type="primary">查看合集</Button>
              </Link>
            ) : null,
            !upload.material_id ? (
              <Link key="materials" to="/materials">
                <Button>查看全部资料</Button>
              </Link>
            ) : null,
            <Link key="job" to={`/jobs/${jobId}`}><Button>查看处理记录</Button></Link>,
          ]}
        />
      </Card>
    );
  }

  return (
    <Card title="归档任务">
      {jobQueryError ? <Alert type="error" showIcon message="任务状态加载失败" className="mb-3" /> : null}
      <Descriptions
        size="small"
        column={{ xs: 1, md: 3 }}
        items={[
          { key: 'job', label: '任务', children: <Link to={`/jobs/${jobId}`}>查看处理记录</Link> },
          { key: 'status', label: '状态', children: jobPending || !jobStatus ? '加载中…' : <JobStatusTag status={jobStatus} /> },
          { key: 'attempts', label: '尝试次数', children: attempts },
          { key: 'upload-status', label: '清单状态', children: UPLOAD_STATUS_LABELS[upload.status] },
          { key: 'progress', label: '当前进度', children: jobProgress || '等待处理' },
        ]}
      />
      {jobStatus === 'failed' ? (
        <Alert
          className="mt-3"
          type="error"
          showIcon
          message="归档失败"
          description={jobFailure || undefined}
          action={
            canRetryJob ? (
              <Button size="small" danger loading={retrying} onClick={onRetry}>
                重新排队
              </Button>
            ) : undefined
          }
        />
      ) : null}
    </Card>
  );
}

function BackLink() {
  return (
    <Link to="/uploads/new" className="inline-flex items-center gap-1 text-sm">
      <ArrowLeftOutlined />
      返回上传资料
    </Link>
  );
}
