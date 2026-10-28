// 清单上传的前端校验，与 docs/API.md「POST /uploads」合同一致（服务端仍会再次校验）。
import { normalizePath, validateRelativePath } from '../../shared/lib/paths';

export { MAX_PATH_BYTES, normalizePath, utf8ByteLength, validateRelativePath } from '../../shared/lib/paths';

export const ALLOWED_SUFFIXES = ['.zip', '.pptx', '.pdf', '.docx', '.doc'] as const;
export const MAX_FILE_BYTES = 536_870_912; // 512 MiB
export const MAX_BATCH_BYTES = 2_147_483_648; // 2 GiB
export const MAX_ENTRIES = 2000;

/** 系统生成的噪声文件：选择文件夹时自动忽略（服务端 ZIP 展开同样忽略）。 */
const IGNORED_SEGMENTS = new Set(['.DS_Store', '__MACOSX']);

export interface ManifestItem {
  relativePath: string;
  sizeBytes: number;
}

export interface ManifestReport {
  count: number;
  totalBytes: number;
  /** 按下标记录的逐条问题；没有问题的条目不出现在表中。 */
  itemIssues: Record<number, string[]>;
  /** 与单条无关的全局错误（条数、总大小、为空）。 */
  globalErrors: string[];
  valid: boolean;
}

export function hasAllowedSuffix(fileName: string): boolean {
  const lower = fileName.toLowerCase();
  return ALLOWED_SUFFIXES.some((suffix) => lower.endsWith(suffix));
}

/** 是否应被忽略（.DS_Store 与 __MACOSX 任一路径段匹配）。 */
export function isIgnoredPath(relativePath: string): boolean {
  return relativePath.split('/').some((segment) => IGNORED_SEGMENTS.has(segment));
}

/** 校验整份清单。调用方应先过滤 isIgnoredPath 的条目。 */
export function validateManifest(items: readonly ManifestItem[]): ManifestReport {
  const itemIssues: Record<number, string[]> = {};
  const globalErrors: string[] = [];
  const seen = new Map<string, number>();
  let totalBytes = 0;

  items.forEach((item, index) => {
    const issues: string[] = [];
    const pathError = validateRelativePath(item.relativePath);
    if (pathError) {
      issues.push(pathError);
    } else {
      const normalized = normalizePath(item.relativePath);
      const first = seen.get(normalized);
      if (first !== undefined) {
        issues.push(`与第 ${first + 1} 项路径重复`);
      } else {
        seen.set(normalized, index);
      }
      const fileName = item.relativePath.slice(item.relativePath.lastIndexOf('/') + 1);
      if (!hasAllowedSuffix(fileName)) {
        issues.push('格式不支持，仅允许 .zip、.pptx、.pdf、.docx、.doc');
      }
    }

    if (!Number.isInteger(item.sizeBytes) || item.sizeBytes <= 0) {
      issues.push('文件为空或大小无效');
    } else if (item.sizeBytes > MAX_FILE_BYTES) {
      issues.push('超过单文件 512 MiB 限制');
    }

    totalBytes += Number.isFinite(item.sizeBytes) ? item.sizeBytes : 0;
    if (issues.length > 0) itemIssues[index] = issues;
  });

  if (items.length === 0) globalErrors.push('请至少选择一个文件');
  if (items.length > MAX_ENTRIES) globalErrors.push(`一次清单最多 ${MAX_ENTRIES} 个文件，当前 ${items.length} 个`);
  if (totalBytes > MAX_BATCH_BYTES) globalErrors.push('清单总大小超过 2 GiB 限制');

  const valid = globalErrors.length === 0 && Object.keys(itemIssues).length === 0;
  return { count: items.length, totalBytes, itemIssues, globalErrors, valid };
}
