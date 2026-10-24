'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
function setup(handler = async () => ({ items: [] }), { user = 'buyer' } = {}) {
  let token = 'token-a',
    base = 'http://loading.test',
    principal = user,
    profileLoading = false,
    history = [];
  const calls = [];
  const app = createHarness(directory, base, {
    globals: {
      LumoAPI: {
        getBase: () => base,
        getToken: () => token,
        async call(service, action, command = {}, options = {}) {
          calls.push({ service, action, command, options });
          return handler(service, action, command, options);
        },
      },
      LumoAuth: {
        user: () => (principal ? { id: principal } : null),
        isLoggedIn: () => !!principal,
        gate: () => true,
        requireLogin: () => !!principal,
        afterRender() {},
      },
      LumoInventory: {
        stockFor: () => 50,
        limit: () => 10,
        snapshot: () => ({ available: 50 }),
        refreshOne: async () => ({ available: 50 }),
      },
      LumoMember: {
        readState: () => ({ loading: profileLoading, loaded: !profileLoading, error: '' }),
        model: () => ({ history }),
        afterRender() {},
      },
    },
  });
  app.load('skeleton.js');
  app.load('app.js');
  app.evaluate('products.push(...presentationProducts.slice(0,4))');
  return {
    app,
    calls,
    setContext(value) {
      if ('token' in value) token = value.token;
      if ('base' in value) base = value.base;
      if ('user' in value) principal = value.user;
    },
    setProfile(value, rows = []) {
      profileLoading = value;
      history = rows;
    },
    dispose() {
      app.dispose();
    },
  };
}
const skeleton = (html) => assert.match(html, /class="sk-loading [^"]+"[^>]*role="status"[^>]*aria-busy="true"/);

