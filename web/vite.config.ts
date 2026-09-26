import { writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

const outDir = fileURLToPath(new URL('../internal/webui/dist', import.meta.url))

// emptyOutDir removes the committed internal/webui/dist/.gitkeep; put it back.
const keepGitkeep = {
  name: 'keep-gitkeep',
  closeBundle() {
    writeFileSync(`${outDir}/.gitkeep`, '')
  },
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), keepGitkeep],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir,
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:8420', changeOrigin: false },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
})
