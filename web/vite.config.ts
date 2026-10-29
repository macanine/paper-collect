import react from '@vitejs/plugin-react';
import UnoCSS from 'unocss/vite';
import { defineConfig } from 'vitest/config';
import { viteStaticCopy } from 'vite-plugin-static-copy';

// 开发模式下 /api 与 /mcp 代理到本地 Go 服务；生产构建输出 dist，由 Go 进程静态托管。
const API_TARGET = 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [react(), UnoCSS(), viteStaticCopy({ targets: [
    { src: 'node_modules/pdfjs-dist/cmaps', dest: 'pdfjs' },
    { src: 'node_modules/pdfjs-dist/standard_fonts', dest: 'pdfjs' },
    { src: 'node_modules/pdfjs-dist/wasm', dest: 'pdfjs' },
    { src: 'node_modules/pdfjs-dist/iccs', dest: 'pdfjs' },
  ] })],
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
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
  },
});
