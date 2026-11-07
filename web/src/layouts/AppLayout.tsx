import { Suspense, useEffect, useMemo, useState, type ReactNode } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router';
import { App as AntdApp, Avatar, Badge, Button, Drawer, Dropdown } from 'antd';
import type { MenuProps } from 'antd';
import {
  AppstoreOutlined,
  AuditOutlined,
  BarsOutlined,
  BookOutlined,
  CloudServerOutlined,
  ClusterOutlined,
  CloudUploadOutlined,
  DashboardOutlined,
  DeleteOutlined,
  DownOutlined,
  ExperimentOutlined,
  FolderOpenOutlined,
  HistoryOutlined,
  InboxOutlined,
  KeyOutlined,
  LogoutOutlined,
  MenuOutlined,
  RobotOutlined,
  SearchOutlined,
  TeamOutlined,
} from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { getLibraryStats } from '../features/materials/api';
import { UploadDialog } from '../features/uploads/UploadDialog';
import { UploadProvider, useUploads } from '../features/uploads/UploadManager';
import { UploadTray } from '../features/uploads/UploadTray';
import { useAuth } from '../features/auth/AuthContext';
import type { Scope } from '../shared/api/types';
import { USER_ROLE_LABELS } from '../shared/lib/labels';
import { NetworkActivity } from '../shared/ui/NetworkActivity';
import { FullPageLoading, LoadingState } from '../shared/ui/States';

interface NavigationItem {
  key: string;
  path: string;
  label: string;
  icon: ReactNode;
  scope?: Scope;
  badge?: 'review';
}

interface NavigationSection {
  key: string;
  label: string;
  items: NavigationItem[];
}

const NAVIGATION: NavigationSection[] = [
  {
    key: 'workspace',
    label: '资料',
    items: [
      { key: '/', path: '/', label: '工作台', icon: <DashboardOutlined /> },
      { key: '/inbox', path: '/inbox', label: '待整理', icon: <InboxOutlined />, scope: 'materials:write', badge: 'review' },
      { key: '/library', path: '/library', label: '资料库', icon: <FolderOpenOutlined />, scope: 'materials:read' },
      { key: '/search', path: '/search', label: '搜题', icon: <SearchOutlined />, scope: 'materials:read' },
      { key: '/collections', path: '/collections', label: '专题', icon: <AppstoreOutlined />, scope: 'collections:read' },
    ],
  },
  {
    key: 'records',
    label: '记录',
    items: [
      { key: '/uploads', path: '/uploads', label: '上传记录', icon: <HistoryOutlined />, scope: 'uploads:write' },
      { key: '/trash', path: '/trash', label: '回收站', icon: <DeleteOutlined />, scope: 'materials:write' },
      { key: '/jobs', path: '/jobs', label: '处理日志', icon: <BarsOutlined />, scope: 'jobs:read' },
    ],
  },
  {
    key: 'system',
    label: '系统',
    items: [
      { key: '/storage', path: '/storage', label: '存储', icon: <CloudServerOutlined />, scope: 'storage:read' },
      { key: '/admin/nodes', path: '/admin/nodes', label: '节点', icon: <ClusterOutlined />, scope: 'nodes:manage' },
      { key: '/admin/ai-models', path: '/admin/ai-models', label: 'AI 模型', icon: <RobotOutlined />, scope: 'ai:read' },
      { key: '/admin/ai-settings', path: '/admin/ai-settings', label: 'AI 解析', icon: <ExperimentOutlined />, scope: 'ai:read' },
      { key: '/admin/api-keys', path: '/admin/api-keys', label: 'API 密钥', icon: <KeyOutlined />, scope: 'keys:manage' },
      { key: '/admin/users', path: '/admin/users', label: '用户', icon: <TeamOutlined />, scope: 'users:manage' },
      { key: '/admin/audit', path: '/admin/audit', label: '审计日志', icon: <AuditOutlined />, scope: 'audit:read' },
    ],
  },
];

const DETAIL_LABELS: Record<string, string> = {
  packs: '套卷详情',
  materials: '文件详情',
  collections: '专题详情',
  jobs: '处理详情',
  uploads: '上传详情',
};

function routeKey(pathname: string): string {
  if (pathname === '/') return '/';
  if (pathname.startsWith('/admin/nodes')) return '/admin/nodes';
  if (pathname.startsWith('/admin/')) return pathname;
  const first = pathname.split('/')[1] ?? '';
  if (first === 'packs' || first === 'materials') return '/library';
  return `/${first}`;
}

function detailLabel(pathname: string, item: NavigationItem | undefined): string | null {
  if (!item || pathname === item.path) return null;
  if (pathname.startsWith('/admin/nodes/')) return '节点详情';
  if (pathname.startsWith('/admin/')) return null;
  return DETAIL_LABELS[pathname.split('/')[1] ?? ''] ?? null;
}

function useNavigation(can: (scope: Scope) => boolean) {
  return useMemo(
    () =>
      NAVIGATION.map((section) => ({
        ...section,
        items: section.items.filter((item) => !item.scope || can(item.scope)),
      })).filter((section) => section.items.length > 0),
    [can],
  );
}

