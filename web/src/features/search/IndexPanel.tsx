import { useState } from 'react';
import { Group, Stack, Text } from '@mantine/core';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { QuestionRow } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { INDEX_STATUS_LABELS } from '../../shared/lib/labels';
import { DataTable, type Column } from '../../shared/ui/DataTable';
import { Section } from '../../shared/ui/Section';
import { ConfirmButton, message } from '../../shared/ui/feedback';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { Tag } from '../../shared/ui/StatusTags';
import { enqueueMaterialIndex, getMaterialIndexState, listMaterialQuestions } from './api';
import { qk } from '../../app/queryKeys';

const PAGE_SIZE = 20;

export function IndexPanel({ materialId, canWrite }: { materialId: string; canWrite: boolean }) {
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const state = useQuery({
    queryKey: qk.materials.index(materialId),
    queryFn: () => getMaterialIndexState(materialId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 3000 : false;
    },
  });
  const questions = useQuery({
    queryKey: qk.materials.questions(materialId, page),
    queryFn: () => listMaterialQuestions(materialId, page, PAGE_SIZE),
    enabled: state.data?.status === 'succeeded',
  });
  const reindex = useMutation({
    mutationFn: () => enqueueMaterialIndex(materialId, true),
    onSuccess: () => {
      message.success('已加入索引队列');
      void queryClient.invalidateQueries({ queryKey: qk.materials.index(materialId) });
      void queryClient.invalidateQueries({ queryKey: qk.materials.questions(materialId) });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const columns: Column<QuestionRow>[] = [
    { key: 'page', title: '页码', width: 80, render: (q) => q.page_no },
    { key: 'no', title: '题号', width: 100, render: (q) => q.question_no || '—' },
    { key: 'text', wrap: true, title: '题干', render: (q) => <Text size="sm" lineClamp={3} style={{ whiteSpace: 'pre-wrap' }} title={q.text}>{q.text}</Text> },
    { key: 'confidence', title: '置信度', width: 90, render: (q) => `${Math.round(q.confidence * 100)}%` },
  ];

  const status = state.data?.status;
  const tagColor = status === 'succeeded' ? 'success' : status === 'failed' ? 'error' : status === 'not_applicable' ? 'default' : status ? 'processing' : 'default';
  const indexable = status !== 'not_applicable';

  return (
    <Section
      title="题目索引"
      extra={
        <Group gap="xs">
          <Tag color={tagColor}>{status ? INDEX_STATUS_LABELS[status] : '加载中'}</Tag>
          {canWrite && indexable ? <ConfirmButton size="compact-sm" variant="default" loading={reindex.isPending} confirmTitle="重建这份资料的题目索引？" okText="重建" onConfirm={() => reindex.mutate()}>重建索引</ConfirmButton> : null}
        </Group>
      }
    >
      {state.isPending ? <LoadingState /> : null}
      {state.isError ? <ErrorState error={state.error} onRetry={() => void state.refetch()} /> : null}
      {state.isSuccess ? (
        <Stack gap="md">
          {indexable ? (
            <Group gap="lg">
              <Text size="sm">页数：{state.data.page_count || '—'}</Text>
              <Text size="sm">题目：{state.data.question_count}</Text>
              <Text size="sm">索引时间：{state.data.indexed_at ? formatDateTime(state.data.indexed_at) : '—'}</Text>
            </Group>
          ) : (
            <Text size="sm" c="dimmed">只有试卷和试卷+答案资料需要建立题目索引。</Text>
          )}
          {state.data.status === 'failed' ? <Text size="sm" c="red">{state.data.error || '索引失败'}</Text> : null}
          {state.data.status === 'succeeded' ? (
            <DataTable
              rows={questions.data?.items ?? []}
              columns={columns}
              rowKey={(q) => q.id}
              loading={questions.isPending}
              minWidth={560}
              empty={<Text size="sm" c="dimmed">暂无题目</Text>}
              pagination={{ page: questions.data?.page ?? page, pageSize: PAGE_SIZE, total: questions.data?.total ?? 0, pageSizes: [PAGE_SIZE], onChange: (p) => setPage(p) }}
            />
          ) : null}
        </Stack>
      ) : null}
    </Section>
  );
}
