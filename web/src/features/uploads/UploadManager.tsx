import { Center, Paper, Stack, Text } from '@mantine/core';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { UploadHints } from '../../shared/api/types';
import { invalidateLibrary } from '../materials/queries';
import { createUpload, finalizeUpload, getUpload, uploadEntryFile } from './api';
import { fromDataTransfer, type SelectedFile } from './fileSources';
import { sha256Hex } from './hash';
import { hasAllowedSuffix } from './manifest';
import { AUTO_RETRY_DELAYS_MS, UPLOAD_CONCURRENCY, isRetryableUploadError, runPool, sleep, uploadFailureText } from './transfer';
import { stashFiles } from './stash';

// 全局上传：任何页面拖入或选择文件都在这里排队，传输与归档在后台进行，
// 用户可以继续浏览；托盘展示每一批的进度和结果。

export type BatchPhase = 'preparing' | 'uploading' | 'processing' | 'done' | 'failed';

export interface UploadBatch {
  key: string;
  title: string;
  phase: BatchPhase;
  fileCount: number;
  totalBytes: number;
  sentBytes: number;
  knownCount: number;
  uploadId?: string;
  error?: string;
  newCount?: number;
  duplicateCount?: number;
  skipped: string[];
}

interface UploadContextValue {
  batches: UploadBatch[];
  start: (files: SelectedFile[], hints?: Partial<UploadHints>) => void;
  dismiss: (key: string) => void;
  clearFinished: () => void;
  dialogOpen: boolean;
  openDialog: () => void;
  closeDialog: () => void;
}

const UploadContext = createContext<UploadContextValue | null>(null);

const MAX_ENTRIES = 2000;
const MAX_BATCH_BYTES = 2 * 1024 * 1024 * 1024;
const MAX_FILE_BYTES = 512 * 1024 * 1024;

/** 超出单批上限时自动拆分，用户不用关心清单限制。 */
function chunkFiles(files: SelectedFile[]): SelectedFile[][] {
  const chunks: SelectedFile[][] = [];
  let current: SelectedFile[] = [];
  let bytes = 0;
  for (const file of files) {
    if (current.length >= MAX_ENTRIES || bytes + file.sizeBytes > MAX_BATCH_BYTES) {
      if (current.length > 0) chunks.push(current);
      current = [];
      bytes = 0;
    }
    current.push(file);
    bytes += file.sizeBytes;
  }
  if (current.length > 0) chunks.push(current);
  return chunks;
}

function batchTitle(files: SelectedFile[]): string {
  const first = files[0]?.relativePath ?? '上传';
  const top = first.includes('/') ? first.slice(0, first.indexOf('/')) : first;
  return files.length > 1 && !first.includes('/') ? `${top} 等 ${files.length} 个文件` : top;
}

