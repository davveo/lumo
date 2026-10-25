'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const dir = process.env.FRONTEND_DIR || path.resolve(__dirname, '../dist');
const inventorySource = fs.readFileSync(path.join(dir, 'inventory.js'), 'utf8');
const apiSource = fs.readFileSync(path.join(dir, 'api-client.js'), 'utf8');
const json = (value) => JSON.parse(JSON.stringify(value));
const products = (count) =>
  Array.from({ length: count }, (_, i) => ({ id: 'product-' + i, skuId: 'sku-' + i, visible: true }));
const stockRows = (ids) => ({ items: ids.map((sku_id) => ({ sku_id, available: 8 })), missing_ids: [] });
function setup(handler, items = products(100)) {
  const calls = [];
  const window = {
    LumoAPI: {
      call(service, action, command, options) {
        const call = { service, action, command, options };
        calls.push(call);
        return Promise.resolve().then(() => handler(call));
      },
    },
  };
  const context = { window, products: items, state: { cart: [] } };
  require('./load-constants.cjs')(context, dir);
  vm.runInNewContext(inventorySource, context, { filename: 'inventory.js' });
  return { inventory: window.LumoInventory, calls, context };
}

test('recommendation stock hydrates without another HTTP and validates its rows', () => {
  const app = setup(() => {
    throw Error('hydration must not fetch stock');
  }, products(2));
  const ticket = app.inventory.hydrationTicket();
  assert.equal(
    app.inventory.prime(
      [
        { sku_id: 'sku-0', available: 6, on_hand: 8 },
        { sku_id: 'sku-1', available: -1 },
        { sku_id: 'broken', available: '9' },
      ],
      ticket,
    ),
    1,
  );
  assert.equal(app.inventory.stockFor('product-0'), 6);
  assert.equal(app.inventory.snapshot('sku-0').on_hand, 8);
  assert.equal(app.calls.length, 0);
  assert.equal(app.inventory.prime([{ sku_id: 'sku-1', available: 9 }], NaN), 0);
});

test('late recommendation stock never replaces a stock read started after its request', async () => {
  let finish;
  const app = setup(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
    products(1),
  );
  const ticket = app.inventory.hydrationTicket();
  const fresh = app.inventory.refreshOne('sku-0');
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(app.inventory.prime([{ sku_id: 'sku-0', available: 50 }], ticket), 0);
  finish({ sku_id: 'sku-0', available: 2 });
  await fresh;
  assert.equal(app.inventory.prime([{ sku_id: 'sku-0', available: 50 }], ticket), 0);
  assert.equal(app.inventory.stockFor('product-0'), 2);
});

test('100 products use one explicit stock batch with unique SKU IDs', async () => {
  const items = products(100);
  const app = setup((call) => stockRows(call.command.data.ids), [...items, items[0]]);
  assert.deepEqual(json(await app.inventory.refresh()), { loaded: 100, failed: 0 });
  assert.equal(app.calls.length, 1);
  assert.equal(app.calls[0].service, 'inventory');
  assert.equal(app.calls[0].action, 'batch_get');
  assert.equal(app.calls[0].options.auth, false);
  assert.deepEqual(
    json(app.calls[0].command.data.ids),
    items.map((p) => p.skuId),
  );
  assert.equal(app.inventory.stockFor('product-99'), 8);
  assert.equal(app.inventory.snapshot('sku-0').sku_id, 'sku-0');
});

test('stock reads split only at the 500-SKU domain limit and never fan out to get', async () => {
  const app = setup((call) => stockRows(call.command.data.ids), products(1001));
  assert.deepEqual(json(await app.inventory.refresh()), { loaded: 1001, failed: 0 });
  assert.deepEqual(
    app.calls.map((call) => call.command.data.ids.length),
    [500, 500, 1],
  );
  assert.ok(app.calls.every((call) => call.action === 'batch_get'));
  const ids = app.calls.flatMap((call) => json(call.command.data.ids));
  assert.equal(new Set(ids).size, 1001);
});

