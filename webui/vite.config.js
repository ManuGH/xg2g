// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: '/ui/',
  server: {
    port: 5173,
    host: '0.0.0.0', // Listen on all network interfaces
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      '/auth': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/stream': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/logos': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          // Separate HLS.js into its own chunk (loaded only with V3Player)
          'hls': ['hls.js'],
          // React and core libraries
          'vendor-react': ['react', 'react-dom', 'react-dom/client'],
          // Generated API client
          'api-client': [
            './src/client-ts/client.gen.ts',
            './src/client-ts/sdk.gen.ts',
            './src/client-ts/types.gen.ts'
          ]
        }
      }
    }
  },
})
