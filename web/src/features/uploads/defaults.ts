import type { CollectionKind, SubjectCode } from '../../shared/api/types';

// 选择文件后立即带出的默认清单信息，减少手工填写；归档后可在资料详情应用 AI 建议精修。

export interface DefaultFile {
  relativePath: string;
  sizeBytes: number;
}

const SUBJECT_KEYWORDS: ReadonlyArray<readonly [SubjectCode, RegExp]> = [
  ['chinese', /语文|作文|文言|古诗|chinese/i],
  ['math', /数学|函数|导数|几何|代数|math/i],
  ['english', /英语|english|grammar|reading/i],
  ['physics', /物理|力学|电磁|physics/i],
  ['chemistry', /化学|chemistry/i],
  ['biology', /生物|biology/i],
  ['politics', /政治|道德|法治|politics/i],
  ['history', /历史|history/i],
  ['geography', /地理|geography/i],
  ['information_technology', /信息技术|信息科技|编程|python|information/i],
  ['general_technology', /通用技术|technology/i],
];

function baseName(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1);
}

function stripExtension(name: string): string {
  return name.replace(/\.[^.]+$/, '');
}

/** 单文件取文件名；多个文件取共同的顶层目录；压缩包优先用包名。 */
export function deriveTitle(items: readonly DefaultFile[]): string {
  if (items.length === 0) return '';
  const direct = items.filter((item) => !item.relativePath.toLowerCase().endsWith('.zip'));
  const first = direct[0] ?? items[0];
  if (!first) return '';
  const segments = first.relativePath.split('/').filter(Boolean);
  if (segments.length > 1) return stripExtension(segments[0] ?? '');
  return stripExtension(baseName(first.relativePath));
}

/** 单个 ZIP → zip；有目录层级 → folder；多个文件 → series。 */
export function deriveCollectionKind(items: readonly DefaultFile[]): CollectionKind {
  if (items.length === 0) return 'custom';
  const hasFolder = items.some((item) => item.relativePath.includes('/'));
  if (items.length === 1) {
    if (items[0]?.relativePath.toLowerCase().endsWith('.zip')) return 'zip';
    return hasFolder ? 'folder' : 'custom';
  }
  return hasFolder ? 'folder' : 'series';
}

/** 从文件名/路径推断学科；可能为空，交给用户或 AI 补全。 */
export function inferSubjects(items: readonly DefaultFile[]): SubjectCode[] {
  const text = items.map((item) => item.relativePath).join(' ');
  const found: SubjectCode[] = [];
  for (const [code, pattern] of SUBJECT_KEYWORDS) {
    if (pattern.test(text) && !found.includes(code)) found.push(code);
  }
  return found;
}
