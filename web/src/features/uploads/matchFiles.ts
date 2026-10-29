import type { UploadEntry } from '../../shared/api/types';
import type { SelectedFile } from './fileSources';

export interface MatchResult {
  matched: Map<string, File>;
  /** 本次新匹配到的条目数量。 */
  added: number;
}

interface FileQueue {
  files: File[];
  next: number;
}

/** 按相对路径和大小匹配，保持候选文件的原始顺序。 */
export function matchFiles(entries: readonly UploadEntry[], selected: readonly SelectedFile[], existing: ReadonlyMap<string, File>): MatchResult {
  const byPath = new Map<string, Map<number, FileQueue>>();
  for (const candidate of selected) {
    let bySize = byPath.get(candidate.relativePath);
    if (!bySize) {
      bySize = new Map();
      byPath.set(candidate.relativePath, bySize);
    }
    let queue = bySize.get(candidate.sizeBytes);
    if (!queue) {
      queue = { files: [], next: 0 };
      bySize.set(candidate.sizeBytes, queue);
    }
    queue.files.push(candidate.file);
  }

  const matched = new Map(existing);
  let added = 0;
  for (const entry of entries) {
    if (entry.status === 'received' || matched.has(entry.id)) continue;
    const queue = byPath.get(entry.relative_path)?.get(entry.size_bytes);
    const file = queue?.files[queue.next];
    if (!queue || !file) continue;
    queue.next += 1;
    matched.set(entry.id, file);
    added += 1;
  }
  return { matched, added };
}
