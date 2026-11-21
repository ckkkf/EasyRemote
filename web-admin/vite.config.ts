import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 开发期把 /admin 代理到本地 admin 服务（cmd/admin，默认 :8081）
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/admin': { target: 'http://127.0.0.1:8081', changeOrigin: true },
    },
  },
});
