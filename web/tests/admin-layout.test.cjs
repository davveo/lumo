'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((resolve) => setImmediate(resolve));

function fixture({ role = 'admin', session = true } = {}) {
  const calls = [],
    handlers = new Map(),
    listeners = new Map();
  const app = createHarness(directory, 'http://admin-layout.test', {
    html: 'admin.html',
    globals: {
      LumoAPI: {
        enableReadBatching() {},
        getBase: () => '/api/v1',
        requests: () => [],
        async call(service, action, command, options) {
          calls.push({ service, action, command, options });
          const handler = handlers.get(service + '/' + action);
          return handler ? handler(command) : service === 'iam' ? { role } : { items: [] };
        },
      },
    },
  });
  const addListener = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    addListener(type, fn, ...rest);
  };
  // The lightweight harness does not parse innerHTML; model only the dynamic
  // list containers used by these interaction tests. Layout is checked in a browser.
  for (const id of ['admin-list', 'admin-list-search', 'admin-list-status']) app.ids.set(id, new Element(id));
  const classes = new Set();
  app.document.body.classList = {
    add(name) {
      classes.add(name);
    },
    remove(name) {
      classes.delete(name);
    },
    contains(name) {
      return classes.has(name);
    },
    toggle(name, force) {
      const next = force === undefined ? !classes.has(name) : force;
      if (next) classes.add(name);
      else classes.delete(name);
      return next;
    },
  };
  if (session) app.ctx.sessionStorage.setItem('lumo-admin-session-v1', 'admin-test-session');
  app.load('skeleton.js');
  async function fire(type, target, fields = {}) {
    for (const fn of listeners.get(type) || []) await fn({ target, preventDefault() {}, ...fields });
    await tick();
  }
  return {
    app,
    calls,
    handlers,
    async start() {
      app.load('admin-app.js');
      await app.ctx.LumoAdminReady;
    },
    async tab(name) {
      app.ids.get('admin-list').innerHTML = '';
      await app.click({ tab: name });
      await tick();
    },
    html: () => app.ids.get('admin-main').innerHTML,
    list: () => app.ids.get('admin-list').innerHTML || app.ids.get('admin-main').innerHTML,
    async filter(key, value) {
      const target = new Element();
      target.dataset = { listFilter: key };
      target.value = value;
      await fire(key === 'status' ? 'change' : 'input', target);
    },
    async page(number) {
      await app.click({ listPage: String(number) });
      await tick();
    },
    async key(key) {
      await fire('keydown', new Element(), { key });
    },
  };
}
const records = () =>
  Array.from({ length: 23 }, (_, index) => ({
    id: 'payment-' + String(index + 1).padStart(2, '0'),
    name: index === 22 ? '独特蓝牙订单' : '支付记录 ' + (index + 1),
    status: index % 2 ? 'SUCCEEDED' : 'PENDING',
    order_id: 'order-' + (index + 1),
    amount_cents: 10000 + index,
  }));
function idsInList(html) {
  return new Set([...html.matchAll(/payment-\d{2}/g)].map((match) => match[0]));
}

test('administrative navigation preserves every business destination in groups and marks the current page', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  await f.start();
  const nav = () => f.app.ids.get('admin-nav').innerHTML;
  for (const key of [
    'overview',
    'coupons',
    'activities',
    'products',
    'settings',
    'payments',
    'fulfillment',
    'aftersales',
    'refunds',
    'invoices',
    'interactions',
    'tickets',
    'reconciliation',
    'inventory',
    'categories',
    'popups',
    'content',
    'analytics',
  ]) {
    assert.equal(
      [...nav().matchAll(new RegExp('data-tab="' + key + '"', 'g'))].length,
      1,
      key + ' must have one navigation destination',
    );
  }
  for (const group of ['交易与履约', '商品与库存', '营销与装修', '运营与服务']) assert.match(nav(), new RegExp(group));
  assert.match(nav(), /href="index\.html#\/editor"/);
  await f.tab('payments');
  assert.match(nav(), /<button(?=[^>]*data-tab="payments")(?=[^>]*aria-current="page")[^>]*>/);
  assert.doesNotMatch(nav(), /<button(?=[^>]*data-tab="overview")(?=[^>]*aria-current="page")[^>]*>/);
});

test('loaded administrative records paginate locally and keyword/status filters reset the page without new requests', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.handlers.set('payment/list', () => ({ items: records() }));
  await f.start();
  await f.tab('payments');
  const count = f.calls.length;
  assert.equal(idsInList(f.list()).size, 10);
  assert.match(f.list(), /payment-01/);
  assert.doesNotMatch(f.list(), /payment-11/);
  await f.page(2);
  assert.equal(idsInList(f.list()).size, 10);
  assert.match(f.list(), /payment-11/);
  assert.doesNotMatch(f.list(), /payment-01/);
  await f.filter('keyword', '独特蓝牙');
  assert.equal(idsInList(f.list()).size, 1);
  assert.match(f.list(), /payment-23/);
  await f.filter('status', 'SUCCEEDED');
  assert.equal(idsInList(f.list()).size, 0);
  assert.match(f.list(), /没有符合条件的记录/);
  await f.filter('keyword', '');
  assert.equal(idsInList(f.list()).size, 10);
  assert.match(f.list(), /payment-02/);
  assert.doesNotMatch(f.list(), /payment-01/);
  assert.equal(f.calls.length, count, 'local filters and pages must not reload business APIs');
});

