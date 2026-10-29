'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
function setup() {
  let handler = async () => ({ items: [] }),
    user = null;
  const calls = [];
  const app = createHarness(directory, 'http://home.test', {
    globals: {
      LumoAPI: {
        getBase: () => 'http://home.test',
        getToken: () => '',
        async call(service, action, command) {
          calls.push({ service, action, command });
          return handler(service, action, command);
        },
      },
      LumoAuth: {
        user: () => user,
        isLoggedIn: () => !!user,
        afterRender() {},
        gate: () => true,
        requireLogin: () => !!user,
      },
    },
  });
  app.load('skeleton.js');
  app.load('app.js');
  return {
    app,
    calls,
    setHandler(value) {
      handler = value;
    },
    setUser(value) {
      user = value;
    },
    html: () => app.ids.get('app').innerHTML,
    render: () => app.navigate(''),
  };
}
const products = () =>
  Array.from({ length: 8 }, (_, index) => ({
    id: 'sku-real-' + index,
    price_cents: 10000 + index * 100,
    attributes: {
      product_id: 'real-' + index,
      name: '后台商品 ' + index,
      category: '手机数码',
      image: 'iphone-black.png',
    },
  }));
const featured = (html) =>
  html.slice(html.indexOf('<section class="home-picks"'), html.indexOf('<div class="home-activity-grid"'));

