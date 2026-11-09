import { useState } from 'react';
import { Button, Progress, Tag, Typography } from 'antd';
import { CloseOutlined, DownOutlined, UpOutlined } from '@ant-design/icons';
import { Link } from 'react-router';
import { formatBytes } from '../../shared/lib/format';
import { useUploads, type UploadBatch } from './UploadManager';

const PHASE_TEXT: Record<UploadBatch['phase'], string> = {
  preparing: '比对内容',
  uploading: '传输中',
  processing: '去重、识别中',
  done: '已完成',
  failed: '失败',
};

const PHASE_COLOR: Record<UploadBatch['phase'], string> = {
  preparing: 'default',
  uploading: 'processing',
  processing: 'processing',
  done: 'success',
  failed: 'error',
};

function BatchRow({ batch, onDismiss }: { batch: UploadBatch; onDismiss: () => void }) {
  const percent = batch.totalBytes > 0 ? Math.round((batch.sentBytes / batch.totalBytes) * 100) : 0;
  const finished = batch.phase === 'done' || batch.phase === 'failed';
  return (
    <div className="upload-tray__row">
      <div className="upload-tray__row-head">
        <Typography.Text strong ellipsis className="upload-tray__title">
          {batch.uploadId ? <Link to={`/imports/${batch.uploadId}`}>{batch.title}</Link> : batch.title}
        </Typography.Text>
        <Tag color={PHASE_COLOR[batch.phase]}>{PHASE_TEXT[batch.phase]}</Tag>
        {finished ? <Button type="text" size="small" aria-label="移除" icon={<CloseOutlined />} onClick={onDismiss} /> : null}
      </div>
      {batch.phase === 'uploading' || batch.phase === 'preparing' ? (
        <Progress percent={percent} size="small" status="active" />
      ) : null}
      <div className="upload-tray__meta">
        {batch.phase === 'done' ? (
          <span>
            新增 {batch.newCount ?? 0} · 已存在 {batch.duplicateCount ?? 0}
            {batch.skipped.length > 0 ? ` · 跳过 ${batch.skipped.length}` : ''}
          </span>
        ) : batch.phase === 'failed' ? (
          <span className="upload-tray__error">{batch.error}</span>
        ) : (
          <span>
            {batch.fileCount} 个文件 · {formatBytes(batch.totalBytes)}
            {batch.knownCount > 0 ? ` · ${batch.knownCount} 个已存在免传` : ''}
          </span>
        )}
      </div>
    </div>
  );
}

/** 右下角常驻的导入进度：不打断当前页面。 */
export function UploadTray() {
  const { batches, dismiss, clearFinished } = useUploads();
  const [collapsed, setCollapsed] = useState(false);
  if (batches.length === 0) return null;
  const active = batches.filter((b) => b.phase !== 'done' && b.phase !== 'failed').length;
  return (
    <section className="upload-tray" aria-label="导入进度">
      <header className="upload-tray__header">
        <strong>{active > 0 ? `正在导入 ${active} 批` : '导入完成'}</strong>
        <span className="upload-tray__actions">
          {active < batches.length ? (
            <Button type="link" size="small" onClick={clearFinished}>
              清除已完成
            </Button>
          ) : null}
          <Button
            type="text"
            size="small"
            aria-label={collapsed ? '展开' : '收起'}
            icon={collapsed ? <UpOutlined /> : <DownOutlined />}
            onClick={() => setCollapsed((v) => !v)}
          />
        </span>
      </header>
      {collapsed ? null : (
        <div className="upload-tray__body">
          {batches.map((batch) => (
            <BatchRow key={batch.key} batch={batch} onDismiss={() => dismiss(batch.key)} />
          ))}
          {batches.some((b) => b.phase === 'done') ? (
            <div className="upload-tray__footer">
              <Link to="/inbox">去待确认看看 →</Link>
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}
