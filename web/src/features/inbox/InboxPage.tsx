import { useCallback, useEffect, useRef, useState } from 'react';
import { Accordion, ActionIcon, Alert, Anchor, Box, Button, Checkbox, Flex, Group, NumberInput, Paper, ScrollArea, SegmentedControl, Select, Stack, Text, UnstyledButton } from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconEye } from '@tabler/icons-react';
import { Link, useSearchParams } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import type { PackMember, Part, InboxPack, Stage } from '../../shared/api/types';
import { errorMessage } from '../../shared/api/errors';
import { formatBytes, formatDateTime } from '../../shared/lib/format';
import { ATTENTION_REASON_LABELS, attentionReasonLabel } from '../../shared/lib/labels';
import { PART_OPTIONS } from '../../shared/lib/options';
import { ItemList, ItemRow } from '../../shared/ui/ItemList';
import { PageHeader } from '../../shared/ui/PageHeader';
import { message } from '../../shared/ui/feedback';
import { ErrorState, LoadingState } from '../../shared/ui/States';
import { PartTag, StageTag, Tag } from '../../shared/ui/StatusTags';
import { PackAIPanel } from '../ai/AIPanels';
import { FilePreviewModal, canPreview } from '../materials/FilePreview';
import { invalidateLibrary } from '../materials/queries';
import { useAcceptMaterials, useInbox } from './queries';
import { confirmPack } from '../packs/api';
import { Completeness } from '../packs/PackBadges';
import { PackCatalogFields, formToUpdate, packToForm, validateCatalog, type CatalogField, type CatalogFormValues } from '../packs/PackCatalogFields';
import { usePackFacets } from '../packs/queries';
import { href } from '../../app/routes';

const PRIMARY_CATALOG_FIELDS: CatalogField[] = ['doc_type', 'subject_codes', 'grade', 'cohort', 'region', 'organization', 'exam_name'];
const ADVANCED_CATALOG_FIELDS: CatalogField[] = ['term', 'exam_date', 'chapter', 'tags'];
const CATALOG_FIELDS = [...PRIMARY_CATALOG_FIELDS, ...ADVANCED_CATALOG_FIELDS];
const EMPTY_REVIEW_PACKS: InboxPack[] = [];

interface MemberEdit {
  part: Part;
  answer_start_page: number;
}

function initialMemberEdits(members: PackMember[]): Record<string, MemberEdit> {
  return Object.fromEntries(members.map((member) => [member.id, { part: member.part, answer_start_page: member.answer_start_page }]));
}

function packReasons(group: InboxPack): string[] {
  return group.items.flatMap((item) => item.attention_reasons).filter((reason, index, all) => all.indexOf(reason) === index);
}

