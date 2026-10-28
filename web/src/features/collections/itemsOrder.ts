import { validateRelativePath } from '../../shared/lib/paths';
import type { Role } from '../../shared/api/types';

// 合集条目的纯逻辑：自然排序（1、2、3…10）、上移/下移与提交前校验。

export const MAX_COLLECTION_ITEMS = 5000;

const collator = new Intl.Collator('zh-CN', { numeric: true, sensitivity: 'base' });

export function naturalCompare(a: string, b: string): number {
  return collator.compare(a, b);
}

export function sortNaturally<T>(items: readonly T[], key: (item: T) => string): T[] {
  return [...items].sort((left, right) => naturalCompare(key(left), key(right)));
}

export function moveItem<T>(items: readonly T[], index: number, delta: -1 | 1): T[] {
  const target = index + delta;
  if (index < 0 || index >= items.length || target < 0 || target >= items.length) return [...items];
  const next = [...items];
  const current = next[index] as T;
  next[index] = next[target] as T;
  next[target] = current;
  return next;
}

export interface EditableRow {
  key: string;
  material_id: string;
  relative_path: string;
  role: Role;
}

export interface ItemsValidation {
  rowErrors: Record<string, string>;
  globalErrors: string[];
  valid: boolean;
}

export function validateItemRows(rows: readonly EditableRow[]): ItemsValidation {
  const rowErrors: Record<string, string> = {};
  const globalErrors: string[] = [];
  const seenMaterials = new Set<string>();

  if (rows.length > MAX_COLLECTION_ITEMS) {
    globalErrors.push(`一个合集最多 ${MAX_COLLECTION_ITEMS} 个条目`);
  }

  for (const row of rows) {
    if (seenMaterials.has(row.material_id)) {
      rowErrors[row.key] = '同一资料在合集中只能出现一次';
      continue;
    }
    seenMaterials.add(row.material_id);

    // relative_path 在合同中可为空串（例如手工整理、只按顺序浏览的合集）；填写时才校验路径规则。
    const trimmedPath = row.relative_path.trim();
    if (trimmedPath !== '') {
      const pathError = validateRelativePath(trimmedPath);
      if (pathError) {
        rowErrors[row.key] = pathError;
      }
    }
  }
  return { rowErrors, globalErrors, valid: globalErrors.length === 0 && Object.keys(rowErrors).length === 0 };
}

/** 生成提交载荷：position 即当前顺序（从 0 开始），路径去除首尾空白。 */
export function toItemPayload(rows: readonly EditableRow[]): { material_id: string; position: number; role: Role; relative_path: string }[] {
  return rows.map((row, position) => ({
    material_id: row.material_id,
    position,
    role: row.role,
    relative_path: row.relative_path.trim(),
  }));
}
