const { test, expect } = require('@playwright/test');
async function admin(page) {
  await page.goto('/admin.html');
  await page.locator('#admin-login button[type="submit"]').click();
  await expect(page.locator('#admin-session')).toBeVisible();
}
test('customer journey rules and manual checks persist in the admin portal', async ({ page, request }) => {
  const login = await request.post('/api/v1/iam/login', {
    data: { data: { username: 'demo', password: 'Lumo@2026' } },
  });
  expect(login.ok()).toBe(true);
  const token = (await login.json()).data.token;
  const cart = await request.post('/api/v1/cart/add', {
    headers: { Authorization: 'Bearer ' + token },
    data: { data: { sku_id: 'sku-coffee', quantity: 1 } },
  });
  expect(cart.ok()).toBe(true);
  await admin(page);
  await page.locator('[data-tab="journeys"]').click();
  const form = page.locator('#journey-rule-form');
  await form.locator('[name="name"]').fill('浏览器购物车提醒');
  await form.locator('[name="delay_minutes"]').fill('1');
  await form.locator('[name="enabled"]').selectOption('true');
  await form.locator('button').click();
  await expect(page.locator('#journeys-admin-root')).toContainText('浏览器购物车提醒');
  await page.locator('#journey-customer-search [name="keyword"]').fill('demo');
  await page.locator('#journey-customer-search button').click();
  await page.getByRole('button', { name: /^demo ·/ }).click();
  const manual = page.locator('#journey-run-form');
  await manual.locator('[name="rule_id"]').selectOption({ label: '浏览器购物车提醒' });
  await manual.locator('button').click();
  await expect(page.locator('#journeys-admin-root')).toContainText('等待检查');
  await page.reload();
  await page.locator('[data-tab="journeys"]').click();
  await expect(page.locator('#journeys-admin-root')).toContainText('浏览器购物车提醒');
  await expect(page.locator('#journeys-admin-root')).toContainText('等待检查');
});
test('product server drafts survive reload and versions can be reviewed and restored', async ({ page }) => {
  await admin(page);
  await page.locator('[data-tab="products"]').click();
  await page.locator('[data-pd-action="new"]').click();
  const form = page.locator('#product-admin-form');
  await form.locator('[name="name"]').fill('浏览器发布版本商品');
  await form.locator('[name="category"]').fill('家用电器');
  await form.locator('[name="price"]').fill('100');
  await form.locator('[name="old"]').fill('120');
  const id = await form.locator('input[readonly]').inputValue();
  await page.locator('[data-pub="save-draft"]').click();
  await expect(page.locator('.publishing-panel')).toContainText('服务器草稿 v1');
  await page.reload();
  await page.locator('[data-tab="products"]').click();
  await page.locator(`[data-pub="open-draft"][data-id="${id}"]`).click();
  await expect(form.locator('[name="name"]')).toHaveValue('浏览器发布版本商品');
  await page.locator('[data-pub="check"]').click();
  await expect(page.locator('.publication-check')).toContainText('发布检查通过');
  await page.locator('[data-pub="publish"]').click();
  await expect(page.locator('.publishing-panel')).toContainText('当前商品 v1');
  await form.locator('[name="name"]').fill('浏览器修改后的商品');
  await form.locator('button[type="submit"]').click();
  await expect(form).toHaveCount(0);
  await page.locator(`[data-pd-action="edit"][data-pd-id="${id}"]`).click();
  await expect(page.locator('.publishing-panel')).toContainText('当前商品 v2');
  await page.locator('[data-pub="compare"][data-id="1"]').click();
  await expect(page.locator('.publication-compare')).toContainText('浏览器发布版本商品');
  await expect(page.locator('.publication-compare')).toContainText('浏览器修改后的商品');
  await page.locator('[data-pub="restore"][data-id="1"]').click();
  await expect(form.locator('[name="name"]')).toHaveValue('浏览器发布版本商品');
  await expect(page.locator('.publishing-panel')).toContainText('当前商品 v3');
});
test('exports are submitted to a persistent task centre without reading every page in the browser', async ({
  page,
}) => {
  const requests = [];
  page.on('request', (r) => {
    if (r.url().includes('/api/')) requests.push(r.url());
  });
  await admin(page);
  await page.locator('[data-tab="points"]').click();
  await expect(page.locator('[data-mg="export"]')).toBeEnabled();
  await page.locator('[data-mg="export"]').click();
  await expect(page.locator('#exports-admin-root')).toContainText('会员账号报表');
  await page.locator('[data-tab="overview"]').click();
  await page.locator('[data-tab="exports"]').click();
  await expect(page.locator('#exports-admin-root')).toContainText('会员账号报表');
  expect(requests.filter((u) => u.includes('/helpdesk/export_create')).length).toBe(1);
});

