import { expect, test, type Page } from '@playwright/test'

const base = () => process.env.E2E_BASE_URL!
const emuAddr = () => process.env.E2E_EMU_ADDR!
const rom = () => process.env.E2E_ROM!
const realEmu = !!process.env.E2E_REAL_EMU
const tid = (page: Page, id: string) => page.getByTestId(id)

// A real emulator boots and runs slower than the fake one.
if (realEmu) test.describe.configure({ timeout: 120_000 })

test('connect, blast, harvest, edit and export', async ({ page }) => {
  await page.goto(base())
  await expect(tid(page, 'emulator-label')).toHaveText('disconnected')

  // Connect to the emulator and load a ROM.
  await tid(page, 'connection-status').click()
  await tid(page, 'connect-address').fill(emuAddr())
  await tid(page, 'connect-button').click()
  await expect(tid(page, 'emulator-label')).not.toHaveText('disconnected')
  await tid(page, 'rom-path').fill(rom())
  await tid(page, 'rom-load').click()
  await expect(tid(page, 'game-title')).not.toHaveText('no ROM', {
    timeout: realEmu ? 30_000 : undefined,
  })
  await expect(tid(page, 'game-title')).not.toHaveText('')
  await page.keyboard.press('Escape')
  await tid(page, 'frame-counter').click()

  // The frame counter follows the SSE stream.
  const f0 = Number(await tid(page, 'frame-counter').textContent())
  await expect
    .poll(async () => Number(await tid(page, 'frame-counter').textContent()))
    .toBeGreaterThan(f0)

  // Domains were auto-selected by the ROM load.
  await expect(page.locator('[data-testid^="domain-"][aria-selected="true"]').first()).toBeVisible()

  // More units so Disable 50% has something to do.
  await tid(page, 'intensity-number').fill('10')
  await tid(page, 'intensity-number').press('Enter')
  await expect(tid(page, 'intensity-number')).toHaveValue('10')

  // Manual blast.
  await tid(page, 'manual-blast').click()
  await expect(tid(page, 'log-strip')).toContainText('blast: 10 units')

  // Save slot 1, then corrupt from it.
  await tid(page, 'nav-harvester').click()
  await tid(page, 'savestate-mode').click()
  await expect(tid(page, 'savestate-mode')).toHaveText('SAVE')
  await tid(page, 'slot-1-button').click()
  await expect(tid(page, 'slot-1')).toHaveAttribute('data-filled', 'true')
  await tid(page, 'savestate-mode').click()

  await expect(tid(page, 'gh-main')).toHaveText('Corrupt')
  await tid(page, 'gh-main').click()
  await expect(tid(page, 'stash-item')).toHaveCount(1)
  await expect(tid(page, 'stash-item').first()).toHaveAttribute('aria-selected', 'true')

  // Stash -> stockpile with a name.
  await tid(page, 'stash-to-stockpile').click()
  await tid(page, 'prompt-input').fill('first glitch')
  await tid(page, 'prompt-ok').click()
  await expect(tid(page, 'stockpile-item')).toHaveCount(1)
  await expect(tid(page, 'stockpile-item').first()).toContainText('first glitch')
  await expect(tid(page, 'stash-item')).toHaveCount(0)

  // Open in the Blast Editor, disable 50%, save and apply.
  await tid(page, 'stockpile-item').first().click()
  await tid(page, 'stockpile-edit').click()
  await expect(tid(page, 'be-title')).toContainText('first glitch')
  await expect(tid(page, 'be-size')).toHaveText('Layer size: 10')
  await tid(page, 'be-disable50').click()
  await expect(page.locator('[data-testid="be-row"][data-enabled="false"]')).toHaveCount(5)
  await expect(tid(page, 'be-dirty')).toBeVisible()
  await tid(page, 'be-save').click()
  await expect(tid(page, 'be-dirty')).toHaveCount(0)
  await tid(page, 'be-apply').click()
  await expect(tid(page, 'log-strip')).toContainText('applied 10 units')

  // Reopening shows the saved layer.
  await tid(page, 'nav-harvester').click()
  await tid(page, 'stockpile-edit').click()
  await expect(page.locator('[data-testid="be-row"][data-enabled="false"]')).toHaveCount(5)

  // Export the stockpile.
  await tid(page, 'nav-harvester').click()
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    tid(page, 'stockpile-export').click(),
  ])
  expect(download.suggestedFilename()).toMatch(/\.sks$/)
  const path = await download.path()
  expect(path).toBeTruthy()

  // No errors surfaced during the flow.
  await expect(tid(page, 'toast')).toHaveCount(0)
})

test('real emulator: scheduled units and screenshot', async ({ page, request }) => {
  test.skip(!realEmu, 'needs RTCVISH_MELONDS')
  await page.goto(base())
  await expect(tid(page, 'emulator-label')).not.toHaveText('disconnected')
  await expect(tid(page, 'game-title')).not.toHaveText('no ROM')

  // Hellgenie units live forever, so they stay listed after the blast.
  const patched = await request.patch(`${base()}/api/settings`, {
    data: { engine: 'hellgenie', intensity: 4 },
  })
  expect(patched.ok()).toBe(true)
  await page.reload()
  await tid(page, 'nav-engine').click()
  await tid(page, 'manual-blast').click()
  await expect(tid(page, 'log-strip')).toContainText('blast: 4 units')
  await expect
    .poll(async () => {
      const r = await request.get(`${base()}/api/blast/units`)
      return r.ok() ? ((await r.json()) as unknown[]).length : -1
    })
    .toBeGreaterThan(0)

  // The Memory panel shows a screenshot of the running game.
  await tid(page, 'nav-memory').click()
  await tid(page, 'screenshot-refresh').click()
  const shot = tid(page, 'screenshot')
  await expect(shot).toBeVisible()
  await expect
    .poll(() => shot.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth))
    .toBeGreaterThan(0)

  expect((await request.delete(`${base()}/api/blast/units`)).ok()).toBe(true)
  await request.patch(`${base()}/api/settings`, { data: { engine: 'nightmare' } })
  await expect(tid(page, 'toast')).toHaveCount(0)
})

test('theme toggle persists', async ({ page }) => {
  await page.goto(base())
  const html = page.locator('html')
  await expect(html).not.toHaveAttribute('data-theme', /.+/)
  await tid(page, 'theme-toggle').click()
  await expect(html).toHaveAttribute('data-theme', 'light')
  await tid(page, 'theme-toggle').click()
  await expect(html).toHaveAttribute('data-theme', 'dark')
  const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  expect(bg).toBe('rgb(17, 20, 24)')
  await page.reload()
  await expect(html).toHaveAttribute('data-theme', 'dark')
  await tid(page, 'theme-toggle').click()
  await expect(html).not.toHaveAttribute('data-theme', /.+/)
})