test('partial results clear both explicit and unreported missing stock instead of retaining availability', async () => {
  let partial = false;
  const app = setup(
    (call) =>
      partial
        ? {
            items: [
              { sku_id: 'sku-0', available: 3 },
              { sku_id: 'unrequested', available: 999 },
            ],
            missing_ids: ['sku-1'],
          }
        : stockRows(call.command.data.ids),
    products(3),
  );
  await app.inventory.refresh();
  partial = true;
  assert.deepEqual(json(await app.inventory.refresh()), { loaded: 1, failed: 2 });
  assert.equal(app.inventory.stockFor('product-0'), 3);
  for (const id of ['product-1', 'product-2']) {
    assert.equal(app.inventory.stockFor(id), 0);
    assert.equal(app.inventory.snapshot(id), undefined);
    assert.match(app.inventory.badge(id), /库存暂不可用/);
    assert.match(app.inventory.validate([{ id, qty: 1 }]), /库存服务暂不可用/);
  }
  assert.equal(app.inventory.snapshot('unrequested'), undefined);
  partial = false;
  await app.inventory.refresh(['product-1']);
  assert.equal(app.inventory.stockFor('product-1'), 8);
  assert.equal(app.inventory.validate([{ id: 'product-1', qty: 1 }]), null);
  assert.ok(app.calls.every((call) => call.action === 'batch_get'));
});

test('all missing stock rejects and invalidates every old snapshot without per-SKU fallback', async () => {
  let missing = false;
  const app = setup(
    (call) => (missing ? { items: [], missing_ids: call.command.data.ids } : stockRows(call.command.data.ids)),
    products(2),
  );
  await app.inventory.refresh();
  missing = true;
  await assert.rejects(app.inventory.refresh(), (error) => error.code === 'NotFound');
  assert.equal(app.inventory.stockFor('product-0'), 0);
  assert.equal(app.inventory.snapshot('product-1'), undefined);
  assert.equal(app.calls.length, 2);
  assert.ok(app.calls.every((call) => call.action === 'batch_get'));
});

test('one failed chunk preserves successful sibling stock and counts failures by SKU', async () => {
  let failed = false;
  const outage = new Error('stock service offline');
  const app = setup((call) => {
    if (failed && call.command.data.ids[0] === 'sku-0') throw outage;
    return stockRows(call.command.data.ids);
  }, products(501));
  await app.inventory.refresh();
  failed = true;
  assert.deepEqual(json(await app.inventory.refresh()), { loaded: 1, failed: 500 });
  assert.equal(app.inventory.snapshot('sku-0'), undefined);
  assert.equal(app.inventory.stockFor('sku-499'), 0);
  assert.equal(app.inventory.stockFor('sku-500'), 8);
  assert.match(app.inventory.badge('sku-0'), /库存暂不可用/);
  assert.equal(app.calls.length, 4);
  assert.ok(app.calls.every((call) => call.action === 'batch_get'));
});

test('a total batch outage rejects its original error, removes stale stock, and can be retried', async () => {
  let fail = false;
  const outage = new Error('temporary stock outage');
  const app = setup((call) => {
    if (fail) throw outage;
    return stockRows(call.command.data.ids);
  }, products(2));
  await app.inventory.refresh();
  fail = true;
  await assert.rejects(app.inventory.refresh(), (error) => error === outage);
  assert.equal(app.inventory.stockFor('sku-0'), 0);
  assert.equal(app.inventory.snapshot('sku-1'), undefined);
  assert.equal(app.calls.length, 2);
  fail = false;
  assert.deepEqual(json(await app.inventory.refresh()), { loaded: 2, failed: 0 });
  assert.equal(app.inventory.stockFor('sku-0'), 8);
});

test('single-SKU refresh stays a get and a failed refresh cannot leave stale sellable stock', async () => {
  let fail = false;
  const app = setup((call) => {
    if (fail) throw new Error('unavailable');
    return { sku_id: call.command.id, available: 5 };
  }, products(1));
  await app.inventory.refreshOne('product-0');
  assert.equal(app.inventory.stockFor('product-0'), 5);
  fail = true;
  await assert.rejects(app.inventory.refreshOne('product-0'), /unavailable/);
  assert.equal(app.inventory.snapshot('product-0'), undefined);
  assert.deepEqual(
    app.calls.map((call) => call.action),
    ['get', 'get'],
  );
});

