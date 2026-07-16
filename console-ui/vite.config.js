import { defineConfig } from 'vite'
import process from 'node:process'
import react from '@vitejs/plugin-react'
import { ccPromptPlugin } from 'cc-prompter'
import { codeInspectorPlugin } from 'code-inspector-plugin'

const apiTarget = process.env.DEVBOX_API_TARGET || 'http://localhost:9090'

export default defineConfig({
  plugins: [
    codeInspectorPlugin({ bundler: 'vite' }),
    ccPromptPlugin({ inspector: false }),
    react(),
  ],
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
})
