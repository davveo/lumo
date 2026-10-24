'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const crypto = require('node:crypto').webcrypto;
const source = fs.readFileSync(path.join(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'api-client.js'), 'utf8');
const tokenA = 'a'.repeat(64), tokenB = 'b'.repeat(64), trace = 'c'.repeat(32);
const tick = () => new Promise(resolve => setTimeout(resolve, 5));
const json = value => JSON.parse(JSON.stringify(value));
const response = (data, status = 200) => ({status, ok: status < 400, headers: {get: () => trace}, async json() { return status < 400 ? {data, trace_id: trace} : {error: data, trace_id: trace}; }});
const entry = (service, action, command, data, auth = false, key = service + action) => ({key, service, action, command: {data: {}, ...command}, auth, data});
const catalog = () => entry('catalog', 'list', {data: {limit: 100}}, {items: [{id: 'coffee', name: 'Coffee'}], total: 1});
function setup(handler, enable = true) {
  const calls = [], events = [];
  const context = {URL, AbortController, Uint8Array, setTimeout, clearTimeout, console, crypto,
    location: {origin: 'http://shop.test'},
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
    dispatchEvent(event) { events.push(event); },
    async fetch(url, options) { const call = {url, options, body: JSON.parse(options.body)}; calls.push(call); return handler(call); }
  };
  context.window = context;
  require('./load-constants.cjs')(context);
  vm.runInNewContext(source, context, {filename: 'api-client.js'});
  const api = context.LumoAPI;
  if (enable) api.enableReadBatching();
  return {api, context, calls, events};
}
function batchRows(call, read = item => ({value: item.command.id || item.service})) {
  assert.ok(call.url.endsWith('/storefront/batch'));
  return response({results: call.body.data.requests.map(item => ({...item, auth: !!call.options.headers.Authorization, data: read(item)}))});
}

test('parallel independent reads cross the network in one bounded batch and identical readers share it', async () => {
  const app = setup(call => batchRows(call));
  const first = app.api.call('catalog', 'get', {id: 'coffee'});
  const duplicate = app.api.call('catalog', 'get', {id: 'coffee', data: {}});
  const stock = app.api.call('inventory', 'get', {id: 'coffee'});
  assert.equal(first, duplicate, 'identical reads must share the same in-flight promise');
  const rows = await Promise.all([first, duplicate, stock]);
  assert.deepEqual(json(rows), [{value: 'coffee'}, {value: 'coffee'}, {value: 'coffee'}]);
  assert.equal(app.calls.length, 1);
  assert.equal(app.calls[0].body.data.requests.length, 2);
  assert.equal(app.api.requests().length, 1, 'diagnostics count physical HTTP calls');
  assert.equal(app.api.requests()[0].batchSize, 2);
  await app.api.call('catalog', 'get', {id: 'coffee'});
  assert.equal(app.calls.length, 2, 'completed reads are not retained as a stale global cache');
});

test('batch command keys are canonical despite property order', async () => {
  const app = setup(call => batchRows(call));
  const first = app.api.call('search', 'query', {data: {query: 'coffee', brand: 'LUMO'}});
  const second = app.api.call('search', 'query', {data: {brand: 'LUMO', query: 'coffee'}});
  await Promise.all([first, second]);
  assert.equal(app.calls[0].body.data.requests.length, 1);
});

test('more than 64 independent reads are split without duplicate work', async () => {
  const app = setup(call => batchRows(call));
  await Promise.all(Array.from({length: 130}, (_, id) => app.api.call('inventory', 'get', {id: 'sku-' + id}, {auth: false})));
  assert.equal(app.calls.length, 3);
  assert.deepEqual(app.calls.map(call => call.body.data.requests.length), [64, 64, 2]);
  assert.equal(new Set(app.calls.flatMap(call => call.body.data.requests.map(row => row.key))).size, 130);
});

test('successful siblings survive a per-read failure and preserve the shared trace', async () => {
  const app = setup(call => response({results: call.body.data.requests.map(item => item.service === 'inventory' ? {...item, error: {status: 404, code: 'NotFound', message: 'stock not found'}} : {...item, data: {items: [{id: 'coffee'}]}})}));
  const [goods, stock] = await Promise.allSettled([app.api.call('catalog', 'list'), app.api.call('inventory', 'get', {id: 'absent'})]);
  assert.equal(goods.status, 'fulfilled');
  assert.equal(stock.status, 'rejected');
  assert.equal(stock.reason.status, 404);
  assert.equal(stock.reason.service, 'inventory');
  assert.equal(stock.reason.traceId, trace);
  assert.equal(app.api.requests().length, 1);
  assert.equal(app.api.requests()[0].ok, false);
  assert.equal(app.api.requests()[0].failedReads[0].code, 'NotFound');
});

