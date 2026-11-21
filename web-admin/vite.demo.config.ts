import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 本地演示配置：后台前端直连一体化 stack（默认 :8090，含网关 + /admin）。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5174,
    host: '127.0.0.1',
    proxy: {
      '/admin': { target: 'http://127.0.0.1:18090', changeOrigin: true },
    },
  },
});
