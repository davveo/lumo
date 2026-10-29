'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const copy = (value) => JSON.parse(JSON.stringify(value));
function setup() {
  const app = createHarness(directory, 'http://product-admin.test', { html: 'admin.html' });
  const calls = [],
    messages = [],
    navigations = [],
    listeners = new Map();
  let owner = 'a',
    admin = true,
    readHandler = null,
    failSave = false,
    writeGate = null;
  let rows = Array.from({ length: 42 }, (_, index) => ({
    id: 'product-' + index,
    name: '商品' + index,
    brand: 'LUMO',
    category: '家用电器',
    active: index % 2 === 0,
    status: index % 2 === 0 ? 'ACTIVE' : 'DISABLED',
    price_cents: 12000,
    old: 150,
    image: 'hero.jpg',
    colors: ['白', '黑'],
    options: ['标准', '高配'],
    defaultOption: '标准',
    prices: [120, 180],
    highlights: ['已有亮点'],
    specs: [['材质', '金属']],
    extra_presentation: { preserved: true },
  }));
  const add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    add(type, fn, ...rest);
  };
  app.ids.set('product-admin-root', new Element('product-admin-root'));
  app.ids.set('product-admin-list', new Element('product-admin-list'));
  app.load('skeleton.js');
  app.load('product-admin.js');
  const module = app.ctx.LumoCatalogAdmin;
  module.configure({
    isAdmin: () => admin,
    scope: () => owner,
    draw() {},
    notify: (value) => messages.push(value),
    navigate: (...args) => navigations.push(args),
    async call(service, action, command) {
      calls.push(copy({ service, action, command }));
      assert.equal(service, 'catalog');
      if (action === 'list')
        return {
          items: copy(rows.slice(command.data.offset, command.data.offset + command.data.limit)),
          total: rows.length,
        };
      if (action === 'get') return readHandler ? readHandler(command) : copy(rows.find((row) => row.id === command.id));
      assert.equal(action, 'upsert');
      if (writeGate) await writeGate;
      if (failSave) throw Error('商品保存失败');
      const row = { ...copy(command.data), id: command.id };
      rows = [row, ...rows.filter((item) => item.id !== row.id)];
      return copy(row);
    },
  });
  return {
    app,
    module,
    calls,
    messages,
    navigations,
    get rows() {
      return rows;
    },
    fail(value) {
      failSave = value;
    },
    gate(value) {
      writeGate = value;
    },
    setRead(value) {
      readHandler = value;
    },
    owner(value, allowed = false) {
      owner = value;
      admin = allowed;
    },
    async field(key, value) {
      const target = new Element();
      target.dataset = { pdField: key };
      target.value = value;
      target.checked = value === true;
      for (const fn of listeners.get(key === 'active' ? 'change' : 'input') || []) await fn({ target });
    },
    async filter(key, value) {
      const target = new Element();
      target.dataset = { pdFilter: key };
      target.value = value;
      for (const fn of listeners.get(key === 'status' ? 'change' : 'input') || []) await fn({ target });
    },
    save: () => app.submit('product-admin-form'),
  };
}
test('catalog administration loads disabled products with real server pagination and filters only the current page', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  assert.deepEqual(f.calls[0].command.data, { include_disabled: true, limit: 20, offset: 0 });
  assert.match(f.module.render(), /目录共 42 件/);
  assert.doesNotMatch(f.module.render(), />商品20</);
  const count = f.calls.length;
  await f.filter('search', '商品19');
  assert.match(f.app.ids.get('product-admin-list').innerHTML, /商品19/);
  assert.doesNotMatch(f.app.ids.get('product-admin-list').innerHTML, />商品18</);
  assert.equal(f.calls.length, count);
  await f.app.click({ pdAction: 'next' });
  assert.equal(f.calls.at(-1).command.data.offset, 20);
});
test('editing merges fresh canonical presentation metadata and keeps existing variants and inventory independent', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ pdAction: 'edit', pdId: 'product-0' });
  await f.field('name', '<新名称>');
  await f.field('price', '130.50');
  f.rows[0].highlights = ['另一位管理员更新'];
  f.rows[0].extra_presentation = { preserved: 'new' };
  await f.save();
  const write = f.calls.find((row) => row.action === 'upsert');
  assert.equal(write.command.data.name, '<新名称>');
  assert.equal(write.command.data.price_cents, 13050);
  assert.deepEqual(write.command.data.colors, ['白', '黑']);
  assert.deepEqual(write.command.data.options, ['标准', '高配']);
  assert.deepEqual(write.command.data.prices, [120, 180]);
  assert.deepEqual(write.command.data.highlights, ['另一位管理员更新']);
  assert.deepEqual(write.command.data.extra_presentation, { preserved: 'new' });
  assert.equal(
    f.calls.some((row) => row.service === 'inventory'),
    false,
  );
  assert.match(f.messages[0], /库存保持不变/);
});
test('new products use one stable ID across failures, start disabled and do not initialize or reset inventory', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ pdAction: 'new' });
  for (const [key, value] of Object.entries({
    name: '新商品',
    category: '家用电器',
    brand: 'LUMO',
    price: '99.99',
    old: '120',
    image: 'https://images.example.test/item.jpg',
  }))
    await f.field(key, value);
  f.fail(true);
  await f.save();
  const first = f.calls.find((row) => row.action === 'upsert');
  assert.match(f.module.render(), /商品保存失败/);
  assert.match(f.module.render(), /新商品/);
  f.fail(false);
  await f.save();
  const writes = f.calls.filter((row) => row.action === 'upsert');
  assert.equal(writes[1].command.id, first.command.id);
  assert.equal(writes[1].command.data.active, false);
  assert.deepEqual(writes[1].command.data.options, ['标准版']);
  assert.equal(
    f.calls.some((row) => row.service === 'inventory'),
    false,
  );
  assert.match(f.messages[0], /库存/);
});
test('status changes preserve full SKU metadata and inventory navigation carries the correct SKU', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ pdAction: 'toggle', pdId: 'product-0' });
  const write = f.calls.find((row) => row.action === 'upsert');
  assert.equal(write.command.data.active, false);
  assert.deepEqual(write.command.data.prices, [120, 180]);
  await f.app.click({ pdAction: 'inventory', pdId: 'product-0' });
  assert.deepEqual(copy(f.navigations[0]), ['inventory', { skuId: 'product-0' }]);
});
test('invalid images and fractional-cent prices cannot publish and late edits cannot restore another account', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ pdAction: 'edit', pdId: 'product-0' });
  await f.field('image', 'javascript:alert(1)');
  await f.save();
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 0);
  await f.field('image', 'hero.jpg');
  await f.field('price', '12.345');
  await f.save();
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 0);
  await f.app.click({ pdAction: 'back' });
  let resolve;
  const pending = new Promise((yes) => {
    resolve = yes;
  });
  f.setRead(() => pending);
  const editing = f.app.click({ pdAction: 'edit', pdId: 'product-0' });
  f.owner('b');
  f.module.reset();
  resolve(copy(f.rows[0]));
  await editing;
  assert.equal(f.module.render(), '');
});

