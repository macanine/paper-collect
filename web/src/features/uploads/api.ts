import { apiRequest, apiUploadBinary, type BinaryUploadOptions } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { FinalizeResult, Page, Upload, UploadEntry, UploadHints } from '../../shared/api/types';

interface UploadManifestEntry {
  relative_path: string;
  size_bytes: number;
  /** 提供后服务端可识别已有内容，无需再传字节。 */
  sha256?: string;
}

/** 创建上传任务；提示均可留空。 */
export interface UploadCreateBody {
  title?: string;
  hints?: Partial<UploadHints>;
  entries: UploadManifestEntry[];
}

export function listUploads(params: { page: number; page_size: number }, signal?: AbortSignal): Promise<Page<Upload>> {
  return apiRequest<Page<Upload>>('/uploads', { query: params, signal });
}

export function createUpload(body: UploadCreateBody): Promise<Upload> {
  return apiRequest<Upload>('/uploads', { method: 'POST', body });
}

export function getUpload(id: string, signal?: AbortSignal): Promise<Upload> {
  return apiRequest<Upload>(`/uploads/${segment(id)}`, { signal });
}

export function uploadEntryFile(
  uploadId: string,
  entryId: string,
  file: Blob,
  options: BinaryUploadOptions,
): Promise<UploadEntry> {
  return apiUploadBinary<UploadEntry>(`/uploads/${segment(uploadId)}/files/${segment(entryId)}`, file, options);
}

export function finalizeUpload(id: string): Promise<FinalizeResult> {
  return apiRequest<FinalizeResult>(`/uploads/${segment(id)}/finalize`, { method: 'POST' });
}
