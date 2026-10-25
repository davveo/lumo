'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const crypto = require('node:crypto').webcrypto;
const source = fs.readFileSync(
  path.join(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'api-client.js'),
  'utf8',
);
const tokenA = 'a'.repeat(64),
  tokenB = 'b'.repeat(64),
  trace = 'c'.repeat(32);
const tick = () => new Promise((resolve) => setTimeout(resolve, 5));
const json = (value) => JSON.parse(JSON.stringify(value));
const response = (data, status = 200) => ({
  status,
  ok: status < 400,
  headers: { get: () => trace },
  async json() {
    return status < 400 ? { data } : { error: data };
  },
});
const entry = (service, action, command, data, auth = false, key = service + action) => ({
  key,
  service,
  action,
  command: { data: {}, ...command },
  auth,
  data,
});
const catalog = () =>
  entry('catalog', 'list', { data: { limit: 100 } }, { items: [{ id: 'coffee', name: 'Coffee' }], total: 1 });
function setup(handler, enable = true) {
  const calls = [],
    events = [];
  const context = {
    URL,
    AbortController,
    Uint8Array,
    setTimeout,
    clearTimeout,
    console,
    crypto,
    location: { origin: 'http://shop.test' },
    CustomEvent: class {
      constructor(type, options) {
        this.type = type;
        this.detail = options.detail;
      }
    },
    dispatchEvent(event) {
      events.push(event);
    },
    async fetch(url, options) {
      const call = { url, options, body: JSON.parse(options.body) };
      calls.push(call);
      return handler(call);
    },
  };
  context.window = context;
  require('./load-constants.cjs')(context);
  vm.runInNewContext(source, context, { filename: 'api-client.js' });
  const api = context.LumoAPI;
  if (enable) api.enableReadBatching();
  return { api, context, calls, events };
}

