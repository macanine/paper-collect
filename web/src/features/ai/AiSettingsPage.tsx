import { useEffect } from 'react';
import { App, Button, Card, Form, Input, InputNumber, Space, Switch, Tag } from 'antd';
import { useMutation } from '@tanstack/react-query';
import { describeError } from '../../shared/api/errors';
import { PageHeader } from '../../shared/ui/PageHeader';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { testMinerU } from './api';
import { useAISettings, useUpdateAISettings } from './queries';

interface SettingsFormValues {
  analysis_enabled: boolean;
  mineru_base_url: string;
  mineru_token: string;
  max_pages: number;
  max_images: number;
}

export function AiSettingsPage() {
  const { message } = App.useApp();
  const settings = useAISettings();
  const save = useUpdateAISettings();
  const probe = useMutation({ mutationFn: testMinerU });
  const [form] = Form.useForm<SettingsFormValues>();

  useEffect(() => {
    if (!settings.data) return;
    form.setFieldsValue({
      analysis_enabled: settings.data.analysis_enabled,
      mineru_base_url: settings.data.mineru_base_url,
      mineru_token: '',
      max_pages: settings.data.max_pages,
      max_images: settings.data.max_images,
    });
  }, [settings.data, form]);

  const handleSave = async () => {
    const values = await form.validateFields();
    try {
      await save.mutateAsync({
        analysis_enabled: values.analysis_enabled,
        mineru_base_url: values.mineru_base_url.trim(),
        max_pages: values.max_pages,
        max_images: values.max_images,
        ...(values.mineru_token.trim() ? { mineru_token: values.mineru_token.trim() } : {}),
      });
      form.setFieldValue('mineru_token', '');
      message.success('AI 解析设置已保存');
    } catch (error) {
      const { title, detail } = describeError(error);
      message.error(detail ? `${title}：${detail}` : title);
    }
  };

  const handleTest = () => {
    probe.mutate(undefined, {
      onSuccess: (result) =>
        result.healthy ? message.success(result.message) : message.warning(result.message || 'MinerU 不可用'),
      onError: (error) => message.error(describeError(error).title),
    });
  };

  return (
    <div className="page-stack">
      <PageHeader title="AI 解析" />

      {settings.isPending ? <LoadingState /> : null}
      {settings.isError ? <ErrorState error={settings.error} onRetry={() => void settings.refetch()} /> : null}
      {settings.isSuccess ? (
        <Card className="max-w-[720px]">
          <Form form={form} layout="vertical">
            <Form.Item name="analysis_enabled" label="自动识别" valuePropName="checked">
              <Switch />
            </Form.Item>

            <Form.Item
              name="mineru_base_url"
              label="MinerU Base URL"
              rules={[
                { required: true },
                {
                  validator: async (_rule, value: string) => {
                    try {
                      const url = new URL((value ?? '').trim());
                      if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new Error('scheme');
                    } catch {
                      throw new Error('请输入完整的 http(s) 地址');
                    }
                  },
                },
              ]}
            >
              <Input placeholder="https://mineru.net" />
            </Form.Item>

            <Form.Item
              name="mineru_token"
              label={
                <Space>
                  <span>MinerU 令牌</span>
                  {settings.data.mineru_configured ? <Tag color="green">已保存</Tag> : <Tag color="orange">未设置</Tag>}
                </Space>
              }
            >
              <Input.Password autoComplete="new-password" placeholder="令牌" />
            </Form.Item>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Form.Item name="max_pages" label="最多解析页数" rules={[{ required: true }]}>
                <InputNumber min={1} max={10} className="w-full" />
              </Form.Item>
              <Form.Item name="max_images" label="发送给视觉模型的图片数" rules={[{ required: true }]}>
                <InputNumber min={0} max={4} className="w-full" />
              </Form.Item>
            </div>

            <Space>
              <Button type="primary" onClick={() => void handleSave()} loading={save.isPending}>
                保存设置
              </Button>
              <Button onClick={handleTest} loading={probe.isPending}>
                测试 MinerU
              </Button>
            </Space>
          </Form>
        </Card>
      ) : null}
    </div>
  );
}
