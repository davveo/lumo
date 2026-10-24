'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const {createHarness} = require('./dom-harness.cjs');

const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise(resolve => setImmediate(resolve));
const settleRoutes = () => new Promise(resolve => setTimeout(resolve, 85));
function setup(backend = () => ({items: [], has_more: false, total: 0})) {
  const calls = [];
  const app = createHarness(directory, 'http://navigation.test', {globals: {
    LumoAPI: {
      getBase: () => '', getToken: () => '',
      async call(service, action, command = {}) { calls.push({service, action, command}); return backend(service, action, command); }
    }
  }});
  for (const script of ['app.js', 'promotions.js', 'promo-views.js', 'coupon-center.js']) app.load(script);
  app.evaluate('products.push(...presentationProducts)');
  return {app, calls};
}
function primary(html) {
  const matches = [...html.matchAll(/<nav\b(?=[^>]*\baria-label="商品分类")[^>]*>([\s\S]*?)<\/nav>/g)];
  assert.equal(matches.length, 1, 'browse pages must retain one primary navigation bar');
  return [...matches[0][1].matchAll(/<(a|button)\b([^>]*)>([\s\S]*?)<\/\1>/g)].map(([, tag, attrs, content]) => ({
    tag, attrs, label: content.replace(/<[^>]*>/g, '').trim(),
    active: /\bclass="[^"]*\bactive\b/.test(attrs),
    current: /\baria-current="page"/.test(attrs)
  }));
}
function assertSelected(html, expected, labels) {
  const items = primary(html);
  assert.deepEqual(items.map(item => item.label), labels, 'primary navigation items must keep their order across sections');
  assert.ok(items.every(item => /\bclass="[^"]*\bnav-item\b/.test(item.attrs)), 'all primary controls share the same interaction class');
  assert.deepEqual(items.filter(item => item.active).map(item => item.label), [expected], 'exactly the current section is highlighted');
  assert.deepEqual(items.filter(item => item.current).map(item => item.label), [expected], 'visual state and accessible current state agree');
}

test('primary navigation stays consistent through categories, every activity section and coupons', async () => {
  const {app, calls} = setup();
  try {
    const initial = app.navigate('');
    const labels = primary(initial).map(item => item.label);
    assertSelected(initial, '全部好物', labels);
    await app.click({action: 'category', value: '家用电器'});
    assertSelected(app.ids.get('app').innerHTML, '家用电器', labels);
    assert.match(app.ids.get('app').innerHTML, /<h2>家用电器<\/h2>/);
    for (const section of ['flash', 'group', 'exchange', 'reduction', 'discount', 'quantity_discount', 'direct', 'mygroups', 'flash/tomorrow']) {
      assertSelected(app.navigate('promotions/' + section), '活动福利', labels);
      assert.equal(app.json('ui.category'), '家用电器', 'activity subnavigation must preserve the previous catalog category');
    }
    assertSelected(app.navigate('coupons'), '领券中心', labels);
    await app.click({action: 'category', value: '家用电器'});
    assert.equal(app.ctx.location.hash, '#/', 'category navigation must return from coupons to catalog');
    assertSelected(app.navigate(''), '家用电器', labels); // Deliver the browser hashchange render.
    await app.click({action: 'category', value: '家用电器'});
    assertSelected(app.ids.get('app').innerHTML, '家用电器', labels);
    await app.click({action: 'category', value: '全部好物'});
    assertSelected(app.ids.get('app').innerHTML, '全部好物', labels);
    assert.deepEqual(calls.map(row => [row.service, row.action]), [['coupon', 'templates_snapshot']], 'entering the coupon center performs one legitimate read and never a business mutation');
    assert.equal(calls[0].command.data.page_size, 5000);
    assert.equal(app.failures.length, 0);
  } finally { app.dispose(); }
});

test('a fast coupon-center entry is not fetched again by the bootstrap route timer', async () => {
  const {app, calls} = setup();
  try {
    app.load('api-bootstrap.js');
    await app.ctx.LumoAppReady;
    calls.splice(0);
    app.navigate('coupons');
    app.ctx.dispatchEvent(new app.ctx.CustomEvent('hashchange'));
    await settleRoutes();
    assert.deepEqual(calls.filter(row => row.service === 'coupon').map(row => row.action), ['templates_snapshot']);
    assert.equal(app.ctx.LumoCouponCenter.model().complete, true);
    for (let index = 0; index < 4; index++) app.evaluate('render()');
    await tick();
    assert.equal(calls.filter(row => row.service === 'coupon').length, 1, 'ordinary rerenders do not revalidate the same visit');
    app.navigate('promotions/flash');
    app.navigate('coupons');
    await tick();
    assert.equal(calls.filter(row => row.service === 'coupon').length, 2, 'a later visit still revalidates server eligibility');
    await app.ctx.LumoCouponCenter.refresh(true);
    assert.equal(calls.filter(row => row.service === 'coupon').length, 3, 'explicit refresh always asks the server again');
    assert.equal(app.failures.length, 0);
  } finally { app.dispose(); }
});

