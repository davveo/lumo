'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../dist/api-bootstrap.js'), 'utf8');
const tick = () => new Promise(resolve => setImmediate(resolve));
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return {promise, resolve, reject};
}
function setup({catalog, inventory, discovery, recommendations, categories, prefetch, render, marketing} = {}) {
  let now = 0, sequence = 0;
  const timers = new Map(), statusChanges = [], views = [], events = [], listeners = new Map();
  const clock = {
    setTimeout(fn, delay) { const id = ++sequence; timers.set(id, {fn, at: now + delay}); return id; },
    clearTimeout(id) { timers.delete(id); },
    advance(duration) {
      const end = now + duration;
      while (true) {
        const next = [...timers.entries()].sort((a, b) => a[1].at - b[1].at).find(([, value]) => value.at <= end);
        if (!next) break;
        const [id, task] = next;
        now = task.at; timers.delete(id); task.fn();
      }
      now = end;
    },
    get pending() { return timers.size; }
  };
  const elements = [];
  const document = {
    createElement() {
      const attributes = new Map();
      return {
        className: '', hidden: true, attributes,
        setAttribute(key, value) { attributes.set(key, value); },
        set innerHTML(value) {
          this.html = value;
          statusChanges.push({visible: !this.hidden, message: value, className: this.className});
        },
        get innerHTML() { return this.html; }
      };
    },
    body: {prepend(element) { elements.unshift(element); }},
    addEventListener(name, handler) { const handlers = listeners.get(name) || []; handlers.push(handler); listeners.set(name, handlers); }
  };
  const window = {
    LumoAPI: {getBase: () => '',prefetchBootstrap:prefetch || (async()=>{})},
    LumoCategories:{refresh:categories || (async()=>{})},
    LumoAuth: {async refresh() {}},
    LumoCommerce: {refreshCatalog: catalog || (async () => {})},
    LumoInventory: {refresh: inventory || (async () => {})},
    LumoDiscovery: {refresh: discovery || (async () => {})},
    LumoRecommendations: {refresh: recommendations || (async () => {})},
    LumoMarketing: marketing,
    addEventListener() {},
    dispatchEvent(event) { events.push(event); }
  };
  const context = {
    window, document, location: {hash: '#/'},
    setTimeout: (...args) => clock.setTimeout(...args), clearTimeout: id => clock.clearTimeout(id),
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
    render() { views.push({ready: window.LumoConnection.ready}); render?.(); }
  };
  require('./load-constants.cjs')(context);
  vm.runInNewContext(source, context);
  return {
    window, clock, statusChanges, views, events,
    get status() { return elements[0]; },
    retry() {
      const button = {dataset: {apiAction: 'retry'}};
      for (const handler of listeners.get('click') || []) handler({target: {closest: () => button}});
    }
  };
}

test('fast startup never flashes the global loading banner', async () => {
  const app = setup();
  const result = await app.window.LumoAppReady;
  app.clock.advance(1000);
  assert.equal(result.ready, true);
  assert.equal(result.errors.length, 0);
  assert.equal(app.status.hidden, true);
  assert.equal(app.statusChanges.some(change => change.visible), false);
  assert.equal(app.clock.pending, 0);
});

test('navigation consumes the bootstrap configuration before catalog hydration and core render',async()=>{
  const configuration=deferred(),stages=[];
  const app=setup({prefetch:async()=>{stages.push('prefetch');},categories:()=>{stages.push('categories');return configuration.promise;},catalog:async()=>{stages.push('catalog');},render:()=>{stages.push('render');}});
  await tick();
  assert.deepEqual(stages,['prefetch','categories']);assert.equal(app.views.length,0);
  configuration.resolve();await app.window.LumoAppReady;
  assert.deepEqual(stages.slice(0,3),['prefetch','categories','catalog']);
  assert.ok(stages.indexOf('render')>stages.indexOf('catalog'));
  assert.equal(app.window.LumoConnection.ready,true);assert.equal(app.status.hidden,true);
});

test('navigation errors remain visible while successful catalog data stays usable',async()=>{
  const app=setup({categories:async()=>{throw Error('品类配置加载失败');}});
  const result=await app.window.LumoAppReady;
  assert.equal(result.ready,true);assert.equal(result.errors.length,1);
  assert.match(app.status.innerHTML,/品类配置加载失败/);assert.equal(app.status.hidden,false);
});

test('slow core data shows delayed neutral loading and clears it on completion', async () => {
  const catalog = deferred();
  const app = setup({catalog: () => catalog.promise});
  await tick();
  app.clock.advance(499);
  assert.equal(app.status.hidden, true);
  app.clock.advance(1);
  assert.equal(app.status.hidden, false);
  assert.equal(app.status.innerHTML, '正在加载商城数据…');
  assert.equal(app.status.className, 'api-status');
  assert.equal(app.status.attributes.get('role'), 'status');
  catalog.resolve();
  await app.window.LumoAppReady;
  assert.equal(app.status.hidden, true);
  assert.equal(app.clock.pending, 0);
});

