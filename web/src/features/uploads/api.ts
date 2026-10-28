import { apiRequest, apiUploadBinary, type BinaryUploadOptions } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { CollectionKind, FinalizeResult, Grade, SubjectCode, Upload, UploadEntry } from '../../shared/api/types';

export interface UploadManifestEntry {
  relative_path: string;
  size_bytes: number;
}

/** 创建上传任务。 */
export interface UploadCreateBody {
  title: string;
  collection_kind: CollectionKind;
  subject_codes: SubjectCode[];
  grade: Grade;
  academic_year: number;
  tags: string[];
  entries: UploadManifestEntry[];
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