test('coupon hydration defers automatic reads and consumes its explicit bootstrap refresh once', async () => {
  const {app, calls} = setup();
  try {
    let active = true;
    app.ctx.LumoAPI.isBootstrapActive = () => active;
    app.ctx.LumoConnection = {ready: false, refreshing: true};
    app.navigate('coupons');
    app.evaluate('render()');
    assert.equal(calls.length, 0, 'initial rendering must not fan out before top-level hydration');
    app.ctx.LumoConnection.ready = true;
    app.evaluate('render()');
    assert.equal(calls.length, 0, 'core-ready rendering still leaves refresh to the bootstrap stage');
    await app.ctx.LumoCouponCenter.refresh();
    assert.equal(calls.length, 1);
    active = false;
    app.ctx.dispatchEvent(new app.ctx.CustomEvent('lumo:ready'));
    app.evaluate('render()');
    await tick();
    assert.equal(calls.length, 1, 'finishing hydration must not repeat the already completed full read');
  } finally { app.dispose(); }
});

test('entering the center during hydration is loaded once after readiness', async () => {
  const {app, calls} = setup();
  try {
    let active = true;
    app.ctx.LumoAPI.isBootstrapActive = () => active;
    app.ctx.LumoConnection = {ready: true, refreshing: true};
    app.navigate('');
    app.navigate('coupons');
    app.evaluate('render()');
    assert.equal(calls.length, 0);
    active = false;
    app.ctx.dispatchEvent(new app.ctx.CustomEvent('lumo:ready'));
    await tick();
    app.ctx.dispatchEvent(new app.ctx.CustomEvent('lumo:ready'));
    app.evaluate('render()');
    await tick();
    assert.deepEqual(calls.map(row => [row.service, row.action]), [['coupon', 'templates_snapshot']], 'route changes while hydrating must not miss or duplicate their single read');
  } finally { app.dispose(); }
});

test('a failed automatic center read does not loop on redraw, but a new visit retries', async () => {
  let fail = true;
  const {app, calls} = setup(() => { if (fail) throw new Error('coupon service offline'); return {items: [], has_more: false}; });
  try {
    app.navigate('coupons');
    await tick();
    app.evaluate('render()');
    app.evaluate('render()');
    await tick();
    assert.equal(calls.length, 1);
    assert.match(app.ctx.LumoCouponCenter.model().error, /offline/);
    fail = false;
    app.navigate('promotions/flash');
    app.navigate('coupons');
    await tick();
    assert.equal(calls.length, 2);
    assert.equal(app.ctx.LumoCouponCenter.model().error, '');
  } finally { app.dispose(); }
});

test('returning from activities keeps existing category filtering and clears search/favorites mode', async () => {
  const {app} = setup();
  try {
    app.evaluate("ui.query='手机';ui.onlyFavorites=true;ui.min='100';ui.max='2000';ui.sort='销量'");
    app.navigate('promotions/group');
    await app.click({action: 'category', value: '家用电器'});
    app.navigate('');
    const ui = app.json('ui');
    assert.equal(ui.category, '家用电器');
    assert.equal(ui.query, '');
    assert.equal(ui.onlyFavorites, false);
    assert.equal(ui.min, '100');
    assert.equal(ui.max, '2000');
    assert.equal(ui.sort, '销量');
    assert.match(app.ids.get('app').innerHTML, /复古迷你冰箱/);
    assert.doesNotMatch(app.ids.get('app').innerHTML, /class="product-title"[^>]*>Apple/);
  } finally { app.dispose(); }
});

test('keeping the selected primary item visible only adjusts the horizontal navigation scroll', () => {
  const {app} = setup();
  try {
    let rect = {left: 450, right: 500}, verticalScrolls = 0;
    const active = {getBoundingClientRect: () => rect};
    const nav = {
      scrollLeft: 15,
      querySelector: () => active,
      getBoundingClientRect: () => ({left: 20, right: 370}),
      scrollIntoView() { throw new Error('primary navigation must not move the document vertically'); }
    };
    const original = app.document.querySelector;
    app.document.querySelector = selector => selector === '.nav[aria-label="商品分类"]' ? nav : original(selector);
    app.ctx.scrollTo = () => { verticalScrolls++; };
    app.evaluate('positionPrimaryNavigation()');
    assert.equal(nav.scrollLeft, 145, 'right-clipped current item is brought into the horizontal viewport');
    rect = {left: 10, right: 60};
    app.evaluate('positionPrimaryNavigation()');
    assert.equal(nav.scrollLeft, 135, 'left-clipped current item is brought into the horizontal viewport');
    rect = {left: 80, right: 160};
    app.evaluate('positionPrimaryNavigation()');
    assert.equal(nav.scrollLeft, 135, 'a visible current item does not shift the navigation');
    assert.equal(verticalScrolls, 0);
  } finally { app.dispose(); }
});
