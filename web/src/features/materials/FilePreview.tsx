import { lazy, Suspense, useEffect, useState } from 'react';
import { Alert, Modal, Spin } from 'antd';
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
    <Modal open title={target.title} footer={null} width={target.mediaType.startsWith('audio/') ? 520 : 960} onCancel={onClose} destroyOnHidden>
      {error ? <Alert type="error" showIcon message={error} /> : null}
      {!url && !error ? <Spin /> : null}
      {url && target.mediaType.startsWith('image/') ? <img src={url} alt={target.title} className="max-w-full" /> : null}
      {url && target.mediaType.startsWith('audio/') ? <audio src={url} controls className="w-full" /> : null}
    </Modal>
  );
}

export function FilePreviewModal({ target, onClose }: { target: Target | null; onClose: () => void }) {
  if (!target) return null;
  if (target.mediaType.startsWith('image/') || target.mediaType.startsWith('audio/')) {
    return <BlobPreview target={target} onClose={onClose} />;
  }
  return (
    <Suspense fallback={<Spin fullscreen tip="正在加载预览" />}>
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
