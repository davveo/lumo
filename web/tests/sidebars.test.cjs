'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const {createHarness} = require('./dom-harness.cjs');

const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const clone = value => JSON.parse(JSON.stringify(value));
const tick = () => new Promise(resolve => setImmediate(resolve));
const product = id => ({id, name: '近期商品 ' + id, price_cents: 12900, category: '手机数码', brand: 'LUMO', image: 'headphones.jpg', active: true});
const recentLinks = html => [...html.matchAll(/<a\b([^>]+)>/g)].filter(([, attrs]) => /\bclass="[^"]*\binterest-item\b/.test(attrs)).map(([, attrs]) => /\bhref="([^"]+)"/.exec(attrs)?.[1]);

function setup(handler = async () => ({items: []}), options = {}) {
  let user = options.user === undefined ? 'buyer' : options.user, token = 'session-a', base = 'http://shop.test', ticket = 0;
  let history = options.history || [];
  const calls = [], primes = [], scrolls = [], preferences = new Map(), preferenceWrites = [], stocks = new Map();
  const app = createHarness(directory, base, {globals: {
    LumoAPI: {getBase: () => base, getToken: () => token, async call(service, action, command = {}, settings = {}) { calls.push({service, action, command: clone(command), options: clone(settings)}); return handler(service, action, command, settings); }},
    LumoAuth: {user: () => user ? {id: user} : null, isLoggedIn: () => !!user, gate: () => true, requireLogin: () => !!user, afterRender() {}},
    LumoMember: {model: () => ({history: clone(history)}), afterRender() {}},
    LumoInventory: {
      snapshot: value => stocks.get(typeof value === 'string' ? value : value?.skuId || value?.sku_id || value?.id),
      stockFor: value => stocks.get(typeof value === 'string' ? value : value?.skuId || value?.sku_id || value?.id)?.available || 0,
      hydrationTicket: () => ++ticket,
      prime(rows, version) { primes.push({rows: clone(rows), version}); for (const row of rows) stocks.set(row.sku_id, clone(row)); }
    },
    localStorage: {getItem: key => preferences.get(key) ?? null, setItem(key, value) { preferences.set(key, String(value)); preferenceWrites.push({key, value: String(value)}); }, removeItem: key => preferences.delete(key)},
    scrollTo: (...args) => scrolls.push(args)
  }});
  app.load('app.js');
  app.evaluate('products.push(...presentationProducts)');
  for (const p of app.json('products')) stocks.set(p.id, {sku_id: p.id, available: 20, on_hand: 20, allocated: 0, reserved: 0});
  app.load('recommendations.js');
  app.ctx.LumoRecommendations.catalogChanged();
  app.load('sidebars.js');
  return {app, calls, primes, stocks, scrolls, preferences, preferenceWrites, sidebar: app.ctx.LumoSidebars,
    setHistory(value) {history = value;},
    setContext(next) {if ('user' in next) user = next.user; if ('token' in next) token = next.token; if ('base' in next) base = next.base;}
  };
}

async function accountRail(historyIDs, favoriteIDs) {
  let history = historyIDs.map((sku_id, index) => ({id: 'history-' + index, sku_id}));
  let favorites = favoriteIDs.map((sku_id, index) => ({id: 'favorite-' + index, sku_id}));
  const f = setup(async (service, action, command) => {
    if (service === 'engagement') {
      const kind = command.data?.kind;
      if (action === 'list') return {items: clone(kind === 'history' ? history : favorites)};
      assert.equal(action, 'delete', 'these interactions must not create engagement records');
      const keep = row => command.id ? row.id !== command.id : row.sku_id !== command.data.sku_id;
      if (kind === 'history') history = history.filter(keep);
      else favorites = favorites.filter(keep);
      return {};
    }
    if (service === 'catalog') return product(command.id);
    if (service === 'inventory') return {sku_id: command.id, available: 5};
    return {items: []};
  });
  f.app.load('member.js');
  await f.app.ctx.LumoMember.refresh();
  await f.sidebar.refresh();
  assert.equal(recentLinks(f.sidebar.render()).length, 6);
  assert.equal(f.calls.filter(call => call.service === 'catalog').length, 0, 'the initial six known products need no metadata reads');
  return f;
}

