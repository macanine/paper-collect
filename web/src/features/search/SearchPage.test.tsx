import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';
import type { SearchHit } from '../../shared/api/types';
import { SearchResultCard } from './SearchPage';

function renderResult(hit: SearchHit): string {
  return renderToStaticMarkup(
    <MemoryRouter>
      <SearchResultCard hit={hit} onOpenPage={() => {}} />
    </MemoryRouter>,
  );
}

describe('搜题结果', () => {
  const hit: SearchHit = {
    question_id: 'question-1',
    question_no: '1',
    text: '很长的题目开头。已知 x+y=2，求值。很长的题目结尾。',
    snippet: '…已知 x+y=2，求值…',
    highlights: [[4, 9]],
    score: 1,
    page_no: 3,
    material: { id: 'material-1', title: '数学试卷', file_name: '数学试卷.pdf' },
    subject_codes: ['math'],
    collection_id: '',
  };

  it('默认展示高亮的匹配文段，完整题目按需展开', () => {
    const html = renderResult(hit);
    expect(html).toContain('匹配文段');
    expect(html).toContain('<mark>x+y=2</mark>');
    expect(html).toContain('查看完整题目');
    expect(html).toContain('数学试卷');
    expect(html).not.toContain('很长的题目开头');
  });

  it('片段已经是完整题目时不再重复展示展开入口', () => {
    const html = renderResult({ ...hit, text: '已知函数', snippet: '已知函数', highlights: [[0, 4]] });
    expect(html).toContain('<mark>已知函数</mark>');
    expect(html).not.toContain('查看完整题目');
  });

  it('按纯文本展示结果，不执行内容中的 HTML', () => {
    const html = renderResult({ ...hit, text: '<script>alert(1)</script>', snippet: '<script>alert(1)</script>', highlights: [] });
    expect(html).not.toContain('<script>');
    expect(html).toContain('&lt;script&gt;');
  });
});
