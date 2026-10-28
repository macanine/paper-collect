import { useState } from 'react';
import { App, Button, Card, Popconfirm, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { QuestionRow } from '../../shared/api/types';
import { formatDateTime } from '../../shared/lib/format';
import { INDEX_STATUS_LABELS } from '../../shared/lib/labels';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { enqueueMaterialIndex, getMaterialIndexState, listMaterialQuestions } from './api';

const PAGE_SIZE = 20;

export function IndexPanel({ materialId, canWrite }: { materialId: string; canWrite: boolean }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const state = useQuery({
    queryKey: ['material-index', materialId],
    queryFn: () => getMaterialIndexState(materialId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 3000 : false;
    },
  });
  const questions = useQuery({
    queryKey: ['material-questions', materialId, page],
    queryFn: () => listMaterialQuestions(materialId, page, PAGE_SIZE),
    enabled: state.data?.status === 'succeeded',
  });
  const reindex = useMutation({
    mutationFn: () => enqueueMaterialIndex(materialId, true),
    onSuccess: () => {
      message.success('已加入索引队列');
      void queryClient.invalidateQueries({ queryKey: ['material-index', materialId] });
      void queryClient.invalidateQueries({ queryKey: ['material-questions', materialId] });
    },
    onError: (error) => message.error(describeError(error).title),
  });

  const columns: ColumnsType<QuestionRow> = [
    { title: '页码', dataIndex: 'page_no', key: 'page', width: 80 },
    { title: '题号', dataIndex: 'question_no', key: 'no', width: 100, render: (value: string) => value || '—' },
    {
      title: '题干',
      dataIndex: 'text',
      key: 'text',
      render: (value: string) => (
        <Typography.Paragraph className="mb-0 whitespace-pre-wrap" ellipsis={{ rows: 3, expandable: true, symbol: '展开' }}>
          {value}
        </Typography.Paragraph>
      ),
    },
    {
      title: '置信度',
      dataIndex: 'confidence',
      key: 'confidence',
      width: 90,
      render: (value: number) => `${Math.round(value * 100)}%`,
    },
  ];

  const status = state.data?.status;
  const tagColor = status === 'succeeded' ? 'green' : status === 'failed' ? 'red' : status ? 'blue' : 'default';

  return (
    <Card
      title="题目索引"
      extra={
        <div className="flex items-center gap-2">
          <Tag color={tagColor}>{status ? INDEX_STATUS_LABELS[status] : '加载中'}</Tag>
          {canWrite ? (
            <Popconfirm title="重建这份资料的题目索引？" okText="重建" cancelText="取消" onConfirm={() => reindex.mutate()}>
              <Button size="small" loading={reindex.isPending}>
                重建索引
              </Button>
            </Popconfirm>
          ) : null}
        </div>
      }
    >
      {state.isPending ? <LoadingState /> : null}
      {state.isError ? <ErrorState error={state.error} onRetry={() => void state.refetch()} /> : null}
      {state.isSuccess ? (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap gap-4 text-sm">
            <span>页数：{state.data.page_count || '—'}</span>
            <span>题目：{state.data.question_count}</span>
            <span>索引时间：{state.data.indexed_at ? formatDateTime(state.data.indexed_at) : '—'}</span>
          </div>
          {state.data.status === 'failed' ? (
            <Typography.Text type="danger">{state.data.error || '索引失败'}</Typography.Text>
          ) : null}
          {state.data.status === 'succeeded' ? (
            <Table<QuestionRow>
              rowKey="id"
              size="small"
              columns={columns}
              dataSource={questions.data?.items ?? []}
              loading={questions.isPending}
              locale={{ emptyText: '暂无题目' }}
              pagination={{
                current: questions.data?.page ?? page,
                pageSize: PAGE_SIZE,
                total: questions.data?.total ?? 0,
                showSizeChanger: false,
                onChange: setPage,
              }}
            />
          ) : null}
        </div>
      ) : null}
    </Card>
  );
}