test('first script paint reserves the complete home composition without service reads or empty CTAs', (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  const html = f.html();
  assert.match(html, /class="container storefront-page catalog-page home-page"/);
  assert.match(html, /class="category-directory" aria-label="正在加载商品分类"/);
  assert.match(html, /class="home-feature-area"><div class="home-feature-top"><section class="hero"/);
  assert.match(html, /class="home-benefits"/);
  assert.match(featured(html), /正在加载精选好物/);
  assert.equal((featured(html).match(/class="sk-product"/g) || []).length, 4);
  assert.equal((html.match(/class="home-activity-card /g) || []).length, 4);
  assert.doesNotMatch(html, /没有找到合适的好物|更多活动准备中|0 件好物/);
  assert.ok(html.indexOf('class="nav home-category-nav"') < html.indexOf('class="home-feature-area"'));
  assert.deepEqual(f.calls, []);
});

test('independently delayed catalogue and category reads retain the hero; renders add no physical requests', async (t) => {
  const f = setup(),
    catalog = deferred(),
    categories = deferred();
  t.after(() => f.app.dispose());
  f.setHandler((service) => (service === 'catalog' ? catalog.promise : categories.promise));
  f.app.load('categories.js');
  const productRead = f.app.ctx.LumoCommerce.refreshCatalog();
  const categoryRead = f.app.ctx.LumoCategories.refresh();
  let html = f.render();
  assert.match(featured(html), /sk-loading-products/);
  assert.match(html, /智享生活精选/);
  catalog.resolve({ items: products() });
  await productRead;
  html = f.render();
  assert.equal((featured(html).match(/class="home-pick"/g) || []).length, 4);
  assert.match(featured(html), /后台商品 0/);
  assert.doesNotMatch(featured(html), /sk-loading-products/);
  const configuration = JSON.parse(
    fs.readFileSync(path.join(__dirname, '../../services/content/internal/usecase/category_navigation.json'), 'utf8'),
  );
  categories.resolve({ id: 'storefront-categories', kind: 'storefront', body: JSON.stringify(configuration) });
  await categoryRead;
  for (let index = 0; index < 4; index++) html = f.render();
  assert.match(html, /class="home-feature-area"><div class="home-feature-top"><section class="hero"/);
  assert.match(html, /class="category-directory-item/);
  assert.match(html, /class="nav home-category-nav"/);
  assert.deepEqual(
    f.calls.map(({ service, action }) => [service, action]),
    [
      ['catalog', 'list'],
      ['content', 'get'],
    ],
  );
  f.setHandler(() => {
    throw Error('分类暂不可用');
  });
  await assert.rejects(f.app.ctx.LumoCategories.refresh());
  html = f.render();
  assert.match(html, /智享生活精选/);
  assert.equal((featured(html).match(/class="home-pick"/g) || []).length, 4);
});

test('successful empty and failed catalogue have distinct home states, while background failures keep real picks', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.app.ctx.LumoCommerce.refreshCatalog();
  assert.match(featured(f.render()), /新好物正在上架/);
  assert.doesNotMatch(featured(f.html()), /sk-loading-products|重新加载/);
  f.setHandler(() => {
    throw Error('商品接口暂不可用');
  });
  await assert.rejects(f.app.ctx.LumoCommerce.refreshCatalog());
  assert.match(featured(f.render()), /精选商品暂未加载/);
  assert.match(featured(f.html()), /data-action="commerce-refresh"/);
  f.setHandler(() => ({ items: products() }));
  await f.app.ctx.LumoCommerce.refreshCatalog();
  const nextRead = deferred();
  f.setHandler(() => nextRead.promise);
  const updating = f.app.ctx.LumoCommerce.refreshCatalog();
  assert.match(featured(f.render()), /后台商品 0/);
  assert.doesNotMatch(featured(f.html()), /sk-loading-products/);
  nextRead.reject(Error('后台更新失败'));
  await assert.rejects(updating);
  assert.match(featured(f.render()), /后台商品 0/);
});

test('home activity entries use actual offers and rules, with pending, empty and failed states kept separate', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  let loaded = false,
    error = '',
    flash = [],
    campaigns = [];
  f.app.ctx.LumoPromotions = {
    flashOffers: () => flash,
    groupOffers: () => [],
    exchangeOffers: () => [],
    campaigns: () => campaigns,
    campaignRule: (row) => '每单直减 ¥' + row.discount_cents / 100,
    status: () => ({ loaded, error }),
    afterRender() {},
  };
  assert.match(f.render(), /正在加载活动/);
  loaded = true;
  assert.match(f.render(), /正在加载活动/);
  assert.doesNotMatch(f.html(), /更多活动准备中/);
  await f.app.ctx.LumoCommerce.refreshCatalog();
  assert.match(f.render(), /更多活动准备中/);
  error = '活动不可用';
  assert.match(f.render(), /活动暂未加载，进入重试/);
  assert.doesNotMatch(f.html(), /正在加载活动|更多活动准备中/);
  error = '';
  flash = [{ status: 'active', price: 7.25, product: { id: 'real-offer', name: '<真实秒杀>', image: 'coffee.jpg' } }];
  campaigns = [{ name: '<真实活动>', kind: 'direct', discount_cents: 350 }];
  const html = f.render();
  assert.match(html, /&lt;真实秒杀&gt;/);
  assert.match(html, /home-activity-price">¥7.25/);
  assert.match(html, /每单直减 ¥3.5/);
  assert.doesNotMatch(html, /<真实秒杀>|<真实活动>|满 ¥1,000/);
  assert.deepEqual(
    f.calls.map(({ service, action }) => [service, action]),
    [['catalog', 'list']],
  );
});

test('flash home card waits for delayed offer details and retains a genuine offer during background refresh', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  f.setHandler(() => ({ items: products() }));
  await f.app.ctx.LumoCommerce.refreshCatalog();
  let details = deferred();
  const stockReads = [];
  f.app.ctx.LumoInventory = {
    stockFor: () => 30,
    async refresh(ids) {
      stockReads.push([...ids]);
    },
  };
  f.setHandler((service, action) => {
    if (service === 'campaign')
      return { items: [{ id: 'live-flash', active: true, kind: 'flash', quota: 30, discount_cents: 2000 }] };
    if (service === 'flash')
      return { items: [{ id: 'live-session', active: true, sku_id: 'sku-real-0', activity_id: 'live-flash' }] };
    if (service === 'catalog' && action === 'batch_get') return details.promise;
    return { items: [] };
  });
  f.app.load('promotions.js');
  const flashCard = () => f.render().match(/<a class="home-activity-card is-red"[\s\S]*?<\/a>/)[0];
  const initial = f.app.ctx.LumoPromotions.refresh();
  await new Promise((resolve) => setImmediate(resolve));
  const status = f.app.ctx.LumoPromotions.status();
  assert.equal(status.ready.flash, true, 'the session list has already completed');
  assert.equal(status.loading, true, 'the offer detail is still unresolved');
  assert.match(flashCard(), /正在加载活动/);
  assert.doesNotMatch(flashCard(), /更多活动准备中/);
  assert.equal(f.calls.length, 5, 'repeated rendering adds no detail requests');
  details.resolve({ items: [products()[0]], missing_ids: [] });
  await initial;
  assert.match(flashCard(), /后台商品 0/);
  assert.match(flashCard(), /home-activity-price">¥80/);
  assert.doesNotMatch(flashCard(), /正在加载活动|更多活动准备中/);
  details = deferred();
  const background = f.app.ctx.LumoPromotions.refresh();
  await new Promise((resolve) => setImmediate(resolve));
  assert.match(flashCard(), /后台商品 0/);
  assert.doesNotMatch(flashCard(), /正在加载活动/);
  details.reject(Error('详情更新失败'));
  await assert.rejects(background, /详情更新失败/);
  assert.match(flashCard(), /后台商品 0/);
  assert.doesNotMatch(flashCard(), /正在加载活动|更多活动准备中/);
  assert.equal(f.calls.length, 9);
  assert.deepEqual(stockReads, [['sku-real-0'], ['sku-real-0']]);
});

test('member benefits use the account summary without loading or inventing full resource state', (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  let loaded = false;
  f.app.ctx.LumoMember = {
    model() {
      throw Error('the home card must not depend on full member resources');
    },
    afterRender() {},
  };
  const summary = {
    profile: { display_name: '<后台昵称>', level: 'standard' },
    points: 321,
    coupon_count: 2,
    favorite_count: 7,
  };
  f.app.ctx.LumoAccountSummary = { model: () => ({ ...summary, loaded, loading: !loaded, error: '' }) };
  assert.match(f.render(), /Hi，欢迎来到 LUMO/);
  assert.doesNotMatch(f.html(), /href="#\/register"/);
  assert.doesNotMatch(f.html(), /后台昵称|<strong>321<\/strong>/);
  f.setUser({ id: 'buyer' });
  assert.match(f.render(), /欢迎回来/);
  assert.doesNotMatch(f.html(), /后台昵称|<strong>321<\/strong>/);
  loaded = true;
  assert.match(f.render(), /&lt;后台昵称&gt;/);
  assert.match(f.html(), /<strong>321<\/strong>/);
  assert.match(f.html(), /<strong>2<\/strong><span>可用优惠券/);
  assert.match(f.html(), /<strong>7<\/strong><span>我的收藏/);
  summary.points = null;
  summary.coupon_count = null;
  assert.match(f.render(), /<strong>—<\/strong><span>我的积分/);
  assert.match(f.html(), /<strong>—<\/strong><span>可用优惠券/);
  assert.doesNotMatch(f.html(), /<strong>0<\/strong>/);
  f.setUser(null);
  assert.doesNotMatch(f.render(), /后台昵称|<strong>7<\/strong>/);
  assert.deepEqual(f.calls, []);
});

test('store management links are exclusive to administrators and disappear immediately after a role switch', (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  for (const user of [null, { id: 'buyer', role: 'user' }, { id: 'worker', role: 'system' }]) {
    f.setUser(user);
    assert.doesNotMatch(f.render(), /href="(?:#\/editor|admin\.html)"|编辑商城|商城管理|运营台/);
  }
  f.setUser({ id: 'manager', role: 'admin' });
  assert.match(f.render(), /class="edit-store-button" href="#\/editor"/);
  assert.match(f.html(), /href="admin\.html">运营台/);
  assert.match(f.html(), /href="admin\.html">商城管理/);
  f.setUser({ id: 'buyer', role: 'user' });
  assert.doesNotMatch(f.render(), /href="(?:#\/editor|admin\.html)"/);
  assert.deepEqual(f.calls, []);
});

test('home, category and search share one catalogue shell and primary navigation while only home has the hero', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  const configuration = JSON.parse(
    fs.readFileSync(path.join(__dirname, '../../services/content/internal/usecase/category_navigation.json'), 'utf8'),
  );
  f.app.load('categories.js');
  f.app.ctx.LumoCategories.apply(configuration);
  f.setHandler(() => ({
    items: products().map((row, index) => ({
      ...row,
      attributes: { ...row.attributes, category: index < 4 ? '家用电器' : '手机数码' },
    })),
  }));
  await f.app.ctx.LumoCommerce.refreshCatalog();
  const home = f.render();
  const header = (html) => /<header class="header">[\s\S]*?<\/header>/.exec(html)[0];
  const categoryNavigation = (html) => /<nav class="nav home-category-nav"[\s\S]*?<\/nav>/.exec(html)[0];
  const items = (html) =>
    [...categoryNavigation(html).matchAll(/data-action="category" data-value="([^"]+)"/g)].map((row) => row[1]);
  for (const [category, query, selected] of [
    ['家用电器', '', '家用电器'],
    ['全部好物', '后台商品', '全部好物'],
  ]) {
    f.app.evaluate('Object.assign(ui,' + JSON.stringify({ category, query }) + ');');
    const html = f.render();
    assert.match(html, /class="container storefront-page catalog-page"/);
    assert.equal(header(html).replace('value="后台商品"', 'value=""'), header(home));
    assert.equal((html.match(/aria-label="商品分类"/g) || []).length, 1);
    assert.deepEqual(items(html), items(home));
    assert.match(
      categoryNavigation(html),
      new RegExp('data-value="' + selected + '" class="nav-item active" aria-current="page"'),
    );
    assert.ok(html.indexOf('class="home-discovery-bar"') < html.indexOf('class="catalog"'));
    assert.match(html, /class="product-grid"/);
    assert.doesNotMatch(html, /class="home-category-layout"|class="home-feature-area"|class="home-benefits"/);
    assert.doesNotMatch(html, /style="margin-top:30px"/);
  }
  assert.match(home, /class="container storefront-page catalog-page home-page"/);
  assert.match(home, /class="home-feature-area"/);
  assert.equal((home.match(/aria-label="商品分类"/g) || []).length, 1);
  assert.deepEqual(
    f.calls.map(({ service, action }) => [service, action]),
    [['catalog', 'list']],
  );
});

test('header cart count prefers a complete cart, then summary, and exposes no zero while member data is pending', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  let summary = { loaded: false, cart_count: null };
  f.app.ctx.LumoAccountSummary = { model: () => summary };
  assert.equal(f.app.evaluate('displayCartCount()'), 0, 'a guest has no account cart');
  f.setUser({ id: 'buyer', role: 'user' });
  assert.equal(f.app.evaluate('displayCartCount()'), null);
  assert.match(f.render(), /class="icon-button cart-action" href="#\/cart" aria-label="购物车"/);
  assert.doesNotMatch(f.html(), /aria-label="购物车 0 件商品"/);
  summary = { loaded: true, cart_count: 9 };
  assert.equal(f.app.evaluate('displayCartCount()'), 9);
  assert.match(f.render(), /aria-label="购物车 9 件商品"/);
  f.setHandler(() => ({ items: [{ id: 'row', sku_id: 'sku', quantity: 2, unit_price_cents: 100 }] }));
  await f.app.ctx.LumoCommerce.refreshCart();
  assert.equal(f.app.evaluate('displayCartCount()'), 2);
  assert.equal(f.app.evaluate('cartCount()'), 2, 'the business count continues using complete cart rows');
  f.setUser(null);
  assert.equal(f.app.evaluate('displayCartCount()'), 0, 'another account cannot inherit the prior display count');
});

test('the existing editor guard rejects guests and regular members even when the route is entered directly', async (t) => {
  const app = createHarness(directory, 'http://catalog-permissions.test', {
    globals: {
      LumoAPI: {
        setToken() {},
        async call(service, action, command) {
          assert.equal(service, 'iam');
          assert.equal(action, 'login');
          return {
            user_id: command.data.username,
            role: command.data.username === 'manager' ? 'admin' : 'user',
            token: 'a'.repeat(64),
            expires_at: new Date(Date.now() + 60000).toISOString(),
          };
        },
      },
    },
  });
  t.after(() => app.dispose());
  app.load('auth.js');
  const auth = app.ctx.LumoAuth;
  app.ctx.location.hash = '#/editor';
  assert.equal(auth.gate(['editor']), false);
  assert.equal(app.ctx.location.hash, '#/login');
  await auth.login('buyer', 'test');
  app.ctx.location.hash = '#/editor';
  assert.equal(auth.gate(['editor']), false);
  assert.equal(app.ctx.location.hash, '#/member');
  await auth.login('manager', 'test');
  app.ctx.location.hash = '#/editor';
  assert.equal(auth.gate(['editor']), true);
});