export function UploadProvider({ enabled, children }: { enabled: boolean; children: ReactNode }) {
  const queryClient = useQueryClient();
  const [batches, setBatches] = useState<UploadBatch[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);

  const patch = useCallback((key: string, update: Partial<UploadBatch> | ((b: UploadBatch) => Partial<UploadBatch>)) => {
    setBatches((list) => list.map((b) => (b.key === key ? { ...b, ...(typeof update === 'function' ? update(b) : update) } : b)));
  }, []);

  const runBatch = useCallback(
    async (key: string, files: SelectedFile[], hints: Partial<UploadHints> | undefined) => {
      try {
        const hashes = new Map<string, string>();
        for (const file of files) {
          const sum = await sha256Hex(file.file);
          if (sum) hashes.set(file.relativePath, sum);
        }
        const upload = await createUpload({
          title: batchTitle(files),
          hints,
          entries: files.map((f) => ({ relative_path: f.relativePath, size_bytes: f.sizeBytes, sha256: hashes.get(f.relativePath) })),
        });
        const byPath = new Map(files.map((f) => [f.relativePath, f.file]));
        const pending = upload.entries.filter((e) => e.status === 'pending');
        const known = upload.entries.filter((e) => e.status === 'existing');
        const knownBytes = known.reduce((sum, e) => sum + e.size_bytes, 0);
        stashFiles(upload.id, new Map(pending.flatMap((e) => (byPath.has(e.relative_path) ? [[e.id, byPath.get(e.relative_path) as File]] : []))));
        patch(key, { uploadId: upload.id, phase: 'uploading', knownCount: known.length, sentBytes: knownBytes });

        const progress = new Map<string, number>();
        let reportTimer: ReturnType<typeof setTimeout> | null = null;
        let reportQueued = false;
        const emitProgress = () => {
          reportQueued = false;
          reportTimer = null;
          let sent = knownBytes;
          for (const value of progress.values()) sent += value;
          patch(key, { sentBytes: sent });
        };
        // XHR may emit dozens of progress events per second. Batching these
        // updates keeps the upload tray responsive without making progress
        // look stale to the user.
        const report = () => {
          if (reportQueued) return;
          reportQueued = true;
          reportTimer = setTimeout(emitProgress, 100);
        };
        const failures: string[] = [];
        await runPool(pending, UPLOAD_CONCURRENCY, async (entry) => {
          const file = byPath.get(entry.relative_path);
          if (!file) return;
          for (let attempt = 0; ; attempt += 1) {
            try {
              await uploadEntryFile(upload.id, entry.id, file, {
                onProgress: ({ loaded }) => {
                  progress.set(entry.id, loaded);
                  report();
                },
              });
              progress.set(entry.id, file.size);
              report();
              return;
            } catch (error) {
              const delay = AUTO_RETRY_DELAYS_MS[attempt];
              if (isRetryableUploadError(error) && delay !== undefined) {
                await sleep(delay);
                continue;
              }
              failures.push(`${entry.relative_path}：${uploadFailureText(error)}`);
              return;
            }
          }
        });
        if (reportTimer !== null) {
          clearTimeout(reportTimer);
          emitProgress();
        }
        if (failures.length > 0) {
          patch(key, { phase: 'failed', error: `${failures.length} 个文件传输失败，可在导入中心重试。${failures[0]}` });
          return;
        }
        await finalizeUpload(upload.id);
        patch(key, { phase: 'processing' });
        for (;;) {
          await sleep(2000);
          const current = await getUpload(upload.id);
          if (current.status === 'completed' || current.status === 'failed') {
            patch(key, (b) => ({
              phase: current.status === 'completed' ? 'done' : 'failed',
              error: current.status === 'failed' ? '归档失败，请在导入中心查看原因' : undefined,
              newCount: current.new_count,
              duplicateCount: current.duplicate_count,
              skipped: [...b.skipped, ...current.skipped],
            }));
            invalidateLibrary(queryClient);
            return;
          }
        }
      } catch (error) {
        patch(key, { phase: 'failed', error: uploadFailureText(error) });
      }
    },
    [patch, queryClient],
  );

  const start = useCallback(
    (selected: SelectedFile[], hints?: Partial<UploadHints>) => {
      const skipped: string[] = [];
      const usable = selected.filter((f) => {
        const ok = hasAllowedSuffix(f.relativePath) && f.sizeBytes > 0 && f.sizeBytes <= MAX_FILE_BYTES;
        if (!ok) skipped.push(f.relativePath);
        return ok;
      });
      const chunks = chunkFiles(usable);
      if (chunks.length === 0) {
        const key = crypto.randomUUID?.() ?? String(Date.now());
        setBatches((list) => [
          { key, title: batchTitle(selected), phase: 'failed', fileCount: 0, totalBytes: 0, sentBytes: 0, knownCount: 0, skipped, error: '没有可上传的文件（格式不支持或文件为空）' },
          ...list,
        ]);
        return;
      }
      chunks.forEach((files, index) => {
        const key = `${Date.now()}-${index}-${Math.random().toString(36).slice(2)}`;
        const batch: UploadBatch = {
          key,
          title: batchTitle(files),
          phase: 'preparing',
          fileCount: files.length,
          totalBytes: files.reduce((sum, f) => sum + f.sizeBytes, 0),
          sentBytes: 0,
          knownCount: 0,
          skipped: index === 0 ? skipped : [],
        };
        setBatches((list) => [batch, ...list]);
        void runBatch(key, files, hints);
      });
    },
    [runBatch],
  );

  // 整个窗口都是投放区：拖入文件时显示遮罩，放下即开始上传。
  useEffect(() => {
    if (!enabled) return;
    const hasFiles = (event: DragEvent) => Array.from(event.dataTransfer?.types ?? []).includes('Files');
    const onEnter = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      dragDepth.current += 1;
      setDragging(true);
    };
    const onLeave = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      dragDepth.current = Math.max(0, dragDepth.current - 1);
      if (dragDepth.current === 0) setDragging(false);
    };
    const onOver = (event: DragEvent) => {
      if (hasFiles(event)) event.preventDefault();
    };
    const onDrop = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      dragDepth.current = 0;
      setDragging(false);
      // 上传对话框里的选择器自行处理放置。
      if ((event.target as HTMLElement | null)?.closest?.('[data-upload-dropzone]')) return;
      event.preventDefault();
      const items = event.dataTransfer?.items;
      if (!items) return;
      void fromDataTransfer(items).then((result) => {
        if (result.files.length > 0) start(result.files);
      });
    };
    window.addEventListener('dragenter', onEnter);
    window.addEventListener('dragleave', onLeave);
    window.addEventListener('dragover', onOver);
    window.addEventListener('drop', onDrop);
    return () => {
      window.removeEventListener('dragenter', onEnter);
      window.removeEventListener('dragleave', onLeave);
      window.removeEventListener('dragover', onOver);
      window.removeEventListener('drop', onDrop);
    };
  }, [enabled, start]);

  const value = useMemo<UploadContextValue>(
    () => ({
      batches,
      start,
      dismiss: (key) => setBatches((list) => list.filter((b) => b.key !== key)),
      clearFinished: () => setBatches((list) => list.filter((b) => b.phase !== 'done' && b.phase !== 'failed')),
      dialogOpen,
      openDialog: () => setDialogOpen(true),
      closeDialog: () => setDialogOpen(false),
    }),
    [batches, dialogOpen, start],
  );

  return (
    <UploadContext.Provider value={value}>
      {children}
      {dragging && !dialogOpen ? (
        <Center aria-hidden="true" pos="fixed" inset={0} bg="rgba(22, 119, 255, 0.08)" style={{ zIndex: 1000, pointerEvents: 'none' }}>
          <Paper p="xl" shadow="md" style={{ border: '2px dashed var(--mantine-color-blue-6)' }}>
            <Stack gap={4} align="center">
              <Text fw={600} size="lg">松开即可上传</Text>
              <Text size="sm" c="dimmed">文件、文件夹或压缩包都可以，系统会自动去重、识别和配套</Text>
            </Stack>
          </Paper>
        </Center>
      ) : null}
    </UploadContext.Provider>
  );
}

export function useUploads(): UploadContextValue {
  const value = useContext(UploadContext);
  if (!value) throw new Error('useUploads must be used inside UploadProvider');
  return value;
}
