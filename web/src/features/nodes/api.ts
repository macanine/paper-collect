import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type { ClusterNode, CreatedNode, NodePoint } from '../../shared/api/types';

export interface NodePatchBody {
  name?: string;
  enabled?: boolean;
  draining?: boolean;
}

export function listNodes(signal?: AbortSignal): Promise<ClusterNode[]> {
  return apiRequest<ClusterNode[]>('/nodes', { signal });
}

export function createNode(name: string): Promise<CreatedNode> {
  return apiRequest<CreatedNode>('/nodes', { method: 'POST', body: { name } });
}

export function patchNode(id: string, body: NodePatchBody): Promise<ClusterNode> {
  return apiRequest<ClusterNode>(`/nodes/${segment(id)}`, { method: 'PATCH', body });
}

export function getNodeMetrics(id: string, signal?: AbortSignal): Promise<NodePoint[]> {
  return apiRequest<NodePoint[]>(`/nodes/${segment(id)}/metrics`, { signal });
}
