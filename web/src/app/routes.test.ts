import { describe, expect, it } from 'vitest';
import { href, resolveLocation, ROUTES, SETTINGS } from './routes';

describe('route manifest', () => {
  it('gives every detail page an existing navigation parent', () => {
    const paths = new Set(ROUTES.map((r) => r.path));
    for (const route of ROUTES.filter((r) => r.parent)) {
      expect(paths.has(route.parent!), route.path).toBe(true);
      expect(ROUTES.find((r) => r.path === route.parent)?.nav, route.path).toBeDefined();
    }
    expect(new Set(SETTINGS.map((s) => s.key)).size).toBe(SETTINGS.length);
  });

  it('resolves detail pages to their section for the sidebar and breadcrumb', () => {
    expect(resolveLocation(href.pack('p 1'))).toMatchObject({ navKey: '/library', title: '资料库', detail: '套卷详情' });
    expect(resolveLocation(href.node('n1'))).toMatchObject({ navKey: '/nodes', detail: '节点详情' });
    expect(resolveLocation(href.settings('users'))).toMatchObject({ navKey: '/settings', detail: '用户' });
    expect(resolveLocation('/inbox')).toMatchObject({ navKey: '/inbox', title: '需关注' });
    expect(resolveLocation('/inbox').detail).toBeUndefined();
    expect(resolveLocation('/nope').navKey).toBe('');
  });

  it('encodes ids and queries in links', () => {
    expect(href.material('a/b')).toBe('/materials/a%2Fb');
    expect(href.jobs({ node_id: 'x y' })).toBe('/jobs?node_id=x+y');
    expect(href.search('二次函数')).toBe(`/search?q=${encodeURIComponent('二次函数')}`);
  });
});
