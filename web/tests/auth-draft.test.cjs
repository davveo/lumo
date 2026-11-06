'use strict';
const test = require('node:test'),
  assert = require('node:assert/strict'),
  path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
test('background rendering preserves login input without exposing it through state or storage', async () => {
  const h = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://shop.test');
  try {
    h.load('api-config.js');
    h.load('api-client.js');
    h.load('auth.js');
    h.ctx.LumoAPI.call = async () => ({ items: [{ id: 'password' }], allow_registration: true });
    h.ctx.location.hash = '#/login';
    const name = await h.input('auth-username', 'draft-buyer'),
      password = await h.input('auth-password', 'PrivateDraft123');
    name.value = '';
    password.value = '';
    h.ctx.LumoAuth.afterRender();
    assert.equal(name.value, 'draft-buyer');
    assert.equal(password.value, 'PrivateDraft123');
    assert.ok(!JSON.stringify(h.ctx.LumoAuth.model()).includes('PrivateDraft123'));
    h.evaluate("location.hash='#/';window.dispatchEvent(new CustomEvent('hashchange'))");
    h.ctx.location.hash = '#/login';
    name.value = '';
    password.value = '';
    h.ctx.LumoAuth.afterRender();
    assert.equal(password.value, 'Lumo@2026');
    assert.equal(name.value, 'demo');
    await h.input('auth-username', '');
    await h.input('auth-password', '');
    h.ctx.LumoAuth.afterRender();
    assert.equal(password.value, '');
    assert.equal(name.value, '');
  } finally {
    h.dispose();
  }
});

for (const method of ['password', 'phone', 'wechat', 'qq', 'wechat-merge-failure']) {
  test(`${method} login attempts guest cart merge before announcing login`, { timeout: 3000 }, async () => {
    const h = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://shop.test');
    try {
      h.load('api-config.js');
      h.load('api-client.js');
      h.load('auth.js');
      const events = [];
      h.ctx.LumoAPI.call = async (_, action) =>
        action === 'login_methods'
          ? { items: [{ id: 'password' }], allow_registration: true }
          : {
              token: 'a'.repeat(64),
              user_id: 'buyer',
              role: 'user',
              expires_at: new Date(Date.now() + 3600000).toISOString(),
            };
      h.ctx.LumoGuestCart = {
        async merge() {
          assert.equal(h.ctx.LumoAuth.user().id, 'buyer');
          assert.equal(h.ctx.LumoAPI.getToken(), 'a'.repeat(64));
          await Promise.resolve();
          events.push('merge');
          if (method.endsWith('failure')) throw Error('temporary merge failure');
        },
      };
      const completed = new Promise((resolve) =>
        h.ctx.addEventListener('lumo:login', () => {
          events.push('login');
          resolve();
        }),
      );
      if (['password', 'phone'].includes(method)) {
        await h.ctx.LumoAuth.login('buyer', 'secret', false, method);
      } else {
        h.ctx.sessionStorage.setItem(
          'lumo-oauth-flow-v1',
          JSON.stringify({
            provider: method.split('-')[0],
            proof: 'proof',
            state: 'test-state',
            returnTo: '#/checkout',
            at: Date.now(),
          }),
        );
        h.ctx.location.href = 'http://shop.test/index.html?code=auth-code&state=test-state';
        h.ctx.LumoAuth.afterRender();
      }
      await completed;
      assert.deepEqual(events, ['merge', 'login']);
      assert.equal(h.ctx.LumoAuth.isLoggedIn(), true);
      if (!['password', 'phone'].includes(method)) assert.equal(h.ctx.location.hash, '#/checkout');
    } finally {
      h.dispose();
    }
  });
}
