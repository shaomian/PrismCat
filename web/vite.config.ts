import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    rollupOptions: {
      output: {
        // 框架与 UI 基座单独成块:它们不随业务代码变更,拆出后每次发版用户只需重下业务 chunk。
        // mermaid / date-fns / react-markdown 已在各自的懒加载 chunk 里,这里不碰,
        // 避免拆碎反而增加请求数。
        manualChunks(id: string) {
          const match = /[\\/]node_modules[\\/]([^\\/]+)/.exec(id)
          if (!match) return undefined
          const name = match[1]
          if (['react', 'react-dom', 'scheduler', 'react-router', 'react-router-dom', 'i18next', 'react-i18next', 'i18next-browser-languagedetector'].includes(name)) {
            return 'vendor-react'
          }
          if (name === '@radix-ui' || ['radix-ui', 'lucide-react', 'clsx', 'tailwind-merge', 'class-variance-authority', 'sonner'].includes(name)) {
            return 'vendor-ui'
          }
          return undefined
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8711',
        changeOrigin: true,
      },
    },
  },
})
