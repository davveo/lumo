'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');

const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const clone = (value) => JSON.parse(JSON.stringify(value));
const tick = () => new Promise((resolve) => setImmediate(resolve));
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
const product = (id) => ({
  id,
  name: '近期商品 ' + id,
  price_cents: 12900,
  category: '手机数码',
  brand: 'LUMO',
  image: 'headphones.jpg',
  active: true,
  recent_source: 'featured',
});
const result = (ids, personalized = false) => ({
  items: ids.map((id) => (typeof id === 'string' ? product(id) : id)),
  inventory_items: ids.map((id) => ({
    sku_id: typeof id === 'string' ? id : id.id,
    available: 7,
    on_hand: 9,
    reserved: 2,
    allocated: 0,
  })),
  personalized,
  degraded: false,
  limit: 6,
  count: ids.length,
});
const recentLinks = (html) =>
  [...html.matchAll(/<a\b([^>]+)>/g)]
    .filter(([, attrs]) => /\bclass="[^"]*\binterest-item\b/.test(attrs))
    .map(([, attrs]) => /\bhref="([^"]+)"/.exec(attrs)?.[1]);
const links = (ids) => ids.map((id) => '#/product/' + id);
const skeleton = (html) => assert.match(html, /class="sk-loading [^"]+"[^>]*role="status"[^>]*aria-busy="true"/);

function setup(handler = async () => result([]), options = {}) {
  let user = options.user === undefined ? 'buyer' : options.user,
    token = 'session-a',
    base = 'http://shop.test',
    ticket = 0;
  const calls = [],
    primes = [],
    scrolls = [],
    preferences = new Map(),
    preferenceWrites = [],
    stocks = new Map();
  const app = createHarness(directory, base, {
    globals: {
      LumoAPI: {
        getBase: () => base,
        getToken: () => token,
        async call(service, action, command = {}, settings = {}) {
          calls.push({ service, action, command: clone(command), options: clone(settings) });
          return handler(service, action, command, settings);
        },
      },
      LumoAuth: {
        user: () => (user ? { id: user } : null),
        isLoggedIn: () => !!user,
        gate: () => true,
        requireLogin: () => !!user,
        afterRender() {},
      },
      LumoMember: { model: () => ({ history: clone(options.history || []) }), afterRender() {} },
      LumoInventory: {
        snapshot: (value) => stocks.get(typeof value === 'string' ? value : value?.skuId || value?.sku_id || value?.id),
        stockFor: (value) =>
          stocks.get(typeof value === 'string' ? value : value?.skuId || value?.sku_id || value?.id)?.available || 0,
        hydrationTicket: () => ++ticket,
        prime(rows, version) {
          primes.push({ rows: clone(rows), version });
          for (const row of rows) stocks.set(row.sku_id, clone(row));
        },
      },
      localStorage: {
        getItem: (key) => preferences.get(key) ?? null,
        setItem(key, value) {
          preferences.set(key, String(value));
          preferenceWrites.push({ key, value: String(value) });
        },
        removeItem: (key) => preferences.delete(key),
      },
      scrollTo: (...args) => scrolls.push(args),
    },
  });
  app.load('skeleton.js');
  app.load('app.js');
  app.evaluate('products.push(...presentationProducts)');
  for (const p of app.json('products'))
    stocks.set(p.id, { sku_id: p.id, available: 20, on_hand: 20, allocated: 0, reserved: 0 });
  app.load('recommendations.js');
  app.ctx.LumoRecommendations.catalogChanged();
  app.load('sidebars.js');
  return {
    app,
    calls,
    primes,
    stocks,
    scrolls,
    preferences,
    preferenceWrites,
    sidebar: app.ctx.LumoSidebars,
    setContext(next) {
      if ('user' in next) user = next.user;
      if ('token' in next) token = next.token;
      if ('base' in next) base = next.base;
    },
  };
}

