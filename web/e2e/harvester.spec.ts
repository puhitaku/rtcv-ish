import { expect, test, type Page } from '@playwright/test'

const base = () => process.env.E2E_BASE_URL!
const emuAddr = () => process.env.E2E_EMU_ADDR!
const tid = (page: Page, id: string) => page.getByTestId(id)

test('connect, blast, harvest, edit and export', async ({ page }) => {
  await page.goto(base())
  await expect(tid(page, 'emulator-label')).toHaveText('disconnected')

  // Connect to the fake emulator and load a ROM.
  await tid(page, 'connection-status').click()
  await tid(page, 'connect-address').fill(emuAddr())
  await tid(page, 'connect-button').click()
  await expect(tid(page, 'emulator-label')).not.toHaveText('disconnected')
  await tid(page, 'rom-path').fill('/roms/e2e.nds')
  await tid(page, 'rom-load').click()
  await expect(tid(page, 'game-title')).not.toHaveText('no ROM')
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
