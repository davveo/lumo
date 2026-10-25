'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((resolve) => setImmediate(resolve));
const clone = (value) => JSON.parse(JSON.stringify(value));
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
function product(id, extra = {}) {
  return {
    id,
    product_id: id,
    name: '真实商品 ' + id,
    image: 'coffee.jpg',
    category: '食品生鲜',
    brand: '真实品牌',
    price_cents: 4590,
    active: true,
    color: '黑色',
    option: '标准版',
    ...extra,
  };
}
async function setup(t, options = {}) {
  let user = 'buyer',
    token = 'token-a',
    base = 'http://commerce-metadata.test',
    handler;
  const app = createHarness(directory, base);
  t.after(() => app.dispose());
  const clicks = [],
    addListener = app.document.addEventListener;
  app.document.addEventListener = (type, listener, capture) => {
    if (type === 'click') clicks.push(listener);
    addListener(type, listener, capture);
  };
  const calls = [],
    records = new Map(),
    cart = [],
    favorites = [],
    compares = [];
  const publicRows = Array.from({ length: 100 }, (_, index) => product('public-' + index));
  app.ctx.LumoAuth = {
    user: () => (user ? { id: user, role: 'user' } : null),
    isLoggedIn: () => !!user,
    requireLogin: () => !!user,
    gate: () => true,
    afterRender() {},
  };
  app.ctx.LumoAPI = {
    getToken: () => token,
    getBase: () => base,
    async call(service, action, command = {}, request = {}) {
      calls.push({ service, action, command: clone(command), request: clone(request) });
      if (handler) {
        const result = await handler(service, action, command, request);
        if (result !== undefined) return result;
      }
      if (service === 'catalog' && action === 'list') return { items: publicRows, total: 10000 };
      if (service === 'catalog' && action === 'batch_get')
        return {
          items: command.data.ids.map((id) => records.get(id)).filter(Boolean),
          missing_ids: command.data.ids.filter((id) => !records.has(id)),
        };
      if (service === 'inventory' && action === 'batch_get')
        return {
          items: command.data.ids.map((sku_id) => ({ sku_id, available: 30 })),
          missing_ids: [],
        };
      if (service === 'cart' && action === 'list') {
        const offset = command.data?.offset || 0;
        return {
          items: cart.slice(offset, offset + (command.data?.limit || 20)),
          ...(options.legacyCart ? {} : { total: cart.length }),
        };
      }
      if (service === 'engagement' && action === 'list')
        return {
          items: (command.data.kind === 'favorite' ? favorites : command.data.kind === 'compare' ? compares : []).map(
            (sku_id) => ({ sku_id }),
          ),
        };
      if (service === 'engagement' && action === 'delete') {
        const index = compares.indexOf(command.data.sku_id);
        if (index >= 0) compares.splice(index, 1);
        return {};
      }
      return { items: [] };
    },
  };
  app.load('app.js');
  app.load('inventory.js');
  app.load('discovery.js');
  await tick();
  return {
    app,
    calls,
    records,
    cart,
    favorites,
    compares,
    async selectAll() {
      const target = new Element();
      target.dataset = { action: 'select-all' };
      target.checked = true;
      const event = { target, preventDefault() {}, stopImmediatePropagation() {}, stopPropagation() {} };
      for (const listener of clicks) await listener(event);
    },
    setHandler(next) {
      handler = next;
    },
    switchScope(nextUser = 'other', nextToken = 'token-b', nextBase = base) {
      user = nextUser;
      token = nextToken;
      base = nextBase;
      app.ctx.dispatchEvent(
        new app.ctx.CustomEvent(nextBase === 'http://commerce-metadata.test' ? 'lumo:login' : 'lumo:api-scope-changed'),
      );
    },
  };
}

