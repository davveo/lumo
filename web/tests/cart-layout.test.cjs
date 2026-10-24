'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((resolve) => setImmediate(resolve));

async function setup(t) {
  const app = createHarness(directory, 'http://cart-layout.test');
  t.after(() => app.dispose());
  let user = 'buyer',
    failure = false,
    pending = null;
  let rows = [0, 1].map((index) => ({
    id: 'row-' + index,
    sku_id: 'sku-' + index,
    product_id: 'product-' + index,
    quantity: index + 1,
    unit_price_cents: (index + 1) * 1000,
  }));
  const favorites = new Set();
  const calls = [];
  app.ctx.LumoAuth = {
    user: () => ({ id: user, role: 'user' }),
    isLoggedIn: () => !!user,
    requireLogin: () => !!user,
    gate: () => true,
    afterRender() {},
  };
  app.load('api-client.js');
  app.ctx.LumoAPI.call = async (service, action, command = {}) => {
    calls.push({ service, action, command });
    if (service === 'engagement' && action === 'put') {
      if (pending) await pending;
      if (failure) throw new Error('收藏暂不可用');
      favorites.add(command.data.sku_id);
      return { sku_id: command.data.sku_id };
    }
    if (service === 'engagement' && action === 'list') return { items: [...favorites].map((sku_id) => ({ sku_id })) };
    if (service === 'cart') {
      if (action === 'remove') rows = rows.filter((row) => row.id !== command.id);
      if (action === 'clear') rows = [];
      return { items: rows };
    }
    return { items: [], total: 0 };
  };
  app.load('app.js');
  await tick();
  const products = Array.from({ length: 16 }, (_, index) => ({
    id: 'product-' + index,
    skuId: 'sku-' + index,
    name: '真实商品 ' + index,
    brand: index ? '品牌 B' : '品牌 A',
    category: '生活用品',
    price: (index + 1) * 10,
    old: (index + 1) * 10,
    sales: 0,
    visible: true,
    image: 'coffee.png',
    sub: '',
    colors: ['默认'],
    options: ['标准'],
    defaultOption: '标准',
    highlights: [],
    highSubs: [],
    variants: [{ sku_id: 'sku-' + index, color: '默认', option: '标准', price_cents: (index + 1) * 1000 }],
  }));
  app.evaluate('products.splice(0, products.length, ...' + JSON.stringify(products) + ');');
  app.evaluate('commerceState.catalogLoaded = true;');
  await app.ctx.LumoCommerce.refreshCart();
  return {
    app,
    calls,
    favorites,
    fail() {
      failure = true;
    },
    wait(value) {
      pending = value;
    },
    switchUser() {
      user = 'other';
      app.ctx.dispatchEvent(new app.ctx.CustomEvent('lumo:login'));
    },
  };
}

test('cart groups real brands, keeps row indices, and renders one bottom settlement bar and six-column recommendations', async (t) => {
  const { app, calls } = await setup(t);
  calls.length = 0;
  const html = app.navigate('cart');
  assert.equal((html.match(/class="cart-brand-group"/g) || []).length, 2);
  assert.equal((html.match(/class="cart-bottom-wrap"/g) || []).length, 1);
  assert.equal((html.match(/class="product-card"/g) || []).length, 12);
  assert.match(html, /data-action="select" data-index="0"/);
  assert.match(html, /data-action="select" data-index="1"/);
  assert.match(html, /优惠和运费将在结算时由服务端确认/);
  assert.equal(calls.length, 0, 'rendering does not request each product or inventory');
  await app.click({ action: 'cart-select-brand', brand: '品牌 A' });
  assert.deepEqual(app.json('state.cart.map(item => !!item.selected)'), [false, true]);
});

test('moving to favorites persists the favorite before removing the cart row and preserves an existing favorite', async (t) => {
  const { app, calls, favorites } = await setup(t);
  app.navigate('cart');
  calls.length = 0;
  await app.evaluate('moveCartToFavorites([state.cart[0]])');
  const writes = calls.filter((row) => ['put', 'remove', 'clear'].includes(row.action));
  assert.deepEqual(
    writes.map((row) => row.service + '/' + row.action),
    ['engagement/put', 'cart/remove'],
  );
  assert.ok(favorites.has('sku-0'));
  assert.equal(app.json('state.cart').length, 1);
  app.evaluate('state.favorites.push("product-1")');
  favorites.add('sku-1');
  calls.length = 0;
  await app.evaluate('moveCartToFavorites([state.cart[0]])');
  assert.equal(calls.filter((row) => row.service === 'engagement' && ['put', 'delete'].includes(row.action)).length, 0);
  assert.equal(calls.filter((row) => row.service === 'cart' && row.action === 'clear').length, 1);
  assert.ok(favorites.has('sku-1'));
});

test('failed or late favorite writes never delete a cart row or continue under another account', async (t) => {
  const first = await setup(t);
  first.fail();
  first.calls.length = 0;
  await first.app.evaluate('moveCartToFavorites([state.cart[0]])');
  assert.equal(first.calls.filter((row) => ['remove', 'clear'].includes(row.action)).length, 0);
  assert.equal(first.app.json('state.cart').length, 2);
  const second = await setup(t);
  let resolve;
  second.wait(
    new Promise((done) => {
      resolve = done;
    }),
  );
  second.calls.length = 0;
  const request = second.app.evaluate('moveCartToFavorites([...state.cart])');
  await tick();
  second.switchUser();
  resolve();
  await request;
  assert.equal(second.calls.filter((row) => row.action === 'put').length, 1);
  assert.equal(second.calls.filter((row) => ['remove', 'clear'].includes(row.action)).length, 0);
});

test('unresolved cart shows row placeholders without inventing a zero count', async (t) => {
  const { app } = await setup(t);
  app.load('skeleton.js');
  app.evaluate("state.cart = []; commerceReads.delete('cart');");
  const html = app.navigate('cart');
  assert.match(html, /正在加载购物车/);
  assert.doesNotMatch(html, /<h1>购物车 <span>\(0\)<\/span>/);
  assert.doesNotMatch(html, /购物车还是空的/);
  assert.doesNotMatch(html, /cart-bottom-wrap/);
});
