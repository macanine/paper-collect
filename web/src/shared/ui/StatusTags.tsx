import { Tag } from 'antd';
import type { TagProps } from 'antd';
import type {
  DocType,
  JobKind,
  JobStatus,
  MaterialStatus,
  NodeStatus,
  Part,
  ReplicaStatus,
  ShareStatus,
  Stage,
  SubjectCode,
  SyncStatus,
  UploadStatus,
  Verification,
} from '../api/types';
import {
  DOC_TYPE_LABELS,
  JOB_KIND_LABELS,
  JOB_STATUS_LABELS,
  MATERIAL_STATUS_LABELS,
  NODE_STATUS_LABELS,
  PART_LABELS,
  REPLICA_STATUS_LABELS,
  SHARE_STATUS_LABELS,
  STAGE_LABELS,
  SUBJECT_LABELS,
  SYNC_STATUS_LABELS,
  UPLOAD_STATUS_LABELS,
  VERIFICATION_LABELS,
} from '../lib/labels';

type TagColor = NonNullable<TagProps['color']>;

const MATERIAL_STATUS_COLORS: Record<MaterialStatus, TagColor> = {
  archived: 'success',
  missing: 'error',
};

const SYNC_STATUS_COLORS: Record<SyncStatus, TagColor> = {
  waiting_catalog: 'default',
  not_required: 'default',
  syncing: 'processing',
  synced: 'success',
  attention: 'warning',
};

const JOB_STATUS_COLORS: Record<JobStatus, TagColor> = {
  pending: 'default',
  running: 'processing',
  succeeded: 'success',
  failed: 'error',
};

const UPLOAD_STATUS_COLORS: Record<UploadStatus, TagColor> = {
  pending: 'default',
  processing: 'processing',
  completed: 'success',
  failed: 'error',
};

const REPLICA_STATUS_COLORS: Record<ReplicaStatus, TagColor> = {
  pending: 'default',
  uploading: 'processing',
  moving: 'processing',
  ready: 'success',
  failed: 'error',
  skipped: 'warning',
  orphaned: 'default',
};

const SHARE_STATUS_COLORS: Record<ShareStatus, TagColor> = {
  pending: 'default',
  ready: 'success',
  unsupported: 'default',
  failed: 'error',
};

const PART_COLORS: Record<Part, TagColor> = {
  questions: 'blue',
  answers: 'green',
  full: 'cyan',
  teacher: 'purple',
  answer_sheet: 'geekblue',
  listening: 'magenta',
  main: 'gold',
  other: 'default',
};

const STAGE_COLORS: Record<Stage, TagColor> = {
  processing: 'processing',
  review: 'warning',
  ready: 'success',
  failed: 'error',
};

export function MaterialStatusTag({ status }: { status: MaterialStatus }) {
  return <Tag color={MATERIAL_STATUS_COLORS[status]}>{MATERIAL_STATUS_LABELS[status]}</Tag>;
}

export function SyncStatusTag({ status }: { status: SyncStatus }) {
  return <Tag color={SYNC_STATUS_COLORS[status]}>{SYNC_STATUS_LABELS[status]}</Tag>;
}

const NODE_STATUS_COLORS: Record<NodeStatus, TagColor> = {
  online: 'success',
  stale: 'warning',
  offline: 'error',
  draining: 'processing',
  disabled: 'default',
};

export function NodeStatusTag({ status }: { status: NodeStatus }) {
  return <Tag color={NODE_STATUS_COLORS[status]}>{NODE_STATUS_LABELS[status]}</Tag>;
}

export function JobStatusTag({ status }: { status: JobStatus }) {
  return <Tag color={JOB_STATUS_COLORS[status]}>{JOB_STATUS_LABELS[status]}</Tag>;
}

export function JobKindTag({ kind }: { kind: JobKind }) {
  return <Tag>{JOB_KIND_LABELS[kind] ?? kind}</Tag>;
}

export function UploadStatusTag({ status }: { status: UploadStatus }) {
  return <Tag color={UPLOAD_STATUS_COLORS[status]}>{UPLOAD_STATUS_LABELS[status]}</Tag>;
}

export function ReplicaStatusTag({ status }: { status: ReplicaStatus }) {
  return <Tag color={REPLICA_STATUS_COLORS[status]}>{REPLICA_STATUS_LABELS[status]}</Tag>;
}

export function ShareStatusTag({ status }: { status: ShareStatus }) {
  return <Tag color={SHARE_STATUS_COLORS[status]}>{SHARE_STATUS_LABELS[status]}</Tag>;
}

export function VerificationTag({ verification }: { verification: Verification }) {
  return <Tag color={verification === 'sha256' ? 'success' : 'default'}>{VERIFICATION_LABELS[verification]}</Tag>;
}

export function PartTag({ part }: { part: Part }) {
  return <Tag color={PART_COLORS[part]}>{PART_LABELS[part]}</Tag>;
}

export function StageTag({ stage }: { stage: Stage }) {
  return <Tag color={STAGE_COLORS[stage]}>{STAGE_LABELS[stage]}</Tag>;
}

export function DocTypeTag({ type }: { type: DocType | '' }) {
  if (type === '') return <Tag>未分类</Tag>;
  return <Tag>{DOC_TYPE_LABELS[type]}</Tag>;
}

export function SubjectTags({ codes }: { codes: SubjectCode[] }) {
  if (codes.length === 0) return <span className="text-slate-400">—</span>;
  return (
    <span className="inline-flex flex-wrap gap-1">
      {codes.map((code) => (
        <Tag key={code}>{SUBJECT_LABELS[code]}</Tag>
      ))}
    </span>
  );
}

