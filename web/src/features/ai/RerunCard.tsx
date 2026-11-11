import { useState } from 'react';
import { Alert, Button, Group, Radio, Stack, Text } from '@mantine/core';
import { useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { RerunResult } from '../../shared/api/types';
import { Section } from '../../shared/ui/Section';
import { message } from '../../shared/ui/feedback';
import { invalidateLibrary } from '../materials/queries';
import { rerunAI } from './api';

type Mode = 'decide' | 'full';
type Scope = 'attention' | 'all';

/** 让已有资料重新经过 AI：先预览影响范围和预计调用次数，确认后执行。 */
export function RerunCard({ canWrite }: { canWrite: boolean }) {
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<Mode>('decide');
  const [scope, setScope] = useState<Scope>('attention');
  const [preview, setPreview] = useState<RerunResult | null>(null);
  const [busy, setBusy] = useState(false);

  const run = async (dryRun: boolean) => {
    setBusy(true);
    try {
      const result = await rerunAI({ mode, scope, dry_run: dryRun });
      if (dryRun) {
        setPreview(result);
      } else {
        setPreview(null);
        invalidateLibrary(queryClient);
        message.success(`已安排 ${result.materials} 个文件重新整理`);
      }
    } catch (error) {
      message.error(describeError(error).title);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section title="重新整理已有资料">
      <Text size="sm" c="dimmed">让已经入库的资料重新经过 AI。人工修改过的字段和部件保持不变。</Text>
      <Radio.Group label="范围" value={scope} onChange={(v) => { setScope(v as Scope); setPreview(null); }}>
        <Stack gap="xs" mt="xs"><Radio value="attention" label="仅需关注的资料" /><Radio value="all" label="整个资料库" /></Stack>
      </Radio.Group>
      <Radio.Group label="方式" value={mode} onChange={(v) => { setMode(v as Mode); setPreview(null); }}>
        <Stack gap="xs" mt="xs"><Radio value="decide" label="保留现有分组，只重新编目" /><Radio value="full" label="重新阅读、分组并编目（复用已解析的正文）" /></Stack>
      </Radio.Group>
      {preview ? <Alert color="blue">{`将处理 ${preview.materials} 个文件、${preview.packs} 套，预计调用模型约 ${preview.estimated_calls} 次`}</Alert> : null}
      <Group>
        <Button variant="default" onClick={() => void run(true)} loading={busy && !preview} disabled={!canWrite}>预览影响</Button>
        <Button onClick={() => void run(false)} loading={busy && preview !== null} disabled={!canWrite || !preview || preview.materials === 0}>开始重新整理</Button>
      </Group>
    </Section>
  );
}
