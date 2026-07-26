import { expect, test } from '@playwright/test'

const device = {
  deviceName: 'e2e-node', hostname: 'e2e-node', platform: 'linux', cpuModel: 'fixture', uptimeHuman: '1h', agentVersion: 'test',
}

async function launchConsole(page) {
  await page.addInitScript(() => localStorage.setItem('edge_token', 'e2e-token'))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    let body = []
    if (url.pathname.endsWith('/device')) body = device
    if (url.pathname.endsWith('/metrics')) body = {
      cpuUsedPercent: 12,
      memoryUsed: 2 * 1024 ** 3,
      memoryTotal: 8 * 1024 ** 3,
      memoryUsedPercent: 25,
      gpuData: [],
      diskData: [],
    }
    if (url.pathname.endsWith('/metrics/history')) body = {}
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })
  await page.goto('/')
}

test('opens keyboard help from shortcut and status bar', async ({ page }) => {
  await launchConsole(page)
  await page.keyboard.press('Control+/')
  await expect(page.getByRole('dialog', { name: '键盘快捷键' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '键盘快捷键' })).toBeHidden()
  await page.getByRole('button', { name: '打开键盘快捷键帮助' }).click()
  await expect(page.getByText('跨域 iframe')).toBeVisible()
})

test('window shortcuts operate current apps and ignore input', async ({ page }) => {
  await launchConsole(page)
  await page.getByText('仪表盘', { exact: true }).dblclick()
  await expect(page.getByText('仪表盘 · 总览', { exact: true })).toBeVisible()

  await page.keyboard.press('Control+Alt+m')
  await expect(page.getByText('仪表盘 · 总览', { exact: true })).toBeHidden()
  await page.keyboard.press('Alt+1')
  await expect(page.getByText('仪表盘 · 总览', { exact: true })).toBeVisible()
  await page.keyboard.press('Control+Alt+f')
  await page.keyboard.press('Control+Alt+d')
  await expect(page.getByText('仪表盘 · 总览', { exact: true })).toBeHidden()
  await page.keyboard.press('Alt+1')
  await page.keyboard.press('Control+Alt+w')
  await expect(page.getByText('仪表盘 · 总览', { exact: true })).toBeHidden()

  await page.getByText('操作日志', { exact: true }).dblclick()
  const input = page.getByPlaceholder('用户名（模糊）')
  await input.focus()
  await page.keyboard.press('Control+Alt+w')
  await expect(input).toBeVisible()
})
