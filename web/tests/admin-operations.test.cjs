'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
function setup() {
  const app = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://operations.test', {
    html: 'admin.html',
  });
  app.load('skeleton.js');
  app.load('admin-ui.js');
  return app;
}
test('trade lists use server pagination and show domain fields rather than a generic four-column row', async () => {
  const app = setup();
  app.load('operations-admin.js');
  const m = app.ctx.LumoOperationsAdmin.create('payments');
  app.ids.set('op-payments', new Element('op-payments'));
  const calls = [];
  m.configure({
    isAdmin: () => true,
    scope: () => 'staff',
    draw() {},
    async call(service, action, request) {
      calls.push({ service, action, request });
      return {
        items: [
          {
            id: 'pay-a',
            order_id: 'order-a',
            user_id: 'buyer',
            amount_cents: 12345,
            status: 'SUCCEEDED',
            provider: 'demo',
            receipt: 'receipt-a',
            paid_at: '2026-10-27T01:00:00Z',
            created_at: '2026-10-27T00:00:00Z',
          },
        ],
        total: 253,
      };
    },
  });
  await m.refresh();
  const html = m.render();
  assert.match(html, /receipt-a/);
  assert.match(html, /buyer/);
  assert.match(html, /123\.45/);
  assert.match(html, /253/);
  assert.match(html, /第 1 \/ 13 页/);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].action, 'admin_list');
  assert.equal(calls[0].request.data.page_size, 20);
  assert.doesNotMatch(html, /<pre>/);
});
test('inventory reads current product variants and one stock batch, with no per-SKU request waterfall', async () => {
  const app = setup();
  app.load('inventory-admin.js');
  app.ids.set('inventory-admin-root', new Element('inventory-admin-root'));
  const m = app.ctx.LumoInventoryAdmin,
    calls = [];
  const product = {
    id: 'product',
    name: '多规格耳机',
    category: '数码',
    active: true,
    variants: [
      { sku_id: 'white', color: '白色', option: '标准', price_cents: 10000 },
      { sku_id: 'black', color: '黑色', option: '升级', price_cents: 12000 },
    ],
  };
  m.configure({
    isAdmin: () => true,
    scope: () => 'staff',
    draw() {},
    async call(service, action, request) {
      calls.push({ service, action, request });
      if (service === 'catalog') return action === 'list' ? { items: [product], total: 301851 } : product;
      return { items: [{ sku_id: 'white', on_hand: 20, reserved: 2, allocated: 3, available: 15 }] };
    },
  });
  await m.refresh();
  assert.deepEqual(
    calls.map((c) => c.service + '/' + c.action),
    ['catalog/list', 'inventory/batch_get'],
  );
  assert.deepEqual(Array.from(calls[1].request.data.ids), ['white', 'black']);
  await m.selectSKU('product');
  const html = m.render();
  assert.match(html, /多规格耳机/);
  assert.match(html, /白色 \/ 标准/);
  assert.match(html, /黑色 \/ 升级/);
  assert.match(html, /15/);
  assert.match(html, /调整记录/);
  assert.equal((html.match(/class="iv-stock-grid"/g) || []).length, 1);
  await m.selectSKU('black');
  assert.match(m.render(), /尚未建库存/);
  assert.equal(m.state.selectedSKU, 'black');
});
test('dashboard requests one purpose-specific aggregate and never invents money or missing-service zeroes', async () => {
  const app = setup();
  app.load('dashboard-admin.js');
  app.ids.set('dashboard-admin-root', new Element('dashboard-admin-root'));
  const calls = [];
  const m = app.ctx.LumoDashboardAdmin;
  m.configure({
    isAdmin: () => true,
    scope: () => 'staff',
    draw() {},
    async call(service, action, request) {
      calls.push({ service, action, request });
      return {
        order: { total: 753, states: [{ status: 'PENDING_PAYMENT', count: 4 }], daily: [] },
        payment: {
          total: 80,
          states: [
            { status: 'SUCCEEDED', count: 79, amount_cents: 885500 },
            { status: 'PENDING', count: 1 },
          ],
          daily: [],
        },
        users: { total: 3, states: [{ status: 'user', count: 2 }] },
        errors: { refund: 'Unavailable' },
        degraded: true,
      };
    },
  });
  await m.refresh();
  const html = m.render();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].service, 'management');
  assert.equal(calls[0].action, 'dashboard');
  assert.match(html, /753/);
  assert.match(html, /8,855\.00/);
  assert.match(html, /部分看板暂不可用/);
  assert.match(html, /趋势暂不可用/);
  assert.doesNotMatch(html, /同比|环比/);
});
test('late operations reads and account changes cannot populate another admin session', async () => {
  const app = setup();
  app.load('users-admin.js');
  app.ids.set('users-admin-root', new Element('users-admin-root'));
  let resolve,
    scope = 'first',
    admin = true;
  const m = app.ctx.LumoUsersAdmin;
  m.configure({ isAdmin: () => admin, scope: () => scope, draw() {}, call: () => new Promise((r) => (resolve = r)) });
  const pending = m.refresh();
  scope = 'second';
  admin = false;
  m.reset();
  resolve({ items: [{ id: 'private-buyer', username: 'secret', role: 'user' }], total: 1 });
  await pending;
  assert.equal(m.render(), '');
  admin = true;
  assert.doesNotMatch(m.render(), /private-buyer|secret/);
});

test('identical wire statuses retain domain-specific labels in rows, filters and linked order details', async () => {
  for (const [key, status, expected] of [
    ['orders', 'COMPLETED', '已完成'],
    ['aftersales', 'COMPLETED', '售后已完成'],
    ['aftersales', 'REQUESTED', '待审核'],
    ['invoices', 'REQUESTED', '待开票'],
    ['payments', 'SUCCEEDED', '支付成功'],
    ['refunds', 'SUCCEEDED', '退款成功'],
  ]) {
    const app = setup();
    app.load('operations-admin.js');
    const m = app.ctx.LumoOperationsAdmin.create(key);
    const row = { id: 'record', status, order_id: 'order', payable_cents: 10000 };
    m.configure({
      isAdmin: () => true,
      scope: () => 'staff',
      draw() {},
      async call() {
        return { items: [row], total: 1 };
      },
    });
    await m.refresh();
    const html = m.render();
    assert.match(html, new RegExp(`value="${status}"[^>]*>${expected}</option>`), key + ' filter');
    assert.match(html, new RegExp(`admin-state [^"]*">${expected}</span>`), key + ' row');
    m.state.detail = row;
    m.state.order = { id: 'order', status: 'COMPLETED' };
    const detail = m.render();
    assert.match(detail, new RegExp(`admin-state [^"]*">${expected}</span>`), key + ' detail');
    assert.match(detail, /订单状态<\/dt><dd>已完成<\/dd>/, key + ' linked order');
  }
});
