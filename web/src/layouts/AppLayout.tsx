import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router';
import { App as AntdApp, Avatar, Button, Drawer, Dropdown } from 'antd';
import type { MenuProps } from 'antd';
import {
  AppstoreOutlined,
  AuditOutlined,
  BarsOutlined,
  BookOutlined,
  CloudServerOutlined,
  CloudUploadOutlined,
  DashboardOutlined,
  DownOutlined,
  ExperimentOutlined,
  FileTextOutlined,
  KeyOutlined,
  LogoutOutlined,
  MenuOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  SearchOutlined,
  TeamOutlined,
} from '@ant-design/icons';
import { useAuth } from '../features/auth/AuthContext';
import type { Scope } from '../shared/api/types';
import { USER_ROLE_LABELS } from '../shared/lib/labels';
import { FullPageLoading } from '../shared/ui/States';

interface NavigationItem {
  key: string;
  path: string;
  label: string;
  icon: ReactNode;
  scope?: Scope;
}

interface NavigationSection {
  key: string;
  label: string;
  index: string;
  items: NavigationItem[];
}

const NAVIGATION: NavigationSection[] = [
  {
    key: 'workspace',
    label: '资料工作台',
    index: '01',
    items: [
      { key: '/', path: '/', label: '仪表盘', icon: <DashboardOutlined /> },
      { key: '/search', path: '/search', label: '搜题', icon: <SearchOutlined />, scope: 'materials:read' },
      { key: '/materials', path: '/materials', label: '资料', icon: <FileTextOutlined />, scope: 'materials:read' },
      { key: '/collections', path: '/collections', label: '合集', icon: <AppstoreOutlined />, scope: 'collections:read' },
      { key: '/uploads/new', path: '/uploads/new', label: '上传资料', icon: <CloudUploadOutlined />, scope: 'uploads:write' },
      { key: '/jobs', path: '/jobs', label: '任务', icon: <BarsOutlined />, scope: 'jobs:read' },
    ],
  },
  {
    key: 'infrastructure',
    label: '存储与运行',
    index: '02',
    items: [{ key: '/storage', path: '/storage', label: '存储端点', icon: <CloudServerOutlined />, scope: 'storage:read' }],
  },
  {
    key: 'system',
    label: '系统管理',
    index: '03',
    items: [
      { key: '/admin/ai-models', path: '/admin/ai-models', label: 'AI 模型', icon: <RobotOutlined />, scope: 'ai:read' },
      { key: '/admin/ai-settings', path: '/admin/ai-settings', label: 'AI 解析', icon: <ExperimentOutlined />, scope: 'ai:read' },
      { key: '/admin/api-keys', path: '/admin/api-keys', label: 'API 密钥', icon: <KeyOutlined />, scope: 'keys:manage' },
      { key: '/admin/users', path: '/admin/users', label: '用户', icon: <TeamOutlined />, scope: 'users:manage' },
      { key: '/admin/audit', path: '/admin/audit', label: '审计日志', icon: <AuditOutlined />, scope: 'audit:read' },
    ],
  },
];

const DETAIL_LABELS: Record<string, string> = {
  materials: '资料详情',
  collections: '合集详情',
  jobs: '任务详情',
  uploads: '上传进度',
};

function routeKey(pathname: string): string {
  if (pathname === '/') return '/';
  if (pathname.startsWith('/admin/')) return pathname;
  if (pathname.startsWith('/uploads/')) return '/uploads/new';
  return `/${pathname.split('/')[1] ?? ''}`;
}

function detailLabel(pathname: string, item: NavigationItem | undefined): string | null {
  if (!item || pathname === item.path) return null;
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
  onNavigate?: () => void;
}

function SidebarContent({ sections, activeKey, onNavigate }: SidebarContentProps) {
  return (
    <div className="app-sidebar__inner">
      <Link to="/" className="app-brand" onClick={onNavigate} aria-label="返回仪表盘">
        <span className="app-brand__mark">
          <BookOutlined />
        </span>
        <span className="app-brand__copy">
          <span className="app-brand__title">高中资料归档</span>
          <span className="app-brand__subtitle">Paper Collect</span>
        </span>
      </Link>
      <nav className="app-nav" aria-label="主导航">
        {sections.map((section) => (
          <section key={section.key} className="app-nav__section">
            <div className="app-nav__heading">
              <span>{section.label}</span>
              <span>{section.index}</span>
            </div>
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
                    <span>{item.label}</span>
                    <span className="app-nav__marker" aria-hidden="true" />
                  </Link>
                );
              })}
            </div>
          </section>
        ))}
      </nav>
      <div className="app-sidebar__footer">
        <SafetyCertificateOutlined className="app-sidebar__footer-icon" />
        <span>资料按 SHA-256 去重归档</span>
      </div>
    </div>
  );
}

function AuthenticatedAppLayout() {
  const { user, can, signOut } = useAuth();
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
        <SidebarContent sections={navigation} activeKey={activeKey} />
      </aside>

      <Drawer
        open={mobileNavOpen}
        onClose={() => setMobileNavOpen(false)}
        placement="left"
        size={312}
        closable={false}
        rootClassName="app-mobile-drawer"
        styles={{ body: { padding: 0 } }}
      >
        <SidebarContent
          sections={navigation}
          activeKey={activeKey}
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
            {can('materials:read') ? (
              <Link to="/search" className="app-header__search">
                <SearchOutlined />
                <span>搜题</span>
              </Link>
            ) : null}
            {can('uploads:write') ? (
              <Link to="/uploads/new" className="app-header__upload">
                <Button type="primary" icon={<CloudUploadOutlined />}>
                  <span className="app-header__upload-label">上传资料</span>
                </Button>
              </Link>
            ) : null}
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
            <Outlet />
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
