import { describe, expect, it } from 'vitest';
import { splitHighlights } from './highlight';

describe('搜题高亮切分', () => {
  it('合并重叠区间并保持纯文本', () => {
    expect(splitHighlights('已知函数求最小值', [[0, 2], [1, 3], [5, 8]])).toEqual([
      { text: '已知函', marked: true },
      { text: '数求', marked: false },
      { text: '最小值', marked: true },
    ]);
  });

  it('忽略非法区间并裁剪越界', () => {
    expect(splitHighlights('abc', [[-1, 1], [2, 99]])).toEqual([
      { text: 'ab', marked: false },
      { text: 'c', marked: true },
    ]);
  });

  it('使用字符而非 UTF-16 长度定位，并忽略截断后的空区间', () => {
    expect(splitHighlights('…🚀函数 x²+1…', [[1, 2], [5, 9], [100, 101]])).toEqual([
      { text: '…', marked: false },
      { text: '🚀', marked: true },
      { text: '函数 ', marked: false },
      { text: 'x²+1', marked: true },
      { text: '…', marked: false },
    ]);
  });
});
