import { useState } from 'react';
import { App, Button, Card, Collapse, Form, Input, Select, Space, Tag, Typography } from 'antd';
import { FileSearchOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import type { Grade, SearchHit, SubjectCode } from '../../shared/api/types';
import { downloadMaterial } from '../materials/api';
import { GRADE_OPTIONS, SUBJECT_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { SubjectTags } from '../../shared/ui/StatusTags';
import { searchQuestions } from './api';
import { splitHighlights } from './highlight';

interface SearchFormValues {
  query: string;
  subject?: SubjectCode;
  grade?: Grade;
}

export function SearchPage() {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const [params, setParams] = useState<{ query: string; subject?: string; grade?: string } | null>(null);
  const query = useQuery({
    queryKey: ['search', params],
    queryFn: ({ signal }) =>
      searchQuestions({ query: params?.query ?? '', subject: params?.subject, grade: params?.grade, limit: 20 }, signal),
    enabled: params !== null && (params?.query.trim().length ?? 0) > 0,
  });

  const handleSearch = (values: SearchFormValues) => {
    const q = values.query.trim();
    if (!q) {
      message.warning('请输入要搜索的题干');
      return;
    }
    setParams({ query: q, subject: values.subject || undefined, grade: values.grade || undefined });
  };

  const handleOpenPage = async (hit: SearchHit) => {
    if (!hit.material.file_name.toLowerCase().endsWith('.pdf')) {
      message.info(`这道题在第 ${hit.page_no} 页；非 PDF 请在资料详情中查看`);
      navigate(`/materials/${hit.material.id}`);
      return;
    }
    try {
      const { blob } = await downloadMaterial(hit.material.id);
      const url = URL.createObjectURL(blob);
      window.open(`${url}#page=${hit.page_no}`, '_blank', 'noopener');
      window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  return (
    <div className="page-stack">
      <PageHeader title="搜题" />

      <Card>
        <Form<SearchFormValues> layout="vertical" onFinish={handleSearch}>
          <Form.Item name="query" rules={[{ required: true, message: '请输入题干' }]}>
            <Input.TextArea
              autoSize={{ minRows: 2, maxRows: 5 }}
              placeholder="粘贴题干或题目片段"
              maxLength={2000}
            />
          </Form.Item>
          <div className="flex flex-wrap items-center gap-3">
            <Form.Item name="subject" className="mb-0">
              <Select allowClear placeholder="学科" options={SUBJECT_OPTIONS} style={{ width: 160 }} />
            </Form.Item>
            <Form.Item name="grade" className="mb-0">
              <Select allowClear placeholder="年级" options={GRADE_OPTIONS} style={{ width: 140 }} />
            </Form.Item>
            <Button type="primary" htmlType="submit" icon={<FileSearchOutlined />} loading={query.isFetching}>
              搜索
            </Button>
          </div>
        </Form>
      </Card>

      {query.isPending && params !== null ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} title="搜索失败" /> : null}
      {query.isSuccess ? (
        <>
          {query.data.items.length === 0 ? (
            <EmptyState description="没有找到匹配的题目" />
          ) : (
            <div className="flex flex-col gap-3">
              {query.data.items.map((hit) => (
                <ResultCard key={hit.question_id} hit={hit} onOpenPage={() => void handleOpenPage(hit)} />
              ))}
            </div>
          )}
        </>
      ) : null}
    </div>
  );
}

function ResultCard({ hit, onOpenPage }: { hit: SearchHit; onOpenPage: () => void }) {
  return (
    <Card size="small">
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Tag>第 {hit.page_no} 页</Tag>
          {hit.question_no ? <Tag>第 {hit.question_no} 题</Tag> : null}
          <SubjectTags codes={hit.subject_codes} />
        </div>
        <Typography.Paragraph className="mb-0 whitespace-pre-wrap">
          {splitHighlights(hit.snippet, hit.highlights).map((segment, index) =>
            segment.marked ? <mark key={index}>{segment.text}</mark> : <span key={index}>{segment.text}</span>,
          )}
        </Typography.Paragraph>
        <Collapse
          size="small"
          items={[
            {
              key: 'full',
              label: '完整题干',
              children: (
                <Typography.Paragraph className="mb-0 whitespace-pre-wrap">
                  {hit.text}
                </Typography.Paragraph>
              ),
            },
          ]}
        />
        <Space wrap>
          <Link to={`/materials/${hit.material.id}`}>{hit.material.title}</Link>
          {hit.collection_id ? <Link to={`/collections/${hit.collection_id}`}>查看合集</Link> : null}
          <Button size="small" onClick={onOpenPage}>
            打开原卷第 {hit.page_no} 页
          </Button>
        </Space>
      </div>
    </Card>
  );
}