test('returning to the catalog while creation is pending joins the write and exits the creation form after confirmation', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ pdAction: 'new' });
  for (const [key, value] of Object.entries({
    name: 'pending-new-product',
    category: '家用电器',
    price: '88.00',
    old: '99.00',
  }))
    await f.field(key, value);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  f.gate(gate);
  const saving = f.save();
  await new Promise((resolve) => setImmediate(resolve));
  let refreshed = false;
  const refresh = f.module.refresh().then((result) => {
    refreshed = true;
    return result;
  });
  await new Promise((resolve) => setImmediate(resolve));
  const returnedBeforeWrite = refreshed;
  release();
  await saving;
  assert.equal(await refresh, true);
  assert.equal(returnedBeforeWrite, false, 'same-session refresh must await the active write');
  assert.doesNotMatch(f.module.render(), /id="product-admin-form"/);
  assert.match(f.module.render(), /pending-new-product/);
  assert.equal(f.messages.length, 1);
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 1);
  assert.equal(
    f.calls.filter((row) => row.action === 'list').length,
    2,
    'only the write confirmation reloads the collection',
  );
});

test('returning to the catalog during a status write preserves its canonical result', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  f.gate(gate);
  const writing = f.app.click({ pdAction: 'toggle', pdId: 'product-0' });
  await new Promise((resolve) => setImmediate(resolve));
  const refresh = f.module.refresh();
  release();
  await writing;
  assert.equal(await refresh, true);
  assert.equal(f.rows.find((row) => row.id === 'product-0').active, false);
  assert.deepEqual(f.messages, ['商品已下架']);
});

test('an old product write cannot block or overwrite a new administrator session', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ pdAction: 'new' });
  for (const [key, value] of Object.entries({
    name: 'old-session-pending-product',
    category: '家用电器',
    price: '88.00',
    old: '99.00',
  }))
    await f.field(key, value);
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  f.gate(gate);
  const saving = f.save();
  await new Promise((resolve) => setImmediate(resolve));
  f.owner('b', true);
  f.module.reset();
  assert.equal(await f.module.refresh(), true);
  release();
  await saving;
  assert.doesNotMatch(f.module.render(), /old-session-pending-product|id="product-admin-form"/);
  assert.equal(f.messages.length, 0);
});