test('guest sidebar has an honest empty state and rendering never performs reads', async () => {
  const f = setup(undefined, {user: null});
  try {
    await f.sidebar.refresh();
    const html = f.sidebar.render();
    assert.match(html, /最近感兴趣/);
    assert.equal(recentLinks(html).length, 0, 'public catalogue products must not be presented as private history');
    for (let index = 0; index < 5; index++) f.sidebar.render();
    assert.equal(f.calls.length, 0);
  } finally {f.app.dispose();}
});

test('recent rail uses personal history first, de-duplicates favorites and caps at six', async () => {
  const f = setup(undefined, {history: ['iphone', 'macbook', 'headphones', 'watch', 'speaker', 'sneakers', 'perfume'].map(id => ({id}))});
  try {
    f.app.evaluate("state.favorites=['iphone','camera']");
    await f.sidebar.refresh();
    assert.deepEqual(recentLinks(f.sidebar.render()), ['iphone', 'macbook', 'headphones', 'watch', 'speaker', 'sneakers'].map(id => '#/product/' + id));
    const count = f.calls.length;
    for (let index = 0; index < 8; index++) f.sidebar.render();
    assert.equal(f.calls.length, count, 'rendering should only project already hydrated sidebar data');
    assert.ok(f.calls.every(call => call.service !== 'engagement'), 'sidebar must reuse the member history snapshot');
  } finally {f.app.dispose();}
});

test('actual member history is invalidated before a new user, session or gateway can render it', async () => {
  for (const change of [{user: null}, {user: 'other'}, {token: 'session-b'}, {base: 'http://other.test'}]) {
    const f = setup(async (service, action, command) => {
      if (service === 'engagement' && command.data.kind === 'history') return {items: [{id: 'view-one', sku_id: 'iphone', created_at: '2026-10-24T00:00:00Z'}]};
      if (service === 'member') return {display_name: 'Buyer'};
      return {items: []};
    });
    try {
      f.app.load('member.js'); await f.app.ctx.LumoMember.refresh();
      await f.sidebar.refresh(); assert.equal(recentLinks(f.sidebar.render()).length, 1);
      f.setContext(change);
      assert.equal(f.app.ctx.LumoMember.model().history.length, 0, 'member exposed another scope history ' + JSON.stringify(change));
      assert.equal(recentLinks(f.sidebar.render()).length, 0, 'old recent items survived ' + JSON.stringify(change));
    } finally {f.app.dispose();}
  }
});

test('payment, checkout, login and editing routes leave business forms unobstructed', async () => {
  const f = setup();
  try {
    for (const route of ['login', 'payment', 'checkout', 'editor']) {
      f.app.ctx.location.hash = '#/' + route;
      assert.equal(f.sidebar.render(), '', route + ' should not render either sidebar');
    }
    f.app.ctx.location.hash = '#/';
    assert.match(f.sidebar.render(), /商城快捷工具/);
  } finally {f.app.dispose();}
});

test('a variant history item resolves its parent and hydrates authoritative default stock', async () => {
  const records = {
    'old-option': {...product('old-option'), product_id: 'old-parent', parent_id: 'old-parent', catalog_visible: false},
    'old-parent': {...product('old-parent'), name: '历史中的完整商品', variants: [{sku_id: 'old-option', color: '蓝色', option: '标准版', price_cents: 12900}]}
  };
  const f = setup(async (service, action, command) => {
    if (service === 'catalog') return command.id ? records[command.id] : {items: command.data.ids.map(id => records[id]).filter(Boolean), missing_ids: []};
    if (service === 'inventory') {
      const stock = id => ({sku_id: id, available: 7, on_hand: 9, reserved: 2, allocated: 0});
      return command.id ? stock(command.id) : {items: command.data.ids.map(stock), missing_ids: []};
    }
    throw Error('unexpected sidebar read');
  }, {history: [{id: 'old-option'}, {id: 'old-parent'}]});
  try {
    await f.sidebar.refresh();
    const html = f.sidebar.render();
    assert.deepEqual(recentLinks(html), ['#/product/old-parent']);
    assert.match(html, /历史中的完整商品/);
    assert.equal(f.app.json("products.find(p=>p.id==='old-parent').price"), 129);
    assert.equal(f.stocks.get('old-parent').available, 7);
    assert.ok(f.calls.every(call => call.options.auth === false), 'public metadata and inventory must consume public bootstrap snapshots');
    assert.ok(f.calls.every(call => ['catalog', 'inventory'].includes(call.service)));
  } finally {f.app.dispose();}
});

