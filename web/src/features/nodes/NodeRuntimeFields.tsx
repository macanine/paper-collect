import { Alert, MultiSelect, NumberInput, SimpleGrid, Stack, Text } from '@mantine/core';
import type { UseFormReturnType } from '@mantine/form';
import type { OCRProvider } from '../../shared/api/types';
import { JOB_KIND_LABELS } from '../../shared/lib/labels';
import { ocrProviderLabel, type NodeRuntimeFormValues } from './nodeUtils';

interface NodeRuntimeFieldsProps {
  form: UseFormReturnType<NodeRuntimeFormValues & { name?: string }>;
  providers: OCRProvider[];
  providersLoading?: boolean;
  providersError?: boolean;
}

/** 校验节点运行配置，返回各字段错误。 */
export function validateRuntime(values: NodeRuntimeFormValues) {
  const errors: Record<string, string> = {};
  for (const kind of ['replicate', 'analyze', 'index'] as const) {
    const v = values.slots?.[kind];
    if (typeof v !== 'number' || v < 0 || v > 64) errors[`slots.${kind}`] = '请输入 0 到 64';
  }
  if (typeof values.cache_max_gib !== 'number' || values.cache_max_gib < 0 || values.cache_max_gib > 1048576) errors.cache_max_gib = '请输入 0 到 1048576 GiB';
  return errors;
}

export function NodeRuntimeFields({ form, providers, providersLoading, providersError }: NodeRuntimeFieldsProps) {
  const options = providers.map((provider) => ({ value: provider.id, label: ocrProviderLabel(provider), disabled: !provider.enabled }));

  return (
    <Stack gap="lg">
      <Stack gap="xs">
        <div>
          <Text fw={600} size="sm">任务并发槽位</Text>
          <Text size="xs" c="dimmed">0 表示不在该节点运行此类任务；修改会在从端下一次心跳时生效。</Text>
        </div>
        <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="sm">
          {(['replicate', 'analyze', 'index'] as const).map((kind) => (
            <NumberInput key={kind} label={JOB_KIND_LABELS[kind]} min={0} max={64} allowDecimal={false} {...form.getInputProps(`slots.${kind}`)} />
          ))}
        </SimpleGrid>
      </Stack>

      <NumberInput
        label="原件缓存上限"
        description="只限制从端本机缓存，0 表示不限；缓存目录仍由从端通过 CACHE_DIR 指定。"
        min={0}
        max={1048576}
        allowDecimal={false}
        rightSection={<Text size="xs" c="dimmed" pr="xs">GiB</Text>}
        rightSectionWidth={44}
        {...form.getInputProps('cache_max_gib')}
      />

      <Stack gap="xs">
        <div>
          <Text fw={600} size="sm">可访问的 OCR 服务</Text>
          <Text size="xs" c="dimmed">RapidOCR、PaddleOCR、MinerU 使用同一套任务协议。节点只领取其可访问服务的分析和索引任务，多个服务按 AI 解析页中的全局优先级选择。</Text>
        </div>
        {providersError ? (
          <Alert color="yellow" title="OCR 服务列表加载失败">先修复 AI 解析配置页，再保存节点，避免覆盖现有 OCR 选择。</Alert>
        ) : (
          <MultiSelect
            clearable
            searchable
            data={options}
            disabled={providersLoading}
            placeholder={providers.length > 0 ? '选择该节点可以访问的 OCR 服务' : '还没有配置 OCR 服务'}
            nothingFoundMessage={providersLoading ? '正在加载' : '请先在 AI 解析页添加 OCR 服务'}
            {...form.getInputProps('ocr_provider_ids')}
          />
        )}
      </Stack>
    </Stack>
  );
}