test('coupon first load shows ticket skeletons, with no zero result count or fake claim buttons', async (t) => {
  const pending = deferred(),
    f = setup(() => pending.promise, { user: '' });
  t.after(() => f.dispose());
  f.app.ctx.location.hash = '#/coupons';
  f.app.load('coupon-center.js');
  const center = f.app.ctx.LumoCouponCenter;
  skeleton(center.home());
  skeleton(center.page());
  assert.doesNotMatch(center.page(), /共 0 种优惠券|这里暂时没有优惠券|data-cc-action="claim"/);
  assert.equal(f.calls.length, 0, 'rendering cannot fetch data');
  const request = center.refresh();
  skeleton(center.page());
  pending.resolve({ items: [], has_more: false });
  await request;
  assert.match(center.page(), /这里暂时没有优惠券/);
  assert.doesNotMatch(center.page(), /class="sk-loading/);
  assert.equal(f.calls.length, 1);
});
test('coupon background refresh keeps genuine cards; account changes cannot expose an older response', async (t) => {
  let pending = null;
  const f = setup(() =>
    pending
      ? pending.promise
      : {
          items: [
            {
              id: 'real-coupon',
              name: '已加载的好券',
              amount_cents: 1000,
              min_cents: 10000,
              valid_days: 7,
              remaining: 20,
              expires_at: new Date(Date.now() + 86400000).toISOString(),
              claim_status: 'available',
            },
          ],
          has_more: false,
        },
  );
  t.after(() => f.dispose());
  f.app.ctx.location.hash = '#/coupons';
  f.app.load('coupon-center.js');
  const center = f.app.ctx.LumoCouponCenter;
  await center.refresh();
  pending = deferred();
  const old = center.refresh(true);
  assert.match(center.page(), /已加载的好券/);
  assert.doesNotMatch(center.page(), /class="sk-loading/);
  f.setContext({ user: 'other', token: 'token-b' });
  skeleton(center.page());
  pending.resolve({ items: [{ id: 'old-private', name: '不应跨账号显示' }], has_more: false });
  await old;
  assert.doesNotMatch(center.page(), /不应跨账号显示|已加载的好券/);
  skeleton(center.page());
});
test('activity tabs show product-shaped loading and confirmed empty only after existing reads settle', async (t) => {
  const gates = [deferred(), deferred(), deferred()];
  let index = 0;
  const f = setup(() => gates[index++].promise);
  t.after(() => f.dispose());
  f.app.load('promotions.js');
  f.app.load('promo-views.js');
  const engine = f.app.ctx.LumoPromotions,
    view = f.app.ctx.LumoPromoView;
  for (const tab of ['flash', 'group', 'exchange', 'direct', 'mygroups']) {
    const html = view.page(tab);
    skeleton(html);
    assert.doesNotMatch(html, /共 0 |本活动暂未开放|还没有参与拼团/);
  }
  assert.equal(f.calls.length, 0);
  const request = engine.refresh();
  const shared = engine.refresh();
  assert.equal(f.calls.length, 3);
  gates.forEach((gate) => gate.resolve({ items: [] }));
  await Promise.all([request, shared]);
  const empty = view.page('flash');
  assert.match(empty, /共 0 件好物/);
  assert.doesNotMatch(empty, /class="sk-loading/);
});
test('activity failure shows retry without a false empty result; stale gateway responses cannot finish loading', async (t) => {
  const f = setup(async (service) => {
    if (service === 'campaign') throw Error('活动服务暂不可用');
    return { items: [] };
  });
  t.after(() => f.dispose());
  f.app.load('promotions.js');
  f.app.load('promo-views.js');
  await assert.rejects(f.app.ctx.LumoPromotions.refresh(), /活动服务/);
  const error = f.app.ctx.LumoPromoView.page('flash');
  assert.match(error, /role="alert"/);
  assert.match(error, /重新加载活动/);
  assert.doesNotMatch(error, /共 0 件|本场好物暂未上架|class="sk-loading/);
  const pending = deferred(),
    next = setup(() => pending.promise);
  t.after(() => next.dispose());
  next.app.load('promotions.js');
  next.app.load('promo-views.js');
  const old = next.app.ctx.LumoPromotions.refresh();
  next.setContext({ base: 'http://other.test' });
  skeleton(next.app.ctx.LumoPromoView.page('direct'));
  pending.resolve({ items: [] });
  await old;
  assert.equal(next.app.ctx.LumoPromotions.status().loaded, false);
  skeleton(next.app.ctx.LumoPromoView.page('direct'));
});
test('product reviews, questions and similar products wait with shapes, then show confirmed empties', async (t) => {
  const gate = deferred(),
    f = setup(() => gate.promise, { user: '' });
  t.after(() => f.dispose());
  f.app.load('product-extras.js');
  const extras = f.app.ctx.LumoProductExtras,
    p = f.app.json("products.find(product=>product.id==='iphone')");
  skeleton(extras.reviews('iphone'));
  skeleton(extras.questions('iphone'));
  skeleton(extras.related(p));
  assert.doesNotMatch(extras.reviews('iphone'), /0 条公开评价|暂无这类评价/);
  assert.doesNotMatch(extras.questions('iphone'), /0 个问题|还没有问题/);
  assert.equal(f.calls.length, 0);
  const request = extras.refresh('iphone'),
    shared = extras.refresh('iphone');
  assert.equal(f.calls.length, 3);
  gate.resolve({ items: [] });
  await Promise.all([request, shared]);
  assert.match(extras.reviews('iphone'), /暂无这类评价/);
  assert.match(extras.questions('iphone'), /还没有问题/);
  assert.doesNotMatch(extras.reviews('iphone'), /class="sk-loading/);
});
test('product read errors keep retry distinct from empty, and old account data remains hidden', async (t) => {
  const f = setup(
    async (service) => {
      if (service === 'interaction') throw Error('评价读取失败');
      return { items: [] };
    },
    { user: '' },
  );
  t.after(() => f.dispose());
  f.app.load('product-extras.js');
  const extras = f.app.ctx.LumoProductExtras;
  await assert.rejects(extras.refresh('iphone'), /评价读取失败/);
  const failed = extras.reviews('iphone');
  assert.match(failed, /role="alert"/);
  assert.match(failed, /data-product-extra-action="retry"/);
  assert.doesNotMatch(failed, /暂无这类评价|class="sk-loading/);
  f.setContext({ user: 'another', token: 'new-token' });
  skeleton(extras.reviews('iphone'));
  assert.doesNotMatch(extras.reviews('iphone'), /评价读取失败/);
});
test('postpurchase recommendations show shapes without a request loop and retain actual cards while updating', async (t) => {
  let gate = deferred();
  const f = setup(() => gate.promise);
  t.after(() => f.dispose());
  f.app.load('recommendations.js');
  const recommendations = f.app.ctx.LumoRecommendations,
    order = { id: 'real-order', items: [] };
  skeleton(recommendations.render(order));
  skeleton(recommendations.render(order));
  assert.equal(f.calls.length, 1);
  gate.resolve({ items: [{ id: 'iphone', recommendation_reason: '来自已加载结果' }] });
  await recommendations.refresh(order);
  assert.match(recommendations.render(order), /来自已加载结果/);
  gate = deferred();
  const refresh = recommendations.refresh(order);
  assert.match(recommendations.render(order), /来自已加载结果/);
  assert.doesNotMatch(recommendations.render(order), /class="sk-loading/);
  gate.reject(Error('暂时无法更新'));
  await assert.rejects(refresh);
  const failed = recommendations.render(order);
  assert.match(failed, /来自已加载结果/);
  assert.match(failed, /暂时无法更新/);
});
test('interest rail waits for its aggregate without inventing products or reading member caches', async (t) => {
  const gate = deferred(),
    f = setup(() => gate.promise);
  t.after(() => f.dispose());
  f.app.load('sidebars.js');
  f.setProfile(true);
  skeleton(f.app.ctx.LumoSidebars.render());
  assert.doesNotMatch(f.app.ctx.LumoSidebars.render(), /浏览或收藏商品后/);
  assert.equal(f.calls.length, 0);
  const request = f.app.ctx.LumoSidebars.refresh();
  skeleton(f.app.ctx.LumoSidebars.render());
  gate.resolve({
    items: [{ id: 'off-page-product', name: '实际商品', price_cents: 1200, active: true }],
    inventory_items: [],
    personalized: true,
  });
  await request;
  assert.match(f.app.ctx.LumoSidebars.render(), /实际商品/);
  assert.doesNotMatch(f.app.ctx.LumoSidebars.render(), /class="sk-loading/);
  assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].service, 'storefront');
  assert.equal(f.calls[0].action, 'recent');
});
test('interest aggregate failure shows retry and only rereads that aggregate', async (t) => {
  let failed = true,
    refreshes = 0;
  const f = setup(async () => {
    if (failed) throw Error('兴趣好物读取失败');
    return { items: [], inventory_items: [], personalized: false };
  });
  t.after(() => f.dispose());
  f.app.ctx.LumoMember.refresh = async () => {
    refreshes++;
  };
  f.app.load('sidebars.js');
  await f.app.ctx.LumoSidebars.refresh();
  const before = f.app.ctx.LumoSidebars.render();
  assert.match(before, /兴趣好物读取失败/);
  assert.match(before, /data-sidebar-action="retry-interest"/);
  assert.doesNotMatch(before, /暂时没有可推荐|class="sk-loading/);
  failed = false;
  await f.app.click({ sidebarAction: 'retry-interest' });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(refreshes, 0);
  assert.match(f.app.ctx.LumoSidebars.render(), /暂时没有可推荐的在售好物/);
  assert.doesNotMatch(f.app.ctx.LumoSidebars.render(), /兴趣好物读取失败/);
  assert.equal(f.calls.length, 2);
  assert.ok(f.calls.every((row) => row.service === 'storefront' && row.action === 'recent'));
});
