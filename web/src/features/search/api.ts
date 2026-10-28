import { apiRequest } from '../../shared/api/client';
import { segment } from '../../shared/api/paths';
import type {
  MaterialIndexState,
  Page,
  QuestionRow,
  SearchQuestionsResult,
} from '../../shared/api/types';

export interface SearchQuestionsParams {
  query: string;
  subject?: string;
  grade?: string;
  academic_year?: number;
  limit?: number;
  offset?: number;
}

export function searchQuestions(params: SearchQuestionsParams, signal?: AbortSignal): Promise<SearchQuestionsResult> {
  return apiRequest<SearchQuestionsResult>('/search/questions', { method: 'POST', body: params, signal });
}

export function listMaterialQuestions(materialId: string, page: number, pageSize: number): Promise<Page<QuestionRow>> {
  return apiRequest<Page<QuestionRow>>(`/materials/${segment(materialId)}/questions`, {
    query: { page, page_size: pageSize },
  });
}

export function getMaterialIndexState(materialId: string): Promise<MaterialIndexState> {
  return apiRequest<MaterialIndexState>(`/materials/${segment(materialId)}/index`);
}

export function enqueueMaterialIndex(materialId: string, force: boolean): Promise<{ job_id: string; state: MaterialIndexState }> {
  return apiRequest<{ job_id: string; state: MaterialIndexState }>(`/materials/${segment(materialId)}/index`, {
    method: 'POST',
    body: { force },
  });
}