for (const change of [{user: 'other'}, {token: 'session-b'}, {base: 'http://other.test'}]) {
  test('late recent metadata is discarded across scope change ' + JSON.stringify(change), async () => {
    let release;
    const f = setup((service, action, command) => service === 'catalog' ? new Promise(resolve => { release = () => resolve(command.id ? product('old-private') : {items: [product('old-private')]}); }) : {sku_id: 'old-private', available: 8}, {history: [{id: 'old-private'}]});
    try {
      const pending = f.sidebar.refresh();
      await tick(); assert.equal(typeof release, 'function');
      f.setContext(change); release(); await pending;
      assert.equal(recentLinks(f.sidebar.render()).length, 0);
      assert.equal(f.app.json("products.some(p=>p.id==='old-private')"), false);
      assert.equal(f.primes.length, 0);
    } finally {f.app.dispose();}
  });
}

test('missing recent metadata cannot discard other valid history or invent products', async () => {
  const f = setup(async () => {throw Error('SKU unavailable');}, {history: [{id: 'not-a-real-product'}, {id: 'iphone'}]});
  try {
    await f.sidebar.refresh();
    assert.deepEqual(recentLinks(f.sidebar.render()), ['#/product/iphone']);
    assert.equal(f.app.json("products.some(p=>p.id==='not-a-real-product')"), false);
  } finally {f.app.dispose();}
});

test('rail collapse only saves interface preferences and can restore both rails', async () => {
  const f = setup(undefined, {history: [{id: 'iphone'}]});
  try {
    await f.sidebar.refresh();
    await f.app.click({sidebarAction: 'toggle-interest'});
    assert.match(f.sidebar.render(), /interest-rail-reopen/);
    await f.app.click({sidebarAction: 'toggle-quick'});
    assert.match(f.sidebar.render(), /quick-rail-reopen/);
    assert.equal(f.calls.length, 0, 'collapsing a rail must never mutate a business service');
    const stored = JSON.parse(f.preferences.get('lumo-sidebar-ui-v1'));
    assert.deepEqual(stored, {interestCollapsed: true, quickCollapsed: true});
    assert.ok(f.preferenceWrites.every(row => !/iphone|商品|history/i.test(row.value)), 'preferences must not persist a private recent-products list');
    await f.app.click({sidebarAction: 'toggle-interest'});
    await f.app.click({sidebarAction: 'toggle-quick'});
    assert.deepEqual(recentLinks(f.sidebar.render()), ['#/product/iphone']);
    assert.doesNotMatch(f.sidebar.render(), /quick-rail-reopen/);
  } finally {f.app.dispose();}
});

test('quick tools expose real destinations, cart quantity and feedback without submitting a ticket', async () => {
  const f = setup(); let feedbackOpened = 0;
  try {
    f.app.evaluate("state.cart=[{id:'iphone',qty:2},{id:'macbook',qty:3}];ui.category='家用电器';ui.query='冰箱';ui.onlyFavorites=true;ui.min='10';ui.max='1000'");
    f.app.ctx.LumoServices = {afterRender() {}, unreadCount: () => 0, supportLauncher: () => '<a class="svc-support-launcher">old launcher</a>', openFeedback() {feedbackOpened++;}};
    const html = f.sidebar.render();
    for (const destination of ['#/cart', '#/member', '#/member/support', '#/coupons', '#/member/history']) assert.ok(html.includes('href="' + destination + '"'), destination);
    assert.match(html, /quick-rail-badge[^>]*>5</);
    assert.doesNotMatch(html, /问医生|插件版|href="#"/);
    await f.app.click({sidebarAction: 'feedback'});
    assert.equal(feedbackOpened, 1); assert.equal(f.calls.length, 0);
    await f.app.click({sidebarAction: 'top'});
    assert.ok(f.scrolls.length > 0);
    await f.app.click({sidebarAction: 'home'});
    assert.equal(f.app.json('ui.category'), '全部好物');
    assert.equal(f.app.json('ui.query'), '');
    assert.equal(f.app.json('ui.onlyFavorites'), false);
    assert.equal(f.app.json('ui.min'), ''); assert.equal(f.app.json('ui.max'), '');
    assert.doesNotMatch(f.app.navigate(''), /svc-support-launcher/, 'quick customer service replaces the old floating launcher');
    assert.equal(f.calls.filter(call => !['get', 'list', 'batch_get', 'query'].includes(call.action)).length, 0);
  } finally {f.app.dispose();}
});