test('cold cart, favorites and compare resolve outside-home SKUs and their actual parent without per-item requests', async (t) => {
  const f = await setup(t);
  f.records.set(
    'outside--1',
    product('outside--1', { product_id: 'outside', color: '蓝色', option: '大份', price_cents: 6900 }),
  );
  f.records.set(
    'outside',
    product('outside', {
      name: '目录外真实商品',
      variants: [
        { sku_id: 'outside', color: '黑色', option: '标准版', price_cents: 4590 },
        { sku_id: 'outside--1', color: '蓝色', option: '大份', price_cents: 6900 },
      ],
    }),
  );
  f.cart.push({ id: 'row-1', sku_id: 'outside--1', quantity: 2 });
  f.favorites.push('outside--1');
  f.compares.push('outside--1');
  f.app.load('member.js');
  await f.app.ctx.LumoCommerce.refreshCatalog();
  assert.equal(f.app.json('products.length'), 100);
  f.calls.length = 0;
  await Promise.all([
    f.app.ctx.LumoCommerce.refreshCart(),
    f.app.ctx.LumoMember.refresh(false, ['favorites']),
    f.app.ctx.LumoDiscovery.refresh(),
  ]);
  assert.deepEqual(
    f.app.json(
      'state.cart.map(({id,sku_id,name,color,option,unitPrice,qty}) => ({id,sku_id,name,color,option,unitPrice,qty}))',
    ),
    [
      {
        id: 'outside',
        sku_id: 'outside--1',
        name: '目录外真实商品',
        color: '蓝色',
        option: '大份',
        unitPrice: 69,
        qty: 2,
      },
    ],
  );
  assert.deepEqual(f.app.json('state.favorites'), ['outside']);
  assert.match(f.app.ctx.LumoDiscovery.page(), /目录外真实商品/);
  assert.deepEqual(f.app.json('state.discovery.compareIds'), ['outside']);
  assert.equal(f.app.ctx.LumoInventory.validate(f.app.json('state.cart')), null);
  assert.match(f.app.navigate('cart'), /<div class="cart-unit-price">[^]*?<small>¥<\/small>69<\/span><small>小计 ¥138/);
  assert.doesNotMatch(f.app.navigate('cart'), /商品信息加载中/);
  const batches = f.calls.filter((row) => row.service === 'catalog' && row.action === 'batch_get');
  assert.deepEqual(
    batches.flatMap((row) => row.command.data.ids),
    ['outside--1', 'outside'],
  );
  assert.equal(f.calls.filter((row) => row.service === 'inventory').length, 1);
  assert.equal(
    f.calls.filter((row) => ['get', 'list'].includes(row.action) && ['catalog', 'inventory'].includes(row.service))
      .length,
    0,
  );
  await f.app.ctx.LumoCommerce.refreshCatalog();
  assert.equal(
    f.app.json('actualProduct("outside--1").id'),
    'outside',
    'a slower public catalogue refresh retains referenced metadata',
  );
  await f.app.click({ discoveryAction: 'toggle-compare', id: 'outside' });
  assert.equal(
    f.calls.find((row) => row.service === 'engagement' && row.action === 'delete').command.data.sku_id,
    'outside--1',
  );
  assert.deepEqual(f.app.json('state.discovery.compareIds'), []);
});

test('reference hydration deduplicates and bounds catalogue and inventory batches to 500', async (t) => {
  const f = await setup(t),
    ids = Array.from({ length: 1001 }, (_, index) => 'outside-' + index);
  ids.forEach((id) => f.records.set(id, product(id)));
  await f.app.ctx.LumoCommerce.hydrateProducts([...ids, ...ids]);
  for (const service of ['catalog', 'inventory']) {
    const calls = f.calls.filter((row) => row.service === service);
    assert.deepEqual(
      calls.map((row) => row.command.data.ids.length),
      [500, 500, 1],
    );
    assert.equal(new Set(calls.flatMap((row) => row.command.data.ids)).size, 1001);
    assert.ok(calls.every((row) => row.action === 'batch_get'));
  }
  assert.equal(f.app.json('products.length'), 1001);
});

for (const legacyCart of [false, true])
  test(`cart loads every row with explicit 100-row pages (${legacyCart ? 'legacy without total' : 'precise total'})`, async (t) => {
    const f = await setup(t, { legacyCart }),
      count = legacyCart ? 200 : 201;
    for (let index = 0; index < count; index++) {
      const id = 'cart-' + index;
      f.records.set(id, product(id));
      f.cart.push({ id: 'row-' + index, sku_id: id, quantity: (index % 3) + 1 });
    }
    await f.app.ctx.LumoCommerce.refreshCart();
    const reads = f.calls.filter((row) => row.service === 'cart');
    assert.deepEqual(
      reads.map((row) => row.command.data),
      [0, 100, 200].map((offset) => ({ limit: 100, offset })),
    );
    assert.equal(f.app.json('state.cart.length'), count);
    assert.equal(
      f.app.json('cartCount()'),
      f.cart.reduce((sum, row) => sum + row.quantity, 0),
    );
    assert.equal(f.app.json('state.cart.at(-1).name'), '真实商品 cart-' + (count - 1));
    assert.equal(f.app.ctx.LumoCommerce.readState('cart').loaded, true);
  });

