import { STORAGE_MODES, type StorageMode } from '../../shared/api/types';
import { STORAGE_MODE_LABELS } from '../../shared/lib/labels';

export const POLICY_MODE_COPY: Record<StorageMode, { label: string }> = {
  mirror_all: { label: STORAGE_MODE_LABELS.mirror_all },
  primary_backup: { label: STORAGE_MODE_LABELS.primary_backup },
  balanced: { label: STORAGE_MODE_LABELS.balanced },
};

export const POLICY_MODE_OPTIONS = STORAGE_MODES.map((mode) => ({
  value: mode,
  label: POLICY_MODE_COPY[mode].label,
}));

export function describePolicy(mode: StorageMode, minimumReplicas: number): string {
  return `${POLICY_MODE_COPY[mode].label}，最低副本数 ${minimumReplicas}`;
}
