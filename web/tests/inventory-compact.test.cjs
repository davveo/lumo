'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
function fixture(file = 'inventory-admin.js') {
  const app = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://inventory.test', {
    html: 'admin.html',
  });
  const listeners = new Map(),
    add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    add(type, fn, ...rest);
  };
  app.load('skeleton.js');
  app.load('admin-ui.js');
  app.load(file);
  const m = file === 'inventory-admin.js' ? app.ctx.LumoInventoryAdmin : app.ctx.LumoReconciliationAdmin;
  app.ids.set(file === 'inventory-admin.js' ? 'inventory-admin-root' : 'reconciliation-admin-root', new Element());
  return {
    app,
    m,
    async fire(type, selector, fields = {}, value = '') {
      const target = new Element();
      target.value = value;
      target.fields = fields;
      target.matches = (query) => query === selector;
      for (const fn of listeners.get(type) || []) await fn({ target, preventDefault() {} });
    },
  };
}
const product = (id, count = 2) => ({
  id,
  name: '商品 ' + id,
  category: '数码',
  brand: 'LUMO',
  active: true,
  variants: Array.from({ length: count }, (_, i) => ({
    sku_id: i === 0 ? id : id + '--' + i,
    color: '颜色' + i,
    option: '标准规格',
    price_cents: 10000,
  })),
});
function configure(f, call) {
  f.m.configure({ isAdmin: () => true, scope: () => 'staff', draw() {}, call });
}
function stocks(ids) {
  return { items: ids.map((sku_id) => ({ sku_id, on_hand: 20, reserved: 2, allocated: 3, available: 15 })) };
}

