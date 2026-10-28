// 资料与清单共享的元数据规则（与 docs/API.md 字典一致）。

/** 去除空白、去重并保持首次出现的顺序。 */
export function normalizeTags(tags: readonly string[]): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const raw of tags) {
    const tag = raw.trim();
    if (tag === '' || seen.has(tag)) continue;
    seen.add(tag);
    result.push(tag);
  }
  return result;
}

/** 学年：0（未指定）或 1900 至 2200 的整数；空值视为未指定。 */
export function isValidAcademicYear(value: number | null | undefined): boolean {
  if (value === null || value === undefined) return true;
  return value === 0 || (Number.isInteger(value) && value >= 1900 && value <= 2200);
}
