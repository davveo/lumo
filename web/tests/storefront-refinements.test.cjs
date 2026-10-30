'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((resolve) => setImmediate(resolve));
const delay = () => new Promise((resolve) => setTimeout(resolve, 230));
function quote(name, amount, quantity = 1) {
  return {
    subtotal_cents: 10000 * quantity,
    discount_cents: amount,
    coupon_options: [{ id: 'coupon-a', name, eligible: true, selected: true, discount_cents: amount }],
  };
}

test('product uses server pricing for its selected SKU and quantity, discarding late results', async (t) => {
  const calls = [],
    replies = [];
  let token = 'owner-a';
  const app = createHarness(directory, 'http://coupon.test', {
    globals: {
      LumoAuth: { isLoggedIn: () => true, user: () => ({ id: token }), gate: () => true, afterRender() {} },
      LumoAPI: {
        getBase: () => '',
        getToken: () => token,
        call(service, action, command) {
          calls.push({ service, action, command });
          return new Promise((resolve) => replies.push(resolve));
        },
      },
    },
  });
  t.after(() => app.dispose());
  app.load('app.js');
  app.evaluate(
    "products.push(normalizeProduct({id:'iphone',name:'测试手机',category:'手机数码',price_cents:10000,colors:['黑色'],options:['标准'],variants:[{sku_id:'sku-phone',color:'黑色',option:'标准',price_cents:10000}]})); commerceState.catalogLoaded = true",
  );
  app.load('product-coupons.js');
  app.navigate('product/iphone');
  await delay();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].service, 'coupon');
  assert.equal(calls[0].action, 'calculate');
  assert.equal(calls[0].command.data.coupon_mode, 'auto');
  assert.equal(calls[0].command.data.items[0].quantity, 1);
  app.evaluate('detail.qty = 2; render()');
  replies[0](quote('过期请求优惠券', 100));
  await tick();
  assert.doesNotMatch(app.ids.get('app').innerHTML, /过期请求优惠券/);
  await delay();
  assert.equal(calls[1].command.data.items[0].quantity, 2);
  replies[1](quote('<最佳优惠券>', 2500, 2));
  await tick();
  const html = app.ids.get('app').innerHTML;
  assert.match(html, /&lt;最佳优惠券&gt;/);
  assert.match(html, /可减 ¥25/);
  assert.match(html, /券后商品合计 ¥175/);
  assert.doesNotMatch(html, /过期请求优惠券/);
  token = 'owner-b';
  app.evaluate('render()');
  assert.doesNotMatch(app.ids.get('app').innerHTML, /&lt;最佳优惠券&gt;/);
});

test('anonymous product preview requests login without calculating another member’s coupons', async (t) => {
  let calls = 0;
  const app = createHarness(directory, 'http://coupon.test', {
    globals: {
      LumoAuth: { isLoggedIn: () => false, user: () => null, gate: () => true, afterRender() {} },
      LumoAPI: {
        getBase: () => '',
        getToken: () => '',
        call() {
          calls++;
          return Promise.resolve({});
        },
      },
    },
  });
  t.after(() => app.dispose());
  app.load('app.js');
  app.evaluate(
    "products.push(normalizeProduct({id:'iphone',name:'测试手机',category:'手机数码',price_cents:10000,colors:['黑色'],options:['标准'],variants:[{sku_id:'sku-phone',color:'黑色',option:'标准',price_cents:10000}]})); commerceState.catalogLoaded = true",
  );
  app.load('product-coupons.js');
  assert.match(app.navigate('product/iphone'), /登录查看最佳优惠券/);
  await delay();
  assert.equal(calls, 0);
});

test('catalog renders two rows, removes decorative copy and recalculates page count on mobile', (t) => {
  const app = createHarness(directory, 'http://catalog.test', {
    globals: { LumoAPI: { getBase: () => '', getToken: () => '' } },
  });
  t.after(() => app.dispose());
  app.load('app.js');
  app.load('discovery.js');
  const list = Array.from({ length: 25 }, (_, i) => ({ id: 'product-' + i, price: i }));
  assert.equal(app.ctx.LumoDiscovery.paginate(list).length, 12);
  assert.match(app.ctx.LumoDiscovery.pagination(25), /第 1 \/ 3 页/);
  app.ctx.matchMedia = (query) => ({ matches: query.includes('540') });
  assert.equal(app.ctx.LumoDiscovery.paginate(list).length, 4);
  assert.match(app.ctx.LumoDiscovery.pagination(25), /第 1 \/ 7 页/);
  const html = app.navigate('');
  assert.doesNotMatch(
    html,
    /好设计，让日常多一点美好|根据你的浏览、收藏、搜索和购买偏好挑选|生活的美好，藏在用心的选择里/,
  );
});

test('registration has a visible entry, confirmation validation and server-controlled availability', async (t) => {
  const writes = [];
  const app = createHarness(directory, 'http://register.test', {
    globals: {
      LumoAPI: {
        getBase: () => '',
        getToken: () => '',
        setToken() {},
        call(service, action, command) {
          if (action === 'login_methods')
            return Promise.resolve({ items: [{ id: 'password', name: '账号密码' }], allow_registration: true });
          writes.push({ service, action, command });
          return Promise.resolve({ user_id: 'USR_123', username: command.data.username });
        },
      },
    },
  });
  t.after(() => app.dispose());
  app.load('auth.js');
  app.load('app.js');
  app.load('auth-views.js');
  await app.ctx.LumoAuth.loadMethods();
  assert.match(app.navigate('login'), /href="#\/register">免费注册/);
  assert.match(app.navigate('register'), /id="auth-register-form"/);
  await app.submit('auth-register-form', {
    username: 'NEW.BUYER',
    password: 'Member@2026',
    confirm_password: 'different',
  });
  assert.equal(writes.length, 0);
  assert.match(app.ids.get('app').innerHTML, /两次输入的密码不一致/);
  await app.submit('auth-register-form', {
    username: 'NEW.BUYER',
    password: 'Member@2026',
    confirm_password: 'Member@2026',
  });
  await tick();
  assert.equal(writes[0].action, 'register');
  assert.equal(writes[0].command.data.username, 'new.buyer');
  assert.equal(app.ctx.location.hash, '#/login');
  app.ctx.LumoAuth.model = () => ({ methodsLoaded: true, methods: [{ id: 'password' }], allowRegistration: false });
  assert.doesNotMatch(app.ctx.LumoAuthView.page(), /免费注册/);
  app.ctx.location.hash = '#/register';
  assert.match(app.ctx.LumoAuthView.page(), /暂未开放新会员注册/);
  assert.doesNotMatch(app.ctx.LumoAuthView.page(), /id="auth-register-form"/);
});
