import { useState } from 'react';
import { Accordion, Alert, Anchor, Button, Group, Modal, Stack, Text } from '@mantine/core';
import { useForm } from '@mantine/form';
import type { UploadHints } from '../../shared/api/types';
import { formatBytes } from '../../shared/lib/format';
import { message } from '../../shared/ui/feedback';
import { PackCatalogFields, type CatalogFormValues } from '../packs/PackCatalogFields';
import { usePackFacets } from '../packs/queries';
import { FileSelector } from './FileSelector';
import type { SelectedFile, SourceResult } from './fileSources';
import { useUploads } from './UploadManager';

const HINT_FIELDS = ['doc_type', 'subject_codes', 'grade', 'cohort', 'region', 'organization', 'tags'] as const;

/** 选择文件并可选地给整批加提示；不填提示也能直接上传。 */
export function UploadDialog() {
  const { dialogOpen, closeDialog, start } = useUploads();
  const facets = usePackFacets();
  const form = useForm<CatalogFormValues>({ initialValues: {} });
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
    form.reset();
  };

  const submit = () => {
    const values = form.values;
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
    message.success('已开始导入，可以继续做别的事');
    reset();
    closeDialog();
  };

  const cancel = () => {
    reset();
    closeDialog();
  };

  const totalBytes = selected.reduce((sum, f) => sum + f.sizeBytes, 0);
  return (
    <Modal opened={dialogOpen} onClose={cancel} title="导入资料" size={720}>
      <Stack gap="md" data-upload-dropzone>
        <FileSelector inputId="global-upload" onSelected={handleSelected} />
        {selected.length > 0 ? (
          <Alert color="blue" title={`已选 ${selected.length} 个文件，共 ${formatBytes(totalBytes)}`}>
            <Group justify="space-between" align="flex-start" wrap="nowrap">
              <Text size="sm" lineClamp={2}>{selected.map((f) => f.relativePath).join('、')}</Text>
              <Anchor component="button" type="button" size="sm" onClick={() => setSelected([])}>清空</Anchor>
            </Group>
          </Alert>
        ) : null}
        <Accordion variant="contained">
          <Accordion.Item value="hints">
            <Accordion.Control>给这批资料加提示（可选，AI 会参考）</Accordion.Control>
            <Accordion.Panel><PackCatalogFields form={form} fields={[...HINT_FIELDS]} facets={facets.data} /></Accordion.Panel>
          </Accordion.Item>
        </Accordion>
        <Text size="sm" c="dimmed">支持 PDF、Word、PPT、Excel、图片、音频和 ZIP（含多层压缩包）。也可以把文件直接拖到页面任意位置。</Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={cancel}>取消</Button>
          <Button disabled={selected.length === 0} onClick={submit}>{selected.length > 0 ? `开始导入 ${selected.length} 个文件` : '开始导入'}</Button>
        </Group>
      </Stack>
    </Modal>
  );
}
