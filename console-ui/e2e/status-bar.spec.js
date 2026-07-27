import { expect, test } from '@playwright/test'

const VIEWPORTS = [
  { width: 320, height: 568 },
  { width: 360, height: 800 },
  { width: 390, height: 844 },
  { width: 768, height: 1024 },
  { width: 1024, height: 768 },
  { width: 1440, height: 900 },
]

const API_RESPONSES = {
  '/api/v1/device': {
    deviceName: 'edge-link-01',
    hostname: 'edge-link-01',
    platform: 'linux',
    cpuModel: 'Test CPU',
    uptimeHuman: '1 小时',
    agentVersion: 'test',
  },
  '/api/v1/metrics': {
    cpuUsedPercent: 32,
    memoryUsed: 4 * 1024 ** 3,
    memoryTotal: 16 * 1024 ** 3,
    memoryUsedPercent: 25,
    gpuData: [],
    diskData: [],
  },
  '/api/v1/metrics/history': {},
  '/api/v1/apps': [],
  '/api/v1/alerts': [],
}

async function openConsole(page, viewport) {
  await page.setViewportSize(viewport)
  await page.addInitScript(() => {
    localStorage.setItem('edge_token', 'status-bar-test-token')
    localStorage.setItem('edgex-user-prefs', JSON.stringify({ topbar: 'dark', online: true }))
  })
  await page.route('**/api/v1/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(API_RESPONSES[pathname] ?? []),
    })
  })
  await page.goto('/')
  await expect(page.locator('.edge-status-bar')).toBeVisible()
}

for (const viewport of VIEWPORTS) {
  test(`${viewport.width}px Cloud Link 可见且状态栏无横向溢出`, async ({ page }) => {
    const errors = []
    page.on('console', (message) => {
      if (message.type() === 'error') errors.push(message.text())
    })
    page.on('pageerror', (error) => errors.push(error.message))

    await openConsole(page, viewport)

    const link = page.locator('.edge-cloud-link')
    const led = link.locator('.edge-cloud-link-led')
    const label = link.locator('.edge-cloud-link-label')
    await expect(link).toHaveAttribute('aria-label', '云端连接状态：云端在线')
    await expect(link).toHaveAttribute('title', '云端连接状态：云端在线')
    await expect(link).toHaveAttribute('data-cloud-link-state', 'online')
    await expect(led).toBeVisible()
    await expect(led).toHaveCSS('background-color', 'rgb(34, 197, 94)')
    await expect(led).toHaveCSS('width', '8px')
    await expect(led).toHaveCSS('height', '8px')
    await expect(link).toHaveCSS('min-width', '44px')
    await expect(link).toHaveCSS('min-height', '44px')

    if (viewport.width < 1024) {
      await expect(label).toBeHidden()
    } else {
      await expect(label).toBeVisible()
      await expect(label).toHaveText('云端在线')
    }

    const overflow = await page.evaluate(() => {
      const statusBar = document.querySelector('.edge-status-bar')
      return {
        documentClientWidth: document.documentElement.clientWidth,
        documentScrollWidth: document.documentElement.scrollWidth,
        statusClientWidth: statusBar.clientWidth,
        statusScrollWidth: statusBar.scrollWidth,
      }
    })
    expect(overflow.documentScrollWidth).toBe(overflow.documentClientWidth)
    expect(overflow.statusScrollWidth).toBe(overflow.statusClientWidth)
    expect(errors).toEqual([])
  })
}

test('Cloud Link 四态颜色与连接中动画语义', async ({ page }) => {
  await openConsole(page, { width: 1440, height: 900 })
  const link = page.locator('.edge-cloud-link')
  const led = link.locator('.edge-cloud-link-led')
  const states = [
    ['online', 'rgb(34, 197, 94)', false],
    ['connecting', 'rgb(245, 158, 11)', true],
    ['offline', 'rgb(220, 107, 103)', false],
    ['unknown', 'rgb(148, 163, 184)', false],
  ]

  for (const [state, color, animated] of states) {
    await link.evaluate((element, nextState) => {
      element.className = `edge-cloud-link edge-cloud-link--${nextState}`
      element.dataset.cloudLinkState = nextState
    }, state)
    await expect(led).toHaveCSS('background-color', color)
    const animationName = await led.evaluate((element) => getComputedStyle(element).animationName)
    expect(animationName === 'edgeCloudLinkConnecting').toBe(animated)
  }
})

test('连接中动画尊重 prefers-reduced-motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await openConsole(page, { width: 1440, height: 900 })
  const link = page.locator('.edge-cloud-link')
  await link.evaluate((element) => {
    element.className = 'edge-cloud-link edge-cloud-link--connecting'
  })
  await expect(link.locator('.edge-cloud-link-led')).toHaveCSS('animation-name', 'none')
})
