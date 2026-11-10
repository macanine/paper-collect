import { useState } from 'react';
import { Alert, App, Button, Card, Form, Radio, Space, Typography } from 'antd';
import { useQueryClient } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import type { RerunResult } from '../../shared/api/types';
import { invalidateLibrary } from '../materials/queries';
import { rerunAI } from './api';

type Mode = 'decide' | 'full';
type Scope = 'attention' | 'all';

/** 让已有资料重新经过 AI：先预览影响范围和预计调用次数，确认后执行。 */
export function RerunCard({ canWrite }: { canWrite: boolean }) {
  const { message } = App.useApp();
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
    <Card title="重新整理已有资料" className="max-w-[760px]">
      <Typography.Paragraph type="secondary">
        让已经入库的资料重新经过 AI。人工修改过的字段和部件保持不变。
      </Typography.Paragraph>
      <Form layout="vertical">
        <Form.Item label="范围">
          <Radio.Group
            value={scope}
            onChange={(event) => {
              setScope(event.target.value as Scope);
              setPreview(null);
            }}
            options={[
              { value: 'attention', label: '仅需关注的资料' },
              { value: 'all', label: '整个资料库' },
            ]}
          />
        </Form.Item>
        <Form.Item label="方式">
          <Radio.Group
            value={mode}
            onChange={(event) => {
              setMode(event.target.value as Mode);
              setPreview(null);
            }}
            options={[
              { value: 'decide', label: '保留现有分组，只重新编目' },
              { value: 'full', label: '重新阅读、分组并编目（复用已解析的正文）' },
            ]}
          />
        </Form.Item>
      </Form>
      {preview ? (
        <Alert
          className="mb-3"
          type="info"
          showIcon
          title={`将处理 ${preview.materials} 个文件、${preview.packs} 套，预计调用模型约 ${preview.estimated_calls} 次`}
        />
      ) : null}
      <Space>
        <Button onClick={() => void run(true)} loading={busy && !preview} disabled={!canWrite}>
          预览影响
        </Button>
        <Button onClick={() => void run(false)} loading={busy && preview !== null} disabled={!canWrite || !preview || preview.materials === 0}>
          开始重新整理
        </Button>
      </Space>
    </Card>
  );
}
