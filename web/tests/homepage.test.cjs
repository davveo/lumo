'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function setup() {
  let handler = async () => ({ items: [] }), user = null;
  const calls = [];
  const app = createHarness(directory, 'http://home.test', {
    globals: {
      LumoAPI: {
        getBase: () => 'http://home.test', getToken: () => '',
        async call(service, action, command) {
          calls.push({ service, action, command });
          return handler(service, action, command);
        },
      },
      LumoAuth: {
        user: () => user, isLoggedIn: () => !!user,
        afterRender() {}, gate: () => true, requireLogin: () => !!user,
      },
    },
  });
  app.load('skeleton.js');
  app.load('app.js');
  return {
    app, calls,
    setHandler(value) { handler = value; },
    setUser(value) { user = value; },
    html: () => app.ids.get('app').innerHTML,
    render: () => app.navigate(''),
  };
}
const products = () => Array.from({ length: 8 }, (_, index) => ({
  id: 'sku-real-' + index, price_cents: 10000 + index * 100,
  attributes: {
    product_id: 'real-' + index, name: '后台商品 ' + index,
    category: '手机数码', image: 'iphone-black.png',
  },
}));
const featured = (html) => html.slice(html.indexOf('<section class="home-picks"'), html.indexOf('<div class="home-activity-grid"'));

test('first script paint reserves the complete home composition without service reads or empty CTAs', (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  const html = f.html();
  assert.match(html, /class="container home-page"/);
  assert.match(html, /class="category-directory" aria-label="正在加载商品分类"/);
  assert.match(html, /class="home-feature-area"><div class="home-feature-top"><section class="hero"/);
  assert.match(html, /class="home-benefits"/);
  assert.match(featured(html), /正在加载精选好物/);
  assert.equal((featured(html).match(/class="sk-product"/g) || []).length, 4);
  assert.equal((html.match(/class="home-activity-card /g) || []).length, 4);
  assert.doesNotMatch(html, /没有找到合适的好物|更多活动准备中|0 件好物/);
  assert.ok(html.indexOf('class="nav home-category-nav"') > html.indexOf('class="home-benefits"'));
  assert.deepEqual(f.calls, []);
});

test('independently delayed catalogue and category reads retain the hero; renders add no physical requests', async (t) => {
  const f = setup(), catalog = deferred(), categories = deferred();
  t.after(() => f.app.dispose());
  f.setHandler((service) => service === 'catalog' ? catalog.promise : categories.promise);
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
  const configuration = JSON.parse(fs.readFileSync(path.join(__dirname, '../../services/content/internal/usecase/category_navigation.json'), 'utf8'));
  categories.resolve({ id: 'storefront-categories', kind: 'storefront', body: JSON.stringify(configuration) });
  await categoryRead;
  for (let index = 0; index < 4; index++) html = f.render();
  assert.match(html, /class="home-feature-area"><div class="home-feature-top"><section class="hero"/);
  assert.match(html, /class="category-directory-item/);
  assert.match(html, /class="nav home-category-nav"/);
  assert.deepEqual(f.calls.map(({ service, action }) => [service, action]), [['catalog', 'list'], ['content', 'get']]);
  f.setHandler(() => { throw Error('分类暂不可用'); });
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
  f.setHandler(() => { throw Error('商品接口暂不可用'); });
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

test('home activity entries use actual offers and rules, with pending, empty and failed states kept separate', (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  let loaded = false, error = '', flash = [], campaigns = [];
  f.app.ctx.LumoPromotions = {
    flashOffers: () => flash, groupOffers: () => [], exchangeOffers: () => [], campaigns: () => campaigns,
    campaignRule: (row) => '每单直减 ¥' + row.discount_cents / 100,
    status: () => ({ loaded, error }), afterRender() {},
  };
  assert.match(f.render(), /正在加载活动/);
  loaded = true;
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
  assert.deepEqual(f.calls, []);
});

test('member benefits wait for the current account reads instead of exposing defaults as real balances', (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  let loaded = false;
  f.app.ctx.LumoMember = {
    readState: () => ({ loaded, loading: !loaded, error: '' }),
    model: () => ({ profile: { nickname: '<后台昵称>' }, points: 321, coupons: [{ status: 'available' }] }),
    afterRender() {},
  };
  assert.match(f.render(), /Hi，欢迎来到 LUMO/);
  assert.doesNotMatch(f.html(), /后台昵称|<strong>321<\/strong>/);
  f.setUser({ id: 'buyer' });
  assert.match(f.render(), /欢迎回来/);
  assert.doesNotMatch(f.html(), /后台昵称|<strong>321<\/strong>/);
  loaded = true;
  assert.match(f.render(), /&lt;后台昵称&gt;/);
  assert.match(f.html(), /<strong>321<\/strong>/);
  assert.match(f.html(), /<strong>1<\/strong><span>可用优惠券/);
  assert.deepEqual(f.calls, []);
});
