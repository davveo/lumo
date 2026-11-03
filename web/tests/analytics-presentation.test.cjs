'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');

function setup(data) {
  const app = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://analytics.test', {
    html: 'admin.html',
  });
  app.load('admin-ui.js');
  app.load('diagnostics-admin.js');
  app.ids.set('diagnostics-admin-root', new Element('diagnostics-admin-root'));
  const module = app.ctx.LumoDiagnosticsAdmin,
    requests = [];
  module.configure({
    isAdmin: () => true,
    scope: () => 'admin',
    draw() {},
    async call(service, action, request) {
      requests.push({ service, action, request });
      return data;
    },
  });
  return { app, module, requests };
}

test('ordered funnel derives adjacent conversion and overall conversion from distinct denominators', async (t) => {
  const f = setup({
    anonymous_sessions: 50,
    logged_sessions: 30,
    paid_orders: 5,
    cost_known_orders: 4,
    campaign_cost: 2000,
    coupon_cost: 1000,
    points_cost: 0,
    ordered_funnel: [
      { stage: 'paid', sessions: 5 },
      { stage: 'checkout', sessions: 10 },
      { stage: 'view', sessions: 80 },
      { stage: 'add_cart', sessions: 40 },
    ],
    total: 0,
    items: [],
  });
  t.after(() => f.app.dispose());
  await f.module.refresh();
  const html = f.module.render();
  assert.match(html, /6\.3%/);
  assert.equal((html.match(/上一步转化 <b>50\.0%/g) || []).length, 2);
  assert.match(html, /上一步转化 <b>25\.0%/);
  assert.match(html, /每 80 个浏览会话中，5 个完成支付/);
  assert.match(html, /已记录优惠合计 <strong>¥30\.00/);
  assert.match(html, /还有 1 笔付款单未记录优惠/);
  assert.equal(f.requests.length, 1, 'presentation must reuse the existing aggregate');
  assert.equal(f.requests[0].service + '/' + f.requests[0].action, 'analytics/drilldown');
  await f.app.submit('diagnostics-filter', { days: '30', dimension: 'query', keyword: '耳机' });
  assert.deepEqual(JSON.parse(JSON.stringify(f.requests.at(-1).request.data)), {
    days: 30,
    dimension: 'query',
    keyword: '耳机',
    page: 1,
  });
});

test('empty funnel avoids undefined percentages and missing discount records never masquerade as zero cost', async (t) => {
  const data = {
    anonymous_sessions: 0,
    logged_sessions: 0,
    paid_orders: 1,
    cost_known_orders: 0,
    campaign_cost: 0,
    coupon_cost: 0,
    points_cost: 0,
    ordered_funnel: ['view', 'add_cart', 'checkout', 'paid'].map((stage) => ({ stage, sessions: 0 })),
    items: [],
    total: 0,
  };
  const f = setup(data);
  t.after(() => f.app.dispose());
  await f.module.refresh();
  let html = f.module.render();
  assert.doesNotMatch(html, /NaN|Infinity|¥0\.00/);
  assert.match(html, /有浏览会话后展示转化表现/);
  assert.equal((html.match(/等待优惠明细/g) || []).length, 3);
  data.cost_known_orders = 1;
  await f.module.refresh();
  html = f.module.render();
  assert.match(html, /¥0\.00/);
  assert.doesNotMatch(html, /等待优惠明细/);
  assert.match(html, /已记录订单暂无此项优惠/);
});
