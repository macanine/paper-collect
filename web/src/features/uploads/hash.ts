/** 浏览器端计算 SHA-256，供服务端识别已归档的相同内容（秒传）。 */
const HASH_LIMIT_BYTES = 64 * 1024 * 1024;

export async function sha256Hex(file: Blob): Promise<string | undefined> {
  if (file.size > HASH_LIMIT_BYTES || typeof crypto === 'undefined' || !crypto.subtle) return undefined;
  try {
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
    return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('');
  } catch {
    return undefined;
  }
}
