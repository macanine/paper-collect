/** 将资源 ID 安全地拼接进路径段（ID 为不透明字符串，不做路径解释）。 */
export function segment(id: string): string {
  return encodeURIComponent(id);
}
