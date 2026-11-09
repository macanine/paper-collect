import { Alert, Form, InputNumber, Select, Space, Typography } from 'antd';
import type { OCRProvider } from '../../shared/api/types';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { ocrProviderLabel } from './nodeUtils';

interface NodeRuntimeFieldsProps {
  providers: OCRProvider[];
  providersLoading?: boolean;
  providersError?: boolean;
}

export function NodeRuntimeFields({ providers, providersLoading, providersError }: NodeRuntimeFieldsProps) {
  const options = providers.map((provider) => ({
    value: provider.id,
    label: ocrProviderLabel(provider),
    disabled: !provider.enabled,
  }));

  return (
    <Space direction="vertical" size={20} style={{ width: '100%' }}>
      <div>
        <Typography.Text strong>任务并发槽位</Typography.Text>
        <Typography.Paragraph type="secondary" style={{ margin: '4px 0 12px' }}>
          0 表示不在该节点运行此类任务；修改会在从端下一次心跳时生效。
        </Typography.Paragraph>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          {(['replicate', 'analyze', 'index'] as const).map((kind) => (
            <Form.Item
              key={kind}
              name={['slots', kind]}
              label={JOB_KIND_LABELS[kind]}
              rules={[{ required: true, type: 'number', min: 0, max: 64, message: '请输入 0 到 64' }]}
              style={{ marginBottom: 0 }}
            >
              <InputNumber min={0} max={64} precision={0} className="w-full" />
            </Form.Item>
          ))}
        </div>
      </div>

      <Form.Item
        name="cache_max_gib"
        label="原件缓存上限"
        extra="只限制从端本机缓存，0 表示不限；缓存目录仍由从端通过 CACHE_DIR 指定。"
        rules={[{ required: true, type: 'number', min: 0, max: 1048576, message: '请输入 0 到 1048576 GiB' }]}
        style={{ marginBottom: 0 }}
      >
        <InputNumber min={0} max={1048576} precision={0} addonAfter="GiB" className="w-full" />
      </Form.Item>

      <div>
        <Typography.Text strong>可访问的 OCR 服务</Typography.Text>
        <Typography.Paragraph type="secondary" style={{ margin: '4px 0 12px' }}>
          RapidOCR、PaddleOCR、MinerU 使用同一套任务协议。节点只领取其可访问服务的分析和索引任务，多个服务按 AI 解析页中的全局优先级选择。
        </Typography.Paragraph>
        {providersError ? (
          <Alert
            type="warning"
            showIcon
            title="OCR 服务列表加载失败"
            description="先修复 AI 解析配置页，再保存节点，避免覆盖现有 OCR 选择。"
          />
        ) : (
          <Form.Item name="ocr_provider_ids" style={{ marginBottom: 0 }}>
            <Select
              mode="multiple"
              allowClear
              loading={providersLoading}
              optionFilterProp="label"
              placeholder={providers.length > 0 ? '选择该节点可以访问的 OCR 服务' : '还没有配置 OCR 服务'}
              options={options}
              notFoundContent={providersLoading ? '正在加载' : '请先在 AI 解析页添加 OCR 服务'}
            />
          </Form.Item>
        )}
      </div>
    </Space>
  );
}
