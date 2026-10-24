'use strict';
const path = require('node:path');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const { createHarness } = require('./dom-harness.cjs');
const options = Object.fromEntries(
  process.argv.slice(2).map((arg) => {
    const eq = arg.indexOf('=');
    return [arg.slice(2, eq), arg.slice(eq + 1)];
  }),
);
const directory = options.frontend || process.env.FRONTEND_DIR || path.join(__dirname, '..', 'dist');
const baseURL = options.url || process.env.LUMO_TEST_URL || 'http://127.0.0.1:8080';
let checks = 0;
const check = (condition, message) => {
  assert.ok(condition, message);
  checks++;
};
const equal = (value, expected, message) => {
  assert.equal(value, expected, message);
  checks++;
};
async function until(fn, message, timeout = 15000) {
  const deadline = Date.now() + timeout;
  let value;
  do {
    value = await fn();
    if (value) return value;
    await new Promise((resolve) => setTimeout(resolve, 150));
  } while (Date.now() < deadline);
  throw new Error('Timed out: ' + message);
}
async function main() {
  assert.ok(fs.existsSync(path.join(directory, 'index.html')), 'Pass --frontend=/absolute/dist or set FRONTEND_DIR');
  const boot = createHarness(directory, baseURL);
  try {
    await boot.loadAll();
    const result = await boot.ctx.LumoAppReady;
    check(result?.ready, 'Full application bootstrap must connect to the actual catalog');
    equal(
      result?.errors?.length,
      0,
      'All bootstrap modules must refresh without API/contract errors: ' +
        result?.errors?.map((e) => e.message).join('; '),
    );
    check(!boot.failures.length, 'Bootstrap lifecycle must not throw');
    const reads = boot.ctx.LumoAPI.requests();
    equal(reads.length, 1, 'Guest homepage uses one physical HTTP request');
    equal(
      reads[0].service + '/' + reads[0].action,
      'storefront/bootstrap',
      'Homepage data is read through the top-level bootstrap',
    );
    check(
      boot.ids.get('app').innerHTML.includes('精选推荐') &&
        boot.ids.get('app').innerHTML.includes('home-recommendation-card'),
      'Guest homepage must render actual recommendation results and reasons',
    );
    const railLinks = [...boot.ctx.LumoSidebars.render().matchAll(/<a class="interest-item" href="([^"]+)"/g)].map(
      (row) => row[1],
    );
    equal(railLinks.length, 6, 'Guest rail is filled by six actual server-selected products');
    equal(new Set(railLinks).size, 6, 'Variant SKUs cannot consume duplicate guest rail slots');
  } finally {
    boot.dispose();
  }
  const app = createHarness(directory, baseURL);
  const api = (...args) => app.ctx.LumoAPI.call(...args);
  const uid = (name) => app.ctx.LumoAPI.key('http-test-' + name);
  try {
    await app.loadAll({ bootstrap: false });
    app.ctx.LumoAPI.enableReadBatching();
    for (const name of [
      'LumoAPI',
      'LumoAuth',
      'LumoCommerce',
      'LumoInventory',
      'LumoMember',
      'LumoMemberView',
      'LumoCouponCenter',
      'LumoServices',
      'LumoDiscovery',
      'LumoProductExtras',
      'LumoPromotions',
      'LumoPromoView',
      'LumoRecommendations',
      'LumoStudio',
    ])
      check(app.ctx[name], name + ' must load in the real HTML script order');
    equal(app.ctx.LumoAuth.isLoggedIn(), false, 'Guest must not receive a synthetic signed-in account');
    equal(app.json('state.orders').length, 0, 'Guest must not receive prototype orders');
    equal(app.json('products').length, 0, 'Products wait for actual catalog response');
    await app.ctx.LumoCommerce.refreshCatalog();
    await app.ctx.LumoInventory.refresh();
    await Promise.all([
      app.ctx.LumoDiscovery.refresh(),
      app.ctx.LumoPromotions.refresh(),
      app.ctx.LumoCouponCenter.refresh(),
      app.ctx.LumoStudio.refresh(),
      app.ctx.LumoProductExtras.refresh(),
      app.ctx.LumoRecommendations.refresh(),
    ]);
    check(app.json('products').length >= 4, 'Actual catalog projection must contain server products');
    const initialCatalog = app.json('products');
    const phone = initialCatalog.find((p) => p.id === 'iphone') || initialCatalog.find((p) => p.price >= 1000);
    check(phone && phone.skuId, 'Product card must retain the authoritative SKU');
    const coffee = initialCatalog.find((p) => p.id === 'coffee') || initialCatalog[0];
    const page = app.navigate('');
    check(page.includes(initialCatalog[0].name), 'Catalog must render the server product name on its current page');
    check(
      page.includes('cc-home') && page.includes('#/coupons'),
      'Homepage must expose the server-backed coupon center',
    );
    const guestCoupons = app.ctx.LumoCouponCenter.model().items;
    check(
      guestCoupons.some((c) => c.id === 'everyday-10' && c.claim_status === 'login_required'),
      'Guests browse open coupons before login',
    );
    app.navigate('coupons');
    await app.ctx.LumoCouponCenter.refresh();
    check(app.navigate('coupons').includes('明日惊喜券'), 'Public center must show upcoming coupons with their rules');
    check(
      app.navigate('product/' + phone.id).includes(phone.name),
      'Product detail must render the requested server product',
    );
    app.navigate('');
    await assert.rejects(app.ctx.LumoAuth.login('demo', 'wrong'), (error) => error.status === 401);
    checks++;
    equal(app.ctx.LumoAuth.isLoggedIn(), false, 'Rejected IAM login must not establish local auth');
    await app.ctx.LumoAuth.login('demo', 'Lumo@2026', false);
    equal(app.ctx.LumoAuth.user().id, 'demo', 'Buyer identity comes from IAM');
    const memberBoot = createHarness(directory, baseURL);
    try {
      memberBoot.ctx.sessionStorage.setItem(
        'lumo-api-session-v2',
        app.ctx.sessionStorage.getItem('lumo-api-session-v2'),
      );
      await memberBoot.loadAll();
      equal(
        memberBoot.ctx.LumoAppReady ? (await memberBoot.ctx.LumoAppReady).errors.length : -1,
        0,
        'Member bootstrap completes without hidden read errors',
      );
      const reads = memberBoot.ctx.LumoAPI.requests();
      equal(reads.length, 2, 'Member homepage uses one identity check and one bootstrap request');
      equal(
        reads.filter((r) => r.service === 'storefront' && r.action === 'bootstrap').length,
        1,
        'Member homepage data is aggregated once',
      );
      equal(
        reads.filter((r) => r.service === 'iam' && r.action === 'current').length,
        1,
        'Saved session is validated once',
      );
      const railLinks = [
        ...memberBoot.ctx.LumoSidebars.render().matchAll(/<a class="interest-item" href="([^"]+)"/g),
      ].map((row) => row[1]);
      equal(railLinks.length, 6, 'Member rail receives six products even with no browsing history');
      equal(new Set(railLinks).size, 6, 'Member rail products are unique');
    } finally {
      memberBoot.dispose();
    }
    await app.ctx.LumoCommerce.refresh();
    // This harness skips api-bootstrap: reproduce its inventory hydration after
    // login, including cancellation of the earlier guest detail read.
    await app.ctx.LumoInventory.refresh();
    await app.ctx.LumoMember.refresh();
    await app.ctx.LumoCouponCenter.refresh(true);
    check(
      app.ctx.LumoCouponCenter.model().items.some((c) => c.id === 'member-80' && c.claim_status === 'conditions_unmet'),
      'Center must display authoritative membership eligibility',
    );
    await app.ctx.LumoCouponCenter.claim('everyday-10');
    const claimedCoupon = app.ctx.LumoMember.coupons().find((c) => c.templateId === 'everyday-10');
    check(claimedCoupon?.id, 'Center claim action must refresh the actual owned wallet');
    equal(
      app.ctx.LumoCouponCenter.model().items.find((c) => c.id === 'everyday-10').claim_status,
      'claimed',
      'Center must show successful claim after server refresh',
    );
    await Promise.all([app.ctx.LumoServices.refresh(), app.ctx.LumoDiscovery.refresh()]);
    for (const route of [
      'coupons',
      'member',
      'member/addresses',
      'member/coupons',
      'member/points',
      'member/favorites',
      'member/history',
      'member/orders',
      'member/aftersales',
      'member/settings',
      'member/messages',
      'member/invoices',
      'member/support',
      'cart',
      'checkout',
      'promotions/flash',
      'promotions/group',
      'promotions/exchange',
      'promotions/mygroups',
      'compare',
    ]) {
      check(
        app.navigate(route).includes('id="main"'),
        route + ' must render with server-backed empty/projection models',
      );
    }
    app.navigate('');
    await api('cart', 'clear');
    await app.ctx.LumoCommerce.refreshCart();
    await app.ctx.LumoCommerce.addItem({ id: phone.id, color: phone.colors[0], option: phone.defaultOption, qty: 1 });
    equal(
      app.json('state.cart').length,
      1,
      'Add-to-cart must refresh the server cart; inventory=' +
        JSON.stringify(app.ctx.LumoInventory.snapshot(phone.id)) +
        '; toast=' +
        app.ids.get('toast').innerHTML,
    );
    equal(
      app.json('state.cart')[0].sku_id,
      app.evaluate('LumoCommerce.skuForItem(state.cart[0])'),
      'Cart item preserves the purchased SKU',
    );
    const serverCart = await api('cart', 'list');
    equal(serverCart.items.length, 1, 'Cart mutation must exist on the backend');
    const item = app.json('state.cart')[0];
    await app.ctx.LumoCommerce.setQuantity(item, 2);
    equal((await api('cart', 'list')).items[0].quantity, 2, 'Quantity editing must persist, not only alter DOM state');
    await app.ctx.LumoCommerce.setQuantity(item, 1);
    const address = await api('address', 'create', {
      data: {
        recipient: 'HTTP Test Buyer',
        phone: '13800138000',
        province: '广东省',
        city: '深圳市',
        district: '南山区',
        address: '示例测试地址 ' + uid('address'),
        is_default: true,
      },
    });
    await app.ctx.LumoMember.refresh();
    check(
      app.ctx.LumoMember.addresses().some((a) => a.id === address.id && a.name === 'HTTP Test Buyer'),
      'Address response fields must map to the member form model',
    );
    app.evaluate('state.address=' + JSON.stringify(app.ctx.LumoMember.addresses().find((a) => a.id === address.id)));
    app.evaluate("setCheckout('cart');state.points=false");
    app.ctx.LumoMember.selectCoupon(claimedCoupon.id);
    await app.ctx.LumoPromotions.selectCampaign('direct-demo', app.json('state.cart'));
    const quote = await app.ctx.LumoCommerce.ensureQuote(true);
    check(quote.quote_id, 'Checkout must retain the immutable backend quote ID');
    equal(quote.activity_id, 'direct-demo', 'Checkout must include the selected server campaign');
    equal(quote.campaign_discount_cents, 2000, 'Direct reduction must use the backend campaign amount');
    equal(quote.coupon_discount_cents, 1000, 'Newly claimed center coupon must reach authoritative checkout pricing');
    equal(
      app.evaluate('totals(selectedItems()).total'),
      quote.payable_cents / 100,
      'Displayed checkout total must come from server integer cents',
    );
    await app.ctx.LumoCommerce.submit();
    const pending = app.json('state.pending');
    check(
      pending?.id && pending.backendStatus === 'PENDING_PAYMENT',
      'Submit must project an actual pending backend order',
    );
    const orderID = pending.id;
    const order = await api('order', 'get', { id: orderID });
    equal(order.payable_cents, quote.payable_cents, 'Order must preserve the quoted payable amount');
    equal(order.quote.activity_kind, 'direct', 'Order must retain the campaign type from its quote snapshot');
    const unpaidView = app.navigate('payment');
    check(unpaidView.includes(orderID), 'Payment page must render the actual order number');
    check(unpaidView.includes('模拟支付'), 'Payment page must expose the buyer demo payment flow');
    await app.ctx.LumoCommerce.createPayment();
    const payment = app.ctx.LumoCommerce.state.payment;
    check(payment?.id, 'Pay action must create a provider payment request');
    equal(
      (await api('order', 'get', { id: orderID })).status,
      'PENDING_PAYMENT',
      'Creating payment must not fake payment success',
    );
    equal(
      app.ctx.LumoMember.orderState(app.ctx.LumoCommerce.normalizeOrder(await api('order', 'get', { id: orderID }))),
      'pending',
      'Unconfirmed payment must remain pending in order UI',
    );
    const adminUI = createHarness(directory, baseURL, { html: 'admin.html' });
    await adminUI.loadAll({ bootstrap: false });
    check(
      adminUI.ids.get('admin-main').innerHTML.includes('登录商城管理'),
      'Admin HTML must start with its own login form',
    );
    await adminUI.submit('admin-login', { username: 'admin', password: 'Admin@2026' });
    check(
      adminUI.ids.get('admin-main').innerHTML.includes('服务端实时记录'),
      'Admin login UI must render real overview responses',
    );
    equal(
      adminUI.ctx.LumoAPI.getToken(),
      '',
      'Admin page must use an independent explicit bearer, not the storefront bearer',
    );
    const admin = { token: adminUI.ctx.sessionStorage.getItem('lumo-admin-session-v1') };
    check(admin.token, 'Admin login must persist its server-issued session separately');
    const staff = (service, action, command = {}) => api(service, action, command, { token: admin.token });
    await assert.rejects(api('payment', 'confirm_demo', { id: payment.id }), (error) => error.status === 403);
    checks++;
    await adminUI.click({ tab: 'payments' });
    await until(
      () => adminUI.ids.get('admin-main').innerHTML.includes(payment.id),
      'admin payment list renders the buyer payment',
    );
    await app.click({ action: 'pay' });
    await until(
      () => app.ctx.location.hash.includes('success'),
      'buyer demo submit observes server payment acceptance',
    );
    equal(
      (await api('payment', 'get', { id: payment.id })).status,
      'SUCCEEDED',
      'Buyer payment button must submit a real server-side demo receipt',
    );
    equal(
      (await api('payment', 'submit_demo', { id: payment.id })).id,
      payment.id,
      'Repeated demo submission must reuse the confirmed payment',
    );
    const ready = await until(async () => {
      const current = await api('order', 'get', { id: orderID });
      return current.status === 'READY_TO_FULFILL' ? current : false;
    }, 'payment accepted and resources committed');
    check(ready.payment_id, 'Paid order must reference the accepted payment');
    await until(async () => {
      try {
        return (await api('fulfillment', 'get', { id: orderID })).status === 'READY';
      } catch (error) {
        if (error.status === 404) return false;
        throw error;
      }
    }, 'paid order creates a fulfillment record');
    equal((await api('cart', 'list')).items.length, 0, 'Paid cart checkout must consume the purchased item');
    await app.ctx.LumoCommerce.refreshPayment();
    check(
      app.ctx.location.hash.includes('success'),
      'Success navigation must follow observed server payment acceptance',
    );
    await app.ctx.LumoCommerce.refreshOrders();
    await app.ctx.LumoMember.refresh();
    const paidModel = app.ctx.LumoCommerce.allOrders().find((o) => o.id === orderID);
    equal(app.ctx.LumoMember.orderState(paidModel), 'shipping', 'Paid backend state must map to awaiting shipment');
    const aftersale = await api('aftersale', 'create', {
      idempotency_key: uid('aftersale'),
      data: { order_id: orderID, type: 'refund', reason: 'HTTP integration refund verification' },
    });
    await app.ctx.LumoMember.refresh();
    check(
      app.ctx.LumoMember.model().aftersales.some((a) => a.id === aftersale.id && a.status === 'processing'),
      'Requested aftersale must appear in the member projection',
    );
    await staff('aftersale', 'approve', { id: aftersale.id });
    const refundRequested = await staff('aftersale', 'refund', { id: aftersale.id });
    const refundID = refundRequested.refund_id || (await api('aftersale', 'get', { id: aftersale.id })).refund_id;
    check(refundID, 'Approved refund must create an actual refund record');
    await staff('refund', 'confirm_demo', { id: refundID });
    await until(async () => {
      const o = await api('order', 'get', { id: orderID });
      return Number(o.refunded_total_cents) >= Number(o.payable_cents);
    }, 'refund monetary event updates order');
    await until(
      async () => (await api('aftersale', 'get', { id: aftersale.id })).status === 'COMPLETED',
      'refund event closes aftersale',
    );
    await app.ctx.LumoCommerce.refreshOrders();
    await app.ctx.LumoMember.refresh();
    equal(
      app.ctx.LumoMember.orderState(app.ctx.LumoCommerce.allOrders().find((o) => o.id === orderID)),
      'refunded',
      'Refunded order must render as refund complete',
    );
    check(app.navigate('member/aftersales').includes('退款'), 'Aftersale page must render the server refund result');
    const buyerToken = app.ctx.LumoAPI.getToken();
    await assert.rejects(api('reconciliation', 'list', {}, { token: '0'.repeat(64) }), (error) => error.status === 401);
    checks++;
    equal(app.ctx.LumoAPI.getToken(), buyerToken, 'Independent expired admin token must not log out buyer');
    await api('address', 'delete', { id: address.id });
    await adminUI.click({ action: 'logout' });
    check(adminUI.ids.get('admin-main').innerHTML.includes('登录商城管理'), 'Admin logout must restore the login form');
    equal(app.ctx.LumoAPI.getToken(), buyerToken, 'Admin logout must preserve the buyer session');
    adminUI.dispose();
    await app.ctx.LumoAuth.confirmLogout();
    equal(app.ctx.LumoAuth.isLoggedIn(), false, 'Logout must revoke the backend session');
    await assert.rejects(api('order', 'list', {}, { token: buyerToken }), (error) => error.status === 401);
    checks++;
    equal(app.json('state.orders').length, 0, 'Logout must clear private order projections');
    check(
      app.ctx.LumoAPI.requests().some((row) => row.ok && row.traceId?.length === 32),
      'Actual gateway calls must expose usable Trace IDs',
    );
    check(
      !JSON.stringify(app.ctx.LumoAPI.requests()).includes(buyerToken),
      'Trace diagnostic history must never persist bearer tokens',
    );
    check(
      app.failures.length === 0,
      'No unhandled module lifecycle errors: ' + app.failures.map((e) => e.stack).join('\n'),
    );
    console.log(
      'PASS frontend HTTP integration: ' + checks + ' checks; order=' + orderID + '; scripts=' + app.scripts.length,
    );
  } finally {
    app.dispose();
  }
}
// The DOM harness unrefs browser timers; keep the HTTP suite alive while a
// queued read batch is waiting for its next event-loop turn.
const keepAlive = setInterval(() => {}, 1000);
main()
  .catch((error) => {
    console.error(error.stack || error);
    process.exitCode = 1;
  })
  .finally(() => clearInterval(keepAlive));
