import { useRef, useState, type DragEvent } from 'react';
import { Alert, Button, Space, theme } from 'antd';
import { FileAddOutlined, FolderOpenOutlined, InboxOutlined } from '@ant-design/icons';
import { fromDataTransfer, fromFileList, type SourceResult } from './fileSources';
import { ALLOWED_SUFFIXES } from './manifest';

interface FileSelectorProps {
  onSelected: (result: SourceResult) => void;
  disabled?: boolean;
  compact?: boolean;
  /** 同一页面多个选择器时需要不同的 input id。 */
  inputId: string;
}

const ACCEPT = ALLOWED_SUFFIXES.join(',');

export function FileSelector({ onSelected, disabled = false, compact = false, inputId }: FileSelectorProps) {
  const { token } = theme.useToken();
  const [dragging, setDragging] = useState(false);
  const [reading, setReading] = useState(false);
  const [readError, setReadError] = useState(false);
  const readingRef = useRef(false);
  const busy = disabled || reading;
  const fileInputId = `${inputId}-files`;
  const folderInputId = `${inputId}-folder`;

  const handleList = (list: FileList | null) => {
    if (disabled || readingRef.current || !list || list.length === 0) return;
    setReadError(false);
    onSelected(fromFileList(list));
  };

  const handleDrop = async (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setDragging(false);
    if (disabled || readingRef.current) return;
    readingRef.current = true;
    setReadError(false);
    setReading(true);
    try {
      const result = await fromDataTransfer(event.dataTransfer.items);
      if (result.files.length > 0 || result.ignored > 0) onSelected(result);
    } catch {
      setReadError(true);
    } finally {
      readingRef.current = false;
      setReading(false);
    }
  };

  return (
    <div
      className={[
        'flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed px-4 text-center transition-colors',
        compact ? 'py-4' : 'py-8',
        busy ? 'opacity-60' : '',
      ].join(' ')}
      style={{
        borderColor: dragging ? token.colorPrimary : token.colorBorder,
        background: dragging ? token.colorPrimaryBg : token.colorFillAlter,
      }}
      onDragOver={(event) => {
        event.preventDefault();
        if (!busy) setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(event) => void handleDrop(event)}
    >
      {compact ? null : <InboxOutlined style={{ fontSize: 28, color: token.colorTextSecondary }} />}
      <span role="status">{reading ? '正在读取…' : compact ? '拖拽或选择文件' : '拖拽文件或文件夹到这里'}</span>
      {readError ? <Alert type="error" showIcon message="无法读取文件或文件夹，请重新选择" /> : null}
      <Space wrap>
        <Button icon={<FileAddOutlined />} disabled={busy} onClick={() => document.getElementById(fileInputId)?.click()}>
          选择文件
        </Button>
        <Button icon={<FolderOpenOutlined />} disabled={busy} onClick={() => document.getElementById(folderInputId)?.click()}>
          选择文件夹
        </Button>
      </Space>
      <input
        id={fileInputId}
        disabled={busy}
        type="file"
        multiple
        accept={ACCEPT}
        hidden
        onChange={(event) => {
          handleList(event.target.files);
          event.target.value = '';
        }}
      />
      <input
        id={folderInputId}
        disabled={busy}
        type="file"
        multiple
        hidden
        ref={(element) => {
          element?.setAttribute('webkitdirectory', '');
          element?.setAttribute('directory', '');
        }}
        onChange={(event) => {
          handleList(event.target.files);
          event.target.value = '';
        }}
      />
    </div>
  );
}
