import { lazy, Suspense, useEffect, useMemo, useState } from 'react';
import { App, Button, Card, Collapse, Form, Input, Pagination, Select, Space, Tag, Typography } from 'antd';
import { FileSearchOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router';
import type { Grade, SearchHit, SubjectCode } from '../../shared/api/types';
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

const MaterialPreviewModal = lazy(() => import('../materials/MaterialPreviewModal').then((module) => ({ default: module.MaterialPreviewModal })));

const PAGE_SIZE = 20;

export function SearchPage() {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const [previewHit, setPreviewHit] = useState<SearchHit | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const [form] = Form.useForm<SearchFormValues>();
  const params = useMemo(() => {
    const query = (searchParams.get('q') ?? '').trim();
    if (!query) return null;
    const requestedPage = Number(searchParams.get('page') ?? '1');
    const page = Number.isSafeInteger(requestedPage) && requestedPage > 0 && requestedPage <= Math.floor(Number.MAX_SAFE_INTEGER / PAGE_SIZE)
      ? requestedPage
      : 1;
    return {
      query,
      subject: searchParams.get('subject') || undefined,
      grade: searchParams.get('grade') || undefined,
      page,
    };
  }, [searchParams]);
  const query = useQuery({
    queryKey: ['search', params],
    queryFn: ({ signal }) =>
      searchQuestions({
        query: params?.query ?? '', subject: params?.subject, grade: params?.grade,
        limit: PAGE_SIZE, offset: ((params?.page ?? 1) - 1) * PAGE_SIZE,
      }, signal),
    enabled: params !== null && (params?.query.trim().length ?? 0) > 0,
  });

  useEffect(() => {
    form.setFieldsValue({
      query: params?.query ?? '',
      subject: (params?.subject as SubjectCode | undefined) ?? undefined,
      grade: (params?.grade as Grade | undefined) ?? undefined,
    });
  }, [form, params?.query, params?.subject, params?.grade]);

  const handleSearch = (values: SearchFormValues) => {
    const q = values.query.trim();
    if (!q) {
      message.warning('请输入要搜索的题干');
      return;
    }
    if (params?.page === 1 && params.query === q && params.subject === values.subject && params.grade === values.grade) {
      void query.refetch();
      return;
    }
    const next = new URLSearchParams();
    next.set('q', q);
    if (values.subject) next.set('subject', values.subject);
    if (values.grade) next.set('grade', values.grade);
    setSearchParams(next);
  };

  const handlePageChange = (page: number) => {
    const next = new URLSearchParams(searchParams);
    if (page === 1) next.delete('page');
    else next.set('page', String(page));
    setSearchParams(next);
    window.scrollTo({ top: 0 });
  };

  const handleOpenPage = (hit: SearchHit) => {
    if (!hit.material.file_name.toLowerCase().endsWith('.pdf')) {
      message.info(`这道题在第 ${hit.page_no} 页；非 PDF 请在资料详情中查看`);
      navigate(`/materials/${hit.material.id}`);
      return;
    }
    setPreviewHit(hit);
  };

  return (
    <div className="page-stack">
      <PageHeader title="搜题" />

      <Card>
        <Form<SearchFormValues> form={form} layout="vertical" onFinish={handleSearch}>
          <Form.Item name="query" label="题干或公式" rules={[{ required: true, whitespace: true, message: '请输入题干' }]}>
            <Input.TextArea
              autoSize={{ minRows: 2, maxRows: 5 }}
              aria-label="搜索题干或公式"
              placeholder="输入题干、题目片段或公式"
              maxLength={2000}
            />
          </Form.Item>
          <div className="flex flex-wrap items-center gap-3">
            <Form.Item name="subject" className="mb-0">
              <Select allowClear aria-label="学科" placeholder="学科" options={SUBJECT_OPTIONS} style={{ width: 160 }} />
            </Form.Item>
            <Form.Item name="grade" className="mb-0">
              <Select allowClear aria-label="年级" placeholder="年级" options={GRADE_OPTIONS} style={{ width: 140 }} />
            </Form.Item>
            <Button type="primary" htmlType="submit" icon={<FileSearchOutlined />} loading={query.isFetching}>
              搜索
            </Button>
          </div>
        </Form>
      </Card>

      {previewHit ? (
        <Suspense fallback={<LoadingState />}>
          <MaterialPreviewModal
            key={previewHit.question_id}
            materialId={previewHit.material.id}
            title={previewHit.material.title}
            isWord={false}
            pdfFileName={previewHit.material.file_name}
            initialPage={previewHit.page_no}
            onClose={() => setPreviewHit(null)}
          />
        </Suspense>
      ) : null}
      {query.isPending && params !== null ? <LoadingState /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} title="搜索失败" /> : null}
      {query.isSuccess ? (
        <>
          <div className="search-summary" role="status">
            <span>
              找到 <strong>{query.data.total}</strong> 条相关题目
            </span>
          </div>
          {query.data.items.length === 0 ? (
            <EmptyState description={query.data.total > 0 ? '当前页没有题目' : '没有找到匹配的题目'} />
          ) : (
            <div className="flex flex-col gap-3">
              {query.data.items.map((hit) => (
                <SearchResultCard key={hit.question_id} hit={hit} onOpenPage={() => void handleOpenPage(hit)} />
              ))}
            </div>
          )}
          {query.data.total > PAGE_SIZE && params ? (
            <div className="flex justify-end">
              <Pagination
                current={params.page}
                pageSize={PAGE_SIZE}
                total={query.data.total}
                showSizeChanger={false}
                onChange={handlePageChange}
              />
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

export function SearchResultCard({ hit, onOpenPage }: { hit: SearchHit; onOpenPage: () => void }) {
  return (
    <Card>
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Tag>第 {hit.page_no} 页</Tag>
          {hit.question_no ? <Tag>第 {hit.question_no} 题</Tag> : null}
          <SubjectTags codes={hit.subject_codes} />
        </div>
        <div>
          <Typography.Text type="secondary">匹配文段</Typography.Text>
          <Typography.Paragraph className="search-result-text mb-0 whitespace-pre-wrap">
            {hit.snippet
              ? splitHighlights(hit.snippet, hit.highlights ?? []).map((segment, index) =>
                  segment.marked ? <mark key={index}>{segment.text}</mark> : <span key={index}>{segment.text}</span>,
                )
              : '暂无可展示的匹配文段'}
          </Typography.Paragraph>
        </div>
        {hit.text && hit.snippet !== hit.text ? (
          <Collapse
            size="small"
            items={[
              {
                key: 'full',
                label: '查看完整题目',
                children: (
                  <Typography.Paragraph className="search-result-text mb-0 whitespace-pre-wrap">
                    {hit.text}
                  </Typography.Paragraph>
                ),
              },
            ]}
          />
        ) : null}
        <Space wrap>
          {hit.pack?.id ? <Link to={`/packs/${hit.pack.id}`}>{hit.pack.title}</Link> : <Link to={`/materials/${hit.material.id}`}>{hit.material.title}</Link>}
          {hit.also_in?.length ? (
            <Typography.Text type="secondary">
              也出现在：
              {hit.also_in.map((p, i) => (
                <span key={p.id}>
                  {i > 0 ? '、' : ''}
                  <Link to={`/packs/${p.id}`}>{p.title}</Link>
                </span>
              ))}
            </Typography.Text>
          ) : null}
          <Button size="small" onClick={onOpenPage}>
            打开原卷第 {hit.page_no} 页
          </Button>
        </Space>
      </div>
    </Card>
  );
}
