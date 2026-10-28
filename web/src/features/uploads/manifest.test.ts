import { describe, expect, it } from 'vitest';
import { isIgnoredPath, validateManifest, hasAllowedSuffix } from './manifest';

describe('清单校验', () => {
  it('接受大小写扩展名', () => {
    expect(hasAllowedSuffix('a.PDF')).toBe(true);
    expect(hasAllowedSuffix('a.DocX')).toBe(true);
    expect(hasAllowedSuffix('a.exe')).toBe(false);
  });

  it('忽略系统噪声路径', () => {
    expect(isIgnoredPath('a/.DS_Store')).toBe(true);
    expect(isIgnoredPath('__MACOSX/a.pdf')).toBe(true);
    expect(isIgnoredPath('a/b.pdf')).toBe(false);
  });

  it('拒绝不支持格式、重复路径和空文件', () => {
    const report = validateManifest([
      { relativePath: 'a.pdf', sizeBytes: 10 },
      { relativePath: 'a.pdf', sizeBytes: 10 },
      { relativePath: 'b.exe', sizeBytes: 1 },
      { relativePath: 'c.pdf', sizeBytes: 0 },
    ]);
    expect(report.valid).toBe(false);
    expect(report.itemIssues[1]?.join()).toContain('重复');
    expect(report.itemIssues[2]?.join()).toContain('格式不支持');
    expect(report.itemIssues[3]?.join()).toContain('大小无效');
  });

  it('拒绝超过单文件与整批上限', () => {
    const single = validateManifest([{ relativePath: 'a.pdf', sizeBytes: 536_870_913 }]);
    expect(single.valid).toBe(false);
    expect(single.itemIssues[0]?.join()).toContain('512 MiB');

    const batch = validateManifest([
      { relativePath: 'a.pdf', sizeBytes: 536_870_912 },
      { relativePath: 'b.pdf', sizeBytes: 536_870_912 },
      { relativePath: 'c.pdf', sizeBytes: 536_870_912 },
      { relativePath: 'd.pdf', sizeBytes: 536_870_912 },
      { relativePath: 'e.pdf', sizeBytes: 1 },
    ]);
    expect(batch.valid).toBe(false);
    expect(batch.globalErrors.join()).toContain('2 GiB');
  });
});
