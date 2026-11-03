const { test, expect } = require('@playwright/test');

async function login(page, username, password, userID = username) {
  await page.goto('/#/login');
  await page.waitForFunction(() => window.LumoAppReady);
  await page.evaluate(() => window.LumoAppReady);
  await expect
    .poll(() => page.evaluate(() => window.LumoAuth.model().methodsLoaded && !window.LumoAuth.model().checking))
    .toBe(true);
  await page.locator('#auth-username').fill(username);
  await page.locator('#auth-password').fill(password);
  await page.locator('#auth-login-form button[type="submit"]').click();
  await expect.poll(() => page.evaluate(() => window.LumoAuth?.user()?.id)).toBe(userID);
}
const api = (page, service, action, command = {}) =>
  page.evaluate(({ service, action, command }) => window.LumoAPI.call(service, action, command), {
    service,
    action,
    command,
  });

test('background updates preserve the active login form DOM and typed credentials', async ({ page }) => {
  await page.goto('/#/login');
  await page.waitForFunction(() => window.LumoAppReady);
  await page.evaluate(() => window.LumoAppReady);
  await expect
    .poll(() => page.evaluate(() => window.LumoAuth.model().methodsLoaded && !window.LumoAuth.model().methodsLoading))
    .toBe(true);
  await page.locator('#auth-username').fill('draft-buyer');
  await page.locator('#auth-password').fill('PrivateDraft123');
  const preserved = await page.evaluate(() => {
    const input = document.getElementById('auth-password');
    input.focus();
    for (let i = 0; i < 5; i++) renderKeepScroll();
    return input.isConnected && input === document.getElementById('auth-password') && document.activeElement === input;
  });
  expect(preserved).toBe(true);
  await expect(page.locator('#auth-password')).toHaveValue('PrivateDraft123');
  expect(await page.evaluate(() => JSON.stringify(window.LumoAuth.model()).includes('PrivateDraft123'))).toBe(false);
});

