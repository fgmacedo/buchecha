import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Vite config for the bcc dashboard SPA. The bundle lands at
// internal/webui/web/dist/ and is consumed by the Go embed in
// internal/webui/embed.go (//go:embed web/dist/*).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  test: {
    // happy-dom provides browser-like globals (fetch, EventSource, etc.)
    // without the ESM-in-CJS compatibility issues in jsdom 27, which pulls
    // @asamuzakjp/css-color -> @csstools/css-calc (ESM-only) through CJS
    // require() and causes a hard ERR_REQUIRE_ESM in the pool worker.
    environment: 'happy-dom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
})
