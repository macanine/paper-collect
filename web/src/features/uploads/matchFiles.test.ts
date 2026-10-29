import { describe, expect, it } from 'vitest';
import type { UploadEntry } from '../../shared/api/types';
import type { SelectedFile } from './fileSources';
import { matchFiles } from './matchFiles';

function entry(id: string, relativePath: string, size: number, status: UploadEntry['status'] = 'pending'): UploadEntry {
  return { id, relative_path: relativePath, size_bytes: size, status, sha256: '' };
}
function selected(relativePath: string, size: number, file: File): SelectedFile {
  return { relativePath, sizeBytes: size, file };
}

describe('上传文件匹配', () => {
  it('按路径和大小匹配，跳过已接收与已匹配文件，并按候选顺序处理重复项', () => {
    const a = { name: 'a' } as File;
    const b = { name: 'b' } as File;
    const wrongSize = { name: 'wrong' } as File;
    const previous = { name: 'previous' } as File;
    const entries = [
      entry('received', 'same.pdf', 10, 'received'),
      entry('existing', 'same.pdf', 10),
      entry('one', 'same.pdf', 10),
      entry('two', 'same.pdf', 10),
      entry('different', 'same.pdf', 11),
      entry('missing', 'none.pdf', 10),
    ];
    const original = new Map([['existing', previous]]);
    const result = matchFiles(entries, [selected('same.pdf', 10, a), selected('same.pdf', 11, wrongSize), selected('same.pdf', 10, b)], original);
    expect(result.added).toBe(3);
    expect(result.matched.get('existing')).toBe(previous);
    expect(result.matched.get('one')).toBe(a);
    expect(result.matched.get('two')).toBe(b);
    expect(result.matched.get('different')).toBe(wrongSize);
    expect(result.matched.has('received')).toBe(false);
    expect(result.matched.has('missing')).toBe(false);
    expect(original.size).toBe(1);
  });

  it('可匹配完整的大型清单，包括不同目录中的同名文件', () => {
    const entries = Array.from({ length: 2000 }, (_, index) => entry(String(index), `目录-${index}/试卷.pdf`, index + 1));
    const candidates = entries.map((item) => selected(item.relative_path, item.size_bytes, { name: item.id } as File)).reverse();
    const result = matchFiles(entries, candidates, new Map());
    expect(result.added).toBe(2000);
    expect(result.matched.get('0')?.name).toBe('0');
    expect(result.matched.get('1999')?.name).toBe('1999');
  });
});