test('reset and a newer personal history cannot be overwritten by an earlier read', async () => {
  let release;
  const f = setup((service, action, command) => service === 'catalog' ? new Promise(resolve => {release = () => resolve(command.id ? product('old-private') : {items: [product('old-private')]});}) : {sku_id: 'old-private', available: 8}, {history: [{id: 'old-private'}]});
  try {
    const older = f.sidebar.refresh(); await tick();
    f.sidebar.reset(); f.setHistory([{id: 'iphone'}]); await f.sidebar.refresh();
    release(); await older;
    assert.deepEqual(recentLinks(f.sidebar.render()), ['#/product/iphone']);
    assert.equal(f.app.json("products.some(p=>p.id==='old-private')"), false);
    assert.ok(f.primes.every(entry => entry.rows.every(row => row.sku_id !== 'old-private')));
  } finally {f.app.dispose();}
});

test('raw variant, parent metadata and stock consume the bootstrap primer with one physical HTTP request', async () => {
  const network = [];
  const variant = {...product('old-option'), product_id: 'old-parent', parent_id: 'old-parent', catalog_visible: false};
  const parent = {...product('old-parent'), name: '首页外的浏览商品', variants: [{sku_id: 'old-option', color: '蓝色', option: '标准版', price_cents: 12900}]};
  const stock = {sku_id: 'old-parent', available: 7, on_hand: 9, reserved: 2, allocated: 0};
  const app = createHarness(directory, 'http://shop.test', {globals: {
    LumoAuth: {user: () => ({id: 'buyer'}), isLoggedIn: () => true, gate: () => true, afterRender() {}},
    LumoMember: {model: () => ({history: [{id: 'old-parent', sku_id: 'old-option'}]}), afterRender() {}},
    async fetch(input) {
      network.push(input); assert.ok(input.endsWith('/storefront/bootstrap'), 'sidebar introduced an extra physical request ' + input);
      return {status: 200, ok: true, headers: {get: () => ''}, json: async () => ({data: {results: [
        {service: 'catalog', action: 'list', command: {data: {limit: 100}}, auth: false, data: {items: []}},
        {service: 'catalog', action: 'get', command: {id: 'old-option', data: {}}, auth: false, data: variant},
        {service: 'catalog', action: 'get', command: {id: 'old-parent', data: {}}, auth: false, data: parent},
        {service: 'inventory', action: 'get', command: {id: 'old-parent', data: {}}, auth: false, data: stock}
      ]}})};
    }
  }});
  try {
    for (const script of ['api-client.js', 'app.js', 'inventory.js', 'recommendations.js', 'sidebars.js']) app.load(script);
    app.ctx.LumoAPI.setToken('a'.repeat(64));
    await app.ctx.LumoAPI.prefetchBootstrap();
    await app.ctx.LumoSidebars.refresh();
    assert.equal(network.length, 1);
    assert.deepEqual(recentLinks(app.ctx.LumoSidebars.render()), ['#/product/old-parent']);
    assert.match(app.ctx.LumoSidebars.render(), /首页外的浏览商品/);
    assert.equal(app.ctx.LumoInventory.stockFor('old-parent'), 7);
  } finally {app.ctx.LumoAPI.endBootstrap(); app.dispose();}
});

test('the six-raw-SKU budget stays aligned with bootstrap when many options share one parent', async () => {
  const children = Array.from({length: 6}, (_, i) => 'old-option-' + i);
  const f = setup(undefined, {history: [...children.map(sku_id => ({id: 'parent-initial', sku_id})), {id: 'seventh', sku_id: 'seventh'}]});
  try {
    f.app.evaluate('products.push(' + JSON.stringify({...product('parent-initial'), price: 129, skuId: 'parent-initial', variants: children.map(sku_id => ({sku_id}))}) + ')');
    f.stocks.set('parent-initial', {sku_id: 'parent-initial', available: 4});
    await f.sidebar.refresh();
    assert.deepEqual(recentLinks(f.sidebar.render()), ['#/product/parent-initial']);
    assert.equal(f.calls.length, 0, 'canonical de-duplication must not replace one of the first six raw SKUs with a seventh unprimed read');
  } finally {f.app.dispose();}
});

