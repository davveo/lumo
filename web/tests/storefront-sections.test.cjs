'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
function setup(t, handler = async () => ({ items: [] })) {
  const calls = [];
  const app = createHarness(directory, 'http://section.test', {
    globals: {
      LumoAuth: { user: () => ({ id: 'buyer' }), isLoggedIn: () => true, gate: () => true, afterRender() {} },
      LumoAPI: {
        getToken: () => 'session',
        getBase: () => '',
        call: async (service, action, command = {}) => {
          calls.push({ service, action, command });
          return handler(service, action, command);
        },
      },
    },
  });
  t.after(() => app.dispose());
  app.load('app.js');
  app.load('member.js');
  app.load('services.js');
  return { app, calls };
}
test('history route reads its own collection without marking other member resources complete', async (t) => {
  const { app, calls } = setup(t);
  app.ctx.location.hash = '#/member/history';
  await app.ctx.LumoMember.refreshForView();
  assert.deepEqual(
    calls.map((row) => [row.service, row.action, row.command.data?.kind]),
    [['engagement', 'list', 'history']],
  );
  assert.equal(app.ctx.LumoMember.readState('history').loaded, true);
  for (const resource of ['profile', 'addresses', 'coupons', 'points', 'reviews', 'aftersales', 'favorites'])
    assert.equal(app.ctx.LumoMember.readState(resource).loaded, false, resource);
});
test('invoice route never preloads FAQ, notification or helpdesk lists', async (t) => {
  const { app, calls } = setup(t);
  app.ctx.location.hash = '#/member/invoices';
  await app.ctx.LumoServices.refreshForView();
  assert.deepEqual(
    calls.map((row) => row.service),
    ['invoice'],
  );
  assert.equal(app.ctx.LumoServices.readState('invoices').loaded, true);
  for (const key of ['faq', 'messages', 'tickets'])
    assert.equal(app.ctx.LumoServices.readState(key).loaded, false, key);
});
test('checkout reads actual address, owned coupons and points but not templates or unrelated history', async (t) => {
  const { app, calls } = setup(t);
  app.ctx.location.hash = '#/checkout';
  await app.ctx.LumoMember.refreshForView();
  assert.deepEqual(calls.map((row) => row.service).sort(), ['address', 'coupon', 'points']);
  assert.equal(app.ctx.LumoMember.readState('templates').loaded, false);
  assert.equal(app.ctx.LumoMember.readState('history').loaded, false);
});
test('switching tabs during an in-flight member read still loads the new tab after the prior read', async (t) => {
  let release;
  const history = new Promise((resolve) => {
    release = resolve;
  });
  const { app, calls } = setup(t, async (service) =>
    service === 'engagement' ? history : { available: 50, ledger: [] },
  );
  app.ctx.location.hash = '#/member/history';
  const first = app.ctx.LumoMember.refreshForView();
  app.ctx.location.hash = '#/member/points';
  const second = app.ctx.LumoMember.refreshForView();
  release({ items: [] });
  await Promise.all([first, second]);
  assert.deepEqual(
    calls.map((row) => row.service),
    ['engagement', 'points'],
  );
  assert.equal(app.ctx.LumoMember.model().points, 50);
});