test('precise totals support an older server returning shorter pages and reject a repeated incomplete page', async (t) => {
  const f = await setup(t);
  for (let index = 0; index < 45; index++) {
    const id = 'short-' + index;
    f.records.set(id, product(id));
    f.cart.push({ id, sku_id: id, quantity: 1 });
  }
  f.setHandler((service, action, command) =>
    service === 'cart'
      ? {
          items: f.cart.slice(command.data.offset, command.data.offset + 20),
          total: f.cart.length,
        }
      : undefined,
  );
  await f.app.ctx.LumoCommerce.refreshCart();
  assert.equal(f.app.json('state.cart.length'), 45);
  assert.deepEqual(
    f.calls.filter((row) => row.service === 'cart').map((row) => row.command.data.offset),
    [0, 20, 40],
  );
  f.setHandler((service) => (service === 'cart' ? { items: f.cart.slice(0, 20), total: 45 } : undefined));
  await assert.rejects(f.app.ctx.LumoCommerce.refreshCart(), /分页数据重复/);
  assert.equal(f.app.json('state.cart.length'), 45, 'a broken pagination response cannot truncate the last real cart');
});

test('missing referenced SKUs render explicitly unavailable, cannot be selected, and can recover on a later refresh', async (t) => {
  const f = await setup(t);
  f.cart.push({ id: 'gone-row', product_id: 'gone-parent', sku_id: 'gone', quantity: 1 });
  f.favorites.push('gone');
  f.compares.push('gone');
  await Promise.all([
    f.app.ctx.LumoCommerce.refreshCart(),
    f.app.ctx.LumoCommerce.refreshFavorites(),
    f.app.ctx.LumoDiscovery.refresh(),
  ]);
  const html = f.app.navigate('cart');
  assert.match(html, /商品已不可用/);
  assert.doesNotMatch(html, /商品信息加载中/);
  assert.match(html, /data-action="select" data-index="0"[^>]*disabled/);
  assert.deepEqual(f.app.json('state.cart.map(row => [row.unavailable,row.selected])'), [[true, false]]);
  await f.selectAll();
  assert.equal(f.app.json('state.cart[0].selected'), false, 'select all cannot select a missing SKU');
  assert.match(f.app.ctx.LumoDiscovery.page(), /商品已不可用/);
  f.app.ctx.LumoMember = {
    model: () => ({ profile: {}, history: [] }),
    counts: () => ({}),
    readState: () => ({ loaded: true, loading: false, error: '' }),
  };
  f.app.load('member-views.js');
  const favoritePage = f.app.ctx.LumoMemberView.page('favorites');
  assert.match(favoritePage, /商品已不可用/);
  assert.doesNotMatch(favoritePage, /<div class="price-line">[^<]*0\.00/);
  f.records.set('gone', product('gone', { product_id: 'recovered' }));
  f.records.set('recovered', product('recovered', { name: '重新上架的商品' }));
  await f.app.ctx.LumoCommerce.refreshCart();
  assert.equal(f.app.json('state.cart[0].name'), '重新上架的商品');
  assert.equal(f.app.json('state.cart[0].unavailable'), false);
  assert.equal(f.app.json('products.filter(product => product.metadataUnavailable).length'), 0);
});

