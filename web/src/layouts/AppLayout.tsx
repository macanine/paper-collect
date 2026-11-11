import { memo, Suspense, useCallback, useEffect, useMemo, useState } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router';
import {
  ActionIcon, Anchor, AppShell, Avatar, Breadcrumbs, Burger, Button, Group, Indicator, Kbd, Menu, NavLink, ScrollArea, Stack, Text, Tooltip, UnstyledButton,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { IconBook2, IconCloudUpload, IconLogout, IconSearch } from '@tabler/icons-react';
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
import { message } from '../shared/ui/feedback';
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
  onNavigate?: () => void;
}

const Sidebar = memo(function Sidebar({ groups, activeKey, attention, onNavigate }: SidebarProps) {
  return (
    <Stack gap="md" h="100%" p="xs">
      <Link to="/" onClick={onNavigate} aria-label="返回工作台" style={{ textDecoration: 'none', color: 'inherit' }}>
        <Group wrap="nowrap" py="xs" justify="center">
          <Avatar color="blue" variant="filled" radius="md" size={40}><IconBook2 size={20} /></Avatar>
        </Group>
      </Link>
      <ScrollArea flex={1} type="never">
        <Stack gap="md" component="nav" aria-label="主导航">
          {groups.map((group) => (
            <Stack key={group.key} gap={4} align="center">
              {group.items.map((item) => {
                const active = item.path === activeKey;
                const showBadge = item.badge === 'attention' && attention > 0;
                const link = (
                  <NavLink
                    key={item.path}
                    component={Link}
                    to={item.path}
                    active={active}
                    aria-current={active ? 'page' : undefined}
                    aria-label={item.label}
                    onClick={onNavigate}
                    onMouseEnter={() => void item.preload?.()}
                    onFocus={() => void item.preload?.()}
                    leftSection={showBadge ? <Indicator color="red" size={8} offset={2} aria-label={`${attention} 个需关注文件`}>{item.icon}</Indicator> : item.icon}
                    styles={{ root: { width: 40, height: 40, padding: 0, borderRadius: 'var(--mantine-radius-md)', justifyContent: 'center' }, section: { margin: 0 }, body: { display: 'none' } }}
                    variant="light"
                  />
                );
                return <Tooltip key={item.path} label={item.label} position="right">{link}</Tooltip>;
              })}
            </Stack>
          ))}
        </Stack>
      </ScrollArea>
    </Stack>
  );
});

const HeaderUploadButton = memo(function HeaderUploadButton() {
  const { openDialog } = useUploads();
  return (
    <>
      <Button visibleFrom="sm" leftSection={<IconCloudUpload size={16} />} onClick={openDialog}>导入资料</Button>
      <ActionIcon hiddenFrom="sm" size={40} variant="filled" aria-label="导入资料" onClick={openDialog}><IconCloudUpload size={20} /></ActionIcon>
    </>
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
  const [mobileOpened, mobile] = useDisclosure(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const groups = useNavigation(can);
  const where = resolveLocation(location.pathname);
  const openPalette = useCallback(() => setPaletteOpen(true), []);
  const closePalette = useCallback(() => setPaletteOpen(false), []);
  useCommandShortcut(openPalette);

  useEffect(() => {
    mobile.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname]);

  useEffect(() => {
    document.title = `${where.detail ?? where.title} · 中学资料库`;
  }, [where.detail, where.title]);

  const handleLogout = async () => {
    try {
      await signOut();
    } finally {
      message.success('已退出登录');
      navigate('/login', { replace: true });
    }
  };

  return (
    <AppShell
      header={{ height: 64 }}
      navbar={{ width: 72, breakpoint: 'sm', collapsed: { mobile: !mobileOpened } }}
      padding={{ base: 'md', sm: 'xl' }}
      bg="gray.0"
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap" miw={0}>
            <Burger opened={mobileOpened} onClick={mobile.toggle} hiddenFrom="sm" size="sm" aria-label="打开导航" />
            <Breadcrumbs separator="/" aria-label="当前位置" style={{ flexWrap: 'nowrap', minWidth: 0 }}>
              {where.detail ? (
                <Anchor component={Link} to={where.navKey || '/'} size="sm" c="dimmed">{where.title}</Anchor>
              ) : null}
              <Text size="sm" fw={600} aria-current="page" truncate>{where.detail ?? where.title}</Text>
            </Breadcrumbs>
          </Group>
          <Group gap="sm" wrap="nowrap">
            {can('audit:read') ? <NetworkActivity /> : null}
            <UnstyledButton visibleFrom="md" onClick={openPalette} aria-label="打开命令面板" aria-keyshortcuts="Control+K Meta+K" style={{ border: '1px solid var(--mantine-color-gray-3)', borderRadius: 'var(--mantine-radius-md)', padding: '0 12px', height: 40, minWidth: 240 }}>
              <Group gap="xs" justify="space-between" h="100%">
                <Group gap="xs"><IconSearch size={16} color="var(--mantine-color-dimmed)" /><Text size="sm" c="dimmed">搜索或跳转</Text></Group>
                <Kbd size="sm">⌘K</Kbd>
              </Group>
            </UnstyledButton>
            <ActionIcon hiddenFrom="md" variant="default" size={40} onClick={openPalette} aria-label="打开命令面板"><IconSearch size={16} /></ActionIcon>
            {can('uploads:write') ? <HeaderUploadButton /> : null}
            {user ? (
              <Menu position="bottom-end" width={200}>
                <Menu.Target>
                  <UnstyledButton aria-label="打开账户菜单"><Avatar color="blue" size={40}>{user.username.slice(0, 1).toUpperCase()}</Avatar></UnstyledButton>
                </Menu.Target>
                <Menu.Dropdown>
                  <Menu.Label>{user.username} · {USER_ROLE_LABELS[user.role]}</Menu.Label>
                  <Menu.Divider />
                  <Menu.Item leftSection={<IconLogout size={14} />} onClick={() => void handleLogout()}>退出登录</Menu.Item>
                </Menu.Dropdown>
              </Menu>
            ) : null}
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar>
        <Sidebar groups={groups} activeKey={where.navKey} attention={attention} onNavigate={mobile.close} />
      </AppShell.Navbar>

      <AppShell.Main id="main-content">
        <Stack maw={1360} mx="auto" gap="lg" miw={0}>
          <Suspense fallback={<LoadingState />}>
            <Outlet />
          </Suspense>
        </Stack>
      </AppShell.Main>
      <PaletteHost open={paletteOpen} onClose={closePalette} />
    </AppShell>
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
