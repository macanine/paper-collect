import { Suspense } from 'react';
import { Link, Navigate, useNavigate, useParams } from 'react-router';
import { Box, Flex, NavLink, Paper, Select, Stack } from '@mantine/core';
import { href, SETTINGS } from '../../app/routes';
import { useAuth } from '../auth/AuthContext';
import { ForbiddenState, LoadingState } from '../../shared/ui/States';

/** 设置中心：把模型、解析、词典、密钥、用户和审计收进一个入口，按角色显示分区。 */
export function SettingsPage() {
  const { section } = useParams();
  const { can } = useAuth();
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
    <Flex gap="lg" direction={{ base: 'column', md: 'row' }} align="flex-start">
      <Box w={{ base: '100%', md: 240 }} style={{ flexShrink: 0 }}>
        <Select hiddenFrom="md" value={current.key} data={sections.map((s) => ({ value: s.key, label: s.title }))} onChange={(key) => key && navigate(href.settings(key))} aria-label="选择设置分区" />
        <Paper visibleFrom="md" p="xs" component="nav" aria-label="设置分区">
          <Stack gap={2}>
            {sections.map((s) => (
              <NavLink
                key={s.key}
                component={Link}
                to={href.settings(s.key)}
                active={s.key === current.key}
                label={s.title}
                description={s.description}
                leftSection={s.icon}
                variant="light"
                onMouseEnter={() => void s.page.preload()}
                style={{ borderRadius: 'var(--mantine-radius-md)' }}
              />
            ))}
          </Stack>
        </Paper>
      </Box>
      <Stack component="section" flex={1} miw={0} w="100%" gap="lg">
        <Suspense fallback={<LoadingState />}>
          <Page />
        </Suspense>
      </Stack>
    </Flex>
  );
}