interface SidebarContentProps {
  sections: NavigationSection[];
  activeKey: string;
  reviewCount: number;
  onNavigate?: () => void;
}

function SidebarContent({ sections, activeKey, reviewCount, onNavigate }: SidebarContentProps) {
  return (
    <div className="app-sidebar__inner">
      <Link to="/" className="app-brand" onClick={onNavigate} aria-label="返回仪表盘">
        <span className="app-brand__mark">
          <BookOutlined />
        </span>
        <span className="app-brand__copy">
          <span className="app-brand__title">中学资料库</span>
          <span className="app-brand__subtitle">Paper Collect</span>
        </span>
      </Link>
      <nav className="app-nav" aria-label="主导航">
        {sections.map((section) => (
          <section key={section.key} className="app-nav__section">
            <div className="app-nav__heading">{section.label}</div>
            <div className="app-nav__items">
              {section.items.map((item) => {
                const active = item.key === activeKey;
                return (
                  <Link
                    key={item.key}
                    to={item.path}
                    className={`app-nav__link${active ? ' is-active' : ''}`}
                    aria-current={active ? 'page' : undefined}
                    onClick={onNavigate}
                  >
                    <span className="app-nav__icon">{item.icon}</span>
                    <span className="app-nav__label">{item.label}</span>
                    {item.badge === 'review' && reviewCount > 0 ? <Badge count={reviewCount} size="small" overflowCount={999} /> : null}
                  </Link>
                );
              })}
            </div>
          </section>
        ))}
      </nav>
    </div>
  );
}

function HeaderUploadButton() {
  const { openDialog } = useUploads();
  return (
    <span className="app-header__upload">
      <Button type="primary" aria-label="上传资料" icon={<CloudUploadOutlined />} onClick={openDialog}>
        <span className="app-header__upload-label">上传资料</span>
      </Button>
    </span>
  );
}

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

function AppFrame() {
  const { user, can, signOut } = useAuth();
  const stats = useQuery({
    queryKey: ['dashboard', 'library-stats'],
    queryFn: ({ signal }) => getLibraryStats(signal),
    enabled: can('materials:read'),
    refetchInterval: 30_000,
  });
  const reviewCount = stats.data?.review ?? 0;
  const navigate = useNavigate();
  const location = useLocation();
  const { message } = AntdApp.useApp();
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const navigation = useNavigation(can);
  const activeKey = routeKey(location.pathname);
  const activeItem = useMemo(
    () => navigation.flatMap((section) => section.items).find((item) => item.key === activeKey),
    [activeKey, navigation],
  );
  const detail = detailLabel(location.pathname, activeItem);

  useEffect(() => {
    setMobileNavOpen(false);
  }, [location.pathname]);

  useEffect(() => {
    const current = detail ?? activeItem?.label ?? '管理台';
    document.title = `${current} · 中学资料库`;
  }, [activeItem?.label, detail]);

  const handleLogout = async () => {
    try {
      await signOut();
    } finally {
      message.success('已退出登录');
      navigate('/login', { replace: true });
    }
  };

  const accountItems: MenuProps['items'] = [
    {
      key: 'identity',
      disabled: true,
      label: user ? `${user.username} · ${USER_ROLE_LABELS[user.role]}` : '当前账户',
    },
    { type: 'divider' },
    { key: 'logout', icon: <LogoutOutlined />, label: '退出登录' },
  ];

  return (
    <div className="app-shell">
      <aside className="app-sidebar">
        <SidebarContent sections={navigation} activeKey={activeKey} reviewCount={reviewCount} />
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
        <SidebarContent
          sections={navigation}
          activeKey={activeKey}
          reviewCount={reviewCount}
          onNavigate={() => setMobileNavOpen(false)}
        />
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
              <span className="app-breadcrumb__root">资料库</span>
              <span className="app-breadcrumb__separator" aria-hidden="true">
                /
              </span>
              <span className="app-breadcrumb__current">{activeItem?.label ?? '管理台'}</span>
              {detail ? (
                <>
                  <span className="app-breadcrumb__separator app-breadcrumb__detail" aria-hidden="true">
                    /
                  </span>
                  <span className="app-breadcrumb__detail">{detail}</span>
                </>
              ) : null}
            </nav>
          </div>

          <div className="app-header__actions">
            <NetworkActivity />
            {can('materials:read') ? (
              <Link to="/search" className="app-header__search">
                <SearchOutlined />
                <span>搜题</span>
              </Link>
            ) : null}
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
                  <span className="app-account__copy">
                    <strong>{user.username}</strong>
                    <small>{USER_ROLE_LABELS[user.role]}</small>
                  </span>
                  <DownOutlined className="app-account__arrow" />
                </button>
              </Dropdown>
            ) : null}
          </div>
        </header>

        <main className="app-content">
          <div className="app-content__inner">
            <Suspense fallback={<LoadingState />}>
              <Outlet />
            </Suspense>
          </div>
        </main>
      </div>
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
