import type { AICard, AnalysisStatus } from '../../shared/api/types';
import { ANALYSIS_STATUS_LABELS } from '../../shared/lib/labels';

// 纯展示辅助：把分析状态与置信度转成可测试的文案。

export function confidencePercent(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.round(Math.min(1, Math.max(0, value)) * 100);
}

export function analysisStatusCopy(status: AnalysisStatus, error: string): string {
  if (status === 'failed') return error ? `识别失败：${error}` : '识别失败';
  return ANALYSIS_STATUS_LABELS[status];
}

/** 阅读记录中已填写的字段数（不含证据与内部标记）。 */
export function cardFieldCount(card: AICard): number {
  return Object.entries(card).filter(([key, value]) => {
    if (key === 'evidence' || key === 'metadata_only' || key === 'fingerprint') return false;
    if (Array.isArray(value)) return value.length > 0;
    return value !== undefined && value !== '' && value !== 0;
  }).length;
}
