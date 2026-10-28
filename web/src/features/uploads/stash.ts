// 当前 SPA 会话内的文件暂存；File 对象无法持久化，刷新页面后需要重新选择。
const stash = new Map<string, Map<string, File>>();

export function stashFiles(uploadId: string, files: Map<string, File>): void {
  stash.set(uploadId, files);
}

export function getStashedFiles(uploadId: string): Map<string, File> | undefined {
  return stash.get(uploadId);
}

export function clearStashedFiles(uploadId: string): void {
  stash.delete(uploadId);
}