test('core is usable before recommendations finish while AppReady still waits', async () => {
  const inventory = deferred(), discovery = deferred(), recommendations = deferred();
  const app = setup({inventory: () => inventory.promise, discovery: () => discovery.promise, recommendations: () => recommendations.promise});
  let complete = false;
  app.window.LumoAppReady.then(() => { complete = true; });
  await tick();
  app.clock.advance(500);
  assert.equal(app.status.hidden, false);
  discovery.resolve();
  await tick();
  assert.equal(app.window.LumoConnection.ready, false);
  inventory.resolve();
  await tick();
  assert.equal(app.window.LumoConnection.ready, true);
  assert.equal(app.status.hidden, true);
  assert.equal(app.views.some(view => view.ready), true);
  assert.equal(complete, false);
  app.clock.advance(5000);
  assert.equal(app.status.hidden, true);
  recommendations.resolve();
  const result = await app.window.LumoAppReady;
  assert.equal(result.errors.length, 0);
  assert.equal(complete, true);
});

test('catalog errors stay visible with working retry controls', async () => {
  let fail = true, requests = 0;
  const app = setup({catalog: async () => { requests++; if (fail) throw new Error('商品服务暂时不可用'); }});
  const result = await app.window.LumoAppReady;
  assert.equal(result.ready, false);
  assert.equal(app.status.hidden, false);
  assert.equal(app.status.className, 'api-status is-error');
  assert.equal(app.status.attributes.get('role'), 'alert');
  assert.match(app.status.innerHTML, /商品服务暂时不可用/);
  assert.match(app.status.innerHTML, /data-api-action="retry"/);
  assert.match(app.status.innerHTML, /data-api-action="settings"/);
  fail = false;
  app.retry();
  await tick();
  assert.equal(requests, 2);
  assert.equal(app.window.LumoConnection.ready, true);
  assert.equal(app.status.hidden, true);
  assert.equal(app.window.LumoConnection.error, null);
});

test('background errors are reported after successful core loading', async () => {
  const recommendations = deferred();
  const app = setup({recommendations: () => recommendations.promise});
  await tick();
  assert.equal(app.window.LumoConnection.ready, true);
  assert.equal(app.status.hidden, true);
  recommendations.reject(new Error('推荐服务暂时不可用'));
  const result = await app.window.LumoAppReady;
  assert.equal(result.ready, true);
  assert.equal(result.errors.length, 1);
  assert.equal(app.status.hidden, false);
  assert.equal(app.status.className, 'api-status is-error');
  assert.match(app.status.innerHTML, /推荐服务暂时不可用/);
  assert.match(app.status.innerHTML, /data-api-action="retry"/);
});

test('unexpected render errors leave an error state instead of stuck loading', async () => {
  const catalog = deferred(), error = new Error('页面渲染失败');
  const app = setup({catalog: () => catalog.promise, render() { throw error; }});
  await tick();
  app.clock.advance(500);
  assert.equal(app.status.innerHTML, '正在加载商城数据…');
  catalog.resolve();
  const result = await app.window.LumoAppReady;
  assert.equal(result.errors.includes(error), true);
  assert.equal(app.window.LumoConnection.error, error);
  assert.equal(app.status.className, 'api-status is-error');
  assert.match(app.status.innerHTML, /页面渲染失败/);
  app.clock.advance(5000);
  assert.doesNotMatch(app.status.innerHTML, /正在加载/);
  assert.equal(app.clock.pending, 0);
});

test('bootstrap passes placement once and delivers its popup only after all account hydration', async () => {
  const background = deferred(), stages = [], ticket = {placement: 'coupons', route: '#/coupons'};
  const popup = {delivery_id: 'server-decided'};
  const app = setup({prefetch: async command => {assert.equal(command.data.placement, 'coupons');stages.push('bootstrap');return {results: [],popup};}, recommendations: () => background.promise,
    marketing: {beginBootstrap() {stages.push('begin');return ticket;},hydrate(value, context) {assert.equal(value,popup);assert.equal(context,ticket);stages.push('popup');}}});
  await tick(); assert.deepEqual(stages,['begin','bootstrap']);
  background.resolve(); await app.window.LumoAppReady;
  assert.deepEqual(stages,['begin','bootstrap','popup']); assert.equal(app.window.LumoConnection.ready,true);
});

test('optional popup renderer errors never replace a usable storefront with a connection error', async () => {
  const app = setup({prefetch: async () => ({results:[],popup:{delivery_id:'candidate'}}),marketing:{beginBootstrap(){return {placement:'home'};},hydrate(){throw Error('optional marketing renderer failed');}}});
  const result = await app.window.LumoAppReady;
  assert.equal(result.ready,true);assert.equal(result.errors.length,0);assert.equal(app.status.hidden,true);
});