async function accountRail(historyIDs, favoriteIDs) {
  let history = historyIDs.map((sku_id, index) => ({ id: 'history-' + index, sku_id }));
  let favorites = favoriteIDs.map((sku_id, index) => ({ id: 'favorite-' + index, sku_id }));
  const f = setup(async (service, action, command) => {
    if (service === 'storefront') {
      assert.equal(action, 'recent');
      const unique = [
        ...new Set([
          ...history.map((row) => row.sku_id),
          ...favorites.map((row) => row.sku_id),
          'suggested-1',
          'suggested-2',
          'suggested-3',
          'suggested-4',
          'suggested-5',
          'suggested-6',
        ]),
      ].slice(0, 6);
      return result(unique, true);
    }
    if (service === 'engagement') {
      const kind = command.data?.kind;
      if (action === 'list') return { items: clone(kind === 'history' ? history : favorites) };
      assert.equal(action, 'delete');
      const keep = (row) => (command.id ? row.id !== command.id : row.sku_id !== command.data.sku_id);
      if (kind === 'history') history = history.filter(keep);
      else favorites = favorites.filter(keep);
      return {};
    }
    assert.ok(!['catalog', 'inventory'].includes(service), 'sidebar must never fan out product or stock reads');
    return { items: [] };
  });
  f.app.load('member.js');
  await f.app.ctx.LumoMember.refresh();
  await f.sidebar.refresh();
  assert.equal(recentLinks(f.sidebar.render()).length, 6);
  return f;
}

test('guest rail starts with six shapes and shows real backend featured goods through one public aggregate', async (t) => {
  const ids = ['a', 'b', 'c', 'd', 'e', 'f'],
    gate = deferred(),
    f = setup(() => gate.promise, { user: null });
  t.after(() => f.app.dispose());
  skeleton(f.sidebar.render());
  assert.match(f.sidebar.render(), /精选好物/);
  assert.equal(f.calls.length, 0);
  const first = f.sidebar.refresh(),
    duplicate = f.sidebar.refresh();
  assert.equal(first, duplicate);
  assert.equal(f.calls.length, 1);
  skeleton(f.sidebar.render());
  gate.resolve(result(ids));
  await first;
  assert.deepEqual(recentLinks(f.sidebar.render()), links(ids));
  assert.match(f.sidebar.render(), /精选好物/);
  assert.doesNotMatch(f.sidebar.render(), /最近感兴趣|class="sk-loading/);
  assert.deepEqual(f.calls, [{ service: 'storefront', action: 'recent', command: {}, options: { auth: false } }]);
  for (let index = 0; index < 8; index++) f.sidebar.render();
  assert.equal(f.calls.length, 1, 'rendering cannot issue reads');
  assert.equal(f.primes.length, 1);
  assert.equal(f.stocks.get('a').available, 7);
});

test('member rail trusts canonical backend order, sources and fresh metadata instead of browser history', async (t) => {
  const ids = ['iphone', 'outside-parent', 'coffee', 'watch', 'headphones', 'sneakers'];
  const rows = ids.map((id, index) => ({
    ...product(id),
    name: id === 'iphone' ? '服务端最新商品名称' : product(id).name,
    recent_source: ['history', 'favorite', 'recommendation'][index % 3],
    recommendation_reason: '真实推荐理由',
  }));
  const f = setup(async () => result(rows, true), { history: [{ id: 'stale-private-sku' }] });
  t.after(() => f.app.dispose());
  f.app.evaluate("state.favorites=['not-returned']");
  await f.sidebar.refresh();
  assert.deepEqual(recentLinks(f.sidebar.render()), links(ids));
  assert.match(f.sidebar.render(), /服务端最新商品名称/);
  assert.match(f.sidebar.render(), /title="真实推荐理由"/);
  assert.match(f.sidebar.render(), /最近感兴趣/);
  assert.doesNotMatch(f.sidebar.render(), /stale-private-sku|not-returned/);
  assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].service, 'storefront');
  assert.equal(f.calls[0].options.auth, true);
  assert.equal(f.app.json("products.find(p=>p.id==='outside-parent').price"), 129);
  assert.deepEqual(
    clone(f.sidebar.model()).items.map((row) => row.recent_source),
    rows.map((row) => row.recent_source),
  );
});