test('customer registers through the storefront and logs in as an ordinary member', async ({ page }) => {
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/#/register');
  await page.waitForFunction(() => window.LumoAppReady);
  await page.evaluate(() => window.LumoAppReady);
  await expect.poll(() => page.evaluate(() => window.LumoAuth.model().methodsLoaded)).toBe(true);
  const form = page.locator('#auth-register-form');
  await form.locator('[name="username"]').fill('browser_buyer');
  await form.locator('[name="password"]').fill('Member@2026');
  await form.locator('[name="confirm_password"]').fill('Member@2026');
  const registered = page.waitForResponse(
    (response) => response.url().endsWith('/api/v1/iam/register') && response.request().method() === 'POST',
  );
  await form.locator('button[type="submit"]').click();
  const response = await registered;
  expect(response.ok()).toBe(true);
  const account = (await response.json()).data;
  expect(account.role).toBe('user');
  expect(account.username).toBe('browser_buyer');
  await expect(page).toHaveURL(/#\/login/);
  await login(page, 'browser_buyer', 'Member@2026', account.user_id);
  expect((await api(page, 'iam', 'current')).user_id).toBe(account.user_id);
  expect(await page.evaluate(() => window.LumoAuth.user().role)).toBe('user');
  expect(errors).toEqual([]);
});

test('guest cart survives reload and merges after checkout login', async ({ page }) => {
  await page.goto('/#/product/coffee');
  await page.waitForFunction(() => window.LumoAppReady);
  await page.evaluate(() => window.LumoAppReady);
  await page.locator('[data-action="detail-add"]').click();
  await expect(page.locator('#toast')).toContainText('游客购物车');
  await page.goto('/#/cart');
  await expect(page.locator('.cart-page-heading')).toContainText('游客购物车');
  await page.reload();
  await expect(page.locator('[data-action="checkout"]')).toBeEnabled();
  const guestSKU = await page.evaluate(() => window.LumoGuestCart.read().items[0].sku_id);
  await page.locator('[data-action="checkout"]').click();
  await expect(page).toHaveURL(/#\/login/);
  await login(page, 'demo', 'Lumo@2026');
  await expect.poll(() => page.evaluate(() => window.LumoGuestCart.count())).toBe(0);
  const cart = await api(page, 'cart', 'list');
  expect(cart.items.some((row) => row.sku_id === guestSKU && row.quantity >= 1)).toBe(true);
  await expect(page).toHaveURL(/#\/checkout/);
});

test('security centre binds a verified contact and changes password through the UI', async ({ page }) => {
  const created = await page.request.post('/api/v1/iam/register', {
    data: { data: { username: 'security_browser', password: 'Member@2026' } },
  });
  expect(created.ok()).toBe(true);
  const account = (await created.json()).data;
  await login(page, 'security_browser', 'Member@2026', account.user_id);
  await page.goto('/#/member/settings');
  await page.locator('[data-security-action="open"]').click();
  const bind = page.locator('#security-bind-form');
  await bind.locator('[name="channel"]').selectOption('email');
  await bind.locator('[name="address"]').fill('security-browser@example.test');
  await bind.locator('[name="current_password"]').fill('Member@2026');
  await bind.locator('[data-security-action="send-bind"]').click();
  await expect(bind.locator('[data-security-demo]')).toContainText('演示验证码');
  const code = (await bind.locator('[data-security-demo]').textContent()).match(/[0-9]{6}/)[0];
  await bind.locator('[name="code"]').fill(code);
  await bind.locator('button[type="submit"],button.primary').last().click();
  await expect(page.locator('.modal')).toContainText('s***@example.test');
  const form = page.locator('#security-password-form');
  await form.locator('[name="current_password"]').fill('Member@2026');
  await form.locator('[name="new_password"]').fill('Changed@2026');
  await form.locator('button').click();
  await expect(page).toHaveURL(/#\/login/);
  await login(page, 'security_browser', 'Changed@2026', account.user_id);
});

test('built storefront supports checkout, payment, aftersale and lazy admin editor', async ({ page, browser }) => {
  const errors = [],
    requested = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('request', (request) => requested.push(request.url()));
  await page.goto('/');
  await expect(page.locator('.product-card').first()).toBeVisible();
  expect(requested.some((url) => /\/editor\.[0-9a-f]+\.js/.test(url))).toBe(false);
  await login(page, 'demo', 'Lumo@2026');
  await api(page, 'address', 'create', {
    data: {
      recipient: 'Browser Buyer',
      phone: '13800138000',
      province: '广东省',
      city: '深圳市',
      district: '南山区',
      address: 'CI isolated address',
      is_default: true,
    },
  });
  await page.goto('/#/product/coffee');
  await expect(page.locator('[data-action="buy-now"]')).toBeEnabled();
  await expect.poll(() => page.evaluate(() => window.LumoInventory.validate([detail]) || '')).toBe('');
  await page.locator('[data-action="buy-now"]').click();
  await expect(page).toHaveURL(/#\/checkout/);
  await page.evaluate(() => window.LumoConnection.refresh());
  await expect(page.locator('[data-action="submit-order"]')).toBeEnabled();
  await page.locator('[data-action="submit-order"]').click();
  await expect(page.locator('[data-action="pay"]')).toBeEnabled();
  const orderID = await page.evaluate(
    () => window.LumoCommerce.allOrders().find((order) => order.backendStatus === 'PENDING_PAYMENT')?.id,
  );
  expect(orderID).toBeTruthy();
  await page.locator('[data-action="pay"]').click();
  await expect.poll(async () => (await api(page, 'order', 'get', { id: orderID })).status).toBe('READY_TO_FULFILL');
  await expect(page).toHaveURL(/#\/success/);
  await page.goto('/#/member/orders');
  await page.locator(`[data-member-action="aftersale"][data-id="${orderID}"]`).click();
  await page.locator('#member-aftersale-form [name="note"]').fill('Isolated browser regression');
  await page.locator('#member-aftersale-form button[type="submit"]').click();
  await expect(page.locator('#member-aftersale-form')).toHaveCount(0);
  const aftersale = (await api(page, 'aftersale', 'list')).items.find((row) => row.order_id === orderID);
  expect(aftersale?.status).toBe('REQUESTED');

  const adminContext = await browser.newContext({ baseURL: process.env.BROWSER_BASE_URL });
  try {
    const admin = await adminContext.newPage();
    admin.on('pageerror', (error) => errors.push(error.message));
    const adminRequests = [];
    admin.on('request', (request) => adminRequests.push(request.url()));
    await login(admin, 'admin', 'Admin@2026');
    expect(adminRequests.some((url) => /\/editor\.[0-9a-f]+\.js/.test(url))).toBe(false);
    await api(admin, 'aftersale', 'approve', { id: aftersale.id });
    const refunded = await api(admin, 'aftersale', 'refund', { id: aftersale.id });
    await api(admin, 'refund', 'confirm_demo', { id: refunded.refund_id });
    await expect.poll(async () => (await api(page, 'aftersale', 'get', { id: aftersale.id })).status).toBe('COMPLETED');
    await page.goto('/#/member/aftersales');
    await page.reload();
    await expect(page.locator('#main')).toContainText('已完成');
    await admin.goto('/#/editor');
    await expect(admin.locator('.studio-shell')).toBeVisible();
    await expect(admin.locator('[data-editor-action="tab"][data-value="categories"]')).toBeEnabled();
    await admin.locator('[data-editor-action="tab"][data-value="categories"]').click();
    await expect(admin.locator('.ce-editor')).toBeVisible();
    expect(adminRequests.some((url) => /\/editor\.[0-9a-f]+\.js/.test(url))).toBe(true);
  } finally {
    await adminContext.close();
  }
  expect(errors).toEqual([]);
});

test('administrators manage employees, import products and receive procurement in the portal', async ({
  page,
  browser,
}) => {
  await page.goto('/admin.html');
  await page.locator('#admin-login [name="username"]').fill('admin');
  await page.locator('#admin-login [name="password"]').fill('Admin@2026');
  await page.locator('#admin-login button[type="submit"]').click();
  await page.locator('[data-db-days="90"]').click();
  await expect.poll(() => page.evaluate(() => window.LumoDashboardAdmin.state.data?.insights?.days)).toBe(90);
  await expect.poll(() => page.evaluate(() => window.LumoDashboardAdmin.state.data?.degraded)).toBe(false);
  await page.locator('[data-tab="staff"]').click();
  const staff = page.locator('#staff-save-form');
  await staff.locator('[name="username"]').fill('browser_warehouse');
  await staff.locator('[name="role"]').selectOption('staff_warehouse');
  await staff.locator('[name="password"]').fill('Warehouse@2026');
  await staff.locator('[name="reason"]').fill('浏览器验收仓库岗位');
  await staff.locator('button').click();
  await expect(page.locator('#staff-admin-root')).toContainText('browser_warehouse');

  await page.locator('[data-tab="catalogImport"]').click();
  const upload = page.locator('#catalog-import-form');
  await upload.locator('[name="file"]').setInputFiles({
    name: 'browser-products.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from('id,name,price_cents,active\nbrowser-import,浏览器导入商品,1999,false\n'),
  });
  await upload.locator('button').click();
  await expect(page.locator('[data-import-commit]')).toBeEnabled();
  page.once('dialog', (dialog) => dialog.accept());
  await page.locator('[data-import-commit]').click();
  await expect
    .poll(() => page.evaluate(() => window.LumoCatalogImportAdmin.state.job?.status), { timeout: 20000 })
    .toBe('COMPLETED');
  await page.locator('[data-tab="procurement"]').click();
  const purchase = page.locator('#procurement-create-form');
  await purchase.locator('[name="supplier"]').fill('浏览器供应商');
  await page.locator('#procurement-catalog-form [name="keyword"]').fill('browser-import');
  await page.locator('#procurement-catalog-form button').click();
  await expect(page.locator('[data-supply-product="browser-import"]')).toContainText('浏览器导入商品');
  await page.locator('[data-supply-product="browser-import"]').click();
  await page.locator('[data-supply-add]').click();
  await purchase.locator('[data-field="quantity"]').fill('3');
  await purchase.locator('[data-field="cost"]').fill('10.00');
  await purchase.getByRole('button', { name: '创建待审批采购单', exact: true }).click();
  await expect(page.locator('[data-po="approve"]')).toBeVisible();
  await page.locator('[data-po="approve"]').click();
  const receipt = page.locator('#procurement-receive-form');
  await expect(receipt.locator('button')).toBeEnabled();
  await receipt.locator('[data-po-sku="browser-import"]').fill('2');
  await receipt.locator('[data-po-damaged="browser-import"]').fill('1');
  await receipt.locator('button').click();
  await expect
    .poll(() => page.evaluate(() => window.LumoProcurementAdmin.state.detail?.status))
    .toBe('PARTIALLY_RECEIVED');
  const stock = await page.evaluate(() =>
    window.LumoAPI.call(
      'inventory',
      'get',
      { id: 'browser-import' },
      { token: sessionStorage.getItem('lumo-admin-session-v1') },
    ),
  );
  expect(stock.on_hand).toBe(1);

  const context = await browser.newContext({ baseURL: process.env.BROWSER_BASE_URL });
  try {
    const warehouse = await context.newPage();
    await warehouse.goto('/admin.html');
    await warehouse.locator('#admin-login [name="username"]').fill('browser_warehouse');
    await warehouse.locator('#admin-login [name="password"]').fill('Warehouse@2026');
    await warehouse.locator('#admin-login button[type="submit"]').click();
    await expect(warehouse.locator('[data-tab="procurement"]')).toBeVisible();
    await expect(warehouse.locator('[data-tab="staff"]')).toHaveCount(0);
    await expect(warehouse.locator('[data-tab="refunds"]')).toHaveCount(0);
  } finally {
    await context.close();
  }
});

test('shipment exceptions push to the customer and expose an owned timeline', async ({ page, browser }) => {
  await page.addInitScript(() => {
    window.pushUpdates = [];
    window.addEventListener('lumo:notifications-updated', (e) => window.pushUpdates.push(e.detail));
  });
  await login(page, 'demo', 'Lumo@2026');
  await page.evaluate(() => window.LumoRealtime.connect());
  await expect.poll(() => page.evaluate(() => window.pushUpdates.length)).toBeGreaterThan(0);
  const quote = await api(page, 'checkout', 'quote', {
    data: { items: [{ sku_id: 'sku-coffee', quantity: 1 }], coupon_mode: 'none' },
  });
  const order = await api(page, 'checkout', 'submit', {
    idempotency_key: 'browser-shipment-order',
    data: { quote_id: quote.quote_id, address: { recipient: '轨迹验收', phone: '13800138000', address: '测试路101' } },
  });
  const payment = await api(page, 'payment', 'create', { id: order.order_id });
  await api(page, 'payment', 'submit_demo', { id: payment.id });
  const context = await browser.newContext({ baseURL: process.env.BROWSER_BASE_URL });
  try {
    const admin = await context.newPage();
    await login(admin, 'admin', 'Admin@2026');
    await expect
      .poll(
        async () =>
          (await api(admin, 'fulfillment', 'list')).items.find((row) => row.order_id === order.order_id)?.status,
      )
      .toBe('READY');
    const shipment = (await api(admin, 'fulfillment', 'list')).items.find((row) => row.order_id === order.order_id);
    await api(admin, 'fulfillment', 'dispatch', { id: shipment.id });
    const waybill = await api(admin, 'logistics', 'get', { id: shipment.id });
    await admin.goto('/admin.html');
    await admin.locator('#admin-login [name="username"]').fill('admin');
    await admin.locator('#admin-login [name="password"]').fill('Admin@2026');
    await admin.locator('#admin-login button[type="submit"]').click();
    await admin.locator('[data-tab="shipments"]').click();
    const form = admin.locator('#shipment-update-form');
    await form.locator('[name="tracking_id"]').fill(waybill.tracking_id);
    await form.locator('[name="status"]').selectOption('EXCEPTION');
    const localScan = await admin.evaluate(() => {
      const at = new Date();
      return new Date(at.getTime() - at.getTimezoneOffset() * 60000).toISOString().slice(0, 19);
    });
    await form.locator('[name="occurred_at"]').fill(localScan);
    await form.locator('[name="description"]').fill('浏览器配送异常验收');
    await form.locator('button').click();
    await expect(admin.locator('#shipments-admin-root')).toContainText('浏览器配送异常验收');
    await expect
      .poll(async () =>
        (await api(page, 'notification', 'list')).items.some((row) => row.event_type === 'ShipmentException'),
      )
      .toBe(true);
    await page.goto('/#/member/messages');
    await expect(page.locator('#main')).toContainText('配送异常');
    await expect.poll(() => page.evaluate(() => window.pushUpdates.length)).toBeGreaterThan(1);
    await page.evaluate((id) => window.LumoShipmentTracking.open(id), shipment.id);
    await expect(page.locator('.tracking-timeline')).toContainText('浏览器配送异常验收');
    admin.once('dialog', (dialog) => dialog.accept('已联系收件人确认次日重派'));
    await admin.locator('[data-shipment-resolve]').click();
    await expect(admin.locator('[data-shipment-resolve]')).toHaveCount(0);
  } finally {
    await context.close();
  }
});
