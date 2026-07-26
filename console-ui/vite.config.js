import { defineConfig } from 'vite'
import process from 'node:process'
import react from '@vitejs/plugin-react'
import { ccPromptPlugin } from 'cc-prompter'
import { codeInspectorPlugin } from 'code-inspector-plugin'

const apiTarget = process.env.DEVBOX_API_TARGET || 'http://localhost:9090'

export default defineConfig(({ mode }) => ({
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.js',
    css: false,
    restoreMocks: true,
    exclude: ['e2e/**', 'node_modules/**'],
  },
  plugins: [
    mode !== 'test' && codeInspectorPlugin({ bundler: 'vite' }),
    mode !== 'test' && ccPromptPlugin({ inspector: false }),
    react(),
  ].filter(Boolean),
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    
  },
  server: {
    proxy: {
      '/api': {
        target: apiTarget,
        ws: true,
      },
      '/auth': apiTarget,
      '/terminal.html': apiTarget,
      '/app-icons': apiTarget,
    },
  },
}))
