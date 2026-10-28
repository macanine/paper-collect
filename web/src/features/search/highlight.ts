// 将服务端返回的字符区间应用到纯文本，避免使用 HTML 造成注入风险。

export interface TextSegment {
  text: string;
  marked: boolean;
}

export function splitHighlights(text: string, ranges: readonly (readonly [number, number])[]): TextSegment[] {
  const runes = Array.from(text);
  const sorted = ranges
    .filter(([start, end]) => Number.isInteger(start) && Number.isInteger(end) && start >= 0 && end > start)
    .map(([start, end]) => [Math.min(start, runes.length), Math.min(end, runes.length)] as const)
    .sort((a, b) => a[0] - b[0]);
  const merged: [number, number][] = [];
  for (const range of sorted) {
    const last = merged[merged.length - 1];
    if (last && range[0] <= last[1]) {
      last[1] = Math.max(last[1], range[1]);
    } else {
      merged.push([range[0], range[1]]);
    }
  }
  const segments: TextSegment[] = [];
  let cursor = 0;
  for (const [start, end] of merged) {
    if (start > cursor) segments.push({ text: runes.slice(cursor, start).join(''), marked: false });
    segments.push({ text: runes.slice(start, end).join(''), marked: true });
    cursor = end;
  }
  if (cursor < runes.length) segments.push({ text: runes.slice(cursor).join(''), marked: false });
  return segments;
}
