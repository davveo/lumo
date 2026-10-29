'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
function fixture(key, call) {
  const app = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://management.test', {
    html: 'admin.html',
  });
  const listeners = new Map(),
    add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    add(type, fn, ...rest);
  };
  app.load('skeleton.js');
  app.load('admin-ui.js');
  app.load('management-admin.js');
  const m = app.ctx.LumoManagementAdmin.create(key);
  app.ids.set('mg-' + key, new Element());
  let scope = 'admin-a';
  m.configure({ isAdmin: () => true, scope: () => scope, draw() {}, call });
  return {
    app,
    m,
    setScope(value) {
      scope = value;
    },
    async form(selector, fields) {
      const target = new Element();
      target.fields = fields;
      target.matches = (q) => q === selector;
      for (const fn of listeners.get('submit') || []) await fn({ target, preventDefault() {} });
    },
  };
}

test('management uses server paging and keeps rows on connection errors', async (t) => {
  const calls = [];
  let fail = false;
  const f = fixture('special', async (service, action, command) => {
    calls.push({ service, action, command });
    if (fail) throw Error('连接暂不可用');
    return { items: [{ id: 'session-a', name: '测试场次', sku_id: 'sku-a', version: 2 }], total: 25 };
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  assert.equal(calls[0].action, 'admin_list');
  assert.equal(calls[0].command.data.page_size, 10);
  fail = true;
  await f.app.click({ mgPage: '2' });
  assert.equal(f.m.state.page, 1);
  assert.match(f.m.render(), /session-a/);
  assert.match(f.m.render(), /连接暂不可用/);
  await f.app.click({ mgMode: 'invalid' });
  assert.equal(f.m.state.mode, 'sessions');
});

test('session editor submits version, timezone window and change reason', async (t) => {
  const calls = [];
  const f = fixture('special', async (service, action, command) => {
    calls.push({ service, action, command });
    return action === 'admin_list'
      ? {
          items: [{ id: 'flash-a', name: '原场次', sku_id: 'sku-a', activity_id: 'quota-a', active: true, version: 3 }],
          total: 1,
        }
      : {};
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.app.click({ mg: 'edit', id: 'flash-a' });
  await f.form('[data-mg-save]', {
    id: 'flash-a',
    name: '新场次',
    sku_id: 'sku-a',
    activity_id: 'quota-a',
    active: 'false',
    starts_at: '2026-10-29T08:00',
    ends_at: '2026-10-29T10:00',
    reason: '调整时段',
  });
  const write = calls.find((c) => c.action === 'upsert');
  assert.equal(write.service, 'flash');
  assert.equal(write.command.data.version, 3);
  assert.equal(write.command.data.active, false);
  assert.equal(write.command.data.reason, '调整时段');
  assert.ok(write.command.idempotency_key);
  assert.ok(write.command.data.starts_at.endsWith('Z'));
  assert.equal(f.m.state.draft, null);
});

test('points compensation preserves a stable retry key and expected available balance', async (t) => {
  const calls = [];
  let fail = true;
  const f = fixture('points', async (service, action, command) => {
    calls.push({ service, action, command });
    if (action === 'admin_list') return { items: [{ id: 'member-a', username: '会员' }], total: 1 };
    if (action === 'admin_get') return { available: 80, frozen: 20, debt: 0 };
    if (action === 'get') return { display_name: '会员' };
    if (action === 'ledger') return { items: [], total: 0 };
    if (action === 'adjust' && fail) throw Error('网络暂时中断');
    return {};
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.app.click({ mg: 'points-detail', id: 'member-a' });
  await f.app.click({ mg: 'adjust-points' });
  await f.form('[data-mg-save]', { amount: '30', reason: '客服补偿' });
  assert.match(f.m.render(), /网络暂时中断/);
  assert.ok(f.m.state.draft);
  fail = false;
  await f.form('[data-mg-save]', { amount: '30', reason: '客服补偿' });
  const writes = calls.filter((c) => c.action === 'adjust');
  assert.equal(writes.length, 2);
  assert.equal(writes[0].command.idempotency_key, writes[1].command.idempotency_key);
  assert.equal(writes[1].command.data.expected_available, 80);
  assert.equal(writes[1].command.id, 'member-a');
  assert.equal(writes[1].command.data.amount, 30);
  assert.equal(f.m.state.draft, null);
});

test('shipping form sends integer cents, regions and optimistic policy version', async (t) => {
  const calls = [],
    policy = {
      name: '标准配送',
      enabled: true,
      mode: 'flat',
      base_fee_cents: 600,
      additional_fee_cents: 0,
      free_threshold_cents: 0,
      allowed_regions: [],
      blocked_regions: [],
      regions: [],
    };
  const f = fixture('shipping', async (service, action, command) => {
    calls.push({ service, action, command });
    return { config: policy, version: 4 };
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.form('[data-mg-save]', {
    name: '地区配送',
    enabled: 'true',
    mode: 'per_item',
    base_fee_cents: '8.50',
    additional_fee_cents: '2.10',
    free_threshold_cents: '199',
    allowed_regions: '广东省\n浙江省',
    blocked_regions: '西藏',
    regions: '广东省 | 9.90 | 3.00 | 299',
    reason: '调整运费',
  });
  const write = calls.find((c) => c.action === 'policy_upsert');
  assert.equal(write.service, 'logistics');
  assert.equal(write.command.data.version, 4);
  assert.equal(write.command.data.config.base_fee_cents, 850);
  assert.equal(write.command.data.config.additional_fee_cents, 210);
  assert.deepEqual(JSON.parse(JSON.stringify(write.command.data.config.regions)), [
    { region: '广东省', base_fee_cents: 990, additional_fee_cents: 300, free_threshold_cents: 29900 },
  ]);
});

test('scheduled article editor and reconciliation details stay bounded', async (t) => {
  const f = fixture('content', async () => ({ items: [], total: 0 }));
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.app.click({ mg: 'new' });
  assert.match(f.m.render(), /定时发布/);
  assert.match(f.m.render(), /name="unpublish_at"/);
  f.setScope('admin-b');
  assert.equal(f.m.state.draft, null);
  assert.equal(f.m.state.ready, false);
  const g = fixture('reconciliation', async (_s, _a, command) =>
    command.id
      ? {
          id: command.id,
          status: 'DIFFERENCES',
          differences: { items: Array.from({ length: 8 }, (_, i) => ({ payment_id: 'pay-' + i })), total: 500 },
          notes: [],
        }
      : { items: [{ id: 'report-a', status: 'DIFFERENCES' }], total: 1 },
  );
  t.after(() => g.app.dispose());
  await g.m.refresh();
  await g.app.click({ mg: 'report', id: 'report-a' });
  assert.match(g.m.render(), /pay-7/);
  assert.doesNotMatch(g.m.render(), /pay-8/);
  assert.match(g.m.render(), /第 1 \/ 63 页/);
  assert.match(g.m.render(), /最近50条/);
});

test('task retry sends owner service, unchanged event id and an audit reason', async (t) => {
  const calls = [];
  const f = fixture('tasks', async (service, action, command) => {
    calls.push({ service, action, command });
    return {
      items: [{ id: 'event-a', state: 'DEAD', retryable: true }],
      total: 1,
      services: ['inventory', 'notification'],
    };
  });
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.app.click({ mgMode: 'inbox' });
  await f.form('[data-mg-filter]', { service: 'notification', keyword: 'event-a' });
  await f.app.click({ mg: 'retry', id: 'event-a' });
  await f.form('[data-mg-save]', { reason: '依赖恢复后重试' });
  const write = calls.find((c) => c.action === 'retry');
  assert.equal(write.service, 'management');
  assert.equal(write.command.id, 'event-a');
  assert.equal(write.command.data.kind, 'inbox');
  assert.equal(write.command.data.service, 'notification');
  assert.equal(write.command.data.reason, '依赖恢复后重试');
});

test('member detail concurrent reads use API read semantics without invalidating each other', async (t) => {
  let api;
  const f = fixture('points', (...args) => api.call(...args, { token: 'test-admin-token', auth: true }));
  t.after(() => f.app.dispose());
  f.app.load('api-client.js');
  api = f.app.ctx.LumoAPI;
  f.app.ctx.fetch = async (url) => ({
    ok: true,
    status: 200,
    headers: { get: () => '' },
    async json() {
      return {
        data: url.endsWith('/admin_list')
          ? { items: [{ id: 'member-a' }], total: 1 }
          : url.endsWith('/admin_get')
            ? { available: 80, frozen: 20 }
            : url.endsWith('/ledger')
              ? { items: [], total: 0 }
              : { display_name: '会员' },
      };
    },
  });
  await f.m.refresh();
  await f.app.click({ mg: 'points-detail', id: 'member-a' });
  assert.equal(f.m.state.error, '');
  assert.equal(f.m.state.ready, true);
  assert.equal(f.m.state.account.available, 80);
  assert.match(f.m.render(), /人工补偿/);
});

test('notification editor previews safe sample variables without sending a message', async (t) => {
  const f = fixture('messages', async () => ({
    items: [
      {
        id: 'InvoiceIssued',
        event_type: 'InvoiceIssued',
        title: '票据 {{business_id}}',
        body: '金额 {{amount}} <script>',
        enabled: true,
        version: 2,
      },
    ],
    total: 1,
  }));
  t.after(() => f.app.dispose());
  await f.m.refresh();
  await f.app.click({ mg: 'edit', id: 'InvoiceIssued' });
  assert.match(f.m.render(), /站内信示例预览/);
  assert.match(f.m.render(), /票据 order-example-001/);
  assert.match(f.m.render(), /199.00 &lt;script&gt;/);
});

test('CSV export reads every page and neutralizes leading-whitespace formulas', async (t) => {
  let exported;
  const calls = [];
  const f = fixture('audit', async (_service, _action, command) => {
    calls.push(command);
    const page = command.data.page,
      size = command.data.page_size;
    return {
      items: Array.from({ length: Math.max(0, Math.min(size, 105 - (page - 1) * size)) }, (_, i) => ({
        id: 'audit-' + ((page - 1) * size + i),
        reason: '\t=HYPERLINK("x")',
        actor_id: 'admin',
      })),
      total: 105,
    };
  });
  t.after(() => f.app.dispose());
  f.app.ctx.Blob = Blob;
  f.app.ctx.URL = class extends URL {
    static createObjectURL(blob) {
      exported = blob;
      return 'blob:export';
    }
    static revokeObjectURL() {}
  };
  await f.m.refresh();
  await f.app.click({ mg: 'export' });
  const pages = calls.filter((c) => c.data.page_size === 100);
  assert.equal(pages.length, 2);
  assert.equal(pages[1].data.page, 2);
  const text = await exported.text();
  assert.match(text, /audit-104/);
  assert.match(text, /'\t=HYPERLINK/);
  assert.equal(f.m.state.error, '');
});

test('a shrinking server result clamps the management page before drawing', async (t) => {
  const calls = [];
  const f = fixture('audit', async (_service, _action, command) => {
    calls.push(command);
    return { items: command.data.page === 1 ? [{ id: 'only-row', user_id: 'only-row' }] : [], total: 1 };
  });
  t.after(() => f.app.dispose());
  f.m.state.page = 4;
  await f.m.refresh();
  assert.equal(calls.length, 2);
  assert.equal(f.m.state.page, 1);
  assert.match(f.m.render(), /only-row/);
});
