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
    // The bundle is served only from the embedded FS in a local Go binary,
    // so we drop rollup's hash suffix. Stable filenames keep the embed
    // diff small across builds. manualChunks splits vendor code by
    // semantic group so chunk names do not collide and rollup never
    // resorts to numeric suffixes (`index2.js`, `index3.js`).
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name].js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
        manualChunks(id) {
          // Local lazy-loaded view bundles. Naming them explicitly
          // keeps rollup from falling back to numeric chunk names
          // (`index2.js`, `index3.js`) for app-side dynamic imports.
          if (/[\\/]src[\\/]components[\\/]dag-view[\\/]/.test(id)) {
            return 'dag-view'
          }
          if (/[\\/]src[\\/]components[\\/]activity-view[\\/]/.test(id)) {
            return 'activity-view'
          }
          if (!id.includes('node_modules')) return
          if (id.includes('@xyflow') || id.includes('classcat')) {
            return 'vendor-xyflow'
          }
          if (
            /[\\/]d3-(zoom|drag|selection|transition|color|interpolate|ease|timer|dispatch)[\\/]/.test(
              id,
            )
          ) {
            return 'vendor-xyflow'
          }
          if (
            /[\\/]d3-(scale|shape|axis|array|time|format|path|time-format|scale-chromatic)[\\/]/.test(
              id,
            )
          ) {
            return 'vendor-d3'
          }
          if (
            id.includes('react-markdown') ||
            id.includes('remark-') ||
            id.includes('rehype-') ||
            id.includes('micromark') ||
            id.includes('mdast') ||
            id.includes('hast') ||
            id.includes('unist') ||
            id.includes('vfile') ||
            id.includes('decode-named-character-reference') ||
            id.includes('character-entities') ||
            id.includes('property-information')
          ) {
            return 'vendor-markdown'
          }
          if (id.includes('shiki') || id.includes('oniguruma-to-js')) {
            return 'vendor-shiki'
          }
          if (
            id.includes('react-dom') ||
            id.includes('scheduler') ||
            /[\\/]react[\\/]/.test(id)
          ) {
            return 'vendor-react'
          }
        },
      },
    },
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
