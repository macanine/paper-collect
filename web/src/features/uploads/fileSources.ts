import { isIgnoredPath } from './manifest';

// 将浏览器的文件来源转换为清单候选项。路径规则：
// - 选择文件夹：relative_path 取 webkitRelativePath（包含所选文件夹名）；
// - 拖拽目录：通过 webkitGetAsEntry 递归遍历，路径与选择文件夹保持一致；
// - 普通文件：relative_path 为文件名。
// 系统噪声文件（.DS_Store、__MACOSX）在此处直接过滤，并返回被忽略的数量。

export interface SelectedFile {
  relativePath: string;
  sizeBytes: number;
  file: File;
}

export interface SourceResult {
  files: SelectedFile[];
  ignored: number;
}

function filterIgnored(all: SelectedFile[]): SourceResult {
  const files: SelectedFile[] = [];
  let ignored = 0;
  for (const item of all) {
    if (isIgnoredPath(item.relativePath)) {
      ignored += 1;
    } else {
      files.push(item);
    }
  }
  return { files, ignored };
}

export function fromFileList(list: FileList | readonly File[]): SourceResult {
  const collected: SelectedFile[] = Array.from(list).map((file) => {
    const webkitPath = (file as File & { webkitRelativePath?: string }).webkitRelativePath;
    const relativePath = webkitPath && webkitPath.length > 0 ? webkitPath : file.name;
    return { relativePath, sizeBytes: file.size, file };
  });
  return filterIgnored(collected);
}

function readFileEntry(entry: FileSystemFileEntry): Promise<File> {
  return new Promise<File>((resolve, reject) => entry.file(resolve, reject));
}

async function readAllEntries(reader: FileSystemDirectoryReader): Promise<FileSystemEntry[]> {
  const all: FileSystemEntry[] = [];
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
    if (batch.length === 0) return all;
    all.push(...batch);
  }
}

async function walkEntry(entry: FileSystemEntry, parent: string, out: SelectedFile[]): Promise<void> {
  const path = parent ? `${parent}/${entry.name}` : entry.name;
  if (entry.isFile) {
    const file = await readFileEntry(entry as FileSystemFileEntry);
    out.push({ relativePath: path, sizeBytes: file.size, file });
    return;
  }
  if (entry.isDirectory) {
    const children = await readAllEntries((entry as FileSystemDirectoryEntry).createReader());
    for (const child of children) {
      await walkEntry(child, path, out);
    }
  }
}

/** 处理拖拽放置：支持多个文件与整个目录。 */
export async function fromDataTransfer(items: DataTransferItemList): Promise<SourceResult> {
  const roots: FileSystemEntry[] = [];
  const looseFiles: File[] = [];
  for (const item of Array.from(items)) {
    if (item.kind !== 'file') continue;
    const entry = item.webkitGetAsEntry();
    if (entry) {
      roots.push(entry);
    } else {
      const file = item.getAsFile();
      if (file) looseFiles.push(file);
    }
  }
  const collected: SelectedFile[] = [];
  for (const root of roots) {
    await walkEntry(root, '', collected);
  }
  for (const file of looseFiles) {
    collected.push({ relativePath: file.name, sizeBytes: file.size, file });
  }
  return filterIgnored(collected);
}