test('invalid, invisible and duplicate aggregate rows cannot invent goods and the rail is capped at six', async (t) => {
  const raw = [
    product('a'),
    product('a'),
    { ...product('disabled'), active: false },
    { ...product('hidden'), catalog_visible: false },
    { id: 'missing-fields' },
    { ...product('bad-price'), price_cents: 'NaN' },
    ...['b', 'c', 'd', 'e', 'f', 'g'].map(product),
  ];
  const f = setup(async () => result(raw));
  t.after(() => f.app.dispose());
  await f.sidebar.refresh();
  assert.deepEqual(recentLinks(f.sidebar.render()), links(['a', 'b', 'c', 'd', 'e', 'f']));
  assert.equal(f.sidebar.model().count, 6);
  assert.doesNotMatch(f.sidebar.render(), /#\/product\/(?:missing-fields|disabled|hidden|bad-price)/);
  assert.equal(f.calls.length, 1);
});

test('successful empty, malformed response and initial failure have distinct retry states', async (t) => {
  let mode = 'failure';
  const f = setup(async () => {
    if (mode === 'failure') throw Error('兴趣服务暂不可用');
    if (mode === 'malformed') return { items: [] };
    return result([]);
  });
  t.after(() => f.app.dispose());
  await f.sidebar.refresh();
  assert.match(f.sidebar.render(), /role="alert"/);
  assert.match(f.sidebar.render(), /兴趣服务暂不可用/);
  assert.doesNotMatch(f.sidebar.render(), /暂时没有可推荐|class="sk-loading/);
  mode = 'malformed';
  await f.sidebar.refresh();
  assert.match(f.sidebar.render(), /返回格式不完整/);
  assert.equal(f.sidebar.model().loaded, false);
  mode = 'empty';
  await f.app.click({ sidebarAction: 'retry-interest' });
  await tick();
  assert.match(f.sidebar.render(), /暂时没有可推荐的在售好物/);
  assert.doesNotMatch(f.sidebar.render(), /role="alert"|class="sk-loading/);
  assert.equal(f.calls.length, 3);
});

test('background refresh preserves genuine cards on slow or failed reads and retry only requests recent', async (t) => {
  let gate = null;
  const f = setup(() => (gate ? gate.promise : result(['real'])));
  t.after(() => f.app.dispose());
  await f.sidebar.refresh();
  gate = deferred();
  const pending = f.sidebar.refresh();
  assert.deepEqual(recentLinks(f.sidebar.render()), links(['real']));
  assert.match(f.sidebar.render(), /aria-busy="true"/);
  assert.doesNotMatch(f.sidebar.render(), /class="sk-loading/);
  gate.reject(Error('更新暂不可用'));
  await pending;
  assert.deepEqual(recentLinks(f.sidebar.render()), links(['real']));
  assert.match(f.sidebar.render(), /更新暂不可用/);
  gate = null;
  await f.app.click({ sidebarAction: 'retry-interest' });
  await tick();
  assert.doesNotMatch(f.sidebar.render(), /更新暂不可用/);
  assert.equal(f.calls.length, 3);
  assert.ok(f.calls.every((row) => row.service === 'storefront' && row.action === 'recent'));
});

for (const change of [{ user: null }, { user: 'other' }, { token: 'session-b' }, { base: 'http://other.test' }]) {
  test('cached and late aggregate goods stay isolated across scope change ' + JSON.stringify(change), async (t) => {
    let gate = null;
    const f = setup(() => (gate ? gate.promise : result(['old-private'], true)));
    t.after(() => f.app.dispose());
    await f.sidebar.refresh();
    assert.deepEqual(recentLinks(f.sidebar.render()), links(['old-private']));
    gate = deferred();
    const old = f.sidebar.refresh();
    f.setContext(change);
    skeleton(f.sidebar.render());
    assert.equal(recentLinks(f.sidebar.render()).length, 0);
    gate.resolve(result(['late-private'], true));
    await old;
    assert.equal(recentLinks(f.sidebar.render()).length, 0);
    assert.equal(f.app.json("products.some(p=>['old-private','late-private'].includes(p.id))"), false);
    assert.equal(f.primes.length, 1);
    assert.equal(f.sidebar.model().loaded, false);
  });
}

test('reset and a newer aggregate cannot be overwritten by an earlier read', async (t) => {
  const oldGate = deferred();
  let count = 0;
  const f = setup(() => (++count === 1 ? oldGate.promise : result(['new'])));
  t.after(() => f.app.dispose());
  const older = f.sidebar.refresh();
  f.sidebar.reset();
  await f.sidebar.refresh();
  oldGate.resolve(result(['old-private'], true));
  await older;
  assert.deepEqual(recentLinks(f.sidebar.render()), links(['new']));
  assert.equal(f.app.json("products.some(p=>p.id==='old-private')"), false);
  assert.equal(f.primes.length, 1);
});

test('completed behavior write invalidates an old read before its explicit aggregate refresh', async (t) => {
  const oldGate = deferred();
  let count = 0;
  const f = setup(() => (++count === 1 ? oldGate.promise : result(['new-favorite'], true)));
  t.after(() => f.app.dispose());
  const older = f.sidebar.refresh();
  f.app.ctx.dispatchEvent(
    new f.app.ctx.CustomEvent('lumo:api-request', { detail: { service: 'engagement', action: 'put', ok: true } }),
  );
  assert.equal(f.calls.length, 1, 'diagnostic events do not create a request');
  await f.sidebar.refresh();
  oldGate.resolve(result(['pre-write'], true));
  await older;
  assert.deepEqual(recentLinks(f.sidebar.render()), links(['new-favorite']));
  assert.equal(f.primes.length, 1);
  assert.equal(f.calls.length, 2);
});

test('payment, checkout, login and editing routes leave business forms unobstructed', (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  for (const route of ['login', 'payment', 'checkout', 'editor']) {
    f.app.ctx.location.hash = '#/' + route;
    assert.equal(f.sidebar.render(), '', route);
  }
  f.app.ctx.location.hash = '#/';
  assert.match(f.sidebar.render(), /商城快捷工具/);
});

test('rail collapse saves only interface preferences and never refreshes backend goods', async (t) => {
  const f = setup(async () => result(['real']));
  t.after(() => f.app.dispose());
  await f.sidebar.refresh();
  const reads = f.calls.length;
  await f.app.click({ sidebarAction: 'toggle-interest' });
  assert.match(f.sidebar.render(), /interest-rail-reopen/);
  await f.app.click({ sidebarAction: 'toggle-quick' });
  assert.match(f.sidebar.render(), /quick-rail-reopen/);
  assert.equal(f.calls.length, reads);
  assert.deepEqual(JSON.parse(f.preferences.get('lumo-sidebar-ui-v1')), {
    interestCollapsed: true,
    quickCollapsed: true,
  });
  assert.ok(f.preferenceWrites.every((row) => !/real|商品|history/i.test(row.value)));
  await f.app.click({ sidebarAction: 'toggle-interest' });
  await f.app.click({ sidebarAction: 'toggle-quick' });
  assert.deepEqual(recentLinks(f.sidebar.render()), links(['real']));
  assert.equal(f.calls.length, reads);
});

test('quick tools expose real destinations, cart quantity and feedback without submitting a ticket', async (t) => {
  const f = setup();
  let feedbackOpened = 0;
  t.after(() => f.app.dispose());
  f.app.evaluate(
    "state.cart=[{id:'iphone',qty:2},{id:'macbook',qty:3}];ui.category='家用电器';ui.query='冰箱';ui.onlyFavorites=true;ui.min='10';ui.max='1000'",
  );
  f.app.ctx.LumoServices = {
    afterRender() {},
    unreadCount: () => 0,
    supportLauncher: () => '<a class="svc-support-launcher">old launcher</a>',
    openFeedback() {
      feedbackOpened++;
    },
  };
  const html = f.sidebar.render();
  for (const destination of ['#/cart', '#/member', '#/member/support', '#/coupons', '#/member/history'])
    assert.ok(html.includes('href="' + destination + '"'));
  assert.match(html, /quick-rail-badge[^>]*>5</);
  assert.doesNotMatch(html, /问医生|插件版|href="#"/);
  await f.app.click({ sidebarAction: 'feedback' });
  assert.equal(feedbackOpened, 1);
  await f.app.click({ sidebarAction: 'top' });
  assert.ok(f.scrolls.length > 0);
  await f.app.click({ sidebarAction: 'home' });
  assert.equal(f.app.json('ui.category'), '全部好物');
  assert.equal(f.app.json('ui.query'), '');
  assert.equal(f.app.json('ui.onlyFavorites'), false);
  assert.equal(f.app.json('ui.min'), '');
  assert.equal(f.app.json('ui.max'), '');
  assert.doesNotMatch(f.app.navigate(''), /svc-support-launcher/);
  assert.equal(f.calls.length, 0);
});

for (const interaction of ['clear', 'remove', 'favorite']) {
  test(
    'actual ' + interaction + ' interaction refreshes one backend recent aggregate with recommended fill',
    async (t) => {
      const ids = ['iphone', 'macbook', 'headphones', 'watch', 'speaker', 'sneakers'];
      const f = await accountRail(
        interaction === 'favorite' ? [] : ids,
        interaction === 'favorite' ? [...ids, 'outside-page'] : ['outside-page'],
      );
      t.after(() => f.app.dispose());
      const before = f.calls.filter((row) => row.service === 'storefront' && row.action === 'recent').length;
      if (interaction === 'clear') await f.app.click({ memberAction: 'confirm-clear-history' });
      else if (interaction === 'remove') await f.app.click({ memberAction: 'remove-history', id: 'iphone' });
      else {
        await f.app.click({ action: 'favorite', id: 'iphone' });
        for (
          let attempt = 0;
          attempt < 20 && !recentLinks(f.sidebar.render()).includes('#/product/outside-page');
          attempt++
        )
          await tick();
      }
      assert.equal(recentLinks(f.sidebar.render()).length, 6);
      assert.ok(recentLinks(f.sidebar.render()).includes('#/product/outside-page'));
      assert.equal(f.calls.filter((row) => row.service === 'storefront' && row.action === 'recent').length, before + 1);
      assert.equal(f.calls.filter((row) => ['catalog', 'inventory'].includes(row.service)).length, 0);
      const reads = f.calls.length;
      f.sidebar.render();
      f.sidebar.render();
      assert.equal(f.calls.length, reads);
    },
  );
}

for (const member of [false, true]) {
  test(
    (member ? 'member' : 'guest') + ' recent consumes the matching bootstrap primer with one physical HTTP request',
    async (t) => {
      const network = [],
        ids = ['one', 'two', 'three', 'four', 'five', 'six'];
      const app = createHarness(directory, 'http://shop.test', {
        globals: {
          LumoAuth: {
            user: () => (member ? { id: 'buyer' } : null),
            isLoggedIn: () => member,
            gate: () => true,
            afterRender() {},
          },
          async fetch(input, options) {
            network.push({ url: input, options });
            assert.ok(input.endsWith('/storefront/bootstrap'), 'unexpected extra request ' + input);
            return {
              status: 200,
              ok: true,
              headers: { get: () => '' },
              json: async () => ({
                data: {
                  results: [
                    {
                      service: 'catalog',
                      action: 'list',
                      command: { data: { limit: 100 } },
                      auth: false,
                      data: { items: [] },
                    },
                    {
                      key: 'bootstrap-recent',
                      service: 'storefront',
                      action: 'recent',
                      command: { data: {} },
                      auth: member,
                      data: result(ids, member),
                    },
                  ],
                },
              }),
            };
          },
        },
      });
      t.after(() => {
        app.ctx.LumoAPI.endBootstrap();
        app.dispose();
      });
      for (const script of [
        'skeleton.js',
        'api-client.js',
        'app.js',
        'inventory.js',
        'recommendations.js',
        'sidebars.js',
      ])
        app.load(script);
      if (member) app.ctx.LumoAPI.setToken('a'.repeat(64));
      await app.ctx.LumoAPI.prefetchBootstrap();
      await app.ctx.LumoSidebars.refresh();
      assert.equal(network.length, 1);
      assert.deepEqual(recentLinks(app.ctx.LumoSidebars.render()), links(ids));
      assert.equal(app.ctx.LumoInventory.stockFor('one'), 7);
      assert.equal(!!network[0].options.headers.Authorization, member);
    },
  );
}

test('recent is directly deduplicated and stays outside a concurrent read batch', async (t) => {
  const gate = deferred(),
    network = [];
  const app = createHarness(directory, 'http://shop.test', {
    globals: {
      async fetch(url, options) {
        const body = JSON.parse(options.body);
        network.push({ url, options, body });
        if (url.endsWith('/storefront/recent')) {
          await gate.promise;
          return { status: 200, ok: true, headers: { get: () => '' }, json: async () => ({ data: result(['one']) }) };
        }
        assert.ok(url.endsWith('/storefront/batch'));
        assert.ok(body.data.requests.every((row) => row.service !== 'storefront'));
        return {
          status: 200,
          ok: true,
          headers: { get: () => '' },
          json: async () => ({
            data: { results: body.data.requests.map((row) => ({ ...row, data: product(row.command.id) })) },
          }),
        };
      },
    },
  });
  t.after(() => app.dispose());
  app.load('api-client.js');
  const api = app.ctx.LumoAPI;
  api.enableReadBatching();
  api.setToken('a'.repeat(64));
  const first = api.call('storefront', 'recent', {}, { auth: true }),
    same = api.call('storefront', 'recent', { data: {} }, { auth: true });
  assert.equal(first, same);
  await api.call('catalog', 'get', { id: 'one' }, { auth: false });
  gate.resolve();
  await first;
  assert.equal(network.length, 2);
  assert.equal(network[0].options.headers.Authorization, 'Bearer ' + 'a'.repeat(64));
  assert.equal(network[1].options.headers.Authorization, undefined);
  await api.call('storefront', 'recent', {}, { auth: true });
  assert.equal(network.length, 3, 'completed results must not become a stale global cache');
});

for (const change of ['token', 'mutation']) {
  test('direct recent rejects a late result after ' + change + ' invalidation', async (t) => {
    const gate = deferred();
    const app = createHarness(directory, 'http://shop.test', {
      globals: {
        async fetch(url) {
          if (url.endsWith('/storefront/recent')) await gate.promise;
          return {
            status: 200,
            ok: true,
            headers: { get: () => '' },
            json: async () => ({ data: url.endsWith('/storefront/recent') ? result(['private']) : {} }),
          };
        },
      },
    });
    t.after(() => app.dispose());
    app.load('api-client.js');
    const api = app.ctx.LumoAPI;
    api.setToken('a'.repeat(64));
    const pending = api.call('storefront', 'recent', {}, { auth: true });
    const rejected = assert.rejects(pending, (error) => error.code === 'SessionChanged');
    if (change === 'token') api.setToken('b'.repeat(64));
    else await api.call('engagement', 'put', { data: { kind: 'favorite', sku_id: 'private' } });
    gate.resolve();
    await rejected;
  });
}
