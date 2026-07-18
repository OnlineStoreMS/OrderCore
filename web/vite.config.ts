import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 启动前设置：VITE_API_GATEWAY=http://localhost:8088 npm run dev
const gateway = process.env.VITE_API_GATEWAY
const apiTarget = gateway || 'http://localhost:8098'
const useGateway = !!gateway

const proxy: Record<string, object> = {
  '/api': { target: apiTarget, changeOrigin: true },
  '/uploads': { target: apiTarget, changeOrigin: true },
}

if (!useGateway) {
  proxy['/iam'] = {
    target: 'http://localhost:8091',
    changeOrigin: true,
    rewrite: (path: string) => path.replace(/^\/iam/, '/api/v1'),
  }
}

export default defineConfig({
  plugins: [vue()],
  server: { port: 5182, proxy },
})
