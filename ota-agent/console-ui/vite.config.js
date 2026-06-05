import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:9099',
        ws: true,
      },
      '/terminal.html': 'http://localhost:9099',
      '/app-icons': 'http://localhost:9099',
    },
  },
})
