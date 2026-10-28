import { useMemo, useRef, useState } from 'react';
import { Alert, App, Button, Card } from 'antd';
import { useNavigate } from 'react-router';
import { errorMessage } from '../../shared/api/errors';
import { PageHeader } from '../../shared/ui/PageHeader';
import { createUpload } from './api';
import { directUploadBody } from './directUpload';
import { FileSelector } from './FileSelector';
import type { SelectedFile, SourceResult } from './fileSources';
import { validateManifest } from './manifest';
import { stashFiles } from './stash';
import { matchFiles } from './useUploadTransfer';

export function NewUploadPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const [selected, setSelected] = useState<SelectedFile[]>([]);
  const [attempted, setAttempted] = useState<SelectedFile[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [startError, setStartError] = useState('');
  const starting = useRef(false);
  const report = useMemo(() => validateManifest(selected), [selected]);
  const problemIndexes = new Set(Object.keys(report.itemIssues).map(Number));
  const usable = selected.filter((_item, index) => !problemIndexes.has(index));
  const canSkipProblems = problemIndexes.size > 0 && validateManifest(usable).valid;

  const startUpload = async (files: SelectedFile[]) => {
    if (starting.current) return;
    starting.current = true;
    setSubmitting(true);
    setStartError('');
    setAttempted(files);
    try {
      const upload = await createUpload(directUploadBody(files));
      const { matched } = matchFiles(upload.entries, files, new Map());
      stashFiles(upload.id, matched);
      message.success('已开始上传');
      navigate(`/uploads/${upload.id}`);
    } catch (error) {
      setStartError(errorMessage(error));
    } finally {
      starting.current = false;
      setSubmitting(false);
    }
  };

  const handleSelected = (result: SourceResult) => {
    if (starting.current) return;
    setSelected(result.files);
    setAttempted([]);
    setStartError('');
    if (result.ignored > 0) message.info(`已忽略 ${result.ignored} 个系统文件`);
    if (validateManifest(result.files).valid) void startUpload(result.files);
  };

  return (
    <div className="page-stack">
      <PageHeader title="上传资料" />

      <Card title="选择资料">
        <FileSelector inputId="new-upload" onSelected={handleSelected} disabled={submitting} />
        {submitting ? <Alert type="info" showIcon className="mt-4" message="正在创建上传任务…" /> : null}
      </Card>

      {startError ? (
        <Alert
          type="error"
          showIcon
          message="无法开始上传"
          description={startError}
          action={
            <Button size="small" disabled={submitting} onClick={() => void startUpload(attempted)}>
              重试
            </Button>
          }
        />
      ) : null}

      {selected.length > 0 && !report.valid ? (
        <Card title="部分文件无法上传">
          <div className="flex flex-col gap-2">
            {report.globalErrors.map((error) => (
              <span className="text-red-600" key={error}>
                {error}
              </span>
            ))}
            {Object.entries(report.itemIssues).slice(0, 20).map(([index, issues]) => (
              <span className="text-red-600" key={index}>
                {selected[Number(index)]?.relativePath}：{issues.join('；')}
              </span>
            ))}
            {canSkipProblems ? (
              <Button type="primary" disabled={submitting} onClick={() => void startUpload(usable)}>
                跳过 {problemIndexes.size} 个问题文件并上传 {usable.length} 个
              </Button>
            ) : null}
          </div>
        </Card>
      ) : null}
    </div>
  );
}
