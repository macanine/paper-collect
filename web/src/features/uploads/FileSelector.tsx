import { useRef, useState, type DragEvent } from 'react';
import { Alert, Button, Group, Stack, Text } from '@mantine/core';
import { IconFilePlus, IconFolderOpen, IconInbox } from '@tabler/icons-react';
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
    <Stack
      align="center"
      gap="sm"
      px="md"
      py={compact ? 'md' : 'xl'}
      style={{
        border: `1px dashed ${dragging ? 'var(--mantine-color-blue-6)' : 'var(--mantine-color-gray-4)'}`,
        borderRadius: 'var(--mantine-radius-md)',
        background: dragging ? 'var(--mantine-color-blue-0)' : 'var(--mantine-color-gray-0)',
        opacity: busy ? 0.6 : 1,
        textAlign: 'center',
      }}
      onDragOver={(event) => {
        event.preventDefault();
        if (!busy) setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(event) => void handleDrop(event)}
    >
      {compact ? null : <IconInbox size={32} stroke={1.3} color="var(--mantine-color-gray-6)" />}
      <Text size="sm" role="status">{reading ? '正在读取…' : compact ? '拖拽或选择文件' : '拖拽文件或文件夹到这里'}</Text>
      {readError ? <Alert color="red">无法读取文件或文件夹，请重新选择</Alert> : null}
      <Group gap="sm" justify="center">
        <Button variant="default" leftSection={<IconFilePlus size={16} />} disabled={busy} onClick={() => document.getElementById(fileInputId)?.click()}>选择文件</Button>
        <Button variant="default" leftSection={<IconFolderOpen size={16} />} disabled={busy} onClick={() => document.getElementById(folderInputId)?.click()}>选择文件夹</Button>
      </Group>
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
    </Stack>
  );
}
