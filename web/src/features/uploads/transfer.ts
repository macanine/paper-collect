import { ApiError } from '../../shared/api/errors';

// 逐文件传输的通用工具：并发池（上限 3）与可重试判断。

export const UPLOAD_CONCURRENCY = 3;
export const AUTO_RETRY_DELAYS_MS = [1000, 3000] as const;

/** 以固定并发执行任务；任一任务失败不会中止其他任务（失败由 worker 自行记录）。 */
export async function runPool<T>(
  items: readonly T[],
  limit: number,
  worker: (item: T) => Promise<void>,
  signal?: AbortSignal,
): Promise<void> {
  let cursor = 0;
  const runners = Array.from({ length: Math.min(limit, items.length) }, async () => {
    for (;;) {
      if (signal?.aborted) return;
      const index = cursor;
      cursor += 1;
      if (index >= items.length) return;
      await worker(items[index] as T);
    }
  });
  await Promise.all(runners);
}

/** 网络抖动、超时与服务端暂不可用可自动重试；校验类错误（415/422/409/413）不重试。 */
export function isRetryableUploadError(error: unknown): boolean {
  if (!(error instanceof ApiError)) return false;
  if (error.status === 0) return error.code === 'network_error';
  return error.status === 429 || error.status >= 500;
}

export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('已取消', 'AbortError'));
      return;
    }
    const onAbort = () => {
      window.clearTimeout(timer);
      reject(new DOMException('已取消', 'AbortError'));
    };
    const timer = window.setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

/** 将传输错误转为中文提示；415/422 给出针对内容的说明。 */
export function uploadFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 415) return '文件内容与格式不符（签名不匹配），请确认文件未损坏且后缀正确';
    if (error.status === 422) return '文件字节数或内容校验未通过，请重新选择原始文件';
    if (error.status === 413) return '文件超过服务端允许的大小';
    if (error.status === 409) return '该清单已提交归档，无法再上传文件';
  }
  if (error instanceof Error && error.message) return error.message;
  return '上传失败';
}
