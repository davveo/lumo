'use strict';
const test = require('node:test'),
  assert = require('node:assert/strict'),
  path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((r) => setImmediate(r));
test('reward confirmation preserves explicit cost, owned addresses and the request key across a retry', async (t) => {
  let user = 'buyer',
    fail = true;
  const calls = [];
  const h = createHarness(directory, 'http://rewards.test', {
    globals: {
      renderKeepScroll() {},
      LumoAuth: { user: () => ({ id: user }), isLoggedIn: () => true },
      LumoMember: { refresh: async () => {} },
      LumoAPI: {
        getBase: () => '',
        getToken: () => user,
        async call(service, action, c = {}) {
          calls.push({ service, action, c });
          if (action === 'offers')
            return {
              items: [
                {
                  id: 'offer',
                  version: 7,
                  name: '实物礼品',
                  kind: 'GIFT',
                  cost: 100,
                  total_quota: 2,
                  used: 0,
                  max_per_user: 1,
                },
              ],
            };
          if (action === 'redemptions') return { items: [], total: 0 };
          if (action === 'redeem' && fail) throw Error('暂时未确认');
          return { state: 'PENDING' };
        },
      },
    },
  });
  t.after(() => h.dispose());
  h.ctx.location.hash = '#/member/points';
  h.load('rewards.js');
  await h.ctx.LumoRewards.afterRender();
  await tick();
  const model = {
    profile: { nickname: '买家' },
    membership: { growth: 20, remaining: 80, progress: 20 },
    pointsPolicy: { checkin_points: 7 },
    points: 200,
    addresses: [{ id: 'own-address', name: '收件人', region: '测试市', detail: '测试路' }],
  };
  await h.click({ reward: 'choose', id: 'offer' });
  let html = h.ctx.LumoRewards.view(model);
  assert.match(html, /100 积分/);
  assert.match(html, /own-address/);
  assert.match(html, /再获得 80 成长值/);
  await h.submit('reward-confirm', { address: 'own-address' });
  const first = calls.find((r) => r.action === 'redeem');
  assert.equal(first.c.data.version, 7);
  assert.equal(first.c.data.address_id, 'own-address');
  assert.ok(first.c.idempotency_key);
  assert.match(h.ctx.LumoRewards.view(model), /暂时未确认/);
  fail = false;
  await h.submit('reward-confirm', { address: 'own-address' });
  const requests = calls.filter((r) => r.action === 'redeem');
  assert.equal(requests[0].c.idempotency_key, requests[1].c.idempotency_key);
  user = 'other';
  html = h.ctx.LumoRewards.view(model);
  assert.doesNotMatch(html, /确认兑换：实物礼品/);
});
test('browser journey scrubs contact data and rotates attribution on account boundaries', async (t) => {
  let user = null;
  const calls = [];
  const h = createHarness(directory, 'http://journey.test', {
    globals: {
      products: [],
      LumoAuth: { user: () => (user ? { id: user } : null), isLoggedIn: () => !!user },
      LumoAPI: {
        async call(s, a, c, o) {
          calls.push({ s, a, c, o });
          return { accepted: true };
        },
      },
    },
  });
  t.after(() => h.dispose());
  h.load('commerce-analytics.js');
  const before = h.ctx.LumoJourney.context();
  h.ctx.LumoJourney.search('冰箱 buyer@example.test 13800138000', 0);
  await tick();
  assert.equal(calls[0].a, 'track');
  assert.equal(calls[0].c.data.query, '冰箱');
  assert.equal(calls[0].c.data.result_count, 0);
  assert.equal(calls[0].o.auth, false);
  user = 'buyer';
  h.ctx.dispatchEvent(new h.ctx.CustomEvent('lumo:login'));
  const after = h.ctx.LumoJourney.context();
  assert.notEqual(before.session_id, after.session_id);
  assert.equal(after.query, '');
  h.ctx.LumoJourney.track('checkout');
  await tick();
  assert.equal(calls.at(-1).o.auth, true);
});
