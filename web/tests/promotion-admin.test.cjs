'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const copy = (value) => JSON.parse(JSON.stringify(value));
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
const baseRule = () => ({
  id: 'test-rule',
  kind: 'reduction',
  name: '满百减十',
  description: '测试优惠',
  threshold_cents: 10000,
  discount_cents: 1000,
  pay_rate_bps: 0,
  min_quantity: 1,
  quota: 20,
  held: 0,
  used: 0,
  active: true,
  version: 3,
});
function fixture({ admin = true, items = [baseRule()] } = {}) {
  const calls = [],
    messages = [],
    handlers = new Map(),
    listeners = new Map();
  const app = createHarness(directory, 'http://promotion-admin.test', { html: 'admin.html' });
  const listen = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    listen(type, fn, ...rest);
  };
  app.load('skeleton.js');
  app.load('promotion-admin.js');
  let scope = 'admin-a',
    authorized = admin,
    savedVersion = 0;
  const editor = app.ctx.LumoPromotionAdmin;
  editor.configure({
    scope: () => scope,
    isAdmin: () => authorized,
    draw() {},
    notify: (message) => messages.push(message),
    async call(service, action, command) {
      calls.push(copy({ service, action, command }));
      const handler = handlers.get(service + '/' + action);
      if (handler) return handler(command);
      if (service === 'campaign' && action === 'admin_list') return { items: copy(items), total: items.length };
      if (service === 'campaign' && action === 'upsert')
        return { id: command.id, ...copy(command.data), held: 0, used: 0, version: ++savedVersion };
      if (service === 'flash' && action === 'list')
        return {
          items: [{ id: 'flash-one', name: '秒杀场次', sku_id: 'sku-phone', activity_id: 'flash-demo', active: true }],
        };
      if (service === 'group' && action === 'list')
        return {
          items: [
            {
              id: 'team-one',
              sku_id: 'sku-phone',
              required: 2,
              occupied: 1,
              paid: 1,
              status: 'OPEN',
              expires_at: '2027-01-01T00:00:00Z',
            },
          ],
        };
      throw Error('Unexpected operation: ' + service + '/' + action);
    },
  });
  async function fire(type, target) {
    for (const fn of listeners.get(type) || []) await fn({ target, preventDefault() {} });
  }
  return {
    app,
    editor,
    calls,
    messages,
    handlers,
    async load() {
      assert.equal(await editor.refresh(), true);
      calls.length = 0;
    },
    click(action, id) {
      return app.click({ pmAction: action, ...(id ? { pmId: id } : {}) });
    },
    mode(value) {
      return app.click({ pmMode: value });
    },
    async field(key, value, type = 'input') {
      const target = new Element();
      target.dataset = { pmField: key };
      target.value = String(value);
      await fire(type, target);
    },
    async search(values) {
      const form = new Element('promotion-admin-search');
      form.fields = values;
      await fire('submit', form);
    },
    revoke(next = 'admin-b') {
      scope = next;
      authorized = false;
      editor.reset();
    },
  };
}

test('promotion console starts with skeletons and reads only the selected administrative resource', async (t) => {
  const f = fixture({
    items: [baseRule(), { ...baseRule(), id: 'paused', active: false }, { ...baseRule(), id: 'system', kind: 'flash' }],
  });
  t.after(() => f.app.dispose());
  assert.match(f.editor.render(), /sk-loading-table/);
  assert.doesNotMatch(f.editor.render(), /暂无匹配/);
  await f.editor.refresh();
  assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].service, 'campaign');
  assert.equal(f.calls[0].action, 'admin_list');
  assert.deepEqual(f.calls[0].command.data, { page: 1, page_size: 10, keyword: '', kind: '' });
  const html = f.editor.render();
  assert.match(html, /已停用/);
  assert.match(html, /系统规则/);
  assert.doesNotMatch(html, /data-pm-action="edit"[^>]*data-pm-id="system"/);
});

