import { defineConfig, presetWind4 } from 'unocss';

// 使用 UnoCSS 推荐的 Tailwind v4 兼容 preset。关闭 reset，避免与 Ant Design 的基础样式冲突。
export default defineConfig({
  presets: [
    presetWind4({
      preflights: {
        reset: false,
      },
    }),
  ],
  // Keep utility class names readable while mapping their legacy palette to
  // the same Ant Design tokens used by components and page CSS.
  theme: {
    colors: {
      slate: {
        100: 'var(--ant-border-secondary)',
        200: 'var(--ant-border-secondary)',
        400: 'var(--ant-text-tertiary)',
        500: 'var(--ant-text-secondary)',
        600: 'var(--ant-text-secondary)',
      },
      gray: {
        50: 'var(--ant-container-muted)',
        500: 'var(--ant-text-secondary)',
      },
      red: {
        600: 'var(--ant-error)',
      },
      orange: {
        600: 'var(--ant-warning)',
      },
    },
  },
  shortcuts: {
    'page-stack': 'flex flex-col gap-6 min-w-0',
    'toolbar-row': 'flex flex-wrap items-center justify-between gap-4',
    'filter-row': 'flex flex-wrap items-center gap-4',
    'mono-text': 'font-mono text-xs break-all',
    'cell-stack': 'flex flex-col gap-1 min-w-0',
    'table-subtext': 'text-xs text-gray-500',
  },
});
