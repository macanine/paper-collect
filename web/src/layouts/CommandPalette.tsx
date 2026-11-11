import { useDeferredValue, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { Box, Group, Loader, Modal, Text, TextInput, UnstyledButton } from '@mantine/core';
import { IconCloudUpload, IconFileText, IconSearch } from '@tabler/icons-react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { href, ROUTES, SETTINGS, type LazyPage } from '../app/routes';
import { useAuth } from '../features/auth/AuthContext';
import { listPacks } from '../features/packs/api';
import { qk } from '../app/queryKeys';

interface Command {
  id: string;
  group: string;
  label: string;
  hint?: string;
  icon: ReactNode;
  run: () => void;
  preload?: LazyPage['preload'];
}

function matches(query: string, ...fields: (string | undefined)[]): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  return fields.some((field) => field?.toLowerCase().includes(q));
}

/** 全局命令面板：⌘K / Ctrl+K 打开，跳转页面、导入资料、搜题或按名称打开套卷。 */
export function CommandPalette({ open, onClose, onImport }: { open: boolean; onClose: () => void; onImport?: () => void }) {
  const { can } = useAuth();
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const deferred = useDeferredValue(query.trim());
  const searchPacks = can('materials:read') && deferred.length >= 2;

  const packs = useQuery({
    queryKey: qk.packs.lookup(deferred),
    queryFn: ({ signal }) => listPacks({ q: deferred, page: 1, page_size: 6 }, signal),
    enabled: open && searchPacks,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });

  useEffect(() => {
    if (!open) return;
    setQuery('');
    setActive(0);
    const timer = window.setTimeout(() => input.current?.focus(), 0);
    return () => window.clearTimeout(timer);
  }, [open]);

  const commands = useMemo<Command[]>(() => {
    const go = (path: string) => () => {
      navigate(path);
      onClose();
    };
    const out: Command[] = [];
    const q = deferred;
    if (onImport && can('uploads:write') && matches(q, '导入资料', 'upload import 上传')) {
      out.push({ id: 'import', group: '操作', label: '导入资料', icon: <IconCloudUpload size={18} />, run: () => { onClose(); onImport(); } });
    }
    for (const route of ROUTES) {
      if (!route.nav || (route.scope && !can(route.scope)) || !matches(q, route.title, route.keywords)) continue;
      out.push({ id: route.path, group: '页面', label: route.title, icon: route.nav.icon, run: go(route.path), preload: route.page.preload });
    }
    for (const section of SETTINGS) {
      if (!can(section.scope) || !matches(q, section.title, section.description, '设置 settings')) continue;
      out.push({ id: `settings:${section.key}`, group: '设置', label: section.title, hint: section.description, icon: section.icon, run: go(href.settings(section.key)), preload: section.page.preload });
    }
    // 题目搜索排在页面之后，输入页面名称时回车直接跳转页面。
    if (q && can('materials:read')) {
      out.push({ id: 'search', group: '操作', label: `搜题：${q}`, icon: <IconSearch size={18} />, run: go(href.search(q)) });
    }
    if (searchPacks) {
      for (const pack of packs.data?.items ?? []) {
        out.push({ id: `pack:${pack.id}`, group: '套卷', label: pack.title || '未命名套卷', hint: `${pack.members.length} 个文件`, icon: <IconFileText size={18} />, run: go(href.pack(pack.id)) });
      }
    }
    return out;
  }, [can, deferred, navigate, onClose, onImport, packs.data, searchPacks]);

  useEffect(() => {
    setActive((index) => Math.min(index, Math.max(commands.length - 1, 0)));
  }, [commands.length]);

  useEffect(() => {
    void commands[active]?.preload?.();
  }, [active, commands]);

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setActive((index) => (commands.length ? (index + 1) % commands.length : 0));
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      setActive((index) => (commands.length ? (index - 1 + commands.length) % commands.length : 0));
    } else if (event.key === 'Enter') {
      event.preventDefault();
      commands[active]?.run();
    }
  };

  let lastGroup = '';
  return (
    <Modal opened={open} onClose={onClose} withCloseButton={false} size={560} padding={0} yOffset="12vh" centered={false} trapFocus>
      <TextInput
        ref={input}
        size="md"
        variant="unstyled"
        px="md"
        leftSection={<IconSearch size={18} />}
        rightSection={searchPacks && packs.isFetching ? <Loader size="xs" /> : null}
        placeholder="跳转页面、搜题或查找套卷…"
        value={query}
        onChange={(event) => setQuery(event.currentTarget.value)}
        onKeyDown={onKeyDown}
        aria-label="命令"
        aria-controls="command-palette-list"
        aria-activedescendant={commands[active] ? `command-${commands[active].id}` : undefined}
        styles={{ input: { paddingInline: 40 } }}
        style={{ borderBottom: '1px solid var(--mantine-color-gray-2)' }}
      />
      <Box id="command-palette-list" role="listbox" aria-label="结果" mah={360} style={{ overflowY: 'auto' }} p="xs">
        {commands.length === 0 ? (
          <Text c="dimmed" size="sm" ta="center" py="xl">没有匹配的结果</Text>
        ) : (
          commands.map((command, index) => {
            const heading = command.group !== lastGroup ? command.group : null;
            lastGroup = command.group;
            return (
              <div key={command.id}>
                {heading ? <Text size="xs" c="dimmed" fw={600} px="sm" pt="xs" pb={4}>{heading}</Text> : null}
                <UnstyledButton
                  id={`command-${command.id}`}
                  role="option"
                  aria-selected={index === active}
                  w="100%"
                  px="sm"
                  py={8}
                  bg={index === active ? 'blue.0' : undefined}
                  style={{ borderRadius: 'var(--mantine-radius-md)' }}
                  onMouseEnter={() => setActive(index)}
                  onClick={() => command.run()}
                >
                  <Group gap="sm" wrap="nowrap">
                    <Box c={index === active ? 'blue' : 'dimmed'} style={{ display: 'flex' }}>{command.icon}</Box>
                    <Text size="sm" flex={1} truncate>{command.label}</Text>
                    {command.hint ? <Text size="xs" c="dimmed" truncate>{command.hint}</Text> : null}
                  </Group>
                </UnstyledButton>
              </div>
            );
          })
        )}
      </Box>
      <Text size="xs" c="dimmed" px="md" py="xs" style={{ borderTop: '1px solid var(--mantine-color-gray-2)' }}>↑↓ 选择 · Enter 打开 · Esc 关闭</Text>
    </Modal>
  );
}

/** ⌘K / Ctrl+K 与 “/” 打开命令面板；在输入框里输入 “/” 不会触发。 */
export function useCommandShortcut(onOpen: () => void) {
  useEffect(() => {
    const handler = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const typing = !!target && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName));
      if ((event.key === 'k' || event.key === 'K') && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        onOpen();
      } else if (event.key === '/' && !typing && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault();
        onOpen();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onOpen]);
}
