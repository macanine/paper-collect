import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Modal, Space, Spin, Typography, theme } from 'antd';
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
  const { token } = theme.useToken();
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
    <Modal
      title={`预览：${title}`}
      open
      width={1000}
      style={{ top: 16 }}
      onCancel={onClose}
      footer={<Space wrap style={{ justifyContent: 'flex-end' }}>{[
        pageCount ? <Space key="pages" wrap>
          <Button disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>上一页</Button>
          <span role="status">第 {page} / {pageCount} 页</span>
          <Button disabled={page >= pageCount} onClick={() => setPage((value) => value + 1)}>下一页</Button>
        </Space> : null,
        url && isWord ? <Button key="download" href={url} download={pdfFileName}>下载 PDF</Button> : null,
        url ? <Button key="open" href={`${url}#page=${page}`} target="_blank" rel="noopener noreferrer">在新窗口打开</Button> : null,
        <Button key="close" onClick={onClose}>关闭</Button>,
      ]}</Space>}
    >
      {isWord ? <Typography.Paragraph type="secondary">Word 文档已转换为 PDF 预览，字体或排版可能与原件略有不同。</Typography.Paragraph> : null}
      {error ? <Alert type="error" showIcon message="预览失败" description={error} action={<Button onClick={() => setAttempt((value) => value + 1)}>重试</Button>} /> : null}
      {!url && !error ? <div style={{ textAlign: 'center', padding: 48 }}><Spin tip={isWord ? '正在生成预览，请稍候' : '正在加载预览'} size="large"><div style={{ minHeight: 60 }} /></Spin></div> : null}
      <div ref={viewport} style={{ maxHeight: '65vh', overflow: 'auto', background: url ? token.colorFillAlter : undefined, textAlign: 'center' }}>
        {url && width ? <Document
          file={url}
          options={pdfOptions}
          suspense={false}
          loading={<Spin tip="正在加载 PDF"><div style={{ minHeight: 80 }} /></Spin>}
          error={<Alert type="error" showIcon message="PDF 预览失败，请尝试下载原件" />}
          onLoadSuccess={({ numPages }) => { setPageCount(numPages); setPage(Number.isSafeInteger(initialPage) ? Math.max(1, Math.min(initialPage, numPages)) : 1); }}
          onItemClick={({ pageNumber }) => setPage(pageNumber)}
          externalLinkTarget="_blank"
        >
          <Page pageNumber={page} width={width} suspense={false} loading={<Spin tip="正在渲染页面"><div style={{ minHeight: 80 }} /></Spin>} error={<Alert type="error" showIcon message="页面渲染失败" />} />
        </Document> : null}
      </div>
    </Modal>
  );
}
