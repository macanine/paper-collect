import { Tag } from 'antd';
import type { TagProps } from 'antd';
import type {
  CollectionKind,
  JobKind,
  JobStatus,
  MaterialStatus,
  ReplicaStatus,
  Role,
  ShareStatus,
  SubjectCode,
  UploadStatus,
  Verification,
} from '../api/types';
import {
  COLLECTION_KIND_LABELS,
  JOB_KIND_LABELS,
  JOB_STATUS_LABELS,
  MATERIAL_STATUS_LABELS,
  REPLICA_STATUS_LABELS,
  ROLE_LABELS,
  SHARE_STATUS_LABELS,
  SUBJECT_LABELS,
  UPLOAD_STATUS_LABELS,
  VERIFICATION_LABELS,
} from '../lib/labels';

type TagColor = NonNullable<TagProps['color']>;

const MATERIAL_STATUS_COLORS: Record<MaterialStatus, TagColor> = {
  archived: 'success',
  pending_storage: 'processing',
  degraded: 'warning',
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
  ready: 'success',
  failed: 'error',
};

const SHARE_STATUS_COLORS: Record<ShareStatus, TagColor> = {
  pending: 'default',
  ready: 'success',
  unsupported: 'default',
  failed: 'error',
};

const ROLE_COLORS: Record<Role, TagColor> = {
  exam: 'blue',
  answer: 'green',
  questions_answers: 'cyan',
  teacher_edition: 'purple',
  other: 'default',
};

export function MaterialStatusTag({ status }: { status: MaterialStatus }) {
  return <Tag color={MATERIAL_STATUS_COLORS[status]}>{MATERIAL_STATUS_LABELS[status]}</Tag>;
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

export function RoleTag({ role }: { role: Role }) {
  return <Tag color={ROLE_COLORS[role]}>{ROLE_LABELS[role]}</Tag>;
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

export function CollectionKindTag({ kind }: { kind: CollectionKind }) {
  return <Tag>{COLLECTION_KIND_LABELS[kind]}</Tag>;
}

