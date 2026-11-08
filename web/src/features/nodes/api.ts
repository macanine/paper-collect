import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { ClusterNode, ClusterSettings, CreatedNode, NodeKind, NodePoint } from '../../shared/api/types';

export interface NodePatchBody {
  name?: string;
  enabled?: boolean;
  draining?: boolean;
  slots?: Partial<Record<NodeKind, number>>;
  cache_max_bytes?: number;
  ocr_provider_ids?: string[];
}

export interface NodeCreateBody {
  name: string;
  slots?: Partial<Record<NodeKind, number>>;
  cache_max_bytes?: number;
  ocr_provider_ids?: string[];
}

export type ClusterSettingsPatch = Omit<ClusterSettings, 'revision' | 'updated_at'>;

export function listNodes(signal?: AbortSignal): Promise<ClusterNode[]> {
  return apiRequest<ClusterNode[]>('/nodes', { signal });
}

export function createNode(body: NodeCreateBody): Promise<CreatedNode> {
  return apiRequest<CreatedNode>('/nodes', { method: 'POST', body });
}

export function patchNode(id: string, body: NodePatchBody): Promise<ClusterNode> {
  return apiRequest<ClusterNode>(`/nodes/${segment(id)}`, { method: 'PATCH', body });
}

export function getNodeMetrics(id: string, signal?: AbortSignal): Promise<NodePoint[]> {
  return apiRequest<NodePoint[]>(`/nodes/${segment(id)}/metrics`, { signal });
}

export function getClusterSettings(signal?: AbortSignal): Promise<ClusterSettings> {
  return apiRequest<ClusterSettings>('/cluster/settings', { signal });
}

export function patchClusterSettings(body: Partial<ClusterSettingsPatch>): Promise<ClusterSettings> {
  return apiRequest<ClusterSettings>('/cluster/settings', { method: 'PATCH', body });
}
