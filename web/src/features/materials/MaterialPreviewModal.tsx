import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Center, Group, Loader, Modal, Stack, Text } from '@mantine/core';
import { Document, Page, pdfjs } from 'react-pdf';
import 'react-pdf/dist/Page/AnnotationLayer.css';
import 'react-pdf/dist/Page/TextLayer.css';
import { describeError } from '../../shared/api/errors';
import { previewMaterial } from './api';

pdfjs.GlobalWorkerOptions.workerSrc = new URL('pdfjs-dist/build/pdf.worker.min.mjs', import.meta.url).toString();
const pdfOptions = {
  cMapUrl: `${import.meta.env.BASE_URL}pdfjs/cmaps/`,
  cMapPacked: true,
  standardFontDataUrl: `${import.meta.env.BASE_URL}pdfjs/standard_fonts/`,
  wasmUrl: `${import.meta.env.BASE_URL}pdfjs/wasm/`,
  iccUrl: `${import.meta.env.BASE_URL}pdfjs/iccs/`,
};

type Props = { materialId: string; title: string; isWord: boolean; pdfFileName: string; initialPage?: number; onClose: () => void };

export function MaterialPreviewModal({ materialId, title, isWord, pdfFileName, initialPage = 1, onClose }: Props) {
  const [attempt, setAttempt] = useState(0);
  const [url, setUrl] = useState<string>();
  const [error, setError] = useState<string>();
  const [pageCount, setPageCount] = useState(0);
  const [page, setPage] = useState(1);
  const [width, setWidth] = useState(0);
  const viewport = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setUrl(undefined);
    setError(undefined);
    setPageCount(0);
    const controller = new AbortController();
    let objectUrl: string | undefined;
    void previewMaterial(materialId, controller.signal).then(({ blob }) => {
      if (controller.signal.aborted) return;
      objectUrl = URL.createObjectURL(blob);
      setUrl(objectUrl);
    }).catch((reason: unknown) => {
      if (controller.signal.aborted) return;
      const { title: message, detail } = describeError(reason);
      setError(detail ? `${message}：${detail}` : message);
    });
    return () => {
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [materialId, attempt]);

  useEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setWidth(Math.max(1, Math.min(900, Math.floor(entry.contentRect.width - 24))));
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [url]);

  return (
    <Modal opened title={`预览：${title}`} size={1000} onClose={onClose} styles={{ content: { display: 'flex', flexDirection: 'column' } }}>
      <Stack gap="md">
        {isWord ? <Text size="sm" c="dimmed">Word 文档已转换为 PDF 预览，字体或排版可能与原件略有不同。</Text> : null}
        {error ? <Alert color="red" title="预览失败"><Group justify="space-between"><Text size="sm">{error}</Text><Button size="compact-sm" variant="default" onClick={() => setAttempt((value) => value + 1)}>重试</Button></Group></Alert> : null}
        {!url && !error ? <Center py={48}><Stack align="center" gap="xs"><Loader /><Text size="sm" c="dimmed">{isWord ? '正在生成预览，请稍候' : '正在加载预览'}</Text></Stack></Center> : null}
        <Box ref={viewport} mah="65vh" bg={url ? 'gray.1' : undefined} ta="center" style={{ overflow: 'auto' }}>
          {url && width ? <Document
            file={url}
            options={pdfOptions}
            loading={<Center py="xl"><Loader size="sm" /></Center>}
            error={<Alert color="red">PDF 预览失败，请尝试下载原件</Alert>}
            onLoadSuccess={({ numPages }) => { setPageCount(numPages); setPage(Number.isSafeInteger(initialPage) ? Math.max(1, Math.min(initialPage, numPages)) : 1); }}
            onItemClick={({ pageNumber }) => setPage(pageNumber)}
            externalLinkTarget="_blank"
          >
            <Page pageNumber={page} width={width} loading={<Center py="xl"><Loader size="sm" /></Center>} error={<Alert color="red">页面渲染失败</Alert>} />
          </Document> : null}
        </Box>
        <Group justify="flex-end" wrap="wrap">
          {pageCount ? (
            <Group gap="xs">
              <Button variant="default" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>上一页</Button>
              <Text size="sm" role="status">第 {page} / {pageCount} 页</Text>
              <Button variant="default" disabled={page >= pageCount} onClick={() => setPage((value) => value + 1)}>下一页</Button>
            </Group>
          ) : null}
          {url && isWord ? <Button variant="default" component="a" href={url} download={pdfFileName}>下载 PDF</Button> : null}
          {url ? <Button variant="default" component="a" href={`${url}#page=${page}`} target="_blank" rel="noopener noreferrer">在新窗口打开</Button> : null}
          <Button onClick={onClose}>关闭</Button>
        </Group>
      </Stack>
    </Modal>
  );
}