test('an older member read cannot repopulate the rail after a newer token loaded different history', async () => {
  let release, historyReads = 0;
  const f = setup(async (service, action, command) => {
    if (service === 'engagement' && command.data.kind === 'history') {
      historyReads++;
      if (historyReads === 1) return new Promise(resolve => {release = () => resolve({items: [{id: 'old-view', sku_id: 'iphone'}]});});
      return {items: [{id: 'new-view', sku_id: 'coffee'}]};
    }
    return {items: []};
  });
  try {
    f.app.load('member.js');
    const older = f.app.ctx.LumoMember.refresh(); await tick();
    f.setContext({token: 'new-session'});
    assert.equal(f.app.ctx.LumoMember.model().history.length, 0);
    await f.app.ctx.LumoMember.refresh(); await f.sidebar.refresh();
    assert.deepEqual(recentLinks(f.sidebar.render()), ['#/product/coffee']);
    release(); await older; await f.sidebar.refresh();
    assert.deepEqual(recentLinks(f.sidebar.render()), ['#/product/coffee']);
  } finally {f.app.dispose();}
});

test('clearing actual member history hydrates an off-page favorite newly admitted to the recent rail', async () => {
  const f = await accountRail(['iphone', 'macbook', 'headphones', 'watch', 'speaker', 'sneakers'], ['outside-page']);
  try {
    await f.app.click({memberAction: 'confirm-clear-history'});
    assert.equal(f.app.ctx.LumoMember.model().history.length, 0);
    assert.deepEqual(recentLinks(f.sidebar.render()), ['#/product/outside-page']);
    assert.equal(f.calls.filter(call => call.service === 'engagement' && call.action === 'delete').length, 6);
    assert.equal(f.calls.filter(call => call.service === 'catalog' && call.command.id === 'outside-page').length, 1);
    const reads = f.calls.length;
    f.sidebar.render(); f.sidebar.render();
    assert.equal(f.calls.length, reads, 'the mutation refresh must not turn rendering into a fetch');
  } finally {f.app.dispose();}
});

test('removing actual member history hydrates the off-page favorite that enters the first six', async () => {
  const f = await accountRail(['iphone', 'macbook', 'headphones', 'watch', 'speaker', 'sneakers'], ['outside-page']);
  try {
    await f.app.click({memberAction: 'remove-history', id: 'iphone'});
    assert.equal(f.app.ctx.LumoMember.model().history.length, 5);
    assert.deepEqual(recentLinks(f.sidebar.render()), ['macbook', 'headphones', 'watch', 'speaker', 'sneakers', 'outside-page'].map(id => '#/product/' + id));
    assert.equal(f.calls.filter(call => call.service === 'engagement' && call.action === 'delete').length, 1);
    assert.equal(f.calls.filter(call => call.service === 'catalog' && call.command.id === 'outside-page').length, 1);
  } finally {f.app.dispose();}
});

test('the actual product-card favorite action hydrates an off-page favorite exposed by cancellation', async () => {
  const f = await accountRail([], ['iphone', 'macbook', 'headphones', 'watch', 'speaker', 'sneakers', 'outside-page']);
  try {
    await f.app.click({action: 'favorite', id: 'iphone'});
    // The app click handler deliberately launches toggleFavorite without awaiting it.
    for (let attempt = 0; attempt < 20 && !recentLinks(f.sidebar.render()).includes('#/product/outside-page'); attempt++) await tick();
    assert.deepEqual(f.app.json('state.favoriteSKUs'), ['macbook', 'headphones', 'watch', 'speaker', 'sneakers', 'outside-page']);
    assert.deepEqual(recentLinks(f.sidebar.render()), ['macbook', 'headphones', 'watch', 'speaker', 'sneakers', 'outside-page'].map(id => '#/product/' + id));
    assert.equal(f.calls.filter(call => call.service === 'engagement' && call.action === 'delete').length, 1);
    assert.equal(f.calls.filter(call => call.service === 'catalog' && call.command.id === 'outside-page').length, 1);
  } finally {f.app.dispose();}
});
