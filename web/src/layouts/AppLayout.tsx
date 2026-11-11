import { memo, Suspense, useCallback, useEffect, useMemo, useState } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router';
import { App as AntdApp, Avatar, Badge, Button, Drawer, Dropdown, Tooltip } from 'antd';
import type { MenuProps } from 'antd';
import {
  BookOutlined,
  CloudUploadOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuOutlined,
  MenuUnfoldOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { NAV_SECTION_LABELS, resolveLocation, ROUTES, SETTINGS, SETTINGS_NAV, type NavSection } from '../app/routes';
import { getLibraryStats } from '../features/materials/api';
import { UploadDialog } from '../features/uploads/UploadDialog';
import { UploadProvider, useUploads } from '../features/uploads/UploadManager';
import { UploadTray } from '../features/uploads/UploadTray';
import { useAuth } from '../features/auth/AuthContext';
import type { Scope } from '../shared/api/types';
import { USER_ROLE_LABELS } from '../shared/lib/labels';
import { NetworkActivity } from '../shared/ui/NetworkActivity';
import { FullPageLoading, LoadingState } from '../shared/ui/States';
import { CommandPalette, useCommandShortcut } from './CommandPalette';
import { qk } from '../app/queryKeys';

interface NavItem {
  path: string;
  label: string;
  icon: React.ReactNode;
  badge?: 'attention';
  preload?: () => Promise<unknown>;
}

interface NavGroup {
  key: NavSection;
  label: string;
  items: NavItem[];
}

const COLLAPSE_KEY = 'paper-collect:sidebar-collapsed';

function readCollapsed(): boolean {
  try {
    return window.localStorage.getItem(COLLAPSE_KEY) === '1';
  } catch {
    return false;
  }
}

function useNavigation(can: (scope: Scope) => boolean): NavGroup[] {
  return useMemo(() => {
    const groups = new Map<NavSection, NavItem[]>();
    for (const route of ROUTES) {
      if (!route.nav || (route.scope && !can(route.scope))) continue;
      const items = groups.get(route.nav.section) ?? [];
      items.push({ path: route.path, label: route.title, icon: route.nav.icon, badge: route.nav.badge, preload: route.page.preload });
      groups.set(route.nav.section, items);
    }
    const settings = SETTINGS.filter((s) => can(s.scope));
    if (settings.length > 0) {
      groups.set('system', [{ path: SETTINGS_NAV.path, label: SETTINGS_NAV.title, icon: SETTINGS_NAV.icon, preload: settings[0]!.page.preload }]);
    }
    return (Object.keys(NAV_SECTION_LABELS) as NavSection[])
      .filter((key) => groups.has(key))
      .map((key) => ({ key, label: NAV_SECTION_LABELS[key], items: groups.get(key)! }));
  }, [can]);
}

interface SidebarProps {
  groups: NavGroup[];
  activeKey: string;
  attention: number;
  collapsed?: boolean;
  onNavigate?: () => void;
}

const Sidebar = memo(function Sidebar({ groups, activeKey, attention, collapsed = false, onNavigate }: SidebarProps) {
  return (
    <div className="app-sidebar__inner">
      <Link to="/" className="app-brand" onClick={onNavigate} aria-label="返回工作台">
        <span className="app-brand__mark" aria-hidden="true">
          <BookOutlined />
        </span>
        {collapsed ? null : (
          <span className="app-brand__copy">
            <span className="app-brand__title">中学资料库</span>
            <span className="app-brand__subtitle">Paper Collect</span>
          </span>
        )}
      </Link>
      <nav className="app-nav" aria-label="主导航">
        {groups.map((group) => (
          <section key={group.key} className="app-nav__section">
            {collapsed ? null : <div className="app-nav__heading">{group.label}</div>}
            <div className="app-nav__items">
              {group.items.map((item) => {
                const active = item.path === activeKey;
                const showBadge = item.badge === 'attention' && attention > 0;
                const link = (
                  <Link
                    key={item.path}
                    to={item.path}
                    className={`app-nav__link${active ? ' is-active' : ''}`}
                    aria-current={active ? 'page' : undefined}
                    aria-label={collapsed ? item.label : undefined}
                    onClick={onNavigate}
                    onMouseEnter={() => void item.preload?.()}
                    onFocus={() => void item.preload?.()}
                  >
                    <span className="app-nav__icon" aria-hidden="true">
                      {collapsed && showBadge ? <Badge dot>{item.icon}</Badge> : item.icon}
                    </span>
                    {collapsed ? null : <span className="app-nav__label">{item.label}</span>}
                    {!collapsed && showBadge ? (
                      <span className="app-nav__badge" aria-label={`${attention} 个需关注文件`}>
                        <Badge count={attention} size="small" overflowCount={999} />
                      </span>
                    ) : null}
                  </Link>
                );
                return collapsed ? (
                  <Tooltip key={item.path} title={item.label} placement="right">
                    {link}
                  </Tooltip>
                ) : (
                  link
                );
              })}
            </div>
          </section>
        ))}
      </nav>
    </div>
  );
});

const HeaderUploadButton = memo(function HeaderUploadButton() {
  const { openDialog } = useUploads();
  return (
    <span className="app-header__upload">
      <Button aria-label="导入资料" icon={<CloudUploadOutlined />} onClick={openDialog}>
        <span className="app-header__upload-label">导入资料</span>
      </Button>
    </span>
  );
});

function AuthenticatedAppLayout() {
  const { can } = useAuth();
  return (
    <UploadProvider enabled={can('uploads:write')}>
      <AppFrame />
      {can('uploads:write') ? (
        <>
          <UploadTray />
          <UploadDialog />
        </>
      ) : null}
    </UploadProvider>
  );
}

function PaletteHost({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { can } = useAuth();
  if (can('uploads:write')) return <PaletteWithImport open={open} onClose={onClose} />;
  return <CommandPalette open={open} onClose={onClose} />;
}

function PaletteWithImport({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { openDialog } = useUploads();
  return <CommandPalette open={open} onClose={onClose} onImport={openDialog} />;
}

function AppFrame() {
  const { user, can, signOut } = useAuth();
  const stats = useQuery({
    queryKey: qk.dashboard.libraryStats,
    queryFn: ({ signal }) => getLibraryStats(signal),
    enabled: can('materials:read'),
    refetchInterval: 30_000,
  });
  const attention = stats.data?.needs_attention ?? 0;
  const navigate = useNavigate();
  const location = useLocation();
  const { message } = AntdApp.useApp();
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(readCollapsed);
  const groups = useNavigation(can);
  const where = resolveLocation(location.pathname);
  const openPalette = useCallback(() => setPaletteOpen(true), []);
  const closePalette = useCallback(() => setPaletteOpen(false), []);
  useCommandShortcut(openPalette);

  useEffect(() => {
    setMobileNavOpen(false);
  }, [location.pathname]);

  useEffect(() => {
    document.title = `${where.detail ?? where.title} · 中学资料库`;
  }, [where.detail, where.title]);

  const toggleCollapsed = () => {
    setCollapsed((value) => {
      try {
        window.localStorage.setItem(COLLAPSE_KEY, value ? '0' : '1');
      } catch {
        // 无痕模式等禁用存储时只影响本次会话。
      }
      return !value;
    });
  };

  const handleLogout = async () => {
    try {
      await signOut();
    } finally {
      message.success('已退出登录');
      navigate('/login', { replace: true });
    }
  };

  const accountItems = useMemo<MenuProps['items']>(
    () => [
      { key: 'identity', disabled: true, label: user ? `${user.username} · ${USER_ROLE_LABELS[user.role]}` : '当前账户' },
      { type: 'divider' },
      { key: 'logout', icon: <LogoutOutlined />, label: '退出登录' },
    ],
    [user],
  );

  return (
    <div className={`app-shell${collapsed ? ' is-collapsed' : ''}`}>
      <aside className="app-sidebar">
        <Sidebar groups={groups} activeKey={where.navKey} attention={attention} collapsed={collapsed} />
        <button
          type="button"
          className="app-sidebar__toggle"
          onClick={toggleCollapsed}
          aria-label={collapsed ? '展开侧栏' : '收起侧栏'}
          aria-expanded={!collapsed}
        >
          {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
        </button>
      </aside>

      <Drawer
        open={mobileNavOpen}
        onClose={() => setMobileNavOpen(false)}
        placement="left"
        size={312}
        title="主导航"
        closable={{ 'aria-label': '关闭导航' }}
        rootClassName="app-mobile-drawer"
        styles={{ body: { padding: 0 } }}
      >
        <Sidebar groups={groups} activeKey={where.navKey} attention={attention} onNavigate={() => setMobileNavOpen(false)} />
      </Drawer>

      <div className="app-main">
        <header className="app-header">
          <div className="app-header__context">
            <button
              type="button"
              className="app-icon-button app-mobile-trigger"
              aria-label="打开导航"
              aria-expanded={mobileNavOpen}
              onClick={() => setMobileNavOpen(true)}
            >
              <MenuOutlined />
            </button>
            <nav className="app-breadcrumb" aria-label="当前位置">
              {where.detail ? (
                <>
                  <Link to={where.navKey || '/'} className="app-breadcrumb__current">{where.title}</Link>
                  <span className="app-breadcrumb__separator" aria-hidden="true">/</span>
                  <span className="app-breadcrumb__detail" aria-current="page">{where.detail}</span>
                </>
              ) : (
                <span className="app-breadcrumb__current" aria-current="page">{where.title}</span>
              )}
            </nav>
          </div>

          <div className="app-header__actions">
            {can('audit:read') ? <NetworkActivity /> : null}
            <button type="button" className="app-header__search" onClick={openPalette} aria-label="打开命令面板" aria-keyshortcuts="Control+K Meta+K">
              <SearchOutlined />
              <span>搜索或跳转</span>
              <kbd className="app-header__kbd">⌘K</kbd>
            </button>
            {can('uploads:write') ? <HeaderUploadButton /> : null}
            {user ? (
              <Dropdown
                trigger={['click']}
                placement="bottomRight"
                menu={{
                  items: accountItems,
                  onClick: ({ key }) => {
                    if (key === 'logout') void handleLogout();
                  },
                }}
              >
                <button type="button" className="app-account" aria-label="打开账户菜单">
                  <Avatar size={32} className="app-account__avatar">
                    {user.username.slice(0, 1).toUpperCase()}
                  </Avatar>
                </button>
              </Dropdown>
            ) : null}
          </div>
        </header>

        <main className="app-content" id="main-content">
          <div className="app-content__inner">
            <Suspense fallback={<LoadingState />}>
              <Outlet />
            </Suspense>
          </div>
        </main>
      </div>
      <PaletteHost open={paletteOpen} onClose={closePalette} />
    </div>
  );
}

/** 登录守卫：会话校验完成后才渲染管理台，未登录时保留原始地址以便登录后跳回。 */
function AuthenticatedLayout() {
  const { status } = useAuth();
  const location = useLocation();
  if (status === 'loading') return <FullPageLoading />;
  if (status !== 'authenticated') {
    return <Navigate to="/login" replace state={{ from: `${location.pathname}${location.search}` }} />;
  }
  return <AuthenticatedAppLayout />;
}

export { AuthenticatedLayout };
