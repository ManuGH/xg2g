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
        // Vite 8 / Rolldown requires manualChunks as a function, not an object.
        manualChunks(id) {
          if (id.includes('/node_modules/hls.js/')) return 'hls'
          if (id.includes('/node_modules/react/') || id.includes('/node_modules/react-dom/')) return 'vendor-react'
          if (id.includes('/node_modules/react-router-dom/')) return 'vendor-router'
          if (id.includes('/node_modules/@tanstack/react-query/')) return 'vendor-query'
          if (id.includes('/node_modules/i18next/') || id.includes('/node_modules/react-i18next/') || id.endsWith('/src/i18n.ts')) return 'vendor-i18n'
          if (id.endsWith('/src/client-ts/client.gen.ts') || id.endsWith('/src/client-ts/sdk.gen.ts') || id.endsWith('/src/client-ts/types.gen.ts')) return 'api-client'
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
    // Run all tests sequentially in a single worker fork to avoid OOM from
    // multiple workers each loading jsdom + React + all modules independently
    // on memory-constrained CI runners (4 GB heap limit). With a single fork
    // the global afterEach(cleanup) in setupTests.ts keeps per-file garbage
    // collection manageable.
    pool: 'forks',
    poolOptions: {
      forks: {
        singleFork: true,
      },
    },
  },
})
