import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 本地演示配置：后台前端直连一体化 stack（默认 :18090，含网关 + /admin）。
export default defineConfig({
  plugins: [react()],
  server: {
    port: Number(process.env.ADMIN_PORT ?? 5199),
    host: '127.0.0.1',
    proxy: {
      // 端口与 scripts/dev-demo.sh 的 STACK_PORT 保持一致（默认 18090），避免代理打到不存在的实例。
      '/admin': {
        target: `http://127.0.0.1:${process.env.STACK_PORT ?? '18090'}`,
        changeOrigin: true,
      },
    },
  },
});