/** 右侧：一套需关注资料。AI 的编目与校验问题在上，人工修改（会被锁定）在下。 */
function AttentionPanel({ group, stage, onDone }: { group: InboxPack; stage: Stage; onDone: () => void }) {
  const queryClient = useQueryClient();
  const facets = usePackFacets();
  const form = useForm<CatalogFormValues>({ initialValues: {}, validate: validateCatalog });
  const [saving, setSaving] = useState(false);
  const [preview, setPreview] = useState<PackMember | null>(null);
  const [editing, setEditing] = useState(false);
  const [memberEdits, setMemberEdits] = useState<Record<string, MemberEdit>>(() => initialMemberEdits(group.pack.members));
  const editable = stage === 'attention';
  const showEdit = editable && editing;
  const attentionIds = group.pack.members.filter((member) => member.stage === 'attention').map((member) => member.id);

  // Load the pack into the form when it really changed (the list polls), but
  // never while the person is editing: their unsaved input wins.
  const packRef = useRef(group.pack);
  packRef.current = group.pack;
  useEffect(() => {
    if (editing) return;
    const values = packToForm(packRef.current);
    form.setValues(values);
    form.resetDirty(values);
    setMemberEdits(initialMemberEdits(packRef.current.members));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [group.pack.updated_at, editing]);

  const updateMember = (member: PackMember, patch: Partial<MemberEdit>) => {
    setMemberEdits((current) => ({
      ...current,
      [member.id]: { ...(current[member.id] ?? { part: member.part, answer_start_page: member.answer_start_page }), ...patch },
    }));
  };

  const confirm = useCallback(async () => {
    if (!editable || saving || attentionIds.length === 0) return;
    if (form.validate().hasErrors) return;
    const values = form.values;
    setSaving(true);
    try {
      const touched = CATALOG_FIELDS.filter((field) => form.isDirty(field));
      const members = group.pack.members
        .filter((member) => {
          const next = memberEdits[member.id];
          return next && (next.part !== member.part || next.answer_start_page !== member.answer_start_page);
        })
        .map((member) => ({ id: member.id, ...memberEdits[member.id]! }));
      await confirmPack(group.pack.id, { patch: formToUpdate(values, touched), members });
      invalidateLibrary(queryClient);
      message.success(`已确认《${group.pack.title}》`);
      onDone();
    } catch (error) {
      message.error(errorMessage(error));
    } finally {
      setSaving(false);
    }
  }, [attentionIds, editable, form, group.pack, memberEdits, onDone, queryClient, saving]);

  return (
    <Paper p="lg" flex={1} miw={0}>
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Stack gap={2} miw={0}>
            <Text fw={600} truncate>{group.pack.title || '未命名资料'}</Text>
            <Text size="xs" c="dimmed">{group.pack.members.length} 个文件 · {group.pack.is_exam ? '试卷资料' : '普通资料'}</Text>
          </Stack>
          <Group gap="sm" wrap="nowrap" style={{ flexShrink: 0 }}>
            <Completeness pack={group.pack} />
            <Anchor component={Link} to={href.pack(group.pack.id)} size="sm">打开套卷</Anchor>
          </Group>
        </Group>

        <Group gap="xs">
          <StageTag stage={stage} />
          {packReasons(group).map((reason) => <Tag key={reason} color="warning">{attentionReasonLabel(reason)}</Tag>)}
        </Group>

        {stage === 'waiting_ai' ? (
          <Alert color="blue" title="AI 暂不可用">
            请检查 <Anchor component={Link} to={href.settings('ai-models')} size="sm">AI 模型</Anchor> 和 <Anchor component={Link} to={href.settings('ai-parsing')} size="sm">AI 解析</Anchor> 的配置；保存后这些文件会自动继续。
          </Alert>
        ) : null}
        {stage === 'processing' ? <Alert color="blue">AI 正在阅读、分组或编目，完成后会自动入库</Alert> : null}

        <PackAIPanel packId={group.pack.id} canWrite={editable} />

        {showEdit ? (
          <Stack gap="md">
            <Text size="sm" c="dimmed">在下方修改的字段会被锁定，之后 AI 不再改动；未修改的字段保持 AI 的结果。</Text>
            <PackCatalogFields form={form} fields={PRIMARY_CATALOG_FIELDS} facets={facets.data} columns={2} />
            <Accordion variant="contained">
              <Accordion.Item value="advanced">
                <Accordion.Control>更多编目信息（可选）</Accordion.Control>
                <Accordion.Panel><PackCatalogFields form={form} fields={ADVANCED_CATALOG_FIELDS} facets={facets.data} columns={2} /></Accordion.Panel>
              </Accordion.Item>
            </Accordion>
          </Stack>
        ) : null}

        <Stack gap="xs">
          <Text fw={600} size="sm">文件（{group.pack.members.length}）</Text>
          <ItemList>
            {group.pack.members.map((member) => {
              const edit = memberEdits[member.id] ?? { part: member.part, answer_start_page: member.answer_start_page };
              return (
                <ItemRow
                  key={member.id}
                  main={
                    <Stack gap={4}>
                      <Group gap={4} wrap="nowrap">
                        {canPreview(member.media_type) ? <ActionIcon variant="subtle" size="sm" onClick={() => setPreview(member)} aria-label={`预览 ${member.file_name}`}><IconEye size={16} /></ActionIcon> : null}
                        <Anchor component={Link} to={href.material(member.id)} size="sm" truncate>{member.file_name}</Anchor>
                      </Group>
                      <Group gap="sm">
                        <PartTag part={edit.part} />
                        <Text size="xs" c="dimmed">{formatBytes(member.size_bytes)}</Text>
                        <StageTag stage={member.stage} />
                        {edit.part === 'full' && edit.answer_start_page > 0 ? <Text size="xs" c="dimmed">答案从第 {edit.answer_start_page} 页开始</Text> : null}
                      </Group>
                    </Stack>
                  }
                  aside={showEdit ? (
                    <>
                      <Select size="xs" w={128} allowDeselect={false} aria-label={`${member.file_name} 的部件`} value={edit.part} data={PART_OPTIONS} onChange={(part) => part && updateMember(member, { part: part as Part })} />
                      {edit.part === 'full' ? <NumberInput size="xs" w={110} min={1} max={10000} allowDecimal={false} value={edit.answer_start_page || ''} placeholder="答案起始页" onChange={(value) => updateMember(member, { answer_start_page: typeof value === 'number' ? value : 0 })} /> : null}
                    </>
                  ) : null}
                />
              );
            })}
          </ItemList>
        </Stack>

        {editable ? (
          <Group>
            <Button loading={saving} disabled={attentionIds.length === 0} onClick={() => void confirm()}>{showEdit ? '保存并确认' : `确认这套 ${attentionIds.length} 个文件`}</Button>
            {/* Leaving edit mode discards the edits: the effect above reloads the pack. */}
            <Button variant="subtle" onClick={() => setEditing(!showEdit)}>{showEdit ? '取消修改' : '修改编目'}</Button>
          </Group>
        ) : null}
      </Stack>
      {preview ? <FilePreviewModal target={{ id: preview.id, title: preview.title, fileName: preview.file_name, mediaType: preview.media_type }} onClose={() => setPreview(null)} /> : null}
    </Paper>
  );
}

const TABS: { value: Stage; label: string }[] = [
  { value: 'attention', label: '需关注' },
  { value: 'processing', label: 'AI 整理中' },
  { value: 'waiting_ai', label: '等待 AI' },
];

const EMPTY_COPY: Partial<Record<Stage, string>> = {
  attention: '没有需要人工处理的套卷',
  processing: 'AI 当前没有正在整理的套卷',
  waiting_ai: '没有等待 AI 的套卷',
};

export function InboxPage() {
  const [params, setParams] = useSearchParams();
  const stage = (TABS.find((tab) => tab.value === params.get('stage'))?.value ?? 'attention') as Stage;
  const reason = params.get('reason') ?? '';
  const [page, setPage] = useState(1);
  const query = useInbox({ page, page_size: 100, stage, reason: reason || undefined }, { poll: stage === 'attention' ? 30_000 : 8000 });
  const accept = useAcceptMaterials();
  const groups = query.data?.items ?? EMPTY_REVIEW_PACKS;
  const [activeId, setActiveId] = useState<string>();
  const [checked, setChecked] = useState<string[]>([]);
  const [bulk, setBulk] = useState(false);
  const active = groups.find((group) => group.pack.id === activeId) ?? groups[0];

  useEffect(() => {
    setChecked([]);
    setBulk(false);
    setActiveId(undefined);
    setPage(1);
  }, [stage, reason]);

  useEffect(() => {
    setChecked((current) => {
      const next = current.filter((id) => groups.some((group) => group.pack.id === id));
      return next.length === current.length ? current : next;
    });
    setActiveId((current) => (current && groups.some((group) => group.pack.id === current) ? current : groups[0]?.pack.id));
  }, [groups]);

  const advance = useCallback(() => {
    const index = groups.findIndex((group) => group.pack.id === active?.pack.id);
    const next = groups[index + 1] ?? groups[index - 1];
    setActiveId(next?.pack.id);
  }, [active?.pack.id, groups]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (target.closest('input, textarea, select, button, a, [role="button"], [role="combobox"], [role="dialog"]') || target.isContentEditable)) return;
      const index = groups.findIndex((group) => group.pack.id === active?.pack.id);
      if (event.key === 'j' || event.key === 'J') setActiveId(groups[Math.min(groups.length - 1, index + 1)]?.pack.id);
      if (event.key === 'k' || event.key === 'K') setActiveId(groups[Math.max(0, index - 1)]?.pack.id);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [active?.pack.id, groups]);

  const bulkAccept = async () => {
    const ids = groups
      .filter((group) => checked.includes(group.pack.id))
      .flatMap((group) => group.pack.members.filter((member) => member.stage === 'attention').map((member) => member.id));
    if (ids.length === 0) return;
    try {
      const result = await accept.mutateAsync({ ids });
      message.success(`已确认 ${result.count} 个文件`);
      setChecked([]);
    } catch (error) {
      message.error(errorMessage(error));
    }
  };

  const reasonOptions = Object.keys(ATTENTION_REASON_LABELS).map((value) => ({ value, label: attentionReasonLabel(value) }));
  const emptyCopy = reason ? '没有符合该原因的套卷' : (EMPTY_COPY[stage] ?? '没有套卷');
  const bulkMode = stage === 'attention' && bulk;
  return (
    <Stack gap="lg">
      <PageHeader title="需关注" description="编目全部由 AI 完成；这里只列出 AI 没能通过校验的套卷。J/K 可切换套卷。" />
      <Group gap="sm">
        <SegmentedControl value={stage} onChange={(value) => setParams({ stage: value })} data={TABS} />
        {stage === 'attention' ? <Select w={180} clearable placeholder="按原因筛选" value={reason || null} data={reasonOptions} onChange={(value) => setParams(value ? { stage, reason: value } : { stage })} /> : null}
        <Text size="sm" c="dimmed">共 {query.data?.total ?? 0} 套</Text>
        {stage === 'attention' && groups.length > 1 ? <Button variant="subtle" onClick={() => { setBulk((v) => !v); setChecked([]); }}>{bulk ? '完成' : '批量确认'}</Button> : null}
      </Group>

      {query.isError ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
      {query.isPending ? <LoadingState /> : null}
      {!query.isPending && groups.length === 0 ? <Paper p="xl"><Text c="dimmed" ta="center">{emptyCopy}</Text></Paper> : null}
      {!query.isPending && groups.length > 0 ? (
        <Flex gap="lg" align={{ base: "stretch", md: "flex-start" }} direction={{ base: "column", md: "row" }}>
          <Paper w={{ base: '100%', md: 340 }} style={{ flexShrink: 0, overflow: 'hidden' }}>
            {bulkMode ? (
              <Group justify="space-between" p="sm" style={{ borderBottom: '1px solid var(--mantine-color-gray-2)' }}>
                <Checkbox
                  label="全选套卷"
                  checked={checked.length > 0 && checked.length === groups.length}
                  indeterminate={checked.length > 0 && checked.length < groups.length}
                  onChange={(event) => setChecked(event.currentTarget.checked ? groups.map((group) => group.pack.id) : [])}
                />
                {checked.length > 0 ? <Button size="compact-sm" variant="default" onClick={() => void bulkAccept()} loading={accept.isPending}>按当前编目确认 {checked.length} 套</Button> : null}
              </Group>
            ) : null}
            <ScrollArea.Autosize mah={{ base: 320, md: 'calc(100vh - 280px)' }} type="auto">
              {groups.map((group) => {
                const isActive = group.pack.id === active?.pack.id;
                return (
                  <UnstyledButton
                    key={group.pack.id}
                    w="100%"
                    px="md"
                    py="sm"
                    bg={isActive ? 'blue.0' : undefined}
                    aria-current={isActive ? 'true' : undefined}
                    onClick={() => setActiveId(group.pack.id)}
                    style={{ borderBottom: '1px solid var(--mantine-color-gray-2)', borderLeft: `3px solid ${isActive ? 'var(--mantine-color-blue-6)' : 'transparent'}` }}
                  >
                    <Group gap="sm" wrap="nowrap" align="flex-start">
                      {bulkMode ? (
                        <Box onClick={(e) => e.stopPropagation()} pt={2}>
                          <Checkbox aria-label="选择套卷" checked={checked.includes(group.pack.id)} onChange={(event) => setChecked((list) => (event.currentTarget.checked ? [...list, group.pack.id] : list.filter((id) => id !== group.pack.id)))} />
                        </Box>
                      ) : null}
                      <Stack gap={4} miw={0} flex={1}>
                        <Text size="sm" fw={600} lineClamp={2}>{group.pack.title || '未命名资料'}</Text>
                        <Group gap={6}>
                          <Text size="xs" c="dimmed">{group.pack.members.length} 个文件</Text>
                          {packReasons(group).slice(0, 2).map((reasonValue) => <Tag key={reasonValue} color="warning">{attentionReasonLabel(reasonValue)}</Tag>)}
                        </Group>
                        <Text size="xs" c="dimmed">{formatDateTime(group.pack.created_at)}</Text>
                      </Stack>
                    </Group>
                  </UnstyledButton>
                );
              })}
            </ScrollArea.Autosize>
            {(query.data?.total ?? 0) > 100 ? (
              <Group justify="space-between" p="sm" style={{ borderTop: '1px solid var(--mantine-color-gray-2)' }}>
                <Button size="compact-sm" variant="default" disabled={page === 1} onClick={() => setPage(page - 1)}>上一页</Button>
                <Text size="xs" c="dimmed">第 {page} 页</Text>
                <Button size="compact-sm" variant="default" disabled={page * 100 >= (query.data?.total ?? 0)} onClick={() => setPage(page + 1)}>下一页</Button>
              </Group>
            ) : null}
          </Paper>
          {active ? <AttentionPanel key={active.pack.id} group={active} stage={stage} onDone={advance} /> : null}
        </Flex>
      ) : null}
    </Stack>
  );
}
