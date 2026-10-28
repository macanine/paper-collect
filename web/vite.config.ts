import react from '@vitejs/plugin-react';
import UnoCSS from 'unocss/vite';
import { defineConfig } from 'vitest/config';

// 开发模式下 /api 与 /mcp 代理到本地 Go 服务；生产构建输出 dist，由 Go 进程静态托管。
const API_TARGET = 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [react(), UnoCSS()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target: API_TARGET, changeOrigin: false },
      '/mcp': { target: API_TARGET, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id: string) {
          if (!id.includes('node_modules')) return undefined;
          if (id.includes('/antd/') || id.includes('/@ant-design/') || id.includes('/rc-') || id.includes('/@rc-component/')) return 'antd';
          if (id.includes('/react-dom/') || id.includes('/react/') || id.includes('/react-router/') || id.includes('/scheduler/')) return 'react';
          if (id.includes('/@tanstack/')) return 'query';
          return undefined;
        },
      },
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
  },
});