test('identical semantic reads share work while unrelated services keep separate URLs', async () => {
  const app = setup((call) => response({ value: call.body.id || call.url }));
  const a = app.api.call('catalog', 'get', { id: 'coffee' }),
    b = app.api.call('catalog', 'get', { data: {}, id: 'coffee' });
  const stock = app.api.call('inventory', 'get', { id: 'coffee' });
  assert.equal(a, b);
  await Promise.all([a, b, stock]);
  assert.equal(app.calls.length, 2);
  assert.ok(app.calls.some((call) => call.url.endsWith('/catalog/get')));
  assert.ok(app.calls.some((call) => call.url.endsWith('/inventory/get')));
  assert.ok(app.calls.every((call) => !call.url.endsWith('/storefront/batch')));
  await app.api.call('catalog', 'get', { id: 'coffee' });
  assert.equal(app.calls.length, 3, 'completed unrelated reads are not a global cache');
});
test('canonical command keys coalesce equal filters without a command-list request', async () => {
  const app = setup(() => response({ items: [] }));
  await Promise.all([
    app.api.call('search', 'query', { data: { query: 'coffee', brand: 'LUMO' } }),
    app.api.call('search', 'query', { data: { brand: 'LUMO', query: 'coffee' } }),
  ]);
  assert.equal(app.calls.length, 1);
  assert.ok(app.calls[0].url.endsWith('/search/query'));
});
test('products section primes catalog and stock variants, then releases its bounded snapshot', async () => {
  const app = setup((call) =>
    call.url.endsWith('/storefront/products')
      ? response({
          results: [
            catalog(),
            entry(
              'inventory',
              'batch_get',
              { data: { ids: ['coffee', 'variant'] } },
              {
                items: [
                  { sku_id: 'coffee', available: 3 },
                  { sku_id: 'variant', available: 1 },
                ],
              },
            ),
          ],
        })
      : response({ available: 9 }),
  );
  app.api.beginSections();
  await app.api.prefetchSection('products');
  const list = await app.api.call('catalog', 'list', { data: { limit: 100 } }, { auth: false });
  const stock = await app.api.call('inventory', 'batch_get', { data: { ids: ['variant', 'coffee'] } }, { auth: false });
  assert.equal(list.items[0].id, 'coffee');
  assert.equal(stock.items.length, 2);
  assert.equal(app.calls.length, 1);
  app.api.endBootstrap();
  await app.api.call('inventory', 'get', { id: 'coffee' }, { auth: false });
  assert.equal(app.calls.length, 2);
});
test('public shell and products never carry a restored member token', async () => {
  const app = setup((call) => response({ results: call.url.endsWith('/products') ? [catalog()] : [] }));
  app.api.setToken(tokenA);
  app.api.beginSections();
  await Promise.all([app.api.prefetchSection('shell'), app.api.prefetchSection('products')]);
  assert.ok(app.calls.every((call) => !call.options.headers.Authorization));
  await assert.rejects(app.api.prefetchSection('anything'), /未知页面分区/);
});
test('optional missing decoration is cached as a known 404 without hiding navigation failures', async () => {
  const missing = { code: 'NotFound', message: 'content not found', status: 404 };
  const app = setup((call) =>
    call.url.endsWith('/shell')
      ? response({
          results: [
            { ...entry('content', 'get', { id: 'storefront-main' }, null), optional: true, error: missing },
            { ...entry('content', 'get', { id: 'storefront-categories' }, null), error: missing },
          ],
        })
      : response(missing, 404),
  );
  app.api.beginSections();
  await app.api.prefetchSection('shell');
  await assert.rejects(
    app.api.call('content', 'get', { id: 'storefront-main' }, { auth: false }),
    (error) => error.status === 404,
  );
  assert.equal(app.calls.length, 1);
  await assert.rejects(
    app.api.call('content', 'get', { id: 'storefront-categories' }, { auth: false }),
    (error) => error.status === 404,
  );
  assert.equal(app.calls.length, 2);
});
test('summary is an independent projection with nullable failed fields and a short route reuse window', async () => {
  const app = setup(() =>
    response({
      summary: { profile: { display_name: 'Buyer' }, cart_count: 2, points: null },
      errors: { points: { code: 'Unavailable' } },
    }),
  );
  app.api.setToken(tokenA);
  const summary = app.context.LumoAccountSummary;
  const result = await summary.refresh();
  assert.equal(result.loaded, true);
  assert.equal(result.points, null);
  assert.equal(result.cart_count, 2);
  await summary.refresh();
  assert.equal(app.calls.length, 1);
  assert.ok(app.calls[0].url.endsWith('/account_summary'));
  app.api.setToken(tokenB);
  assert.equal(summary.model().loaded, false);
  assert.equal(summary.model().cart_count, null);
});
test('behavior analytics and history do not erase a successful account projection', async () => {
  const app = setup((call) =>
    response(
      call.url.endsWith('/account_summary')
        ? { summary: { cart_count: 2, points: 500 }, errors: {} }
        : { accepted: true },
    ),
  );
  app.api.setToken(tokenA);
  await app.context.LumoAccountSummary.refresh();
  await app.api.call('analytics', 'record', { data: { kind: 'view' } });
  await app.api.call('engagement', 'put', { data: { kind: 'history', sku_id: 'coffee' } });
  assert.equal(app.context.LumoAccountSummary.model().cart_count, 2);
  assert.equal(app.context.LumoAccountSummary.model().points, 500);
  await tick();
  assert.equal(app.calls.length, 3, 'history writes must not re-fetch six summary dependencies');
});
test('existing post-mutation domain reads update just the affected summary field without another group read', async () => {
  const app = setup((call) =>
    response(
      call.url.endsWith('/account_summary')
        ? { summary: { cart_count: 1, points: 500 }, errors: {} }
        : call.url.endsWith('/cart/list')
          ? { items: [{ quantity: 4 }] }
          : { accepted: true },
    ),
  );
  app.api.setToken(tokenA);
  await app.context.LumoAccountSummary.refresh();
  await app.api.call('cart', 'add', { data: { sku_id: 'coffee', quantity: 1 } });
  await app.api.call('cart', 'list');
  await tick();
  assert.equal(app.context.LumoAccountSummary.model().cart_count, 4);
  assert.equal(app.context.LumoAccountSummary.model().points, 500);
  assert.equal(app.calls.filter((call) => call.url.endsWith('/account_summary')).length, 1);
  assert.equal(app.calls.filter((call) => call.url.endsWith('/cart/list')).length, 1);
});
test('summary count updates use exact totals while favorite identities remain bounded', async () => {
  const app = setup((call) =>
    response(
      call.url.endsWith('/account_summary')
        ? { summary: { favorite_count: 2, unread_count: 1, cart_count: 1 }, errors: {} }
        : call.url.endsWith('/engagement/list')
          ? { items: [{ sku_id: 'first' }, { sku_id: 'second' }], total: 151 }
          : call.url.endsWith('/cart/list')
            ? { items: [{ quantity: 2 }], total: 151, quantity_total: 302 }
            : { items: [{ status: 'READ' }], total: 300, unread_total: 125 },
    ),
  );
  app.api.setToken(tokenA);
  await app.context.LumoAccountSummary.refresh();
  await app.api.call('engagement', 'list', { data: { kind: 'favorite', limit: 100 } });
  await app.api.call('notification', 'list', { data: { limit: 100 } });
  await app.api.call('cart', 'list', { data: { limit: 100 } });
  const model = app.context.LumoAccountSummary.model();
  assert.equal(model.favorite_count, 151);
  assert.deepEqual(json(model.favorite_ids), ['first', 'second']);
  assert.equal(model.unread_count, 125, 'read newest page must not erase older unread notifications');
  assert.equal(model.cart_count, 302, 'bounded cart items must not truncate the whole-account quantity');
});
test('filtered domain lists do not replace whole-account summary counts or favorite identities', async () => {
  const app = setup((call) =>
    response(
      call.url.endsWith('/account_summary')
        ? { summary: { favorite_count: 151, favorite_ids: ['first'], unread_count: 125 }, errors: {} }
        : { items: [{ sku_id: 'other-page', status: 'READ' }], total: 0, unread_total: 0 },
    ),
  );
  app.api.setToken(tokenA);
  await app.context.LumoAccountSummary.refresh();
  await app.api.call('notification', 'list', { data: { kind: 'payment', status: 'READ', limit: 100 } });
  await app.api.call('engagement', 'list', { data: { kind: 'favorite', status: 'disabled', limit: 100 } });
  assert.equal(app.context.LumoAccountSummary.model().favorite_count, 151);
  assert.equal(app.context.LumoAccountSummary.model().unread_count, 125);
  await app.api.call('engagement', 'list', { data: { kind: 'favorite', offset: 100 } });
  assert.deepEqual(json(app.context.LumoAccountSummary.model().favorite_ids), ['first']);
});
test('late old-account and invalidated read responses cannot populate the current UI', async () => {
  let finish;
  const app = setup(
    () =>
      new Promise((resolve) => {
        finish = () => resolve(response({ owner: 'old' }));
      }),
  );
  app.api.setToken(tokenA);
  const read = app.api.call('cart', 'list');
  app.api.setToken(tokenB);
  finish();
  await assert.rejects(read, (error) => error.code === app.context.LumoConstants.ApiErrorCode.SessionChanged);
});
test('explicit authentication contexts cannot share a private read', async () => {
  const app = setup(() => response({ items: [] }));
  app.api.setToken(tokenA);
  await Promise.all([app.api.call('cart', 'list'), app.api.call('cart', 'list', {}, { token: tokenB })]);
  assert.equal(app.calls.length, 2);
  assert.equal(new Set(app.calls.map((call) => call.options.headers.Authorization)).size, 2);
});
test('Trace diagnostics use the response header, even when an old server body has a different trace_id', async () => {
  const app = setup(() => ({
    status: 200,
    ok: true,
    headers: { get: () => trace },
    async json() {
      return { data: { items: [] }, trace_id: 'wrong-body-id' };
    },
  }));
  await app.api.call('catalog', 'list');
  assert.equal(app.api.requests()[0].traceId, trace);
});
test('read errors preserve their semantic service and header trace while successful peers remain usable', async () => {
  const app = setup((call) =>
    call.url.includes('/inventory/')
      ? response({ code: 'Unavailable', message: 'offline' }, 503)
      : response({ id: 'coffee' }),
  );
  const rows = await Promise.allSettled([
    app.api.call('catalog', 'get', { id: 'coffee' }),
    app.api.call('inventory', 'get', { id: 'coffee' }),
  ]);
  assert.equal(rows[0].status, 'fulfilled');
  assert.equal(rows[1].reason.service, 'inventory');
  assert.equal(rows[1].reason.traceId, trace);
});
