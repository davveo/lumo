const { test, expect } = require('@playwright/test');
const { adminToken } = require('./admin-session.cjs');
async function signIn(page, request) {
  const token = await adminToken(request);
  await page.addInitScript((token) => sessionStorage.setItem('lumo-admin-session-v1', token), token);
  await page.goto('/admin.html');
  await expect(page.locator('#admin-session')).toBeVisible();
  await page.locator('#admin-demo-toggle').click();
  await page.locator('[data-admin-feedback][open] button.primary').click();
  await expect(page.locator('.admin-demo-banner')).toBeVisible();
}
async function open(page, key, id) {
  const menu = page.locator('#admin-nav-toggle');
  if ((await menu.isVisible()) && (await menu.getAttribute('aria-expanded')) !== 'true') await menu.click();
  await page.locator(`[data-tab="${key}"]`).click();
  const root = page.locator('#' + id);
  await expect(root.locator('[role="tab"]').first()).toBeEnabled();
  return root;
}
const cases = [
  [
    'staff',
    'staff-admin-root',
    ['accounts', 'editor'],
    'editor',
    '#staff-save-form [name="username"]',
    'tab_staff_draft',
  ],
  [
    'shipments',
    'shipments-admin-root',
    ['tracking', 'exceptions', 'manual'],
    'manual',
    '#shipment-update-form [name="description"]',
    '切换后保留人工轨迹草稿',
  ],
  ['products', 'product-admin-root', ['catalog', 'drafts'], 'catalog', '[data-pd-filter="search"]', '耳机'],
  [
    'procurement',
    'procurement-admin-root',
    ['orders', 'detail', 'picker', 'create', 'suppliers'],
    'create',
    '#procurement-create-form [name="supplier"]',
    '切换后保留供应商',
  ],
  [
    'journeys',
    'journeys-admin-root',
    ['overview', 'rules', 'editor', 'manual', 'records'],
    'editor',
    '#journey-rule-form [name="name"]',
    '切换后保留触达规则',
  ],
  [
    'rewards',
    'rewards-admin-root',
    ['offers', 'editor', 'records'],
    'editor',
    '#reward-offer-editor [name="name"]',
    '切换后保留兑换项目',
  ],
];
test('all six pages switch panels without requests or lost drafts on desktop and mobile', async ({ page, request }) => {
  await signIn(page, request);
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    for (const [key, id, sections, formTab, field, value] of cases) {
      const root = await open(page, key, id);
      if (key === 'rewards') await root.locator('[data-rwa="create"]').click();
      await root.locator(`[data-admin-section="${formTab}"]`).click();
      await root.locator(field).fill(value);
      const calls = [];
      const track = (request) => {
        if (request.url().includes('/api/v1/')) calls.push(request.url());
      };
      page.on('request', track);
      await expect(root.locator('[role="tab"]')).toHaveCount(sections.length);
      for (const section of sections) {
        const tab = root.locator(`[data-admin-section="${section}"]`);
        await tab.click();
        await expect(tab).toHaveAttribute('aria-selected', 'true');
        await expect(root.locator('[role="tab"][tabindex="0"]')).toHaveCount(1);
        await expect(root.locator('[role="tabpanel"]:visible')).toHaveCount(1);
        await expect(page.locator('#' + (await tab.getAttribute('aria-controls')))).toBeVisible();
        await expect(root.locator('[role="tabpanel"][hidden][inert]')).toHaveCount(sections.length - 1);
      }
      const first = root.locator('[role="tab"]').first();
      const last = root.locator('[role="tab"]').last();
      await last.press('Home');
      await expect(first).toBeFocused();
      await first.press('ArrowLeft');
      await expect(last).toBeFocused();
      await last.press('Home');
      await first.press('End');
      await expect(last).toHaveAttribute('aria-selected', 'true');
      await root.locator(`[data-admin-section="${formTab}"]`).click();
      await expect(root.locator(field)).toHaveValue(value);
      expect(calls).toEqual([]);
      page.off('request', track);
      expect(
        await root.locator('[role="tablist"]').evaluate((list) => {
          const selected = list.querySelector('[aria-selected="true"]').getBoundingClientRect();
          const bounds = list.getBoundingClientRect();
          return selected.left >= bounds.left - 1 && selected.right <= bounds.right + 1;
        }),
      ).toBe(true);
      expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1);
      await root.locator(`[data-admin-section="${sections[0]}"]`).click();
      await page.evaluate(() => scrollTo(0, 0));
      await page.screenshot({ path: `/private/tmp/lumo-page-tabs-${key}-${width}.png`, fullPage: true });
    }
  }
  expect(errors).toEqual([]);
});
test('edit and purchase actions reveal the matching tab and keep other drafts after reads', async ({
  page,
  request,
}) => {
  await signIn(page, request);
  let root = await open(page, 'staff', 'staff-admin-root');
  await root.locator('[data-staff-edit]').first().click();
  await expect(root.locator('[data-admin-section="editor"]')).toHaveAttribute('aria-selected', 'true');
  await expect(root.locator('#staff-save-form [name="username"]')).not.toHaveValue('');
  await root.locator('#staff-save-form [name="reason"]').fill('待确认的岗位调整');
  await open(page, 'products', 'product-admin-root');
  root = await open(page, 'staff', 'staff-admin-root');
  await expect(root.locator('#staff-save-form [name="reason"]')).toHaveValue('待确认的岗位调整');

  root = await open(page, 'shipments', 'shipments-admin-root');
  await root.locator('[data-admin-section="manual"]').click();
  await root.locator('#shipment-update-form [name="tracking_id"]').fill('TAB-KEEP-001');
  await root.locator('[data-admin-section="exceptions"]').click();
  await root.locator('[data-shipment-filter]').selectOption('');
  await expect(root).toHaveAttribute('aria-busy', 'false');
  await root.locator('[data-admin-section="manual"]').click();
  await expect(root.locator('#shipment-update-form [name="tracking_id"]')).toHaveValue('TAB-KEEP-001');

  root = await open(page, 'procurement', 'procurement-admin-root');
  await root.locator('[data-po-open]').first().click();
  await expect(root.locator('[data-admin-section="detail"]')).toHaveAttribute('aria-selected', 'true');
  await expect(root.locator('#procurement-receive-form')).toBeVisible();
  await root.locator('[data-admin-section="picker"]').click();
  await root.locator('#procurement-catalog-form [name="keyword"]').fill('耳机');
  await root.locator('#procurement-catalog-form button').click();
  await root.locator('[data-supply-product]').first().click();
  await root.locator('[data-supply-add]').click();
  await expect(root.locator('[data-admin-section="create"]')).toHaveAttribute('aria-selected', 'true');
  await expect(root.locator('[data-purchase-line="0"]').first()).toBeVisible();

  root = await open(page, 'journeys', 'journeys-admin-root');
  await root.locator('[data-admin-section="rules"]').click();
  await root.locator('[data-journey="edit-rule"]').first().click();
  await expect(root.locator('[data-admin-section="editor"]')).toHaveAttribute('aria-selected', 'true');
  await expect(root.locator('#journey-rule-form [name="name"]')).not.toHaveValue('');

  root = await open(page, 'rewards', 'rewards-admin-root');
  await root.locator('[data-rwa="edit"]').first().click();
  await expect(root.locator('[data-admin-section="editor"]')).toHaveAttribute('aria-selected', 'true');
  await root.locator('#reward-offer-editor [name="name"]').fill('刷新后仍保留兑换草稿');
  await root.locator('[data-admin-section="records"]').click();
  await root.locator('[data-rwa="refresh"]').click();
  await expect(root).toHaveAttribute('aria-busy', 'false');
  await root.locator('[data-admin-section="editor"]').click();
  await expect(root.locator('#reward-offer-editor [name="name"]')).toHaveValue('刷新后仍保留兑换草稿');
});
