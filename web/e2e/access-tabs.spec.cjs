const { test, expect } = require('@playwright/test');
const { adminToken } = require('./admin-session.cjs');
for (const key of ['loginSettings', 'paymentSettings']) {
  test(`${key} uses tabs, keeps separate drafts and supports keyboard and mobile navigation`, async ({
    page,
    request,
  }) => {
    const token = await adminToken(request);
    await page.addInitScript((token) => {
      sessionStorage.setItem('lumo-admin-session-v1', token);
      sessionStorage.removeItem('lumo-admin-demo-v1');
    }, token);
    await page.goto('/admin.html');
    await expect(page.locator('#admin-session')).toBeVisible();
    await page.locator(`[data-tab="${key}"]`).click();
    const root = page.locator('#access-' + key);
    await expect(root).toHaveAttribute('aria-busy', 'false');
    const methods = root.locator('[data-access="section"][data-id="methods"]');
    const providers = root.locator('[data-access="section"][data-id="providers"]');
    await expect(methods).toHaveAttribute('aria-selected', 'true');
    await expect(root.locator('[data-provider-form]')).toHaveCount(0);
    const calls = [];
    page.on('request', (r) => {
      if (r.url().includes('/api/v1/')) calls.push(r.url());
    });
    const setting = root.locator(
      `[data-access-form] [name="${key === 'loginSettings' ? 'password_enabled' : 'demo_enabled'}"]`,
    );
    const previous = await setting.isChecked();
    await setting.setChecked(!previous);
    await root.locator('[data-access-reason]').fill('保留方式开关草稿');
    if (key === 'loginSettings') await root.locator('[name="otp_ttl_seconds"]').fill('240');
    await providers.click();
    await expect(root.locator('[data-access-form]')).toHaveCount(0);
    await expect(root.locator('[data-provider-form]')).toHaveCount(1);
    const ids = await root
      .locator('[data-access="provider"]')
      .evaluateAll((items) => items.map((item) => item.dataset.id));
    expect(ids.length).toBeGreaterThan(1);
    const first = root.locator(`[data-access="provider"][data-id="${ids[0]}"]`);
    const second = root.locator(`[data-access="provider"][data-id="${ids[1]}"]`);
    const field = root.locator('[data-provider-form] input[data-provider-field]').first();
    const name = await field.getAttribute('name');
    const value = (await field.getAttribute('type')) === 'url' ? 'https://demo.invalid/tab-draft' : 'channel-draft-123';
    await field.fill(value);
    await root.locator('[data-provider-reason]').fill('保留渠道参数草稿');
    await second.click();
    await expect(root.locator('[data-provider-form]')).toHaveAttribute('data-id', ids[1]);
    await first.click();
    await expect(root.locator(`[data-provider-form] [name="${name}"]`)).toHaveValue(value);
    await expect(root.locator('[data-provider-reason]')).toHaveValue('保留渠道参数草稿');
    await methods.click();
    await expect(setting).toBeChecked({ checked: !previous });
    await expect(root.locator('[data-access-reason]')).toHaveValue('保留方式开关草稿');
    if (key === 'loginSettings') await expect(root.locator('[name="otp_ttl_seconds"]')).toHaveValue('240');
    await methods.press('ArrowRight');
    await expect(providers).toHaveAttribute('aria-selected', 'true');
    await expect(providers).toBeFocused();
    await first.focus();
    await first.press('ArrowRight');
    await expect(second).toBeFocused();
    await second.press('End');
    await expect(root.locator('[data-access="provider"]').last()).toBeFocused();
    await page.setViewportSize({ width: 390, height: 844 });
    await root.locator('[data-access="provider"]').last().click();
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    await expect(root.locator('[data-provider-form]')).toHaveCount(1);
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({ path: `/private/tmp/lumo-${key}-tabs-mobile.png`, fullPage: true });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await first.click();
    await page.evaluate(() => scrollTo(0, 0));
    await page.screenshot({ path: `/private/tmp/lumo-${key}-tabs-desktop.png`, fullPage: true });
    expect(calls).toEqual([]);
  });
}