test('empty inputs make no request and concurrent full refreshes share one pending batch', async () => {
  let release;
  const app = setup(
    (call) =>
      new Promise((resolve) => {
        release = () => resolve(stockRows(call.command.data.ids));
      }),
    products(2),
  );
  assert.deepEqual(json(await app.inventory.refresh([])), { loaded: 0, failed: 0 });
  assert.equal(app.calls.length, 0);
  const first = app.inventory.refresh(),
    second = app.inventory.refresh();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(app.calls.length, 1);
  release();
  assert.deepEqual(json(await Promise.all([first, second])), [
    { loaded: 2, failed: 0 },
    { loaded: 2, failed: 0 },
  ]);
});

function interleavedSetup() {
  const pending = [];
  const app = setup((call) => new Promise((resolve, reject) => pending.push({ call, resolve, reject })), products(2));
  const start = (kind) =>
    kind === 'single' ? app.inventory.refreshOne('sku-0') : app.inventory.refresh(['sku-0', 'sku-1']);
  const settleSuccess = (request, available) =>
    request.resolve(
      request.call.action === 'get'
        ? { sku_id: 'sku-0', available }
        : { items: request.call.command.data.ids.map((sku_id) => ({ sku_id, available })), missing_ids: [] },
    );
  return { ...app, pending, start, settleSuccess, tick: () => new Promise((resolve) => setImmediate(resolve)) };
}

for (const [olderKind, newerKind] of [
  ['single', 'batch'],
  ['batch', 'single'],
]) {
  for (const olderOutcome of ['success', 'failure']) {
    test(`late ${olderKind} ${olderOutcome} cannot replace newer ${newerKind} stock`, async () => {
      const app = interleavedSetup();
      const older = app.start(olderKind).then(
        (value) => ({ value }),
        (error) => ({ error }),
      );
      await app.tick();
      const newer = app.start(newerKind);
      await app.tick();
      app.settleSuccess(app.pending[1], 20);
      await newer;
      const staleError = Object.assign(new Error('old session response'), { code: 'SessionChanged' });
      if (olderOutcome === 'success') app.settleSuccess(app.pending[0], 2);
      else app.pending[0].reject(staleError);
      const result = await older;
      if (olderOutcome === 'failure')
        assert.equal(result.error, staleError, 'old callers must still receive their original error');
      else if (olderKind === 'single')
        assert.equal(result.value.available, 2, 'the original caller still receives its own response');
      else
        assert.deepEqual(
          json(result.value),
          { loaded: 2, failed: 0 },
          'read version protection does not change result counts',
        );
      assert.equal(app.inventory.stockFor('sku-0'), 20);
      assert.equal(
        app.inventory.validate([{ id: 'product-0', qty: 1 }]),
        null,
        'old failures must not attach an error to newer stock',
      );
      if (olderKind === 'batch') {
        assert.equal(
          app.inventory.stockFor('sku-1'),
          olderOutcome === 'success' ? 2 : 0,
          'uncontested SKU results must still be applied',
        );
        if (olderOutcome === 'failure') assert.match(app.inventory.badge('sku-1'), /库存暂不可用/);
      }
    });
  }
  test(`late ${olderKind} success cannot clear a newer ${newerKind} failure`, async () => {
    const app = interleavedSetup();
    const older = app.start(olderKind);
    await app.tick();
    const newer = app.start(newerKind);
    const rejected = assert.rejects(newer, (error) => error.code === 'Unavailable');
    await app.tick();
    app.pending[1].reject(Object.assign(new Error('new stock outage'), { code: 'Unavailable' }));
    await rejected;
    app.settleSuccess(app.pending[0], 9);
    const result = await older;
    if (olderKind === 'batch') assert.deepEqual(json(result), { loaded: 2, failed: 0 });
    assert.equal(app.inventory.snapshot('sku-0'), undefined);
    assert.equal(app.inventory.stockFor('sku-0'), 0);
    assert.match(app.inventory.badge('sku-0'), /库存暂不可用/, 'the newest read error must remain visible');
    assert.match(app.inventory.validate([{ id: 'product-0', qty: 1 }]), /库存服务暂不可用/);
  });
}

