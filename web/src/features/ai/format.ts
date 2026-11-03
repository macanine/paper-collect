import type { AISuggestions, AnalysisStatus } from '../../shared/api/types';
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

export function suggestionFieldCount(suggestions: AISuggestions): number {
  return Object.entries(suggestions).filter(([key, value]) => {
    if (key === 'uncertain') return false;
    if (Array.isArray(value)) return value.length > 0;
    return value !== undefined && value !== '' && value !== 0;
  }).length;
}