test('campaign filters and pagination use bounded explicit backend reads', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.handlers.set('campaign/admin_list', (command) => ({
    items: Array.from({ length: 10 }, (_, i) => ({ ...baseRule(), id: 'page-' + command.data.page + '-' + i })),
    total: 23,
  }));
  await f.load();
  await f.click('next');
  assert.equal(f.calls.at(-1).command.data.page, 2);
  assert.match(f.editor.render(), /page-2-0/);
  assert.doesNotMatch(f.editor.render(), /page-1-0/);
  await f.search({ keyword: '百元', kind: 'discount', status: 'paused' });
  assert.deepEqual(f.calls.at(-1).command.data, {
    page: 1,
    page_size: 10,
    keyword: '百元',
    kind: 'discount',
    active: false,
  });
  assert.match(f.editor.render(), /1 \/ 3/);
});

test('new rules stay local until save and convert decimal yuan without binary rounding loss', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  await f.load();
  await f.click('new');
  assert.equal(f.calls.length, 0);
  assert.equal(f.editor.readDraft().active, false);
  await f.field('id', 'new-rule');
  await f.field('name', '<b>新的满减</b>');
  await f.field('threshold', '100.29');
  await f.field('discount', '0.29');
  await f.field('quota', '30');
  assert.match(f.editor.render(), /&lt;b&gt;新的满减&lt;\/b&gt;/);
  await f.click('save');
  const write = f.calls.find((call) => call.action === 'upsert');
  assert.ok(write);
  assert.equal(write.command.id, 'new-rule');
  assert.equal(write.command.data.threshold_cents, 10029);
  assert.equal(write.command.data.discount_cents, 29);
  assert.equal(write.command.data.active, false);
  assert.equal(write.command.data.version, 0);
  assert.equal(f.calls.at(-1).action, 'admin_list');
  assert.equal(f.editor.isDirty(), false);
});

test('percentage rules submit a pay rate and never retain an unrelated fixed reduction', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  await f.load();
  await f.click('new');
  await f.field('id', 'multi');
  await f.field('name', '两件折扣');
  await f.field('kind', 'quantity_discount', 'change');
  await f.field('payRate', '8.555');
  assert.equal(f.editor.readDraft().minQuantity, 2);
  await f.click('save');
  const write = f.calls.find((call) => call.action === 'upsert');
  assert.equal(write.command.data.pay_rate_bps, 8555);
  assert.equal(write.command.data.discount_cents, 0);
  assert.equal(write.command.data.min_quantity, 2);
});

test('invalid amounts and insufficient quota cannot submit and a copied rule resets identity and usage', async (t) => {
  const row = { ...baseRule(), held: 3, used: 2 };
  const f = fixture({ items: [row] });
  t.after(() => f.app.dispose());
  await f.load();
  await f.click('edit', row.id);
  await f.field('discount', '99');
  assert.equal(f.editor.readDraft().discount, 10, 'used benefit fields stay frozen');
  await f.field('quota', '4');
  await f.click('save');
  assert.equal(f.calls.length, 0);
  assert.match(f.editor.render(), /不得低于/);
  await f.click('back');
  await f.click('duplicate', row.id);
  const draft = f.editor.readDraft();
  assert.equal(draft.id, '');
  assert.equal(draft.version, 0);
  assert.equal(draft.held, 0);
  assert.equal(draft.used, 0);
  assert.equal(draft.active, false);
  await f.field('id', 'copied');
  await f.field('discount', '0.001');
  await f.click('save');
  assert.equal(f.calls.length, 0);
  assert.match(f.editor.render(), /最多填写两位小数/);
});

test('save conflicts preserve the draft and its expected version', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  await f.load();
  await f.click('edit', 'test-rule');
  await f.field('name', '保留我的修改');
  f.handlers.set('campaign/upsert', () => {
    throw Error('activity changed; reload before saving');
  });
  await f.click('save');
  assert.equal(f.editor.readDraft().name, '保留我的修改');
  assert.equal(f.editor.readDraft().version, 3);
  assert.equal(f.editor.isDirty(), true);
  assert.match(f.editor.render(), /activity changed/);
});

