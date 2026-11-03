import { useEffect, type ReactNode } from 'react';
import { Alert, App, Button, Card, Collapse, Descriptions, Popconfirm, Progress, Space, Tag, Typography } from 'antd';
import { RobotOutlined } from '@ant-design/icons';
import { useQueryClient } from '@tanstack/react-query';
import { ApiError, describeError } from '../../shared/api/errors';
import type { AISuggestions, MaterialAnalysis } from '../../shared/api/types';
import { DOC_TYPE_LABELS, FIELD_LABELS, GRADE_LABELS, PART_LABELS, SUBJECT_LABELS, TERM_LABELS } from '../../shared/lib/labels';
import { formatDateTime } from '../../shared/lib/format';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { materialKeys } from '../materials/queries';
import { analysisStatusCopy, confidencePercent, suggestionFieldCount } from './format';
import { useApplyAnalysis, useEnqueueAnalysis, useMaterialAnalysis } from './queries';

interface AnalysisPanelProps {
  materialId: string;
  canWrite: boolean;
}

export function AnalysisPanel({ materialId, canWrite }: AnalysisPanelProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const query = useMaterialAnalysis(materialId);
  const enqueue = useEnqueueAnalysis(materialId);
  const apply = useApplyAnalysis(materialId);
  const notFound = query.error instanceof ApiError && query.error.status === 404;

  // The worker applies AI fields when analysis finishes. Refresh the parent
  // material so its title, filename and metadata change without a page reload.
  useEffect(() => {
    if (query.data?.status !== 'succeeded') return;
    void queryClient.invalidateQueries({ queryKey: materialKeys.detail(materialId) });
    void queryClient.invalidateQueries({ queryKey: materialKeys.all });
    void queryClient.invalidateQueries({ queryKey: ['packs'] });
  }, [materialId, query.data?.status, queryClient]);

  const handleRun = (force: boolean) => {
    enqueue.mutate(force, {
      onSuccess: () => message.success(force ? '已重新加入识别队列' : '已加入识别队列'),
      onError: (error) => message.error(describeError(error).title),
    });
  };

  const handleApply = () => {
    apply.mutate(undefined, {
      onSuccess: () => message.success('已按识别结果更新套卷并确认'),
      onError: (error) => {
        const { title, detail } = describeError(error);
        message.error(detail ? `${title}：${detail}` : title);
      },
    });
  };

  const extra = (
    <Space>
      {query.data && query.data.status === 'succeeded' ? (
        <Popconfirm title="重新识别会覆盖现有建议，继续？" okText="重新识别" cancelText="取消" onConfirm={() => handleRun(true)}>
          <Button size="small" loading={enqueue.isPending} disabled={!canWrite}>
            重新识别
          </Button>
        </Popconfirm>
      ) : (
        <Button
          size="small"
          icon={<RobotOutlined />}
          loading={enqueue.isPending}
          disabled={!canWrite || query.data?.status === 'pending' || query.data?.status === 'running'}
          onClick={() => handleRun(false)}
        >
          开始识别
        </Button>
      )}
    </Space>
  );

  if (query.isPending) {
    return (
      <Card title="AI 识别">
        <LoadingState />
      </Card>
    );
  }
  if (query.isError && !notFound) {
    return (
      <Card title="AI 识别">
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      </Card>
    );
  }
  if (notFound || !query.data) {
    return (
      <Card title="AI 识别" extra={extra}>
        <span className="text-gray-500">暂无识别结果</span>
      </Card>
    );
  }

  const analysis: MaterialAnalysis = query.data;
  return (
    <Card title="AI 识别" extra={extra}>
      <Descriptions
        column={{ xs: 1, sm: 1, md: 2 }}
        size="small"
        items={[
          { key: 'status', label: '状态', children: <Tag color={analysis.status === 'failed' ? 'red' : analysis.status === 'succeeded' ? 'green' : 'blue'}>{analysisStatusCopy(analysis.status, analysis.error)}</Tag> },
          { key: 'provider', label: '模型', children: analysis.model ? `${analysis.provider_name || '未记录'} / ${analysis.model}` : '—' },
          { key: 'pages', label: '页数', children: analysis.page_count || '—' },
          { key: 'confidence', label: '置信度', children: <Progress percent={confidencePercent(analysis.confidence)} size="small" className="max-w-[220px]" /> },
          { key: 'updated', label: '更新时间', children: formatDateTime(analysis.updated_at) },
        ]}
      />

      {analysis.status === 'failed' ? <Alert type="error" showIcon className="mt-3" message={analysis.error || '识别失败'} /> : null}

      {analysis.status === 'succeeded' ? (
        <div className="mt-4 flex flex-col gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Typography.Text strong>
              建议字段（{suggestionFieldCount(analysis.suggestions)} 项）
              {analysis.applied_at ? <Tag color="green" className="ml-2">已应用</Tag> : <Tag color="orange" className="ml-2">未应用</Tag>}
            </Typography.Text>
            <Popconfirm title="用识别结果覆盖套卷的编目信息？" okText="覆盖" cancelText="取消" onConfirm={handleApply}>
              <Button size="small" loading={apply.isPending} disabled={!canWrite}>
                按识别结果覆盖
              </Button>
            </Popconfirm>
          </div>
          <SuggestionList suggestions={analysis.suggestions} />
          <Collapse
            size="small"
            items={[
              {
                key: 'markdown',
                label: '解析文本',
                children: (
                  <Typography.Paragraph className="max-h-[320px] overflow-auto whitespace-pre-wrap text-xs">
                    {analysis.markdown_excerpt || '无文本'}
                  </Typography.Paragraph>
                ),
              },
            ]}
          />
        </div>
      ) : null}
    </Card>
  );
}

