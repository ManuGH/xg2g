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
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'hls',
              test: /[\\/]node_modules[\\/]hls\.js[\\/]/,
              priority: 20,
            },
            {
              name: 'vendor-react',
              test: /[\\/]node_modules[\\/](react|react-dom)[\\/]/,
              priority: 20,
            },
            {
              name: 'vendor-router',
              test: /[\\/]node_modules[\\/]react-router-dom[\\/]/,
              priority: 20,
            },
            {
              name: 'vendor-query',
              test: /[\\/]node_modules[\\/]@tanstack[\\/]react-query[\\/]/,
              priority: 20,
            },
            {
              name: 'vendor-i18n',
              test: /[\\/]node_modules[\\/](i18next|react-i18next)[\\/]|src[\\/]i18n\.ts$/,
              priority: 20,
            },
            {
              name: 'api-client',
              test: /src[\\/]client-ts[\\/](client|sdk|types)\.gen\.ts$/,
              priority: 20,
            },
          ],
        },
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
