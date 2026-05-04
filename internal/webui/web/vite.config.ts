import { defineConfig } from 'vite'
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
})