test('missing or misidentified batch results fail closed for that reader', async () => {
  const app = setup(call => response({results: [{...call.body.data.requests[0], action: 'adjust', data: {available: 999}}]}));
  await assert.rejects(app.api.call('inventory', 'get', {id: 'coffee'}), error => error.status === 502);
});

test('bootstrap snapshots hydrate public and private readers once without extra HTTP calls', async () => {
  const app = setup(call => {
    assert.ok(call.url.endsWith('/storefront/bootstrap'));
    return response({results: [catalog(), entry('points', 'get', {}, {available: 5000}, true), entry('inventory', 'get', {id: 'coffee'}, {sku_id: 'coffee', available: 8})]});
  });
  app.api.setToken(tokenA);
  await app.api.prefetchBootstrap();
  assert.equal(app.api.isBootstrapActive(), true);
  const first = await app.api.call('catalog', 'list', {data: {limit: 100}}, {auth: false});
  first.items[0].name = 'local view edit';
  assert.equal((await app.api.call('catalog', 'list', {data: {limit: 100}}, {auth: false})).items[0].name, 'Coffee');
  assert.equal((await app.api.call('points', 'get')).available, 5000);
  assert.equal((await app.api.call('inventory', 'get', {id: 'coffee'}, {auth: false})).available, 8);
  assert.equal(app.calls.length, 1);
  app.api.endBootstrap();
  assert.equal(app.api.isBootstrapActive(), false);
});

test('guest default-auth readers share the public bootstrap bucket', async () => {
  const app = setup(() => response({results: [catalog()]}));
  await app.api.prefetchBootstrap();
  assert.equal((await app.api.call('catalog', 'list', {data: {limit: 100}})).total, 1);
  assert.equal(app.calls.length, 1);
});

test('primed individual stock reads can satisfy one batch_get and vice versa', async () => {
  const app = setup(() => response({results: [catalog(), entry('inventory', 'get', {id: 'one'}, {sku_id: 'one', available: 1}), entry('inventory', 'get', {id: 'two'}, {sku_id: 'two', available: 2}), entry('catalog', 'batch_get', {data: {ids: ['one', 'two']}}, {items: [{id: 'one'}, {id: 'two'}], missing_ids: []})]}));
  await app.api.prefetchBootstrap();
  const result = await app.api.call('inventory', 'batch_get', {data: {ids: ['two', 'one', 'two']}}, {auth: false});
  assert.deepEqual(json(result), {items: [{sku_id: 'two', available: 2}, {sku_id: 'one', available: 1}], missing_ids: []});
  assert.equal((await app.api.call('catalog', 'get', {id: 'two'}, {auth: false})).id, 'two');
  assert.equal(app.calls.length, 1);
});

test('a missing critical bootstrap catalog stops hydration and can be retried', async () => {
  let attempts = 0;
  const app = setup(() => response({results: ++attempts === 1 ? [] : [catalog()]}));
  await assert.rejects(app.api.prefetchBootstrap(), error => error.status === 502);
  assert.equal(app.api.isBootstrapActive(), false);
  await app.api.prefetchBootstrap();
  assert.equal((await app.api.call('catalog', 'list', {data: {limit: 100}}, {auth: false})).total, 1);
  assert.equal(app.calls.length, 2);
});

test('failed bootstrap fragments are never cached as successful data', async () => {
  const app = setup(call => call.url.endsWith('/storefront/bootstrap') ? response({results: [catalog(), {...entry('points', 'get', {}, null, true), error: {status: 503, code: 'Unavailable', message: 'offline'}}]}) : batchRows(call, () => ({available: 50})));
  app.api.setToken(tokenA);
  await app.api.prefetchBootstrap();
  assert.equal((await app.api.call('points', 'get')).available, 50);
  assert.equal(app.calls.length, 2);
});