test('reviews show an honest empty score and support moderation reasons and resubmission', async ({
  page,
  browser,
  request,
}) => {
  await page.goto('/#/login');
  await page.waitForFunction(() => window.LumoAppReady);
  await page.evaluate(() => window.LumoAppReady);
  await expect
    .poll(() => page.evaluate(() => window.LumoAuth.model().methodsLoaded && !window.LumoAuth.model().checking))
    .toBe(true);
  await page.locator('#auth-username').fill('demo');
  await page.locator('#auth-password').fill('Lumo@2026');
  await page.locator('#auth-login-form button[type="submit"]').click();
  await expect.poll(() => page.evaluate(() => window.LumoAuth?.user()?.id)).toBe('demo');
  await page.goto('/#/product/coffee');
  await expect(page.locator('.pex-rating-score')).toContainText('暂无评分');
  await expect(page.locator('.pex-rating-score')).not.toContainText('0.0');
  const call = (service, action, command = {}) =>
    page.evaluate(({ service, action, command }) => window.LumoAPI.call(service, action, command), {
      service,
      action,
      command,
    });
  const login = await request.post('/api/v1/iam/login', {
    data: { data: { username: 'admin', password: 'Admin@2026' } },
  });
  const adminToken = (await login.json()).data.token;
  const staff = async (service, action, command = {}) => {
    const response = await request.post(`/api/v1/${service}/${action}`, {
      headers: { Authorization: 'Bearer ' + adminToken },
      data: command,
    });
    expect(response.ok()).toBe(true);
    return (await response.json()).data;
  };
  const quote = await call('checkout', 'quote', {
    data: { items: [{ sku_id: 'coffee', quantity: 1 }], coupon_mode: 'none' },
  });
  const order = await call('checkout', 'submit', {
    idempotency_key: 'browser-photo-review-order',
    data: {
      quote_id: quote.quote_id,
      address: { recipient: '评价验收', phone: '13800138000', address: '隔离测试路1号' },
    },
  });
  const payment = await call('payment', 'create', { id: order.order_id });
  await call('payment', 'submit_demo', { id: payment.id });
  await expect
    .poll(async () => (await staff('fulfillment', 'list')).items.find((r) => r.order_id === order.order_id)?.status)
    .toBe('READY');
  const shipment = (await staff('fulfillment', 'list')).items.find((r) => r.order_id === order.order_id);
  await staff('fulfillment', 'dispatch', { id: shipment.id });
  await expect.poll(async () => (await call('order', 'get', { id: order.order_id })).status).toBe('SHIPPED');
  await call('order', 'complete', { id: order.order_id });
  await page.goto('/#/member/orders');
  await page.locator(`[data-member-action="review-order"][data-id="${order.order_id}"]`).click();
  const form = page.locator('#member-review-form');
  await expect(form.locator('[name="sku_id"]')).toBeVisible();
  await expect(form.locator('[data-review-files]')).toBeAttached();
  await form.locator('[name="text"]').fill('浏览器真实购买评价');
  await form.locator('button[type="submit"]').click();
  await expect(form).toHaveCount(0);
  const reviewID = await page.evaluate(
    (id) => window.LumoMember.model().reviews.find((r) => r.orderId === id)?.id,
    order.order_id,
  );
  expect(reviewID).toBeTruthy();
  const context = await browser.newContext({ baseURL: process.env.BROWSER_BASE_URL });
  try {
    const adminPage = await context.newPage();
    await admin(adminPage);
    await adminPage.locator('[data-tab="interactions"]').click();
    await adminPage.locator(`[data-operation="interaction.moderate"][data-id="${reviewID}"]`).click();
    const moderation = adminPage.locator('#admin-operation-form');
    await moderation.locator('[name="approved"]').selectOption('false');
    await moderation.locator('[name="reason"]').fill('请补充具体的使用体验');
    await moderation.locator('button[type="submit"]').click();
    await expect(moderation).toHaveCount(0);
    await page.locator(`[data-member-action="review-order"][data-id="${order.order_id}"]`).click();
    await page.locator(`[data-member-action="review-details"][data-id="${reviewID}"]`).click();
    await expect(page.locator('.review-reason')).toContainText('请补充具体的使用体验');
    await page.locator('[data-member-action="review-revise"]').click();
    const revision = page.locator('#member-review-revision-form');
    await revision.locator('[name="body"]').fill('使用一周后，冲煮稳定，包装完好。');
    await revision.locator('button[type="submit"]').click();
    await expect(revision).toHaveCount(0);
    await adminPage.locator('[data-action="reload"]').first().click();
    await adminPage.locator(`[data-operation="interaction.moderate"][data-id="${reviewID}"]`).click();
    await adminPage.locator('#admin-operation-form button[type="submit"]').click();
    await expect(adminPage.locator('#admin-operation-form')).toHaveCount(0);
    await page.goto('/#/product/coffee');
    await page.locator('[data-product-extra-action="retry"]').filter({ hasText: '刷新评价' }).click();
    await expect(page.locator('.pex-rating-score')).toContainText('1 条公开评价');
    await expect(page.locator('.pex-rating-score')).not.toContainText('暂无评分');
  } finally {
    await context.close();
  }
});
