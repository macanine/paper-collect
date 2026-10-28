import { describe, expect, it } from 'vitest';
import { deriveCollectionKind, deriveTitle, inferSubjects } from './defaults';

describe('清单默认值推导', () => {
  it('单文件用文件名，多文件用顶层目录', () => {
    expect(deriveTitle([{ relativePath: '化学平衡.pdf', sizeBytes: 1 }])).toBe('化学平衡');
    expect(deriveTitle([{ relativePath: '2026月考/学生版.pdf', sizeBytes: 1 }])).toBe('2026月考');
    expect(deriveTitle([])).toBe('');
  });

  it('推导合集类型', () => {
    expect(deriveCollectionKind([{ relativePath: 'a.zip', sizeBytes: 1 }])).toBe('zip');
    expect(deriveCollectionKind([{ relativePath: 'a.pdf', sizeBytes: 1 }])).toBe('custom');
    expect(deriveCollectionKind([{ relativePath: '资料/a.pdf', sizeBytes: 1 }])).toBe('folder');
    expect(deriveCollectionKind([{ relativePath: 'a.pdf', sizeBytes: 1 }, { relativePath: 'b.pdf', sizeBytes: 1 }])).toBe('series');
  });

  it('从路径推断学科', () => {
    expect(inferSubjects([{ relativePath: '高三/化学平衡压轴.zip', sizeBytes: 1 }])).toEqual(['chemistry']);
    expect(inferSubjects([{ relativePath: '数学/物理综合.pdf', sizeBytes: 1 }])).toEqual(['math', 'physics']);
    expect(inferSubjects([{ relativePath: '资料.pdf', sizeBytes: 1 }])).toEqual([]);
  });
});