test('writes and volatile quote/payment/order reads remain direct and invalidate bootstrap state', async () => {
  const app = setup(call => call.url.endsWith('/storefront/bootstrap') ? response({results: [catalog(), entry('points', 'get', {}, {available: 1}, true)]}) : call.url.endsWith('/storefront/batch') ? batchRows(call, () => ({available: 2})) : response({status: 'actual server response'}));
  app.api.setToken(tokenA);
  await app.api.prefetchBootstrap();
  await app.api.call('points', 'checkin', {idempotency_key: 'check-in'});
  assert.equal(app.calls[1].url, '/api/v1/points/checkin');
  assert.equal(app.calls[1].options.headers['Idempotency-Key'], 'check-in');
  assert.equal((await app.api.call('points', 'get')).available, 2, 'post-write reads cannot use old balances');
  await Promise.all([app.api.call('checkout', 'quote'), app.api.call('pricing', 'get'), app.api.call('order', 'get'), app.api.call('payment', 'get'), app.api.call('iam', 'current')]);
  assert.deepEqual(app.calls.slice(-5).map(call => call.url), ['/api/v1/checkout/quote', '/api/v1/pricing/get', '/api/v1/order/get', '/api/v1/payment/get', '/api/v1/iam/current']);
});

test('a read initiated before a mutation cannot replace data read after the mutation', async () => {
  let finish;
  const app = setup(call => call.url.endsWith('/storefront/batch') ? new Promise(resolve => { finish = () => resolve(batchRows(call, () => ({available: 1}))); }) : response({accepted: true}));
  const old = app.api.call('points', 'get');
  const rejection = assert.rejects(old, error => error.code === 'SessionChanged');
  await tick();
  await app.api.call('points', 'checkin');
  finish();
  await rejection;
});

test('public, buyer and explicitly authenticated admin reads cannot share a batch', async () => {
  const app = setup(call => batchRows(call));
  app.api.setToken(tokenA);
  await Promise.all([app.api.call('catalog', 'list', {}, {auth: false}), app.api.call('member', 'get'), app.api.call('payment', 'list', {}, {token: tokenB}), app.api.call('member', 'get', {}, {token: tokenA})]);
  assert.equal(app.calls.length, 4);
  assert.deepEqual(app.calls.map(call => call.options.headers.Authorization || ''), ['', 'Bearer ' + tokenA, 'Bearer ' + tokenB, 'Bearer ' + tokenA]);
});

test('an independent admin 401 never clears the buyer session', async () => {
  const app = setup(() => response({code: 'Unauthenticated', message: 'admin token expired'}, 401));
  app.api.setToken(tokenA);
  await assert.rejects(app.api.call('payment', 'list', {}, {token: tokenB}), error => error.status === 401);
  assert.equal(app.api.getToken(), tokenA);
  assert.equal(app.events.some(event => event.type === 'lumo:session-expired'), false);
  await assert.rejects(app.api.call('member', 'get'), error => error.status === 401);
  assert.equal(app.api.getToken(), '');
});

test('late buyer and gateway responses cannot cross the current scope', async () => {
  for (const change of [api => api.setToken(tokenB), api => api.setBase('https://second.example')]) {
    let finish;
    const app = setup(call => new Promise(resolve => { finish = () => resolve(batchRows(call, () => ({owner: 'old'}))); }));
    app.api.setToken(tokenA);
    const old = app.api.call('member', 'get');
    const rejected = assert.rejects(old, error => error.code === 'SessionChanged');
    await tick(); change(app.api); finish(); await rejected;
  }
});

test('batch transport aborts at the shared deadline and releases every waiting reader', async () => {
  let aborted = false;
  const app = setup(call => new Promise((resolve, reject) => call.options.signal.addEventListener('abort', () => {aborted = true; const error = new Error('aborted'); error.name = 'AbortError'; reject(error);}))); 
  const result = await Promise.allSettled([app.api.call('inventory', 'get', {id: 'one'}, {timeout: 10}), app.api.call('inventory', 'get', {id: 'two'}, {timeout: 10})]);
  assert.equal(app.calls.length, 1);
  assert.equal(aborted, true);
  assert.equal(result.filter(row => row.status === 'rejected' && /请求超时/.test(row.reason.message)).length, 2);
});

test('physical diagnostics redact tokens, credentials and business payloads', async () => {
  const app = setup(call => call.url.endsWith('/storefront/batch') ? batchRows(call) : response({accepted: true}));
  app.api.setToken(tokenA);
  await app.api.call('member', 'get');
  await app.api.call('iam', 'login', {data: {username: 'user', password: 'TypedSecret'}}, {auth: false});
  const diagnostic = JSON.stringify(app.api.requests());
  assert.ok(!diagnostic.includes(tokenA));
  assert.ok(!diagnostic.includes('TypedSecret'));
  assert.ok(!diagnostic.includes('username'));
  assert.equal(app.api.requests().length, 2);
});
