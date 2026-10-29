'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((resolve) => setImmediate(resolve));
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};

test('recalculating checkout preserves the last amounts and rejects stale responses', async (t) => {
  const app = createHarness(directory, 'http://price.test');
  t.after(() => app.dispose());
  app.ctx.LumoAuth = { user: () => ({ id: 'buyer' }), isLoggedIn: () => true, gate: () => true, afterRender() {} };
  app.load('api-client.js');
  app.load('skeleton.js');
  app.load('app.js');
  app.evaluate(
    "products.push(normalizeProduct({id:'sku-price',name:'商品',price_cents:20000}));state.cart=[{id:'sku-price',sku_id:'sku-price',qty:1,selected:true}];state.address={id:'adr',name:'测试',phone:'13800138000',region:'广东',detail:'测试地址'}",
  );
  const quote = (id) => ({
    quote_id: id,
    subtotal_cents: 20000,
    payable_cents: 18000,
    coupon_discount_cents: 2000,
    coupon_id: 'CPN_123',
    expires_at: new Date(Date.now() + 60000).toISOString(),
    items: [{ sku_id: 'sku-price', quantity: 1, unit_price_cents: 20000 }],
  });
  const calls = [];
  let handler = () => quote('QTE_1');
  app.ctx.LumoAPI.call = async (service, action, command) => {
    calls.push({ service, action, command });
    return handler();
  };
  await app.ctx.LumoCommerce.ensureQuote();
  assert.equal(calls[0].service, 'coupon');
  assert.equal(calls[0].action, 'calculate');
  assert.equal(calls[0].command.data.coupon_mode, 'auto');
  const pending = deferred();
  handler = () => pending.promise;
  app.evaluate('state.points=true');
  app.ctx.LumoCommerce.invalidateQuote();
  const next = app.ctx.LumoCommerce.ensureQuote();
  const html = app.evaluate('checkoutPage()');
  const summary = html.slice(html.indexOf('<aside'));
  assert.match(summary.replace(/<[^>]*>/g, ''), /¥180/);
  assert.match(summary, /正在更新价格/);
  assert.doesNotMatch(summary, /sk-loading-summary/);
  assert.match(summary, /data-action="submit-order" disabled/);
  assert.equal(calls.at(-1).command.data.points_mode, 'max');
  app.ctx.LumoCommerce.invalidateQuote();
  const current = deferred();
  handler = () => current.promise;
  const latest = app.ctx.LumoCommerce.ensureQuote();
  current.resolve({ ...quote('QTE_3'), payable_cents: 17000 });
  await latest;
  pending.resolve({ ...quote('QTE_2'), payable_cents: 16000 });
  await next;
  assert.equal(app.ctx.LumoCommerce.state.quote.quote_id, 'QTE_3');
  assert.equal(app.ctx.LumoCommerce.state.quote.payable_cents, 17000);
  app.ctx.LumoCommerce.reset();
  assert.equal(app.ctx.LumoCommerce.state.quote, null);
});

test('login-method failures stop retrying until an explicit refresh and enabled methods drive rendering', async (t) => {
  const app = createHarness(directory, 'http://auth-methods.test');
  t.after(() => app.dispose());
  app.load('api-client.js');
  app.load('auth.js');
  app.load('app.js');
  app.load('auth-views.js');
  let calls = 0,
    fail = true;
  app.ctx.LumoAPI.call = async (service, action) => {
    assert.equal(service, 'iam');
    assert.equal(action, 'login_methods');
    calls++;
    if (fail) throw Error('服务暂不可用');
    return { items: [{ id: 'phone', name: '手机验证码' }], allow_registration: true, demo_sms: true };
  };
  app.ctx.location.hash = '#/login';
  await assert.rejects(app.ctx.LumoAuth.loadMethods(), /服务暂不可用/);
  app.ctx.LumoAuth.afterRender();
  await tick();
  assert.equal(calls, 1, 'rendering an error must not create a request loop');
  fail = false;
  await app.ctx.LumoAuth.loadMethods(true);
  const html = app.ctx.LumoAuthView.page();
  assert.match(html, /手机验证码登录/);
  assert.match(html, /id="auth-phone"/);
  assert.doesNotMatch(html, /账号密码登录|data-auth-action="oauth"/);
});

