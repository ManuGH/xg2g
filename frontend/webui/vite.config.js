// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { configDefaults } from 'vitest/config'

const proxyTarget = process.env.XG2G_WEBUI_PROXY_TARGET || 'http://localhost:8080'
const devPort = Number(process.env.XG2G_WEBUI_DEV_PORT || '5173')
const uiBase = process.env.XG2G_WEBUI_BASE || '/ui/'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: uiBase,
  server: {
    port: devPort,
    host: '0.0.0.0', // Listen on all network interfaces
    proxy: {
      '/api': {
        target: proxyTarget,
        changeOrigin: true,
        secure: false,
      },
      '/auth': {
        target: proxyTarget,
        changeOrigin: true,
      },
      '/stream': {
        target: proxyTarget,
        changeOrigin: true,
      },
      '/logos': {
        target: proxyTarget,
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
          // Separate the lighter HLS.js runtime into its own chunk (loaded only with V3Player)
          'hls': ['hls.js/light'],
          // React and core libraries
          'vendor-react': ['react', 'react-dom', 'react-dom/client'],
          // Keep routing stable across app-level changes.
          'vendor-router': ['react-router-dom'],
          // TanStack Query is part of app bootstrap but changes far less often than app code.
          'vendor-query': ['@tanstack/react-query'],
          // i18n runtime stays stable while locale JSON loads on demand per language.
          'vendor-i18n': [
            'i18next',
            'react-i18next',
            './src/i18n.ts'
          ],
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
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './tests/setupTests.ts',
    css: true,
    exclude: [...configDefaults.exclude, '**/._*'],
  },
})
