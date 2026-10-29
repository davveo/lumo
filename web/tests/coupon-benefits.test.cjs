'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
function load() {
  const app = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://coupon-benefits.test');
  app.load('coupon-benefits.js');
  return app.ctx.LumoCouponBenefits;
}
test('coupon benefit labels and scoped estimates match discount rates, caps and category restrictions', () => {
  const b = load();
  const row = {
    coupon_type: 'threshold_discount',
    discount_bps: 8500,
    max_discount_cents: 1500,
    min_cents: 5000,
    scope_type: 'category',
    scope_values: ['家电'],
  };
  assert.equal(b.face(row), '8.5折');
  assert.match(b.condition(row), /最多优惠 ¥15\.00/);
  assert.equal(
    b.estimate(row, 20000, [
      { category: '家电', amount_cents: 12000 },
      { category: '数码', amount_cents: 8000 },
    ]),
    1500,
  );
  assert.equal(b.estimate(row, 20000, [{ category: '数码', amount_cents: 20000 }]), 0);
  assert.match(b.scope(row), /指定品类/);
  assert.equal(b.face({ amount_cents: 1200 }), '¥12');
});
