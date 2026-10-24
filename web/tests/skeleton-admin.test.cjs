'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
const tick = () => new Promise((resolve) => setImmediate(resolve));
const escaped = (value) =>
  String(value ?? '').replace(
    /[&<>"']/g,
    (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
  );

function adminFixture() {
  const calls = [],
    handlers = new Map();
  const app = createHarness(directory, 'http://admin-loading.test', {
    html: 'admin.html',
    globals: {
      LumoAPI: {
        enableReadBatching() {},
        getBase: () => '/api/v1',
        requests: () => [],
        async call(service, action, command) {
          calls.push([service, action]);
          const handler = handlers.get(service + '/' + action);
          return handler ? handler(command) : service === 'iam' ? { role: 'admin' } : { items: [] };
        },
      },
    },
  });
  app.ctx.sessionStorage.setItem('lumo-admin-session-v1', 'admin-session');
  app.load('skeleton.js');
  return {
    app,
    calls,
    handlers,
    start() {
      app.load('admin-app.js');
      return app.ctx.LumoAdminReady;
    },
    html: () => app.ids.get('admin-main').innerHTML,
  };
}

test('admin verifies session and first statistics through noninteractive skeletons before showing numbers', async (t) => {
  const f = adminFixture();
  t.after(() => f.app.dispose());
  const session = deferred(),
    orders = deferred();
  f.handlers.set('iam/current', () => session.promise);
  f.handlers.set('order/list', () => orders.promise);
  const ready = f.start();
  assert.match(f.html(), /sk-loading/);
  assert.doesNotMatch(f.html(), /登录商城管理|服务端实时记录|暂无记录/);
  session.resolve({ role: 'admin' });
  await tick();
  assert.match(f.html(), /sk-loading-summary/);
  assert.doesNotMatch(f.html(), /服务端实时记录/);
  orders.resolve({ items: [] });
  await ready;
  assert.doesNotMatch(f.html(), /sk-loading/);
  assert.match(f.html(), /服务端实时记录/);
  assert.equal(f.calls.filter(([service, action]) => service === 'order' && action === 'list').length, 1);
});

test('admin table loads once, preserves real rows on refresh and retains them with a retryable failure', async (t) => {
  const f = adminFixture();
  t.after(() => f.app.dispose());
  await f.start();
  const first = deferred();
  f.handlers.set('payment/list', () => first.promise);
  await f.app.click({ tab: 'payments' });
  await tick();
  assert.match(f.html(), /sk-loading-table/);
  assert.doesNotMatch(f.html(), /暂无记录/);
  first.resolve({ items: [{ id: 'pay-one', status: 'PENDING', name: '实际支付记录', amount_cents: 6500 }] });
  await tick();
  assert.match(f.html(), /实际支付记录/);
  assert.doesNotMatch(f.html(), /sk-loading/);
  const refresh = deferred();
  f.handlers.set('payment/list', () => refresh.promise);
  await f.app.click({ action: 'reload' });
  await tick();
  assert.match(f.html(), /实际支付记录/);
  assert.doesNotMatch(f.html(), /sk-loading/);
  assert.equal(f.app.ids.get('admin-main').getAttribute('aria-busy'), 'true');
  refresh.reject(Error('<img src=x> 支付读取失败'));
  await tick();
  assert.match(f.html(), /实际支付记录/);
  assert.match(f.html(), /&lt;img src=x&gt; 支付读取失败/);
  assert.doesNotMatch(f.html(), /<img src=x>/);
  assert.equal(f.app.ids.get('admin-main').getAttribute('aria-busy'), null);
  assert.equal(f.calls.filter(([service]) => service === 'payment').length, 2);
});

test('a late administrative list cannot replace another tab or restore private records after logout', async (t) => {
  const f = adminFixture();
  t.after(() => f.app.dispose());
  await f.start();
  const pending = deferred();
  f.handlers.set('payment/list', () => pending.promise);
  await f.app.click({ tab: 'payments' });
  await tick();
  await f.app.click({ tab: 'refunds' });
  await tick();
  assert.match(f.html(), /退款确认/);
  assert.match(f.html(), /暂无记录/);
  pending.resolve({ items: [{ id: 'old-payment', name: '不该出现的旧记录' }] });
  await tick();
  assert.doesNotMatch(f.html(), /不该出现/);
  assert.match(f.html(), /退款确认/);
  const another = deferred();
  f.handlers.set('payment/list', () => another.promise);
  await f.app.click({ tab: 'payments' });
  await f.app.click({ action: 'logout' });
  another.resolve({ items: [{ id: 'private-payment', name: '退出后的私有记录' }] });
  await tick();
  assert.match(f.html(), /登录商城管理/);
  assert.doesNotMatch(f.html(), /退出后的私有记录|sk-loading/);
});

function studioFixture() {
  const handlers = new Map(),
    calls = [];
  let principal = { id: 'admin-a', role: 'admin' },
    token = 'a';
  const app = createHarness(directory, 'http://studio-loading.test', {
    globals: {
      products: [],
      presentationProducts: [],
      categories: ['全部好物'],
      categoryImages: [],
      esc: escaped,
      icon: () => '',
      header: () => '',
      footer: () => '',
      money: String,
      render() {},
      toast() {},
      closeModal() {},
      LumoAuth: { user: () => principal, isLoggedIn: () => !!principal, requireLogin: () => !!principal },
      LumoCommerce: { normalizeProduct: (row) => row },
      LumoInventory: { stockFor: () => 5 },
      LumoAPI: {
        getBase: () => '/api/v1',
        getToken: () => token,
        async call(service, action, command) {
          calls.push([service, action]);
          return handlers.get(service + '/' + action)?.(command) || { items: [] };
        },
      },
    },
  });
  app.ctx.location.hash = '#/editor';
  app.load('skeleton.js');
  app.load('editor.js');
  return {
    app,
    handlers,
    calls,
    studio: app.ctx.LumoStudio,
    changeUser(user, nextToken) {
      principal = user;
      token = nextToken;
      app.ctx.dispatchEvent(new app.ctx.CustomEvent('lumo:login'));
    },
  };
}

test('studio waits for its administrative catalog before rendering statistics or a genuine empty result', async (t) => {
  const f = studioFixture();
  t.after(() => f.app.dispose());
  assert.match(f.studio.renderEditor(), /sk-loading-table/);
  assert.doesNotMatch(f.studio.renderEditor(), /没有符合条件的商品/);
  const content = deferred(),
    catalog = deferred();
  f.handlers.set('content/list', () => content.promise);
  f.handlers.set('catalog/list', () => catalog.promise);
  const loading = f.studio.refresh();
  assert.match(f.studio.renderEditor(), /sk-loading/);
  content.resolve({ items: [] });
  await tick();
  assert.match(f.studio.renderEditor(), /sk-loading-table/);
  catalog.resolve({ items: [] });
  await loading;
  assert.doesNotMatch(f.studio.renderEditor(), /sk-loading/);
  assert.match(f.studio.renderEditor(), /没有符合条件的商品/);
  assert.deepEqual(f.calls, [
    ['content', 'list'],
    ['catalog', 'list'],
  ]);
});

test('studio keeps completed product data on failure and clears readiness on account change', async (t) => {
  const f = studioFixture();
  t.after(() => f.app.dispose());
  f.handlers.set('catalog/list', () => ({
    items: [
      {
        id: 'real-product',
        name: '已加载的商品',
        category: '真实分类',
        sub: '真实简介',
        image: 'hero.jpg',
        price: 15,
        old: 20,
        visible: true,
      },
    ],
  }));
  await f.studio.refresh();
  assert.match(f.studio.renderEditor(), /已加载的商品/);
  const pending = deferred();
  f.handlers.set('content/list', () => pending.promise);
  const loading = f.studio.refresh();
  assert.match(f.studio.renderEditor(), /已加载的商品/);
  assert.doesNotMatch(f.studio.renderEditor(), /sk-loading/);
  pending.reject(Error('配置读取失败'));
  await assert.rejects(loading);
  assert.match(f.studio.renderEditor(), /已加载的商品/);
  assert.match(f.studio.renderEditor(), /重新加载/);
  f.changeUser({ id: 'admin-b', role: 'admin' }, 'b');
  assert.match(f.studio.renderEditor(), /sk-loading-table/);
  assert.doesNotMatch(f.studio.renderEditor(), /已加载的商品/);
});

test('late studio content cannot overwrite a different account and failed first load becomes a retry state', async (t) => {
  const f = studioFixture();
  t.after(() => f.app.dispose());
  const pending = deferred();
  f.handlers.set('content/list', () => pending.promise);
  const loading = f.studio.refresh();
  f.changeUser({ id: 'admin-b', role: 'admin' }, 'b');
  pending.resolve({ items: [{ id: 'storefront-main', body: JSON.stringify({ brand: '旧账号品牌' }) }] });
  await loading;
  assert.equal(f.studio.readState().settingsLoaded, false);
  assert.doesNotMatch(f.studio.renderEditor(), /旧账号品牌/);
  assert.equal(f.calls.filter(([service]) => service === 'catalog').length, 0);
  f.handlers.set('content/list', () => {
    throw Error('无法读取商城配置');
  });
  await assert.rejects(f.studio.refresh());
  const html = f.studio.renderEditor();
  assert.match(html, /无法读取商城配置|重新加载/);
  assert.doesNotMatch(html, /sk-loading|没有符合条件的商品/);
});

test('product editing waits for the stock snapshot and a closed loading dialog cannot reopen on a late reply', async (t) => {
  const f = studioFixture();
  t.after(() => f.app.dispose());
  let dialog = '',
    markers = new Map();
  f.app.ctx.products.push({
    id: 'phone',
    skuId: 'sku-phone',
    name: '真实商品',
    category: '数码',
    sub: '简介',
    image: 'hero.jpg',
    price: 25,
    old: 30,
    visible: true,
    colors: ['黑色'],
    options: ['标准'],
    defaultOption: '标准',
    variants: [],
  });
  const query = f.app.document.querySelector.bind(f.app.document);
  f.app.document.querySelector = (selector) => markers.get(selector) || query(selector);
  f.app.ctx.modal = (_title, body) => {
    dialog = body;
    markers = new Map();
    for (const match of body.matchAll(/id="(studio-stock-loading-\d+)"/g))
      markers.set('#' + match[1], new Element(match[1]));
  };
  f.app.ctx.closeModal = () => {
    dialog = '';
    markers.clear();
  };
  const stock = deferred();
  f.handlers.set('inventory/batch_get', () => stock.promise);
  await f.app.click({ editorAction: 'edit-product', id: 'phone' });
  assert.match(dialog, /sk-loading-form/);
  assert.doesNotMatch(dialog, /name="stock"/);
  stock.resolve({ items: [{ sku_id: 'sku-phone', on_hand: 4 }], missing_ids: [] });
  await tick();
  assert.match(dialog, /id="studio-product-form"/);
  assert.match(dialog, /name="stock"[^>]*value="4"/);
  const later = deferred();
  f.handlers.set('inventory/batch_get', () => later.promise);
  await f.app.click({ editorAction: 'edit-product', id: 'phone' });
  assert.match(dialog, /sk-loading/);
  await f.app.click({ editorAction: 'close' });
  later.resolve({ items: [{ sku_id: 'sku-phone', on_hand: 7 }], missing_ids: [] });
  await tick();
  assert.equal(dialog, '');
  assert.equal(f.calls.filter(([service]) => service === 'inventory').length, 2);
});

test('category and popup configuration show shapes before reads and keep empty distinct from error', async (t) => {
  const f = adminFixture();
  t.after(() => f.app.dispose());
  f.app.load('category-editor.js');
  f.app.load('popup-admin.js');
  let fail = false;
  const pending = deferred();
  const options = {
    isAdmin: () => true,
    scope: () => '/admin-a',
    draw() {},
    async call(service, action) {
      if (fail) throw Error('配置暂不可用');
      if (action === 'get') return pending.promise;
      if (action === 'popup_list')
        return { items: [], settings: { enabled: true, max_per_session: 1, min_gap_seconds: 60 } };
      return { items: [] };
    },
  };
  const category = f.app.ctx.LumoCategoryEditor,
    popup = f.app.ctx.LumoPopupAdmin;
  category.configure(options);
  popup.configure(options);
  assert.match(category.render(), /sk-loading-form/);
  assert.match(popup.render(), /sk-loading-table/);
  assert.doesNotMatch(popup.render(), /已启用弹窗|暂无匹配的弹窗/);
  const categoryLoad = category.refresh();
  pending.resolve({ body: JSON.stringify({ version: 1, groups: [] }) });
  await categoryLoad;
  assert.doesNotMatch(category.render(), /sk-loading/);
  assert.match(category.render(), /还没有分类/);
  await popup.refresh();
  assert.doesNotMatch(popup.render(), /sk-loading/);
  assert.match(popup.render(), /暂无匹配的弹窗/);
  category.reset();
  popup.reset();
  fail = true;
  await category.refresh();
  await popup.refresh();
  assert.match(category.render(), /配置暂不可用/);
  assert.match(popup.render(), /配置暂不可用/);
  assert.doesNotMatch(popup.render(), /sk-loading|暂无匹配的弹窗|已启用弹窗/);
});
