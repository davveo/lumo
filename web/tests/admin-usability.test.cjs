'use strict';
const test = require('node:test'),
  assert = require('node:assert/strict'),
  path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
function fixture(key, call, access = false) {
  const app = createHarness(directory, 'http://readable-admin.test', { html: 'admin.html' }),
    submits = [],
    add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...args) => {
    if (type === 'submit') submits.push(fn);
    add(type, fn, ...args);
  };
  app.load('skeleton.js');
  app.load('admin-ui.js');
  app.load(access ? 'access-admin.js' : 'service-admin.js');
  const m = access ? app.ctx.LumoAccessAdmin.create(key) : app.ctx.LumoServiceAdmin.create(key);
  app.ids.set((access ? 'access-' : 'service-admin-') + key, new Element());
  let scope = 'admin-a';
  m.configure({ isAdmin: () => true, scope: () => scope, draw() {}, call });
  return {
    app,
    m,
    setScope: (v) => (scope = v),
    async form(selector, fields, dataset = {}) {
      const target = new Element();
      target.fields = fields;
      target.dataset = dataset;
      target.matches = (q) => q === selector || q.split(',').includes(selector);
      for (const fn of submits) await fn({ target, preventDefault() {} });
    },
  };
}

test('behavior rows explain actions, search and product names while using real server pages', async (t) => {
  const calls = [];
  const f = fixture('analytics', async (service, action, c) => {
    calls.push({ service, action, c });
    return {
      items: [
        {
          id: 'event-1',
          user_id: 'buyer-1',
          type: 'view',
          action_label: '浏览商品',
          product_name: '咖啡机',
          sku_id: 'coffee',
          source: 'product_detail',
          created_at: '2026-10-29T01:00:00Z',
        },
      ],
      total: 21,
    };
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  assert.equal(calls[0].action, 'admin_list');
  assert.equal(calls[0].c.data.page_size, 10);
  const html = f.m.render();
  assert.match(html, /浏览商品/);
  assert.match(html, /咖啡机/);
  assert.match(html, /商品详情/);
  assert.doesNotMatch(html, /<pre|ACCEPTED/);
  await f.app.click({ svcAdminPage: '2' });
  assert.equal(calls.at(-1).c.data.page, 2);
});

test('ticket conversations render escaped customer/staff messages and reply in place', async (t) => {
  const calls = [];
  let closed = false,
    replied = false;
  const f = fixture('tickets', async (service, action, c) => {
    calls.push({ service, action, c });
    if (action === 'admin_list')
      return {
        items: [
          {
            id: 'TKT_123',
            title: '发货问题',
            body: '请问何时发货',
            status: closed ? 'CLOSED' : 'OPEN',
            user_id: 'buyer',
          },
        ],
        total: 1,
      };
    if (action === 'get')
      return {
        id: 'TKT_123',
        title: '发货问题',
        body: '请问何时发货',
        status: closed ? 'CLOSED' : 'OPEN',
        user_id: 'buyer',
        messages: [
          { role: 'admin', body: '<script>do-not-run</script> 商城回复', created_at: '2026-10-29T01:10:00Z' },
          ...(replied ? [{ role: 'admin', body: '今天安排', created_at: '2026-10-29T01:20:00Z' }] : []),
        ],
        message_total: replied ? 2 : 1,
      };
    if (action === 'reply') {
      replied = true;
      return { id: 'TKT_123' };
    }
    if (action === 'close') {
      closed = true;
      return { id: 'TKT_123' };
    }
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.app.click({ svcAdmin: 'open', id: 'TKT_123' });
  let html = f.m.render();
  assert.match(html, /工单对话/);
  assert.match(html, /顾客 · 发起问题/);
  assert.match(html, /商城客服/);
  assert.match(html, /&lt;script&gt;/);
  assert.doesNotMatch(html, /<pre|<script>/);
  await f.form('[data-svc-reply]', { body: '今天安排' });
  const write = calls.find((c) => c.action === 'reply');
  assert.equal(write.c.id, 'TKT_123');
  assert.equal(write.c.data.body, '今天安排');
  assert.ok(write.c.idempotency_key);
  assert.match(f.m.render(), /今天安排/);
  await f.app.click({ svcAdmin: 'close-ticket' });
  await f.app.click({ svcAdmin: 'confirm-close' });
  html = f.m.render();
  assert.match(html, /工单已关闭，会话记录保留/);
  assert.doesNotMatch(html, /data-svc-reply/);
});

test('provider editor exposes callback fields, masks stored keys and saves its own version', async (t) => {
  const calls = [];
  const provider = {
    id: 'qq',
    name: 'QQ网站登录',
    fields: [
      { key: 'app_id', label: '应用App ID', type: 'text' },
      { key: 'redirect_uri', label: '授权回调地址', type: 'url' },
      { key: 'app_secret', label: '应用密钥', type: 'password', secret: true },
    ],
    values: { app_id: 'app-old', redirect_uri: 'https://shop.example/index.html' },
    secret_configured: { app_secret: true },
    version: 3,
    configured: true,
    secure_storage_ready: true,
  };
  const f = fixture(
    'loginSettings',
    async (service, action, c) => {
      calls.push({ service, action, c });
      return action === 'provider_get'
        ? { items: [provider] }
        : { config: { password_enabled: true }, version: 8, capabilities: [] };
    },
    true,
  );
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.app.click({ access: 'provider', id: 'qq' });
  let html = f.m.render();
  assert.match(html, /授权回调地址/);
  assert.match(html, /name="app_secret"[^>]*type="password"[^>]*value=""/);
  assert.match(html, /已配置，留空保留原值/);
  await f.form(
    '[data-provider-form]',
    { app_id: 'app-new', redirect_uri: 'https://shop.example/return', app_secret: '', reason: '更新回调地址' },
    { id: 'qq' },
  );
  const write = calls.find((c) => c.action === 'provider_upsert');
  assert.equal(write.c.data.version, 3);
  assert.equal(write.c.data.values.redirect_uri, 'https://shop.example/return');
  assert.equal(write.c.data.values.app_secret, '');
  assert.deepEqual(Array.from(write.c.data.clear_secrets), []);
  f.setScope('admin-b');
  assert.deepEqual(JSON.parse(JSON.stringify(f.m.state.providerDrafts)), {});
});

test('generic business results display Chinese fields and hide secret material', (t) => {
  const app = createHarness(directory, 'http://readable-result.test');
  t.after(() => app.dispose());
  app.load('admin-ui.js');
  app.load('admin-details.js');
  const html = app.ctx.LumoAdminDetails.render(
    {
      id: 'record-a',
      status: 'SUCCEEDED',
      amount_cents: 1234,
      details: [{ quantity: 2, body: '业务说明' }],
      app_secret: 'private-key-value',
    },
    { title: '处理结果' },
  );
  assert.match(html, /记录编号/);
  assert.match(html, /已成功/);
  assert.match(html, /¥12.34/);
  assert.match(html, /业务说明/);
  assert.doesNotMatch(html, /<pre|private-key-value|"status"/);
});
