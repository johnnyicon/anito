import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig(({ mode }) => ({
  plugins: [
    react(),
    mode === 'test' ? null : tailwindcss(),
  ].filter(Boolean),
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/health':   'http://localhost:7700',
      '/services': 'http://localhost:7700',
      '/deploy':   'http://localhost:7700',
      '/stop':     { target: 'http://localhost:7700', changeOrigin: true },
      '/restart':  { target: 'http://localhost:7700', changeOrigin: true },
      '/remove':   { target: 'http://localhost:7700', changeOrigin: true },
      '/status':   { target: 'http://localhost:7700', changeOrigin: true },
      '/logs':     { target: 'http://localhost:7700', changeOrigin: true, ws: true },
      '/issues':   { target: 'http://localhost:7700', changeOrigin: true },
      '/doctor':   { target: 'http://localhost:7700', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    css: false,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
  },
}))
