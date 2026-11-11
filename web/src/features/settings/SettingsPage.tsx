import { Suspense } from 'react';
import { Link, Navigate, useNavigate, useParams } from 'react-router';
import { Menu, Select } from 'antd';
import { href, SETTINGS } from '../../app/routes';
import { useAuth } from '../auth/AuthContext';
import { useCompactViewport } from '../../shared/lib/useCompactViewport';
import { ForbiddenState, LoadingState } from '../../shared/ui/States';

/** 设置中心：把模型、解析、词典、密钥、用户和审计收进一个入口，按角色显示分区。 */
export function SettingsPage() {
  const { section } = useParams();
  const { can } = useAuth();
  const compact = useCompactViewport();
  const navigate = useNavigate();
  const sections = SETTINGS.filter((s) => can(s.scope));
  if (sections.length === 0) return <ForbiddenState />;
  const current = sections.find((s) => s.key === section);
  if (!current) {
    if (section && SETTINGS.some((s) => s.key === section)) return <ForbiddenState />;
    return <Navigate to={href.settings(sections[0]!.key)} replace />;
  }
  const Page = current.page;
  return (
    <div className="settings-layout">
      <nav className="settings-layout__nav" aria-label="设置分区">
        {compact ? (
          <Select
            className="w-full"
            value={current.key}
            options={sections.map((s) => ({ value: s.key, label: s.title }))}
            onChange={(key) => navigate(href.settings(key))}
            aria-label="选择设置分区"
          />
        ) : (
          <Menu
            mode="inline"
            selectedKeys={[current.key]}
            items={sections.map((s) => ({
              key: s.key,
              icon: s.icon,
              label: (
                <Link to={href.settings(s.key)} onMouseEnter={() => void s.page.preload()}>
                  {s.title}
                </Link>
              ),
              title: s.description,
            }))}
          />
        )}
      </nav>
      <section className="settings-layout__body">
        <Suspense fallback={<LoadingState />}>
          <Page />
        </Suspense>
      </section>
    </div>
  );
}
