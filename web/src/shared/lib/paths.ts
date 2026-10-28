// 相对路径规则（清单上传与合集条目共用），与 docs/API.md 合同一致。

export const MAX_PATH_BYTES = 1024;

export function utf8ByteLength(text: string): number {
  return new TextEncoder().encode(text).length;
}

export function normalizePath(path: string): string {
  return path.normalize('NFC');
}

/**
 * 校验单个相对路径。返回错误文案；合法时返回 null。
 * 规则：1..1024 UTF-8 字节；正斜杠分隔；拒绝绝对路径、反斜杠、..、空路径段与控制字符。
 */
export function validateRelativePath(relativePath: string): string | null {
  if (relativePath.length === 0) return '路径不能为空';
  if (utf8ByteLength(relativePath) > MAX_PATH_BYTES) return `路径不能超过 ${MAX_PATH_BYTES} 个 UTF-8 字节`;
  if (relativePath.includes('\\')) return '路径不能包含反斜杠，请使用正斜杠 /';
  if (relativePath.startsWith('/')) return '不能使用绝对路径';
  if (/^[A-Za-z]:/.test(relativePath)) return '不能使用盘符开头的绝对路径';
  if (/[\u0000-\u001f\u007f]/.test(relativePath)) return '路径包含控制字符';

  const segments = relativePath.split('/');
  if (segments.some((segment) => segment === '')) return '路径不能包含空路径段（连续或结尾的 /）';
  if (segments.some((segment) => segment === '..' || segment === '.')) return '路径不能包含 . 或 .. 段';
  return null;
}
