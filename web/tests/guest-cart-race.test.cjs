'use strict';
const test = require('node:test'),
  assert = require('node:assert/strict'),
  path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
function deferred() {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
}
function setup(t) {
  const h = createHarness(directory, 'http://shop.test');
  t.after(() => h.dispose());
  let base = 'http://shop.test',
    user = 'buyer';
  const hydration = deferred(),
    calls = [];
  h.ctx.LumoAPI = {
    getBase: () => base,
    getToken: () => 'same-token',
    async call(...args) {
      calls.push(args);
    },
  };
  h.ctx.LumoAuth = { user: () => ({ id: user }), isLoggedIn: () => !!user };
  h.ctx.hydrateProducts = () => hydration.promise;
  h.ctx.actualProduct = () => ({ visible: true });
  h.ctx.normalizeItem = (row) => ({ sku_id: row.sku_id, qty: row.quantity });
  const key = 'lumo-guest-cart-v1::' + base;
  const original = { items: [{ sku_id: 'sku-a', quantity: 2 }], revision: 'one', expires: Date.now() + 60000 };
  h.ctx.localStorage.setItem(key, JSON.stringify(original));
  h.load('guest-cart.js');
  return {
    h,
    hydration,
    calls,
    key,
    original,
    setBase(value) {
      base = value;
    },
    setUser(value) {
      user = value;
    },
  };
}
test('merge never overwrites a different-tab cart edit while product hydration is pending', async (t) => {
  const s = setup(t),
    pending = s.h.ctx.LumoGuestCart.merge();
  const edited = { ...s.original, revision: 'two', items: [...s.original.items, { sku_id: 'sku-b', quantity: 1 }] };
  s.h.ctx.localStorage.setItem(s.key, JSON.stringify(edited));
  s.hydration.resolve();
  await assert.rejects(pending, /购物车.*变化/);
  assert.deepEqual(JSON.parse(s.h.ctx.localStorage.getItem(s.key)), edited);
  assert.equal(s.calls.length, 0);
});
test('merge rejects a different account claiming the same revision from another tab', async (t) => {
  const s = setup(t),
    pending = s.h.ctx.LumoGuestCart.merge();
  s.h.ctx.localStorage.setItem(s.key, JSON.stringify({ ...s.original, owner: 'other-buyer' }));
  s.hydration.resolve();
  await assert.rejects(pending, /原账号/);
  assert.equal(s.calls.length, 0);
  assert.equal(JSON.parse(s.h.ctx.localStorage.getItem(s.key)).owner, 'other-buyer');
});
test('switching API with the same token cannot merge an old environment cart into the new API', async (t) => {
  const s = setup(t),
    pending = s.h.ctx.LumoGuestCart.merge();
  s.setBase('http://other-shop.test');
  s.hydration.resolve();
  await assert.rejects(pending, /登录状态|服务地址/);
  assert.equal(s.calls.length, 0);
  assert.deepEqual(JSON.parse(s.h.ctx.localStorage.getItem(s.key)), s.original);
});
