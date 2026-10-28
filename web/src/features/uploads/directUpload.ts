import type { SubjectCode } from '../../shared/api/types';
import type { UploadCreateBody } from './api';
import { deriveCollectionKind, deriveTitle, inferSubjects, type DefaultFile } from './defaults';

/** A selection starts uploading without requiring catalogue metadata. */
export function directUploadBody(files: readonly DefaultFile[]): UploadCreateBody {
  const first = files[0];
  const firstSubjects = first ? inferSubjects([first]) : [];
  const subjects: SubjectCode[] = firstSubjects.filter((code) =>
    files.every((file) => inferSubjects([file]).includes(code)),
  );
  let title = deriveTitle(files).trim();
  if (files.length > 1 && files.every((file) => !file.relativePath.includes('/'))) {
    title = title ? `${title} 等 ${files.length} 个文件` : `${files.length} 个文件`;
  }
  title = Array.from(title || '未命名资料').slice(0, 255).join('');
  return {
    title,
    collection_kind: deriveCollectionKind(files),
    subject_codes: subjects,
    grade: '',
    academic_year: 0,
    tags: [],
    entries: files.map((file) => ({ relative_path: file.relativePath, size_bytes: file.sizeBytes })),
  };
}
