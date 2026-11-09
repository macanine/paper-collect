import { useState } from 'react';
import { Alert, App, Collapse, Form, Modal, Typography } from 'antd';
import type { UploadHints } from '../../shared/api/types';
import { formatBytes } from '../../shared/lib/format';
import { PackCatalogFields, type CatalogFormValues } from '../packs/PackCatalogFields';
import { usePackFacets } from '../packs/queries';
import { FileSelector } from './FileSelector';
import type { SelectedFile, SourceResult } from './fileSources';
import { useUploads } from './UploadManager';

/** 选择文件并可选地给整批加提示；不填提示也能直接上传。 */
export function UploadDialog() {
  const { dialogOpen, closeDialog, start } = useUploads();
  const { message } = App.useApp();
  const facets = usePackFacets();
  const [form] = Form.useForm<CatalogFormValues>();
  const [selected, setSelected] = useState<SelectedFile[]>([]);

  const handleSelected = (result: SourceResult) => {
    setSelected((current) => {
      const seen = new Set(current.map((f) => f.relativePath));
      return [...current, ...result.files.filter((f) => !seen.has(f.relativePath))];
    });
    if (result.ignored > 0) message.info(`已忽略 ${result.ignored} 个系统文件`);
  };

  const reset = () => {
    setSelected([]);
    form.resetFields();
  };

  const submit = () => {
    const values = form.getFieldsValue();
    const hints: Partial<UploadHints> = {
      subject_codes: values.subject_codes ?? [],
      grade: values.grade ?? '',
      doc_type: values.doc_type ?? '',
      cohort: values.cohort ?? 0,
      region: values.region?.trim() ?? '',
      organization: values.organization?.trim() ?? '',
      tags: values.tags ?? [],
    };
    start(selected, hints);
    message.success('已开始上传，可以继续做别的事');
    reset();
    closeDialog();
  };

  const totalBytes = selected.reduce((sum, f) => sum + f.sizeBytes, 0);
  return (
    <Modal
      open={dialogOpen}
      title="上传资料"
      width={720}
      okText={selected.length > 0 ? `开始上传 ${selected.length} 个文件` : '开始上传'}
      okButtonProps={{ disabled: selected.length === 0 }}
      cancelText="取消"
      onOk={submit}
      onCancel={() => {
        reset();
        closeDialog();
      }}
      destroyOnHidden
    >
      <div className="page-stack" data-upload-dropzone>
        <FileSelector inputId="global-upload" onSelected={handleSelected} />
        {selected.length > 0 ? (
          <Alert
            type="info"
            showIcon
            title={`已选 ${selected.length} 个文件，共 ${formatBytes(totalBytes)}`}
            description={
              <Typography.Paragraph ellipsis={{ rows: 2 }} className="!mb-0">
                {selected.map((f) => f.relativePath).join('、')}
              </Typography.Paragraph>
            }
            action={
              <Typography.Link onClick={() => setSelected([])}>清空</Typography.Link>
            }
          />
        ) : null}
        <Collapse
          size="small"
          items={[
            {
              key: 'hints',
              label: '给这批资料加提示（可选，AI 会参考）',
              children: (
                <Form form={form} layout="vertical">
                  <PackCatalogFields fields={['doc_type', 'subject_codes', 'grade', 'cohort', 'region', 'organization', 'tags']} facets={facets.data} />
                </Form>
              ),
            },
          ]}
        />
        <Typography.Text type="secondary">
          支持 PDF、Word、PPT、Excel、图片、音频和 ZIP（含多层压缩包）。也可以把文件直接拖到页面任意位置。
        </Typography.Text>
      </div>
    </Modal>
  );
}
