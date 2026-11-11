import { useDeferredValue, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { Empty, Input, Modal, Spin, Typography } from 'antd';
import type { InputRef } from 'antd';
import { CloudUploadOutlined, FileTextOutlined, SearchOutlined } from '@ant-design/icons';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { href, ROUTES, SETTINGS, type LazyPage } from '../app/routes';
import { useAuth } from '../features/auth/AuthContext';
import { listPacks } from '../features/packs/api';

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
  const input = useRef<InputRef>(null);
  const deferred = useDeferredValue(query.trim());
  const searchPacks = can('materials:read') && deferred.length >= 2;

  const packs = useQuery({
    queryKey: ['command-palette', 'packs', deferred],
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
      out.push({ id: 'import', group: '操作', label: '导入资料', icon: <CloudUploadOutlined />, run: () => { onClose(); onImport(); } });
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
      out.push({ id: 'search', group: '操作', label: `搜题：${q}`, icon: <SearchOutlined />, run: go(href.search(q)) });
    }
    if (searchPacks) {
      for (const pack of packs.data?.items ?? []) {
        out.push({ id: `pack:${pack.id}`, group: '套卷', label: pack.title || '未命名套卷', hint: `${pack.members.length} 个文件`, icon: <FileTextOutlined />, run: go(href.pack(pack.id)) });
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
    <Modal open={open} onCancel={onClose} footer={null} closable={false} width={560} destroyOnHidden className="command-palette" title={null}>
      <Input
        ref={input}
        size="large"
        variant="borderless"
        prefix={<SearchOutlined />}
        placeholder="跳转页面、搜题或查找套卷…"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={onKeyDown}
        aria-label="命令"
        aria-controls="command-palette-list"
        aria-activedescendant={commands[active] ? `command-${commands[active].id}` : undefined}
        suffix={searchPacks && packs.isFetching ? <Spin size="small" /> : null}
      />
      <div className="command-palette__list" id="command-palette-list" role="listbox" aria-label="结果">
        {commands.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的结果" />
        ) : (
          commands.map((command, index) => {
            const heading = command.group !== lastGroup ? command.group : null;
            lastGroup = command.group;
            return (
              <div key={command.id}>
                {heading ? <div className="command-palette__group">{heading}</div> : null}
                <div
                  id={`command-${command.id}`}
                  role="option"
                  aria-selected={index === active}
                  className={`command-palette__item${index === active ? ' is-active' : ''}`}
                  onMouseEnter={() => setActive(index)}
                  onClick={() => command.run()}
                >
                  <span className="command-palette__icon" aria-hidden="true">{command.icon}</span>
                  <span className="command-palette__label">{command.label}</span>
                  {command.hint ? <Typography.Text type="secondary" className="command-palette__hint">{command.hint}</Typography.Text> : null}
                </div>
              </div>
            );
          })
        )}
      </div>
      <div className="command-palette__footer">↑↓ 选择 · Enter 打开 · Esc 关闭</div>
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
