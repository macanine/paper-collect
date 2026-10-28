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
});
