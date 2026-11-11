import { lazy, Suspense, useMemo, useState } from 'react';
import { Anchor, Button, Group, Pagination, Paper, Select, Stack, Text, Textarea } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconFileSearch } from '@tabler/icons-react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router';
import type { SearchHit } from '../../shared/api/types';
import { GRADE_OPTIONS, SUBJECT_OPTIONS } from '../../shared/lib/options';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message } from '../../shared/ui/feedback';
import { EmptyState, ErrorState, LoadingState } from '../../shared/ui/States';
import { SubjectTags, Tag } from '../../shared/ui/StatusTags';
import { searchQuestions } from './api';
import { splitHighlights } from './highlight';
import { href } from '../../app/routes';
import { qk } from '../../app/queryKeys';

const MaterialPreviewModal = lazy(() => import('../materials/MaterialPreviewModal').then((module) => ({ default: module.MaterialPreviewModal })));

const PAGE_SIZE = 20;

export function SearchPage() {
  const navigate = useNavigate();
  const [previewHit, setPreviewHit] = useState<SearchHit | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const params = useMemo(() => {
    const query = (searchParams.get('q') ?? '').trim();
    if (!query) return null;
    const requestedPage = Number(searchParams.get('page') ?? '1');
    const page = Number.isSafeInteger(requestedPage) && requestedPage > 0 && requestedPage <= Math.floor(Number.MAX_SAFE_INTEGER / PAGE_SIZE) ? requestedPage : 1;
    return { query, subject: searchParams.get('subject') || undefined, grade: searchParams.get('grade') || undefined, page };
  }, [searchParams]);
  const form = useForm<{ query: string; subject: string | null; grade: string | null }>({
    initialValues: { query: params?.query ?? '', subject: params?.subject ?? null, grade: params?.grade ?? null },
  });
  const query = useQuery({
    queryKey: qk.search.questions(params),
    queryFn: ({ signal }) => searchQuestions({ query: params?.query ?? '', subject: params?.subject, grade: params?.grade, limit: PAGE_SIZE, offset: ((params?.page ?? 1) - 1) * PAGE_SIZE }, signal),
    enabled: params !== null && (params?.query.trim().length ?? 0) > 0,
  });

  // URL 变化（前进后退、命令面板跳转）时回填表单。
  const urlKey = `${params?.query ?? ''}|${params?.subject ?? ''}|${params?.grade ?? ''}`;
  const [syncedKey, setSyncedKey] = useState(urlKey);
  if (syncedKey !== urlKey) {
    setSyncedKey(urlKey);
    form.setValues({ query: params?.query ?? '', subject: params?.subject ?? null, grade: params?.grade ?? null });
  }

  const handleSearch = form.onSubmit((values) => {
    const q = values.query.trim();
    if (!q) {
      message.warning('请输入要搜索的题干');
      return;
    }
    const subject = values.subject ?? undefined;
    const grade = values.grade ?? undefined;
    if (params?.page === 1 && params.query === q && params.subject === subject && params.grade === grade) {
      void query.refetch();
      return;
    }
    const next = new URLSearchParams();
    next.set('q', q);
    if (subject) next.set('subject', subject);
    if (grade) next.set('grade', grade);
    setSearchParams(next);
  });

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
      navigate(href.material(hit.material.id));
      return;
    }
    setPreviewHit(hit);
  };

  return (
    <Stack gap="lg">
      <PageHeader title="搜题" />

      <Paper p="lg">
        <form onSubmit={handleSearch}>
          <Stack gap="md">
            <Textarea label="题干或公式" aria-label="搜索题干或公式" placeholder="输入题干、题目片段或公式" autosize minRows={2} maxRows={5} maxLength={2000} {...form.getInputProps('query')} />
            <Group gap="sm" align="flex-end">
              <Select w={160} aria-label="学科" placeholder="学科" clearable data={SUBJECT_OPTIONS} {...form.getInputProps('subject')} />
              <Select w={140} aria-label="年级" placeholder="年级" clearable data={GRADE_OPTIONS} {...form.getInputProps('grade')} />
              <Button type="submit" leftSection={<IconFileSearch size={16} />} loading={query.isFetching}>搜索</Button>
            </Group>
          </Stack>
        </form>
      </Paper>

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
          <Text size="sm" role="status">找到 <Text span fw={600}>{query.data.total}</Text> 条相关题目</Text>
          {query.data.items.length === 0 ? (
            <EmptyState description={query.data.total > 0 ? '当前页没有题目' : '没有找到匹配的题目'} />
          ) : (
            <Stack gap="sm">
              {query.data.items.map((hit) => <SearchResultCard key={hit.question_id} hit={hit} onOpenPage={() => void handleOpenPage(hit)} />)}
            </Stack>
          )}
          {query.data.total > PAGE_SIZE && params ? (
            <Group justify="flex-end"><Pagination value={params.page} total={Math.ceil(query.data.total / PAGE_SIZE)} onChange={handlePageChange} /></Group>
          ) : null}
        </>
      ) : null}
    </Stack>
  );
}

export function SearchResultCard({ hit, onOpenPage }: { hit: SearchHit; onOpenPage: () => void }) {
  const [showFull, setShowFull] = useState(false);
  return (
    <Paper p="lg">
      <Stack gap="md">
        <Group gap="xs">
          <Tag>第 {hit.page_no} 页</Tag>
          {hit.question_no ? <Tag>第 {hit.question_no} 题</Tag> : null}
          <SubjectTags codes={hit.subject_codes} />
        </Group>
        <Stack gap={4}>
          <Text size="xs" c="dimmed">匹配文段</Text>
          <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
            {hit.snippet
              ? splitHighlights(hit.snippet, hit.highlights ?? []).map((segment, index) =>
                  segment.marked ? <mark key={index}>{segment.text}</mark> : <span key={index}>{segment.text}</span>,
                )
              : '暂无可展示的匹配文段'}
          </Text>
        </Stack>
        {hit.text && hit.snippet !== hit.text ? (
          <Stack gap="xs" align="flex-start">
            <Button variant="subtle" size="compact-sm" onClick={() => setShowFull((v) => !v)} aria-expanded={showFull}>{showFull ? '收起完整题目' : '查看完整题目'}</Button>
            {showFull ? <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>{hit.text}</Text> : null}
          </Stack>
        ) : null}
        <Group gap="md">
          {hit.pack?.id ? <Anchor component={Link} to={href.pack(hit.pack.id)} size="sm">{hit.pack.title}</Anchor> : <Anchor component={Link} to={href.material(hit.material.id)} size="sm">{hit.material.title}</Anchor>}
          {hit.also_in?.length ? (
            <Text size="sm" c="dimmed">
              也出现在：
              {hit.also_in.map((p, i) => (
                <span key={p.id}>{i > 0 ? '、' : ''}<Anchor component={Link} to={href.pack(p.id)} size="sm">{p.title}</Anchor></span>
              ))}
            </Text>
          ) : null}
          <Button size="compact-sm" variant="default" onClick={onOpenPage}>打开原卷第 {hit.page_no} 页</Button>
        </Group>
      </Stack>
    </Paper>
  );
}
