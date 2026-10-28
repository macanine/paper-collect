import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { Upload, UploadEntry } from '../../shared/api/types';
import { uploadEntryFile } from './api';
import type { SelectedFile } from './fileSources';
import { clearStashedFiles, getStashedFiles, stashFiles } from './stash';
import { uploadKeys } from './queries';
import { AUTO_RETRY_DELAYS_MS, UPLOAD_CONCURRENCY, isRetryableUploadError, runPool, sleep, uploadFailureText } from './transfer';

export type RowStatus = 'received' | 'queued' | 'uploading' | 'failed' | 'missing-file';

interface EntryRuntime {
  status?: 'queued' | 'uploading' | 'failed';
  loaded: number;
  error?: string;
}

export interface TransferRow {
  entry: UploadEntry;
  file: File | undefined;
  status: RowStatus;
  loaded: number;
  error: string | undefined;
}

export interface MatchResult {
  matched: Map<string, File>;
  /** 本次新匹配到的条目数量。 */
  added: number;
}

/**
 * 将选择的文件与尚未接收的清单条目按「相对路径 + 字节数」匹配。
 * 已接收的条目被跳过；同一路径重复时只使用第一个未使用的文件。
 */
export function matchFiles(entries: readonly UploadEntry[], selected: readonly SelectedFile[], existing: ReadonlyMap<string, File>): MatchResult {
  const matched = new Map(existing);
  const used = new Set<number>();
  let added = 0;
  for (const entry of entries) {
    if (entry.status === 'received' || matched.has(entry.id)) continue;
    const index = selected.findIndex(
      (candidate, position) =>
        !used.has(position) && candidate.relativePath === entry.relative_path && candidate.sizeBytes === entry.size_bytes,
    );
    if (index === -1) continue;
    used.add(index);
    const candidate = selected[index];
    if (candidate) {
      matched.set(entry.id, candidate.file);
      added += 1;
    }
  }
  return { matched, added };
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError';
}

