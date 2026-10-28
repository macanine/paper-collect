import { Alert, App, Button, Card, Collapse, Descriptions, Popconfirm, Progress, Space, Tag, Typography } from 'antd';
import { RobotOutlined } from '@ant-design/icons';
import { ApiError, describeError } from '../../shared/api/errors';
import type { AISuggestions, MaterialAnalysis } from '../../shared/api/types';
import { GRADE_LABELS, ROLE_LABELS, SUBJECT_LABELS } from '../../shared/lib/labels';
import { formatDateTime } from '../../shared/lib/format';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { analysisStatusCopy, confidencePercent, suggestionFieldCount } from './format';
import { useApplyAnalysis, useEnqueueAnalysis, useMaterialAnalysis } from './queries';

interface AnalysisPanelProps {
  materialId: string;
  canWrite: boolean;
}

export function AnalysisPanel({ materialId, canWrite }: AnalysisPanelProps) {
  const { message } = App.useApp();
  const query = useMaterialAnalysis(materialId);
  const enqueue = useEnqueueAnalysis(materialId);
  const apply = useApplyAnalysis(materialId);
  const notFound = query.error instanceof ApiError && query.error.status === 404;

  const handleRun = (force: boolean) => {
    enqueue.mutate(force, {
      onSuccess: () => message.success(force ? '已重新加入识别队列' : '已加入识别队列'),
      onError: (error) => message.error(describeError(error).title),
    });
  };

  const handleApply = () => {
    apply.mutate(undefined, {
      onSuccess: () => message.success('建议已应用到资料并写入审计'),
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
        column={{ xs: 1, md: 2 }}
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
            <Typography.Text strong>建议字段（{suggestionFieldCount(analysis.suggestions)} 项）</Typography.Text>
            <Button type="primary" size="small" loading={apply.isPending} disabled={!canWrite} onClick={handleApply}>
              应用全部建议
            </Button>
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
  const entries: { label: string; value: React.ReactNode }[] = [];
  if (suggestions.title) entries.push({ label: '标题', value: suggestions.title });
  if (suggestions.subject_codes?.length) {
    entries.push({ label: '学科', value: suggestions.subject_codes.map((code) => SUBJECT_LABELS[code] ?? code).join('、') });
  }
  if (suggestions.grade) entries.push({ label: '年级', value: GRADE_LABELS[suggestions.grade] });
  if (suggestions.academic_year) entries.push({ label: '学年', value: `${suggestions.academic_year} 学年` });
  if (suggestions.role) entries.push({ label: '角色', value: ROLE_LABELS[suggestions.role] });
  if (suggestions.tags?.length) entries.push({ label: '标签', value: suggestions.tags.join('、') });
  if (entries.length === 0) return <Typography.Text>暂无建议</Typography.Text>;
  return (
    <Descriptions
      column={{ xs: 1, md: 2 }}
      size="small"
      items={entries.map((entry, index) => ({ key: String(index), label: entry.label, children: entry.value }))}
    />
  );
}