function accessFixture(key, call) {
  const app = createHarness(directory, 'http://access.test', { html: 'admin.html' }),
    listeners = [];
  const add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (type === 'submit') listeners.push(fn);
    add(type, fn, ...rest);
  };
  app.load('skeleton.js');
  app.load('admin-ui.js');
  app.load('access-admin.js');
  const m = app.ctx.LumoAccessAdmin.create(key);
  app.ids.set('access-' + key, new Element());
  let scope = 'admin-a';
  m.configure({ isAdmin: () => true, scope: () => scope, draw() {}, call });
  return {
    app,
    m,
    setScope: (v) => (scope = v),
    async submit(fields) {
      const form = new Element();
      form.fields = fields;
      form.matches = (q) => q === '[data-access-form]';
      for (const fn of listeners) await fn({ target: form, preventDefault() {} });
    },
  };
}
test('payment switches save server configuration with version and reason, and isolate another session', async (t) => {
  const calls = [];
  const f = accessFixture('paymentSettings', async (service, action, c) => {
    calls.push({ service, action, c });
    return {
      config: { demo_enabled: true },
      version: 7,
      capabilities: [{ id: 'wechat', configured: false, reason: '缺少适配器' }],
    };
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  assert.match(f.m.render(), /name="wechat_enabled"[^>]*disabled/);
  await f.submit({ reason: '暂停支付' });
  const write = calls.find((c) => c.action === 'policy_upsert');
  assert.equal(write.service, 'payment');
  assert.equal(write.c.data.version, 7);
  assert.equal(write.c.data.reason, '暂停支付');
  assert.equal(write.c.data.config.demo_enabled, false);
  f.setScope('admin-b');
  assert.equal(f.m.state.ready, false);
  assert.equal(f.m.state.version, 0);
});

test('late subscription lists never enter another buyer session', async (t) => {
  let token = 'buyer-a';
  const response = deferred();
  const app = createHarness(directory, 'http://alerts.test', {
    globals: { LumoAPI: { getBase: () => '', getToken: () => token, call: () => response.promise } },
  });
  t.after(() => app.dispose());
  app.load('app.js');
  app.load('skeleton.js');
  app.load('product-alerts.js');
  const request = app.ctx.LumoProductAlerts.refresh();
  token = 'buyer-b';
  app.ctx.LumoProductAlerts.page();
  response.resolve({ items: [{ id: 'private-a', product_name: '上一位用户的私密订阅' }], total: 1 });
  await request;
  assert.doesNotMatch(app.ctx.LumoProductAlerts.page(), /上一位用户|private-a/);
});

test('a new checkout restores automatic coupon selection after an explicit no-coupon choice', (t) => {
  const app = createHarness(directory, 'http://coupon-mode.test');
  t.after(() => app.dispose());
  app.load('api-client.js');
  app.load('app.js');
  app.load('member.js');
  app.ctx.LumoMember.selectCoupon('0');
  assert.equal(app.ctx.LumoMember.checkoutMeta([]).couponMode, 'none');
  app.ctx.LumoMember.resetCheckoutCoupon();
  assert.equal(app.ctx.LumoMember.checkoutMeta([]).couponMode, 'auto');
  app.ctx.LumoMember.applyCouponQuote({ coupon_id: 'CPN_123', coupon_discount_cents: 2000 });
  assert.equal(
    app.ctx.LumoMember.checkoutMeta([]).couponId,
    null,
    'an automatically chosen coupon never becomes a manual override',
  );
});
