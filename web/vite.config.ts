import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// When running inside the dev Docker cluster, VITE_HMR_HOST is set so the
// browser's HMR WebSocket connects through the Caddy proxy (homectl.local:80)
// instead of trying to reach the Vite container port directly.
const hmrHost = process.env.VITE_HMR_HOST
const hmrPort = process.env.VITE_HMR_PORT ? parseInt(process.env.VITE_HMR_PORT) : undefined
const apiTarget = process.env.VITE_API_URL || 'http://localhost:1338'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    ...(hmrHost && {
      hmr: {
        host: hmrHost,
        clientPort: hmrPort ?? 80,
        protocol: 'ws',
      },
    }),
    proxy: {
      '/api': {
        target: apiTarget,
        ws: true,         // proxy WebSocket connections (state stream, logs)
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
  },
})
