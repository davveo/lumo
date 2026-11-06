const { test, expect } = require('@playwright/test');
const { adminToken } = require('./admin-session.cjs');
let token = '';
async function admin(page, request) {
  token = await adminToken(request);
  await page.addInitScript((t) => {
    sessionStorage.setItem('lumo-admin-session-v1', t);
    sessionStorage.removeItem('lumo-admin-demo-v1');
  }, token);
  await page.goto('/admin.html');
  await expect(page.locator('#admin-session')).toBeVisible();
  await expect(page.locator('[data-db="refresh"]')).toBeEnabled();
}
async function preview(page) {
  await page.locator('#admin-demo-toggle').click();
  await page.locator('[data-admin-feedback][open] button.primary').click();
  await expect(page.locator('.admin-demo-banner')).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => document.querySelectorAll('#admin-main [aria-busy="true"]').length))
    .toBe(0);
}
async function open(page, tab) {
  await page.locator(`[data-tab="${tab}"]`).click();
  await expect(page.locator(`#admin-nav [data-tab="${tab}"]`)).toHaveAttribute('aria-current', 'page');
  await expect
    .poll(() => page.evaluate(() => document.querySelectorAll('#admin-main [aria-busy="true"]').length))
    .toBe(0);
}
test('all administrative pages and list modes have isolated representative preview data', async ({ page, request }) => {
  const failures = [],
    errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await admin(page, request);
  await preview(page);
  const writes = [];
  page.on('request', (r) => {
    if (r.url().includes('/api/v1/')) writes.push(r.url());
  });
  const tabs = await page.locator('#admin-nav [data-tab]').evaluateAll((rows) => rows.map((r) => r.dataset.tab));
  for (const tab of tabs) {
    await open(page, tab);
    if (await page.locator('[data-admin-feedback][open]').count()) {
      failures.push({ tab, error: await page.locator('[data-admin-feedback][open]').innerText() });
      await page.locator('[data-admin-feedback][open] button.primary').click();
      continue;
    }
    if (['products', 'loginMethods', 'paymentMethods', 'popups', 'messages'].includes(tab))
      await page.screenshot({ path: `/private/tmp/lumo-admin-tailwind-${tab}.png`, fullPage: true });
    const rows = await page.locator('#admin-main .admin-table tbody tr').count();
    if (
      [
        'orders',
        'payments',
        'fulfillment',
        'aftersales',
        'refunds',
        'invoices',
        'users',
        'tickets',
        'analytics',
        'coupons',
        'staff',
        'shipments',
        'procurement',
        'exports',
        'messages',
        'journeys',
        'rewards',
        'interactions',
        'reconciliation',
        'audit',
      ].includes(tab) &&
      rows === 0
    )
      failures.push({ tab, error: 'no preview list rows' });
    const modes = await page.locator('#admin-main [data-mg-mode],[data-pm-mode]').evaluateAll((rows) =>
      rows.map((r) => ({
        name: r.dataset.mgMode || r.dataset.pmMode,
        kind: r.hasAttribute('data-mg-mode') ? 'mg' : 'pm',
      })),
    );
    for (const mode of modes) {
      await page.locator(`[data-${mode.kind}-mode="${mode.name}"]`).click();
      await expect
        .poll(() => page.evaluate(() => document.querySelectorAll('#admin-main [aria-busy="true"]').length))
        .toBe(0);
      if (await page.locator('[data-admin-feedback][open]').count()) {
        failures.push({ tab, mode: mode.name, error: await page.locator('[data-admin-feedback][open]').innerText() });
        await page.locator('[data-admin-feedback][open] button.primary').click();
      }
    }
  }
  expect(errors).toEqual([]);
  expect(failures).toEqual([]);
  expect(writes).toEqual([]);
  await open(page, 'orders');
  expect((await page.locator('.admin-header').boundingBox()).height).toBe(72);
  const statusCell = await page.locator('.op-table tbody tr').first().locator('td').nth(6).boundingBox();
  const actionCell = await page.locator('.op-table tbody tr').first().locator('td').last().boundingBox();
  expect(statusCell.x + statusCell.width).toBeLessThanOrEqual(actionCell.x + 1);
  await page.screenshot({ path: '/private/tmp/lumo-admin-tailwind-orders.png', fullPage: true });
  await open(page, 'overview');
  await page.screenshot({ path: '/private/tmp/lumo-admin-tailwind-dashboard.png', fullPage: true });
});
test('admin error dialogs stay visible at the current scroll position and preview never writes business data', async ({
  page,
  request,
}) => {
  await admin(page, request);
  await preview(page);
  await open(page, 'journeys');
  await page.locator('#journeys-admin-root [data-admin-section="editor"]').click();
  const form = page.locator('#journey-rule-form');
  await form.locator('[name="name"]').fill('预览验证规则');
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  const y = await page.evaluate(() => scrollY);
  const calls = [];
  page.on('request', (r) => {
    if (r.url().includes('/api/v1/')) calls.push(r.url());
  });
  await form.evaluate((f) => f.requestSubmit());
  const dialog = page.locator('[data-admin-feedback][open]');
  await expect(dialog).toHaveAttribute('role', 'alertdialog');
  await expect(dialog).toContainText('当前为示例预览');
  expect(Math.abs((await page.evaluate(() => scrollY)) - y)).toBeLessThanOrEqual(1);
  await page.screenshot({ path: '/private/tmp/lumo-admin-tailwind-error.png' });
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(form.locator('[name="name"]')).toHaveValue('预览验证规则');
  expect(calls).toEqual([]);
  await open(page, 'workqueue');
  const checkbox = page.locator('.wq-table tbody tr').first().locator('td').first();
  const business = page.locator('.wq-table tbody tr').first().locator('td').nth(1);
  const a = await checkbox.boundingBox(),
    b = await business.boundingBox();
  expect(a.width).toBeLessThan(70);
  expect(b.x - a.x).toBeLessThan(80);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  await page.screenshot({ path: '/private/tmp/lumo-admin-tailwind-mobile.png', fullPage: true });
});
test('password and SMS tabs keep login geometry stable at desktop and mobile widths', async ({ page, request }) => {
  await admin(page, request);
  const headers = { Authorization: 'Bearer ' + token },
    policyResponse = await request.post('/api/v1/iam/policy_get', { headers, data: {} }),
    policy = (await policyResponse.json()).data;
  const saved = await request.post('/api/v1/iam/policy_upsert', {
    headers,
    data: {
      data: {
        config: { ...policy.config, phone_enabled: true },
        version: policy.version,
        reason: 'isolated browser layout fixture',
      },
    },
  });
  expect(saved.ok()).toBe(true);

  await page.goto('/#/login');
  await page.waitForFunction(() => window.LumoAuth?.model().methodsLoaded);
  await expect(page.locator('[data-auth-action="login-tab"]')).toHaveCount(2);
  for (const size of [
    { width: 1440, height: 1000 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(size);
    await page.locator('[data-auth-action="login-tab"][data-id="password"]').click();
    const before = await page.locator('.auth-card').boundingBox();
    const submit = await page.locator('#auth-login-form .auth-submit').boundingBox();
    for (let i = 0; i < 4; i++) {
      await page.locator('[data-auth-action="login-tab"][data-id="phone"]').click();
      const phone = await page.locator('.auth-card').boundingBox();
      const button = await page.locator('#auth-login-form .auth-submit').boundingBox();
      expect(Math.abs(phone.y - before.y)).toBeLessThanOrEqual(1);
      expect(Math.abs(phone.height - before.height)).toBeLessThanOrEqual(1);
      expect(Math.abs(button.y - submit.y)).toBeLessThanOrEqual(1);
      await page.locator('[data-auth-action="login-tab"][data-id="password"]').click();
    }
  }
  const current = await request.post('/api/v1/iam/policy_get', { headers, data: {} });
  const restored = await request.post('/api/v1/iam/policy_upsert', {
    headers,
    data: {
      data: {
        config: policy.config,
        version: (await current.json()).data.version,
        reason: 'restore isolated layout fixture',
      },
    },
  });
  expect(restored.ok()).toBe(true);
});
test('storefront channel showcases display all samples without authentication or payment mutations', async ({
  page,
}) => {
  const writes = [];
  page.on('request', (r) => {
    if (
      /\/api\/v1\/(?:iam\/(?:login|register|otp_send|otp_login|oauth_start|oauth_callback)|payment\/(?:create|submit_demo))/.test(
        r.url(),
      )
    )
      writes.push(r.url());
  });
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/#/preview/login');
  await expect(page.locator('.channel-preview-banner')).toContainText('样例效果预览');
  await expect(page.locator('.auth-social-methods')).toContainText('微信');
  await expect(page.locator('.auth-social-methods')).toContainText('QQ');
  for (const provider of ['wechat', 'qq']) {
    const login = page.locator(`[data-auth-action="oauth"][data-id="${provider}"]`);
    await expect(login.locator('img')).toBeVisible();
    await expect.poll(() => login.locator('img').evaluate((img) => img.complete && img.naturalWidth > 0)).toBe(true);
    await login.locator('img').click();
    await expect(page.locator('.channel-preview-notice')).toContainText('样例效果预览');
    await page.locator('.channel-preview-notice [data-action="close-modal"]').click();
  }
  await page.screenshot({ path: '/private/tmp/lumo-channel-login-preview.png', fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('.auth-social-options')).toBeInViewport();
  expect(await page.locator('.auth-card').evaluate((card) => card.scrollWidth - card.clientWidth)).toBeLessThanOrEqual(
    1,
  );
  await page.screenshot({ path: '/private/tmp/lumo-channel-login-mobile.png', fullPage: true });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.locator('[data-auth-action="login-tab"][data-id="phone"]').click();
  await page.locator('[data-auth-action="send-otp"]').click();
  await expect(page.locator('.auth-credential-panel:not([aria-hidden="true"])')).toContainText('123456');
  await page.goto('/#/preview/payment');
  await expect(page.locator('[data-channel-method]')).toHaveCount(4);
  await page.screenshot({ path: '/private/tmp/lumo-channel-payment-preview.png', fullPage: true });
  await page.locator('[data-channel-method="alipay"]').click();
  await expect(page.locator('[data-channel-method="alipay"]')).toHaveAttribute('aria-pressed', 'true');
  await page.locator('[data-channel-pay]').click();
  await expect(page.locator('.channel-preview-notice')).toContainText('样例效果预览');
  expect(errors).toEqual([]);
  expect(writes).toEqual([]);
});

test('server errors open one modal with a trace and form validation is grouped', async ({ page, request }) => {
  await admin(page, request);
  const trace = '1234567890abcdef1234567890abcdef';
  await page.route('**/api/v1/interaction/list', (route) =>
    route.fulfill({
      status: 503,
      headers: { 'Content-Type': 'application/json', 'X-Trace-ID': trace },
      body: JSON.stringify({ error: { code: 'unavailable', message: '浏览器回归：服务暂时不可用' } }),
    }),
  );
  await open(page, 'interactions');
  let dialog = page.locator('[data-admin-feedback][open]');
  await expect(dialog).toContainText('服务暂时不可用');
  await dialog.locator('summary').click();
  await expect(dialog.locator('code')).toHaveText(trace);
  const message = await dialog.locator('.admin-feedback-copy > p').textContent();
  await dialog.locator('button.primary').click();
  await expect(dialog).toHaveCount(0);
  await page.evaluate((message) => {
    void window.LumoAdminFeedback.error(message);
  }, message);
  await expect(dialog).toBeVisible();
  await dialog.locator('button.primary').click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('#admin-main .admin-error')).toBeHidden();
  await open(page, 'staff');
  await page.locator('#staff-admin-root [data-admin-section="editor"]').click();
  await page.locator('#staff-save-form').evaluate((form) => form.requestSubmit());
  await expect(dialog).toContainText('请完善表单内容');
  await expect(dialog).toContainText('账号');
  await expect(dialog).toContainText('操作原因');
  await dialog.locator('button.primary').click();
  await expect(dialog).toHaveCount(0);
});