export function useUploadTransfer(upload: Upload) {
  const queryClient = useQueryClient();
  const [files, setFiles] = useState<Map<string, File>>(() => new Map(getStashedFiles(upload.id) ?? []));
  const [runtime, setRuntime] = useState<Record<string, EntryRuntime>>({});
  const [running, setRunning] = useState(false);
  const runningRef = useRef(false);
  const controllerRef = useRef<AbortController | null>(null);

  const patchRuntime = useCallback((entryId: string, patch: Partial<EntryRuntime>) => {
    setRuntime((previous) => ({
      ...previous,
      [entryId]: { loaded: 0, ...previous[entryId], ...patch },
    }));
  }, []);

  const applyReceived = useCallback(
    (entry: UploadEntry) => {
      queryClient.setQueryData<Upload>(uploadKeys.detail(upload.id), (current) =>
        current
          ? {
              ...current,
              entries: current.entries.map((item) => (item.id === entry.id ? { ...item, ...entry, status: 'received' } : item)),
            }
          : current,
      );
    },
    [queryClient, upload.id],
  );

  const runEntry = useCallback(
    async (entry: UploadEntry, file: File, signal: AbortSignal) => {
      patchRuntime(entry.id, { status: 'uploading', loaded: 0, error: undefined });
      for (let attempt = 0; ; attempt += 1) {
        try {
          const received = await uploadEntryFile(upload.id, entry.id, file, {
            signal,
            onProgress: ({ loaded }) => patchRuntime(entry.id, { loaded }),
          });
          patchRuntime(entry.id, { status: undefined, loaded: file.size, error: undefined });
          applyReceived(received);
          return;
        } catch (error) {
          if (isAbortError(error)) {
            patchRuntime(entry.id, { status: 'queued', loaded: 0, error: undefined });
            return;
          }
          const delay = AUTO_RETRY_DELAYS_MS[attempt];
          if (isRetryableUploadError(error) && delay !== undefined) {
            patchRuntime(entry.id, {
              status: 'uploading',
              loaded: 0,
              error: `网络异常，${delay / 1000} 秒后自动重试`,
            });
            try {
              await sleep(delay, signal);
            } catch {
              patchRuntime(entry.id, { status: 'queued', loaded: 0, error: undefined });
              return;
            }
            continue;
          }
          patchRuntime(entry.id, { status: 'failed', loaded: 0, error: uploadFailureText(error) });
          return;
        }
      }
    },
    [applyReceived, patchRuntime, upload.id],
  );

  /** 启动传输：仅处理有本地文件且未接收的条目；可限定为指定条目（用于重试）。 */
  const start = useCallback(
    (entryIds?: string[]) => {
      if (runningRef.current) return;
      const work: { entry: UploadEntry; file: File }[] = [];
      for (const entry of upload.entries) {
        if (entry.status === 'received') continue;
        if (entryIds && !entryIds.includes(entry.id)) continue;
        const file = files.get(entry.id);
        if (file) work.push({ entry, file });
      }
      if (work.length === 0) return;

      const controller = new AbortController();
      controllerRef.current = controller;
      runningRef.current = true;
      setRunning(true);
      for (const item of work) {
        patchRuntime(item.entry.id, { status: 'queued', loaded: 0, error: undefined });
      }
      void runPool(work, UPLOAD_CONCURRENCY, (item) => runEntry(item.entry, item.file, controller.signal), controller.signal).finally(() => {
        runningRef.current = false;
        controllerRef.current = null;
        setRunning(false);
      });
    },
    [files, patchRuntime, runEntry, upload.entries],
  );

  const cancel = useCallback(() => {
    controllerRef.current?.abort();
  }, []);

  useEffect(() => {
    if (!upload.job_id) return;
    clearStashedFiles(upload.id);
    setFiles(new Map());
  }, [upload.id, upload.job_id]);

  /** 将用户重新选择的文件与未接收条目匹配，返回新匹配的数量。 */
  const attachFiles = useCallback(
    (selected: SelectedFile[]): number => {
      const { matched, added } = matchFiles(upload.entries, selected, files);
      setFiles(matched);
      stashFiles(upload.id, matched);
      return added;
    },
    [files, upload.entries, upload.id],
  );

  // 进入页面时若已有暂存文件，自动开始传输（仅挂载时执行一次）。
  useEffect(() => {
    if (upload.status === 'pending' && !upload.job_id) start();
    // 挂载时的一次性启动，依赖项刻意留空。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const rows = useMemo<TransferRow[]>(
    () =>
      upload.entries.map((entry) => {
        const current = runtime[entry.id];
        const file = files.get(entry.id);
        let status: RowStatus;
        if (entry.status === 'received') status = 'received';
        else if (current?.status === 'uploading') status = 'uploading';
        else if (current?.status === 'failed') status = 'failed';
        else if (!file) status = 'missing-file';
        else status = 'queued';
        return {
          entry,
          file,
          status,
          loaded: entry.status === 'received' ? entry.size_bytes : (current?.loaded ?? 0),
          error: current?.error,
        };
      }),
    [files, runtime, upload.entries],
  );

  const stats = useMemo(() => {
    const totalBytes = upload.entries.reduce((sum, entry) => sum + entry.size_bytes, 0);
    const doneBytes = rows.reduce((sum, row) => sum + row.loaded, 0);
    const receivedCount = rows.filter((row) => row.status === 'received').length;
    const failedCount = rows.filter((row) => row.status === 'failed').length;
    const missingCount = rows.filter((row) => row.status === 'missing-file').length;
    const readyToStart = rows.some((row) => row.status === 'queued' || row.status === 'failed');
    return {
      totalBytes,
      doneBytes: Math.min(doneBytes, totalBytes),
      receivedCount,
      total: upload.entries.length,
      failedCount,
      missingCount,
      allReceived: upload.entries.length > 0 && receivedCount === upload.entries.length,
      readyToStart,
    };
  }, [rows, upload.entries]);

  return { rows, stats, running, start, cancel, attachFiles };
}