test('flash and group tabs display real records without exposing internal or fake editor commands', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  await f.load();
  await f.mode('flash');
  assert.match(f.editor.render(), /flash-one/);
  assert.match(f.editor.render(), /尚未开放编辑/);
  assert.doesNotMatch(f.editor.render(), /data-pm-action="new"|data-pm-action="edit"|data-pm-action="save"/);
  await f.click('new');
  assert.equal(f.editor.readDraft(), null, 'a readonly tab never opens an unrelated editor');
  await f.mode('groups');
  assert.match(f.editor.render(), /team-one/);
  assert.match(f.editor.render(), /已付 1 \/ 2 人/);
  assert.deepEqual(
    f.calls.map((call) => [call.service, call.action]),
    [
      ['flash', 'list'],
      ['group', 'list'],
    ],
  );
});

test('late reads and saves cannot repopulate data or drafts after the management session changes', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  const read = deferred();
  f.handlers.set('campaign/admin_list', () => read.promise);
  const pending = f.editor.refresh();
  f.revoke();
  read.resolve({ items: [baseRule()], total: 1 });
  assert.equal(await pending, false);
  assert.doesNotMatch(f.editor.render(), /满百减十|test-rule|data-pm-action="new"/);
  assert.match(f.editor.render(), /管理员/);
  const saved = fixture();
  t.after(() => saved.app.dispose());
  await saved.load();
  await saved.click('edit', 'test-rule');
  const write = deferred();
  saved.handlers.set('campaign/upsert', () => write.promise);
  const saving = saved.click('save');
  saved.revoke();
  write.resolve(baseRule());
  await saving;
  assert.equal(saved.editor.readDraft(), null);
  assert.doesNotMatch(saved.editor.render(), /test-rule|满百减十/);
});

test('refreshing during a pending save preserves the accepted mutation and its canonical version', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  await f.load();
  await f.click('edit', 'test-rule');
  await f.field('name', '已经保存的新名称');
  const write = deferred();
  let committed = false;
  f.handlers.set('campaign/upsert', () => write.promise);
  f.handlers.set('campaign/admin_list', () => ({
    items: [{ ...baseRule(), name: committed ? '已经保存的新名称' : '满百减十', version: committed ? 4 : 3 }],
    total: 1,
  }));
  const saving = f.click('save');
  assert.equal(await f.editor.refresh(), true, 'the page shell may refresh while a write is pending');
  await f.field('name', '不应修改进行中的提交');
  await f.click('save');
  assert.equal(
    f.calls.filter((row) => row.action === 'upsert').length,
    1,
    'read completion must not unlock an in-flight mutation',
  );
  committed = true;
  write.resolve({ ...baseRule(), name: '已经保存的新名称', version: 4 });
  await saving;
  assert.equal(f.editor.readDraft().version, 4);
  assert.equal(f.editor.readDraft().name, '已经保存的新名称');
  assert.equal(f.editor.isDirty(), false);
  assert.equal(f.messages.length, 1);
  assert.doesNotMatch(f.editor.render(), /role="alert"/);
  f.handlers.set('campaign/upsert', (command) => ({ id: command.id, ...command.data, held: 0, used: 0, version: 5 }));
  await f.click('save');
  assert.equal(
    f.calls.filter((row) => row.action === 'upsert').at(-1).command.data.version,
    4,
    'the next edit uses the accepted version',
  );
});

test('a read started before save commits cannot overwrite the authoritative post-save collection', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  await f.load();
  await f.click('edit', 'test-rule');
  await f.field('name', '提交后权威名称');
  const write = deferred(),
    beforeCommit = deferred();
  let reads = 0;
  f.handlers.set('campaign/upsert', () => write.promise);
  f.handlers.set('campaign/admin_list', () =>
    ++reads === 1 ? beforeCommit.promise : { items: [{ ...baseRule(), name: '提交后权威名称', version: 4 }], total: 1 },
  );
  const saving = f.click('save');
  const oldRefresh = f.editor.refresh();
  write.resolve({ ...baseRule(), name: '提交后权威名称', version: 4 });
  await saving;
  beforeCommit.resolve({ items: [{ ...baseRule(), name: '提交前旧名称', version: 3 }], total: 1 });
  assert.equal(await oldRefresh, false);
  assert.equal(f.editor.readDraft().version, 4);
  await f.click('back');
  assert.match(f.editor.render(), /提交后权威名称/);
  assert.doesNotMatch(f.editor.render(), /提交前旧名称/);
});