test('inventory catalog requests eight products and a failed next page keeps the current page and rows', async (t) => {
  const f = fixture(),
    calls = [];
  t.after(() => f.app.dispose());
  let fail = false;
  configure(f, async (service, action, request) => {
    calls.push({ service, action, request });
    if (service === 'inventory') return stocks(request.data.ids);
    if (fail) throw Error('连接中断');
    return { items: Array.from({ length: 8 }, (_, i) => product('p' + i)), total: 80 };
  });
  await f.m.refresh();
  assert.equal(calls[0].request.data.limit, 8);
  assert.equal((f.m.render().match(/class="iv-product /g) || []).length, 8);
  fail = true;
  await f.app.click({ ivPage: '2' });
  assert.equal(calls.at(-1).request.data.offset, 8);
  assert.equal(f.m.state.page, 1);
  assert.match(f.m.render(), /连接中断/);
  assert.match(f.m.render(), /data-id="p0"/);
});

test('inventory name search pages through all active and disabled matches on the server', async (t) => {
  const f = fixture(),
    calls = [];
  t.after(() => f.app.dispose());
  configure(f, async (service, action, request) => {
    calls.push({ service, action, request });
    if (service === 'catalog') {
      if (action === 'list') return { items: [], total: 0 };
      assert.equal(action, 'admin_list');
      assert.equal(request.data.keyword, '商品');
      const offset = (request.data.page - 1) * 8;
      return {
        items: Array.from({ length: Math.min(8, 25 - offset) }, (_, i) => ({
          ...product('find-' + (offset + i)),
          active: false,
        })),
        total: 25,
      };
    }
    return stocks(request.data.ids);
  });
  await f.fire('submit', '[data-iv-search]', { id: '商品' });
  assert.equal((f.m.render().match(/class="iv-product /g) || []).length, 8);
  assert.match(f.m.render(), /匹配 25 件/);
  assert.match(f.m.render(), /未上架/);
  await f.app.click({ ivPage: '4' });
  assert.equal(calls.filter((c) => c.service === 'catalog').length, 2);
  assert.match(f.m.render(), /data-id="find-24"/);
  assert.doesNotMatch(f.m.render(), /data-id="find-0"/);
  await f.fire('input', '[data-iv-filter]', {}, 'find-24');
  assert.equal(f.m.state.page, 4);
  assert.equal((f.m.render().match(/class="iv-product /g) || []).length, 1);
  await f.app.click({ iv: 'clear' });
  assert.equal(f.m.state.filter, '');
  assert.equal(f.m.state.searchMode, false);
});

test('a product with 144 variants displays one SKU and keeps exact SKU targeting after adjustment', async (t) => {
  const f = fixture(),
    p = product('many', 144),
    calls = [];
  t.after(() => f.app.dispose());
  configure(f, async (service, action, request) => {
    calls.push({ service, action, request });
    if (service === 'catalog') return request.id === 'many' ? p : { id: request.id, product_id: 'many' };
    if (action === 'batch_get') return stocks(request.data.ids);
    return {};
  });
  await f.m.selectSKU('many--143');
  assert.equal(f.m.state.selectedSKU, 'many--143');
  assert.equal((f.m.render().match(/<option /g) || []).length, 144);
  assert.equal((f.m.render().match(/class="iv-stock-grid"/g) || []).length, 1);
  await f.app.click({ iv: 'adjust', id: 'many--143' });
  await f.fire('submit', '[data-iv-adjust]', { on_hand: '30', reason: '盘点修正' });
  const adjustment = calls.find((c) => c.action === 'adjust');
  assert.equal(adjustment.request.id, 'many--143');
  assert.equal(adjustment.request.data.expected_on_hand, 20);
  assert.equal(f.m.state.selectedSKU, 'many--143');
  assert.equal(f.m.state.adjust, null);
  await f.fire('change', '[data-iv-sku]', {}, 'many--1');
  assert.equal(f.m.state.selectedSKU, 'many--1');
  assert.match(f.m.render(), /data-iv="adjust" data-id="many--1"/);
});

test('variant stock reads split at the backend batch limit and retain selected stocks when paging', async (t) => {
  const f = fixture(),
    selected = product('selected'),
    products = Array.from({ length: 8 }, (_, i) => product('bulk-' + i, 144)),
    calls = [];
  t.after(() => f.app.dispose());
  configure(f, async (service, action, request) => {
    calls.push({ service, action, request });
    if (service === 'catalog') return action === 'get' ? selected : { items: products, total: 16 };
    return stocks(request.data.ids);
  });
  await f.m.selectSKU('selected');
  calls.length = 0;
  await f.m.refresh();
  assert.equal(calls.filter((c) => c.service === 'inventory').length, 3);
  assert.ok(calls.filter((c) => c.service === 'inventory').every((c) => c.request.data.ids.length <= 500));
  assert.equal(f.m.state.stocks.selected.available, 15);
  await f.app.click({ ivPage: '2' });
  assert.equal(f.m.state.selected.id, 'selected');
  assert.equal(f.m.state.stocks.selected.available, 15);
});

test('inventory history shows five records per page and changing SKU clears its history and form', async (t) => {
  const f = fixture(),
    p = product('history');
  t.after(() => f.app.dispose());
  let reads = 0;
  configure(f, async (service, action, request) => {
    reads++;
    if (service === 'catalog') return p;
    if (action === 'batch_get') return stocks(request.data.ids);
    return { items: Array.from({ length: 20 }, (_, i) => ({ before: i, after: i + 1, reason: '原因-' + i })) };
  });
  await f.m.selectSKU('history');
  await f.app.click({ iv: 'history', id: 'history' });
  assert.equal((f.m.render().match(/<li>/g) || []).length, 5);
  const before = reads;
  await f.app.click({ ivHistoryPage: '4' });
  assert.match(f.m.render(), /原因-19/);
  assert.equal(reads, before);
  await f.app.click({ iv: 'adjust', id: 'history' });
  assert.equal(f.m.state.historySKU, '');
  await f.fire('change', '[data-iv-sku]', {}, 'history--1');
  assert.equal(f.m.state.adjust, null);
  assert.equal(f.m.state.historyPage, 1);
});

test('reconciliation differences paginate independently and reset when selecting another report', async (t) => {
  const f = fixture('reconciliation-admin.js');
  t.after(() => f.app.dispose());
  let reads = 0;
  configure(f, async () => {
    reads++;
    return {
      items: [
        {
          id: 'report-a',
          details: {
            differences: Array.from({ length: 100 }, (_, i) => ({ payment_id: 'pay-' + i, kind: 'amount_mismatch' })),
          },
        },
        { id: 'report-b', details: { differences: [{ payment_id: 'other', kind: 'amount_mismatch' }] } },
      ],
    };
  });
  await f.m.refresh();
  assert.match(f.m.render(), /pay-7</);
  assert.doesNotMatch(f.m.render(), /pay-8</);
  await f.app.click({ rcDiffPage: '13' });
  assert.match(f.m.render(), /pay-99</);
  assert.equal(reads, 1);
  await f.app.click({ rc: 'detail', id: 'report-b' });
  assert.equal(f.m.state.differencePage, 1);
  assert.match(f.m.render(), /other/);
  assert.doesNotMatch(f.m.render(), /pay-99</);
});

test('legacy SKU stock remains independent when it points to a storefront product', async (t) => {
  const f = fixture(),
    calls = [];
  t.after(() => f.app.dispose());
  configure(f, async (service, action, request) => {
    calls.push({ service, action, request });
    if (service === 'catalog')
      return {
        id: 'sku-legacy',
        product_id: 'parent-a',
        catalog_visible: false,
        name: '历史SKU',
        active: true,
        price_cents: 100,
      };
    if (action === 'batch_get') return stocks(request.data.ids);
    return {};
  });
  await f.m.selectSKU('sku-legacy');
  assert.equal(f.m.state.selectedSKU, 'sku-legacy');
  assert.equal(calls.filter((c) => c.service === 'catalog').length, 1);
  assert.match(f.m.render(), /data-iv="adjust" data-id="sku-legacy"/);
});
