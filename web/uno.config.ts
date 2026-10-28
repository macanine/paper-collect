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
  shortcuts: {
    'page-stack': 'flex flex-col gap-6 min-w-0',
    'toolbar-row': 'flex flex-wrap items-center justify-between gap-4',
    'filter-row': 'flex flex-wrap items-center gap-4',
    'mono-text': 'font-mono text-xs break-all',
    'cell-stack': 'flex flex-col gap-1 min-w-0',
    'table-subtext': 'text-xs text-gray-500',
  },
});
