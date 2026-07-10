import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { ccPromptPlugin } from 'cc-prompter'
import { codeInspectorPlugin } from 'code-inspector-plugin'

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
        target: 'http://localhost:9091',
        ws: true,
      },
      '/auth': 'http://localhost:9091',
      '/terminal.html': 'http://localhost:9091',
      '/app-icons': 'http://localhost:9091',
    },
  },
})