test('a late partial batch cannot delete newer single stock but retains its partial result counts', async () => {
  const app = interleavedSetup();
  const older = app.start('batch');
  await app.tick();
  const newer = app.start('single');
  await app.tick();
  app.settleSuccess(app.pending[1], 12);
  await newer;
  app.pending[0].resolve({ items: [{ sku_id: 'sku-1', available: 4 }], missing_ids: ['sku-0'] });
  assert.deepEqual(json(await older), { loaded: 1, failed: 1 });
  assert.equal(app.inventory.stockFor('sku-0'), 12);
  assert.equal(app.inventory.stockFor('sku-1'), 4);
  assert.equal(app.inventory.validate([{ id: 'product-0', qty: 1 }]), null);
});

function gatewaySetup(prefetch = false) {
  const calls = [],
    items = products(100);
  const response = (data) => ({ status: 200, ok: true, headers: { get: () => '' }, json: async () => ({ data }) });
  const context = {
    products: items,
    state: { cart: [] },
    URL,
    AbortController,
    Uint8Array,
    setTimeout,
    clearTimeout,
    location: { origin: 'http://shop.test' },
    CustomEvent: class {},
    dispatchEvent() {},
    async fetch(url, options) {
      const body = JSON.parse(options.body);
      calls.push({ url, body });
      if (url.endsWith('/storefront/products')) {
        assert.ok(prefetch);
        return response({
          results: [
            {
              service: 'catalog',
              action: 'list',
              command: { data: { limit: 100 } },
              auth: false,
              data: { items, total: items.length },
            },
            {
              service: 'inventory',
              action: 'batch_get',
              command: { data: { ids: items.map((p) => p.skuId) } },
              auth: false,
              data: stockRows(items.map((p) => p.skuId)),
            },
          ],
        });
      }
      assert.ok(url.endsWith('/inventory/batch_get'), 'stock reads retain their explicit semantic endpoint');
      assert.equal(body.data.ids.length, 100);
      return response(stockRows(body.data.ids));
    },
  };
  context.window = context;
  vm.createContext(context);
  require('./load-constants.cjs')(context, dir);
  vm.runInContext(apiSource, context, { filename: 'api-client.js' });
  vm.runInContext(inventorySource, context, { filename: 'inventory.js' });
  context.LumoAPI.enableReadBatching();
  return { api: context.LumoAPI, inventory: context.LumoInventory, calls };
}

test('100 SKU refresh crosses the real API client as one HTTP and one domain read', async () => {
  const app = gatewaySetup();
  assert.deepEqual(json(await app.inventory.refresh()), { loaded: 100, failed: 0 });
  assert.equal(app.calls.length, 1);
  assert.ok(app.calls[0].url.endsWith('/inventory/batch_get'));
  assert.equal(app.calls[0].body.data.ids.length, 100);
  await app.inventory.refresh();
  assert.equal(app.calls.length, 2, 'completed stock reads must be refreshed, without a persistent cache');
});

test('public product-section stock fragments satisfy the explicit batch with no extra HTTP', async () => {
  const app = gatewaySetup(true);
  app.api.beginSections();
  await app.api.prefetchSection('products');
  assert.deepEqual(json(await app.inventory.refresh()), { loaded: 100, failed: 0 });
  assert.equal(app.calls.length, 1);
  assert.ok(app.calls[0].url.endsWith('/storefront/products'));
  assert.equal(app.inventory.stockFor('product-99'), 8);
  app.api.endBootstrap();
  await app.inventory.refresh();
  assert.equal(app.calls.length, 2, 'stock refresh after section hydration must fetch current availability');
});