function SuggestionList({ suggestions }: { suggestions: AISuggestions }) {
  const entries: { key: string; value: ReactNode }[] = [];
  if (suggestions.part) entries.push({ key: 'part', value: PART_LABELS[suggestions.part] });
  if (suggestions.doc_type) entries.push({ key: 'doc_type', value: DOC_TYPE_LABELS[suggestions.doc_type] });
  if (suggestions.subject_codes?.length) entries.push({ key: 'subject_codes', value: suggestions.subject_codes.map((code) => SUBJECT_LABELS[code] ?? code).join('、') });
  if (suggestions.grade) entries.push({ key: 'grade', value: GRADE_LABELS[suggestions.grade] });
  if (suggestions.cohort) entries.push({ key: 'cohort', value: `${suggestions.cohort} 届` });
  if (suggestions.term) entries.push({ key: 'term', value: TERM_LABELS[suggestions.term] });
  if (suggestions.region) entries.push({ key: 'region', value: suggestions.region });
  if (suggestions.organization) entries.push({ key: 'organization', value: suggestions.organization });
  if (suggestions.exam_date) entries.push({ key: 'exam_date', value: suggestions.exam_date });
  if (suggestions.exam_name) entries.push({ key: 'exam_name', value: suggestions.exam_name });
  if (suggestions.chapter) entries.push({ key: 'chapter', value: suggestions.chapter });
  if (suggestions.tags?.length) entries.push({ key: 'tags', value: suggestions.tags.join('、') });
  if (suggestions.answer_start_page) entries.push({ key: 'answer_start_page', value: `第 ${suggestions.answer_start_page} 页` });
  if (entries.length === 0) return <Typography.Text>暂无建议</Typography.Text>;
  const uncertain = new Set(suggestions.uncertain ?? []);
  return (
    <Descriptions
      column={{ xs: 1, sm: 1, md: 2 }}
      size="small"
      items={entries.map((entry) => ({
        key: entry.key,
        label: entry.key === 'answer_start_page' ? '答案起始页' : FIELD_LABELS[entry.key],
        children: uncertain.has(entry.key) ? (
          <span>
            {entry.value} <Tag color="warning">不确定</Tag>
          </span>
        ) : (
          entry.value
        ),
      }))}
    />
  );
}
