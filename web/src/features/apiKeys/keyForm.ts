import type { Scope } from '../../shared/api/types';
import type { APIKeyCreateBody } from './api';

export interface KeyFormValues {
  name: string;
  scopes: Scope[];
  expires_at?: string;
}

export function buildCreateKeyBody(values: KeyFormValues): APIKeyCreateBody {
  const expires = (values.expires_at ?? '').trim();
  return {
    name: values.name.trim(),
    scopes: values.scopes,
    ...(expires ? { expires_at: new Date(expires).toISOString() } : {}),
  };
}
