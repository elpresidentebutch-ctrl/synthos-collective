import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// GitHub project Pages: set VITE_BASE=/repo-name/ in CI. Custom domain at repo root: VITE_BASE=/
const envBase = process.env.VITE_BASE?.trim()
const base = !envBase || envBase === '/' ? '/' : envBase.endsWith('/') ? envBase : `${envBase}/`

export default defineConfig({
  base,
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, '')
      }
    }
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
  },
})
