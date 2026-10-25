'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tokenA = 'a'.repeat(64);
const tokenB = 'b'.repeat(64);
const clone = (value) => JSON.parse(JSON.stringify(value));

function setup(t) {
  const requests = [];
  const app = createHarness(directory, 'http://summary-race.test', {
    globals: {
      fetch(url, options) {
        return new Promise((resolve) => {
          requests.push({
            url,
            options,
            body: JSON.parse(options.body),
            reply(data) {
              resolve({ status: 200, ok: true, headers: { get: () => '' }, json: async () => ({ data }) });
            },
          });
        });
      },
    },
  });
  t.after(() => app.dispose());
  app.load('api-client.js');
  const api = app.ctx.LumoAPI;
  api.setToken(tokenA);
  return { api, requests, summary: app.ctx.LumoAccountSummary, constants: app.ctx.LumoConstants };
}

const projections = [
  {
    service: 'cart',
    action: 'list',
    command: {},
    mutation: 'add',
    mutationCommand: { data: { sku_id: 'new-cart-item', quantity: 3 } },
    data: (count) => ({ items: [{ sku_id: 'item', quantity: count }] }),
    field: 'cart_count',
  },
  {
    service: 'member',
    action: 'get',
    command: {},
    mutation: 'update',
    mutationCommand: { data: { display_name: 'After' } },
    data: (count) => ({ display_name: count === 4 ? 'After' : 'Before', level: 'standard', avatar: '' }),
    field: 'profile',
  },
  {
    service: 'points',
    action: 'get',
    command: {},
    mutation: 'checkin',
    mutationCommand: {},
    data: (count) => ({ available: count }),
    field: 'points',
  },
  {
    service: 'coupon',
    action: 'list',
    command: {},
    mutation: 'claim',
    mutationCommand: { id: 'coupon-template' },
    data: (count, C) => ({
      items: Array.from({ length: count }, (_, index) => ({
        id: 'coupon-' + index,
        status: C.CouponStatus.Available,
        expires_at: new Date(Date.now() + 86400000).toISOString(),
      })),
    }),
    field: 'coupon_count',
  },
  {
    service: 'notification',
    action: 'list',
    command: { data: { limit: 100 } },
    mutation: 'read',
    mutationCommand: { id: 'message' },
    data: (count) => ({ items: [], unread_total: count }),
    field: 'unread_count',
  },
  {
    service: 'engagement',
    action: 'list',
    command: { data: { kind: 'favorite', limit: 100 } },
    mutation: 'put',
    mutationCommand: { data: { kind: 'favorite', sku_id: 'new-favorite' } },
    data: (count) => ({
      total: count,
      items: Array.from({ length: count }, (_, index) => ({ sku_id: (count === 4 ? 'new-' : 'old-') + index })),
    }),
    field: 'favorite_count',
  },
];
for (const fixture of projections) {
  test(
    fixture.service + ' rejects a pre-mutation read before it can overwrite the newer account projection',
    async (t) => {
      const { api, summary, requests, constants } = setup(t);
      const oldRead = api.call(fixture.service, fixture.action, fixture.command);
      const discarded = assert.rejects(oldRead, (error) => error.code === 'SessionChanged');
      const mutation = api.call(fixture.service, fixture.mutation, fixture.mutationCommand);
      requests[1].reply({});
      await mutation;
      const newRead = api.call(fixture.service, fixture.action, fixture.command);
      requests[2].reply(fixture.data(4, constants));
      await newRead;
      const fresh = clone(summary.model());
      assert.equal(fresh.loaded, true);
      assert.equal(
        fixture.field === 'profile' ? fresh.profile.display_name : fresh[fixture.field],
        fixture.field === 'profile' ? 'After' : 4,
      );
      requests[0].reply(fixture.data(1, constants));
      await discarded;
      assert.deepEqual(
        clone(summary.model()),
        fresh,
        'rejected data must have no projection or favorite-identity side effect',
      );
      assert.equal(requests.length, 3, 'the newer explicit read also satisfies the affected-domain refresh');
    },
  );
}

for (const boundary of ['token', 'base']) {
  for (const roundTrip of [false, true]) {
    test(
      boundary +
        (roundTrip ? ' round trip' : ' change') +
        ' isolates a late summary projection even when the same identity is restored',
      async (t) => {
        const { api, summary, requests } = setup(t);
        const oldRead = api.call('cart', 'list');
        const discarded = assert.rejects(oldRead, (error) => error.code === 'SessionChanged');
        if (boundary === 'token') {
          api.setToken(tokenB);
          if (roundTrip) api.setToken(tokenA);
        } else {
          api.setBase('http://other-summary.test');
          if (roundTrip) api.setBase('');
          api.setToken(tokenA); // Changing API destinations deliberately clears the authenticated session.
        }
        assert.equal(summary.model().loaded, false);
        const newRead = api.call('cart', 'list');
        requests[1].reply({ items: [{ sku_id: 'new', quantity: 4 }] });
        await newRead;
        requests[0].reply({ items: [{ sku_id: 'old', quantity: 1 }] });
        await discarded;
        assert.equal(summary.model().cart_count, 4);
        assert.equal(summary.model().loaded, true);
        assert.equal(requests.length, 2);
      },
    );
  }
}
