import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The bundle is emitted straight into the Go package that embeds it, so a
// single `make build` produces one self-contained binary.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    // Relative asset URLs keep the bundle working no matter which path the
    // binary is mounted under.
    assetsDir: 'assets',
  },
  base: './',
  server: {
    port: 5173,
    // During development the SPA talks to a locally running Go server.
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/help': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
    },
  },
})
