const { test, expect } = require('@playwright/test');

const { adminToken } = require('./admin-session.cjs');
async function adminLogin(page, request) {
  const token = await adminToken(request);
  await page.addInitScript((token) => sessionStorage.setItem('lumo-admin-session-v1', token), token);
  await page.goto('/admin.html');
  await expect(page.locator('#admin-session')).toBeVisible();
}
async function navigate(page, tab) {
  const toggle = page.locator('#admin-nav-toggle');
  if ((await toggle.isVisible()) && (await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await page.locator(`[data-tab="${tab}"]`).click();
}
async function checkActions(page, selector) {
  const form = page.locator(selector);
  await expect(form).toBeVisible();
  let space;
  await expect
    .poll(async () => {
      const value = await form.evaluate((form) => {
        const controls = [...form.querySelectorAll('input:not([type="hidden"]),textarea,select')].filter(
          (n) => n.getBoundingClientRect().height > 0,
        );
        const action = [...form.querySelectorAll(':scope > .admin-form-actions')].at(-1);
        if (!controls.length || !action || form.closest('[aria-busy]')?.getAttribute('aria-busy') === 'true')
          return null;
        const last = controls.at(-1).getBoundingClientRect(),
          bar = action.getBoundingClientRect();
        const card = form.closest('.admin-section').getBoundingClientRect();
        const buttons = [...action.querySelectorAll('button')].map((n) => n.getBoundingClientRect());
        return {
          gap: bar.top - last.bottom,
          inset: Math.min(...buttons.map((b) => card.bottom - b.bottom)),
          overflow: action.scrollWidth - action.clientWidth,
        };
      });
      if (value) space = value;
      return Boolean(value);
    })
    .toBe(true);
  expect(space.gap).toBeGreaterThanOrEqual(16);
  expect(space.inset).toBeGreaterThanOrEqual(15);
  expect(space.overflow).toBeLessThanOrEqual(1);
}
test('admin action rows have breathing room on desktop and narrow screens', async ({ page, request }) => {
  const login = await request.post('/api/v1/iam/login', {
    data: { data: { username: 'demo', password: 'Lumo@2026' } },
  });
  expect(login.ok()).toBe(true);
  const token = (await login.json()).data.token;
  const created = await request.post('/api/v1/interaction/create', {
    headers: { Authorization: 'Bearer ' + token },
    data: {
      idempotency_key: 'interface-spacing-question',
      data: { kind: 'question', sku_id: 'sku-coffee', body: '布局回归：请问这款商品的规格是什么？' },
    },
  });
  expect(created.ok()).toBe(true);
  const id = (await created.json()).data.id;
  await adminLogin(page, request);
  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await navigate(page, 'staff');
    await page.locator('#staff-admin-root [data-admin-section="editor"]').click();
    await checkActions(page, '#staff-save-form');
    await navigate(page, 'catalogImport');
    await checkActions(page, '#catalog-import-form');
    await navigate(page, 'procurement');
    await page.locator('#procurement-admin-root [data-admin-section="create"]').click();
    await checkActions(page, '#procurement-create-form');
    await navigate(page, 'interactions');
    await page.locator(`[data-operation="interaction.moderate"][data-id="${id}"]`).click();
    await checkActions(page, '#admin-operation-form');
    await page.locator('#admin-operation-form [data-action="cancel-operation"]').first().click();
    await page.locator(`[data-operation="interaction.answer"][data-id="${id}"]`).click();
    await checkActions(page, '#admin-operation-form');
  }
});
test('search recovery uses product cards and the shared filters fit narrow screens', async ({ page }) => {
  await page.goto('/');
  await page.waitForFunction(() => window.LumoAppReady);
  await page.evaluate(() => window.LumoAppReady);
  await page.locator('#search-input').fill('没有这件商品layoutreview');
  await page.locator('#search-input').press('Enter');
  await expect(page.locator('.disc-empty-results')).toBeVisible();
  await expect(page.locator('.disc-recovery-card').first()).toBeVisible();
  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 900 });
    const filters = page.locator('.disc-filter-surface');
    await expect(filters).toBeVisible();
    const geometry = await filters.evaluate((root) => ({
      overflow: root.scrollWidth - root.clientWidth,
      heights: [
        root.querySelector('select'),
        root.querySelector('.disc-spec-filter>div'),
        root.querySelector('.disc-stock-filter'),
      ].map((n) => n.getBoundingClientRect().height),
    }));
    expect(geometry.overflow).toBeLessThanOrEqual(1);
    expect(Math.max(...geometry.heights) - Math.min(...geometry.heights)).toBeLessThanOrEqual(1);
  }
  await page.locator('#discovery-attribute').fill('256GB');
  await page.evaluate(() => renderKeepScroll());
  await expect(page.locator('#discovery-attribute')).toHaveValue('256GB');
  await expect(page.locator('#discovery-attribute')).toBeFocused();
  await page.locator('#discovery-attribute').press('Enter');
  await expect(page.locator('.disc-filter-chip')).toContainText(['256GB']);
});
