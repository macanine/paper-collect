import { lazy, Suspense, useEffect, useState } from 'react';
import { Alert, Image, LoadingOverlay, Loader, Modal, Center } from '@mantine/core';
import { describeError } from '../../shared/api/errors';
import { downloadMaterial } from './api';

const MaterialPreviewModal = lazy(() => import('./MaterialPreviewModal').then((m) => ({ default: m.MaterialPreviewModal })));

const WORD = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';

export function canPreview(mediaType: string): boolean {
  return mediaType === 'application/pdf' || mediaType === WORD || mediaType.startsWith('image/') || mediaType.startsWith('audio/');
}

interface Target {
  id: string;
  title: string;
  fileName: string;
  mediaType: string;
  initialPage?: number;
}

/** 原件内联预览：PDF/Word 走服务端 PDF 预览，图片与音频直接展示原件。 */
function BlobPreview({ target, onClose }: { target: Target; onClose: () => void }) {
  const [url, setUrl] = useState<string>();
  const [error, setError] = useState<string>();
  useEffect(() => {
    let objectUrl: string | undefined;
    void downloadMaterial(target.id)
      .then(({ blob }) => {
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      })
      .catch((reason: unknown) => setError(describeError(reason).title));
    return () => {
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [target.id]);
  return (
    <Modal opened title={target.title} size={target.mediaType.startsWith('audio/') ? 520 : 960} onClose={onClose}>
      {error ? <Alert color="red">{error}</Alert> : null}
      {!url && !error ? <Center py="xl"><Loader size="sm" /></Center> : null}
      {url && target.mediaType.startsWith('image/') ? <Image src={url} alt={target.title} fit="contain" /> : null}
      {url && target.mediaType.startsWith('audio/') ? <audio src={url} controls style={{ width: '100%' }} /> : null}
    </Modal>
  );
}

export function FilePreviewModal({ target, onClose }: { target: Target | null; onClose: () => void }) {
  if (!target) return null;
  if (target.mediaType.startsWith('image/') || target.mediaType.startsWith('audio/')) {
    return <BlobPreview target={target} onClose={onClose} />;
  }
  return (
    <Suspense fallback={<LoadingOverlay visible zIndex={1000} loaderProps={{ children: <Loader size="sm" /> }} />}>
      <MaterialPreviewModal
        materialId={target.id}
        title={target.title}
        isWord={target.mediaType === WORD}
        pdfFileName={`${target.fileName.replace(/\.docx$/i, '')}.pdf`}
        initialPage={target.initialPage}
        onClose={onClose}
      />
    </Suspense>
  );
}
