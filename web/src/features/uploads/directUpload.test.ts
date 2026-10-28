import { describe, expect, it } from 'vitest';
import { directUploadBody } from './directUpload';

describe('直接上传默认清单', () => {
  it('单文件可在学科未知时直接上传', () => {
    expect(directUploadBody([{ relativePath: '资料.pdf', sizeBytes: 10 }])).toMatchObject({
      title: '资料', collection_kind: 'custom', subject_codes: [],
      entries: [{ relative_path: '资料.pdf', size_bytes: 10 }],
    });
  });

  it('文件夹与 ZIP 保留集合信息', () => {
    expect(directUploadBody([{ relativePath: '数学/a.pdf', sizeBytes: 10 }])).toMatchObject({
      title: '数学', collection_kind: 'folder', subject_codes: ['math'],
    });
    expect(directUploadBody([{ relativePath: '月考.zip', sizeBytes: 20 }])).toMatchObject({
      title: '月考', collection_kind: 'zip', subject_codes: [],
    });
  });

  it('不同学科的批次不把所有学科错误地分配给每份资料', () => {
    expect(directUploadBody([
      { relativePath: '数学.pdf', sizeBytes: 10 },
      { relativePath: '物理.pdf', sizeBytes: 20 },
    ])).toMatchObject({ title: '数学 等 2 个文件', collection_kind: 'series', subject_codes: [] });
  });
});