test('metadata failures are retryable and do not commit fake product names or zero prices', async (t) => {
  const f = await setup(t);
  f.cart.push({ id: 'outside-row', sku_id: 'outside', quantity: 1 });
  f.setHandler((service) => {
    if (service === 'catalog') throw Error('商品服务暂不可用');
  });
  await assert.rejects(f.app.ctx.LumoCommerce.refreshCart(), /商品服务暂不可用/);
  assert.equal(f.app.ctx.LumoCommerce.readState('cart').loaded, false);
  assert.match(f.app.navigate('cart'), /购物车加载失败/);
  assert.deepEqual(f.app.json('state.cart'), []);
  f.setHandler(undefined);
  f.records.set('outside', product('outside'));
  await f.app.ctx.LumoCommerce.refreshCart();
  assert.equal(f.app.json('state.cart[0].unitPrice'), 45.9);
});

for (const change of ['account', 'gateway', 'token'])
  test(`late metadata cannot populate a different ${change} scope`, async (t) => {
    const f = await setup(t),
      pending = deferred();
    f.cart.push({ id: 'private-row', sku_id: 'private-sku', quantity: 1 });
    f.setHandler((service, action) => (service === 'catalog' && action === 'batch_get' ? pending.promise : undefined));
    const read = f.app.ctx.LumoCommerce.refreshCart();
    await tick();
    f.switchScope(
      change === 'account' ? 'other' : 'buyer',
      change === 'token' ? 'token-b' : 'token-a',
      change === 'gateway' ? 'http://other.test' : 'http://commerce-metadata.test',
    );
    pending.resolve({ items: [product('private-sku')], missing_ids: [] });
    await read;
    assert.deepEqual(f.app.json('state.cart'), []);
    assert.equal(f.app.json('products.some(product => product.id === "private-sku")'), false);
    assert.equal(f.calls.filter((row) => row.service === 'inventory').length, 0);
  });

test('account reset drops privately hydrated references without dropping the bounded public catalogue', async (t) => {
  const f = await setup(t);
  await f.app.ctx.LumoCommerce.refreshCatalog();
  f.records.set('private-sku', product('private-sku'));
  f.favorites.push('private-sku');
  await f.app.ctx.LumoCommerce.refreshFavorites();
  assert.equal(f.app.json('products.length'), 101);
  f.switchScope();
  assert.equal(f.app.json('products.length'), 100);
  assert.deepEqual(f.app.json('state.favorites'), []);
});

for (const change of ['account', 'gateway', 'token'])
  test(`inventory snapshots and hydration tickets cannot cross a ${change} scope`, async (t) => {
    const f = await setup(t),
      inventory = f.app.ctx.LumoInventory,
      late = deferred();
    await inventory.refresh(['same-sku']);
    assert.equal(inventory.snapshot('same-sku').available, 30);
    const ticket = inventory.hydrationTicket();
    f.setHandler((service) => (service === 'inventory' ? late.promise : undefined));
    const pending = inventory.refresh(['same-sku']);
    await tick();
    f.switchScope(
      change === 'account' ? 'other' : 'buyer',
      change === 'token' ? 'token-b' : 'token-a',
      change === 'gateway' ? 'http://other.test' : 'http://commerce-metadata.test',
    );
    assert.equal(inventory.snapshot('same-sku'), undefined);
    assert.equal(inventory.prime([{ sku_id: 'same-sku', available: 99 }], ticket), 0);
    f.setHandler((service) =>
      service === 'inventory' ? { items: [{ sku_id: 'same-sku', available: 7 }] } : undefined,
    );
    await inventory.refresh(['same-sku']);
    late.resolve({ items: [{ sku_id: 'same-sku', available: 1 }] });
    await pending;
    assert.equal(inventory.snapshot('same-sku').available, 7);
  });

test('a late inventory failure from the prior scope cannot clear current availability or leave its error behind', async (t) => {
  const f = await setup(t),
    inventory = f.app.ctx.LumoInventory,
    late = deferred();
  f.setHandler((service) => (service === 'inventory' ? late.promise : undefined));
  const pending = inventory.refreshOne('same-sku');
  await tick();
  f.switchScope('buyer', 'token-b', 'http://other.test');
  f.setHandler((service, action) =>
    service === 'inventory' && action === 'get' ? { sku_id: 'same-sku', available: 7 } : undefined,
  );
  await inventory.refreshOne('same-sku');
  late.reject(Error('old backend unavailable'));
  await assert.rejects(pending, /old backend unavailable/);
  assert.equal(inventory.snapshot('same-sku').available, 7);
  assert.doesNotMatch(inventory.badge('same-sku'), /暂不可用/);
});