test('list criteria stay within their tab and a filtered result is distinct from a truly empty collection', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.handlers.set('payment/list', () => ({ items: records() }));
  f.handlers.set('refund/list', () => ({ items: [{ id: 'refund-live', name: '退款记录', status: 'PENDING' }] }));
  await f.start();
  await f.tab('payments');
  await f.filter('keyword', '独特蓝牙');
  await f.tab('refunds');
  assert.match(f.list(), /refund-live/);
  assert.doesNotMatch(f.list(), /payment-23/);
  await f.tab('payments');
  assert.match(f.list(), /payment-23/);
  assert.equal(idsInList(f.list()).size, 1);
  await f.filter('keyword', '不存在的记录');
  assert.match(f.list(), /没有符合条件的记录/);
  await f.tab('invoices');
  assert.match(f.list(), /暂无记录/);
  assert.doesNotMatch(f.list(), /没有符合条件的记录/);
});

test('the narrow-screen navigation can close by backdrop or Escape and logout removes private navigation and rows', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.handlers.set('payment/list', () => ({ items: records() }));
  await f.start();
  const toggle = f.app.ids.get('admin-nav-toggle');
  assert.ok(toggle, 'the menu trigger needs a stable accessible identity');
  await f.app.click({ action: 'toggle-nav' });
  assert.equal(toggle.getAttribute('aria-expanded'), 'true');
  await f.app.click({ action: 'close-nav' });
  assert.equal(toggle.getAttribute('aria-expanded'), 'false');
  await f.app.click({ action: 'toggle-nav' });
  await f.key('Escape');
  assert.equal(toggle.getAttribute('aria-expanded'), 'false');
  await f.tab('payments');
  await f.app.click({ action: 'logout' });
  await tick();
  assert.match(f.html(), /登录商城管理/);
  assert.doesNotMatch(f.html(), /payment-01/);
  assert.doesNotMatch(f.app.ids.get('admin-nav').innerHTML, /data-tab=/);
  assert.equal(f.app.ctx.sessionStorage.getItem('lumo-admin-session-v1'), null);
});

test('an expired administrative session returns to login without leaving private rows or destination controls', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.handlers.set('payment/list', () => ({ items: records() }));
  await f.start();
  await f.tab('payments');
  assert.match(f.list(), /payment-01/);
  f.handlers.set('payment/list', () => {
    throw Object.assign(Error('会话已失效'), { status: 401 });
  });
  await f.app.click({ action: 'reload' });
  await tick();
  assert.match(f.html(), /登录商城管理/);
  assert.doesNotMatch(f.html(), /payment-01/);
  assert.doesNotMatch(f.app.ids.get('admin-nav').innerHTML, /data-tab=/);
  assert.equal(f.app.ctx.sessionStorage.getItem('lumo-admin-session-v1'), null);
});

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};

test('filtering a cached tab while its refresh is pending never renders records or actions from another tab', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.handlers.set('refund/list', () => ({ items: [{ id: 'refund-cached', name: '已加载退款', status: 'PENDING' }] }));
  f.handlers.set('payment/list', () => ({
    items: [{ id: 'payment-unrelated', name: '其他页面的支付', status: 'PENDING' }],
  }));
  await f.start();
  await f.tab('refunds');
  await f.tab('payments');
  const refresh = deferred();
  f.handlers.set('refund/list', () => refresh.promise);
  await f.tab('refunds');
  assert.match(f.html(), /refund-cached/);
  await f.filter('keyword', '');
  assert.match(f.list(), /refund-cached/);
  assert.doesNotMatch(f.list(), /payment-unrelated/);
  assert.match(f.list(), /data-operation="refund\.confirm_demo"/);
  refresh.reject(Error('退款刷新失败'));
  await tick();
  assert.match(f.html(), /refund-cached/);
  assert.doesNotMatch(f.html(), /payment-unrelated/);
  await f.filter('status', 'PENDING');
  assert.match(f.list(), /refund-cached/);
  assert.doesNotMatch(f.list(), /payment-unrelated/);
});

test('a failed background refresh preserves the latest local filter view and its controls', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.handlers.set('payment/list', () => ({ items: records() }));
  await f.start();
  await f.tab('payments');
  await f.filter('keyword', '独特蓝牙');
  const refresh = deferred();
  f.handlers.set('payment/list', () => refresh.promise);
  await f.app.click({ action: 'reload' });
  await tick();
  assert.match(f.html(), /value="独特蓝牙"/);
  assert.match(f.html(), /payment-23/);
  await f.filter('keyword', '支付记录 1');
  assert.match(f.list(), /payment-01/);
  assert.doesNotMatch(f.list(), /payment-23/);
  refresh.reject(Error('刷新失败，请重试'));
  await tick();
  assert.match(f.html(), /刷新失败，请重试/);
  assert.match(f.html(), /value="支付记录 1"/);
  assert.match(f.html(), /payment-01/);
  assert.doesNotMatch(f.html(), /payment-23/);
});

test('long ticket content has a compact escaped preview while its full text remains searchable and expandable', async (t) => {
  const f = fixture(),
    body = '<script>内容</script>' + '长正文'.repeat(120) + '最后的检索词',
    answer = '客服答复'.repeat(60);
  t.after(() => f.app.dispose());
  f.handlers.set('helpdesk/list', () => ({
    items: [{ id: 'long-ticket', title: '售后问题', body, answer, status: 'OPEN' }],
  }));
  await f.start();
  await f.tab('tickets');
  const html = f.html();
  assert.equal((html.match(/<summary>展开全文<\/summary>/g) || []).length, 2);
  assert.match(html, /&lt;script&gt;内容&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>内容<\/script>/);
  const previews = [...html.matchAll(/<span class="admin-copy-preview">(.*?)<\/span>/g)];
  assert.equal(previews.length, 2);
  assert.ok(previews.every(([, value]) => value.endsWith('…')));
  const calls = f.calls.length;
  await f.filter('keyword', '最后的检索词');
  assert.match(f.list(), /long-ticket/);
  assert.equal(f.calls.length, calls);
});
