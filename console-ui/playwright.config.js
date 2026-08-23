import { existsSync } from 'node:fs'
import { defineConfig } from '@playwright/test'

const cachedChromium = '/home/kasm-user/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome'
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
  || (existsSync(cachedChromium) ? cachedChromium : undefined)

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure',
    launchOptions: executablePath ? { executablePath } : undefined,
  },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !process.env.CI,
  },
})
