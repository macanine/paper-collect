import { describe, expect, it } from 'vitest';
import { analysisStatusCopy, confidencePercent, suggestionFieldCount } from './format';

describe('AI 分析展示辅助', () => {
  it('置信度限制在 0-100', () => {
    expect(confidencePercent(0.876)).toBe(88);
    expect(confidencePercent(-1)).toBe(0);
    expect(confidencePercent(2)).toBe(100);
    expect(confidencePercent(Number.NaN)).toBe(0);
  });

  it('失败状态附带错误，其余使用中文标签', () => {
    expect(analysisStatusCopy('failed', '令牌被拒绝')).toBe('识别失败：令牌被拒绝');
    expect(analysisStatusCopy('running', '')).toBe('识别中');
  });

  it('统计建议字段数量', () => {
    expect(suggestionFieldCount({})).toBe(0);
    expect(suggestionFieldCount({ title: 't', subject_codes: ['math'], grade: 'grade12', role: 'exam' })).toBe(4);
  });
});
