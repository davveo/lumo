'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const dir = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const copy = (value) => JSON.parse(JSON.stringify(value));
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
const record = (id = 'coupon-live') => ({
  id,
  name: '满百减十',
  amount_cents: 1000,
  min_cents: 10000,
  valid_days: 7,
  remaining: 99,
  active: true,
  starts_at: null,
  expires_at: new Date(Date.now() + 86400000 * 30).toISOString(),
  min_points: 0,
  min_growth: 0,
  version: 2,
  claimed_count: 1,
});
function fixture({ admin = true, rows = [record()] } = {}) {
  const calls = [],
    notices = [],
    listeners = new Map();
  let scope = 'admin-a',
    allowed = admin,
    present = true,
    read = null,
    write = null;
  const app = createHarness(dir, 'http://coupon-admin.test', { html: 'admin.html' });
  const root = new Element('coupon-admin-root'),
    query = app.document.querySelector.bind(app.document);
  app.document.querySelector = (selector) =>
    selector === '#coupon-admin-root' ? (present ? root : null) : query(selector);
  const add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    add(type, fn, ...rest);
  };
  app.load('skeleton.js');
  app.load('coupon-admin.js');
  const module = app.ctx.LumoCouponAdmin;
  module.configure({
    isAdmin: () => allowed,
    scope: () => scope,
    draw() {},
    notify: (message) => notices.push(message),
    async call(service, action, command) {
      calls.push(copy({ service, action, command }));
      if (action === 'templates_admin') {
        if (read) return read(command);
        const selected = rows.filter(
          (row) =>
            (!command.data.active || String(row.active) === command.data.active) &&
            (!command.data.keyword || [row.name, row.id].some((value) => value.includes(command.data.keyword))),
        );
        return {
          items: copy(selected.slice((command.data.page - 1) * 10, command.data.page * 10)),
          total: selected.length,
          page: command.data.page,
        };
      }
      assert.equal(action, 'template_upsert');
      if (write) return write(command);
      const result = { ...copy(command.data), id: command.id, version: 1, claimed_count: 0 };
      rows = [...rows.filter((row) => row.id !== command.id), result];
      return result;
    },
  });
  const fire = async (type, target) => {
    for (const fn of listeners.get(type) || []) await fn({ target, preventDefault() {} });
  };
  return {
    app,
    module,
    calls,
    notices,
    dispose: () => app.dispose(),
    click: (action, id) => app.click({ caAction: action, ...(id ? { caId: id } : {}) }),
    submit: () => app.submit('coupon-admin-save', {}),
    async field(key, value, type) {
      const target = new Element();
      target.dataset = { caField: key };
      target.type = type || (typeof value === 'boolean' ? 'checkbox' : 'text');
      target.value = String(value);
      target.checked = Boolean(value);
      await fire('input', target);
    },
    async filter(value) {
      const target = new Element();
      target.dataset = { caFilter: 'active' };
      target.value = value;
      await fire('change', target);
    },
    setRead(fn) {
      read = fn;
    },
    setWrite(fn) {
      write = fn;
    },
    changeScope(next, authorized = false) {
      scope = next;
      allowed = authorized;
      app.ctx.dispatchEvent(new app.ctx.CustomEvent('lumo:logout'));
    },
    hide() {
      present = false;
    },
  };
}

test('coupon management reads server pages including disabled templates and filters without fetching a public snapshot', async (t) => {
  const rows = Array.from({ length: 23 }, (_, index) => ({
    ...record('coupon-' + index),
    name: '券 ' + index,
    active: index % 2 === 0,
  }));
  const f = fixture({ rows });
  t.after(f.dispose);
  await f.module.refresh();
  assert.match(f.module.render(), /共 <strong>23<\/strong>/);
  assert.equal((f.module.render().match(/data-ca-action="edit"/g) || []).length, 10);
  await f.click('next');
  assert.equal(f.calls.at(-1).command.data.page, 2);
  assert.match(f.module.render(), /coupon-10/);
  assert.doesNotMatch(f.module.render(), /data-ca-id="coupon-0"/);
  await f.filter('false');
  assert.equal(f.calls.at(-1).command.data.page, 1);
  assert.equal(f.calls.at(-1).command.data.active, 'false');
  assert.match(f.module.render(), /共 <strong>11<\/strong>/);
  await f.app.submit('coupon-admin-search', { keyword: 'coupon-21' });
  assert.match(f.module.render(), /coupon-21/);
  assert.doesNotMatch(f.module.render(), /data-ca-id="coupon-1"/);
  assert.ok(f.calls.every((row) => row.action === 'templates_admin'));
});

test('new coupon uses exact cents, real eligibility fields and an authoritative read after save', async (t) => {
  const f = fixture();
  t.after(f.dispose);
  await f.module.refresh();
  f.calls.length = 0;
  await f.click('new');
  await f.field('name', '周末欢迎券');
  await f.field('amount', '10.05');
  await f.field('minimum', '99.90');
  await f.field('remaining', '1000');
  await f.field('min_points', '500');
  await f.field('min_growth', '50');
  const id = f.module.readDraft().id;
  await f.submit();
  assert.equal(f.calls.length, 2);
  assert.equal(f.calls[0].action, 'template_upsert');
  const data = f.calls[0].command.data;
  assert.equal(data.amount_cents, 1005);
  assert.equal(data.min_cents, 9990);
  assert.equal(data.remaining, 1000);
  assert.equal(data.min_points, 500);
  assert.equal(data.min_growth, 50);
  assert.equal(data.version, 0);
  assert.equal(data.starts_at, null);
  assert.ok(data.expires_at.endsWith('Z'));
  assert.equal(f.calls[1].action, 'templates_admin');
  assert.equal(f.module.readDraft(), null);
  assert.match(f.module.render(), new RegExp(id));
  assert.deepEqual(f.notices, ['优惠券已保存']);
});

test('issued benefits are locked while stock edits use deltas and preserve the optimistic version', async (t) => {
  const f = fixture();
  t.after(f.dispose);
  await f.module.refresh();
  f.calls.length = 0;
  await f.click('edit', 'coupon-live');
  assert.match(f.module.render(), /权益已锁定|有效天数已锁定/);
  await f.field('amount', '999.00');
  await f.field('minimum', '0');
  await f.field('valid_days', '999');
  await f.field('name', '新的名称');
  await f.field('stock_delta', '5');
  await f.submit();
  const command = f.calls[0].command;
  assert.equal(command.data.amount_cents, 1000);
  assert.equal(command.data.min_cents, 10000);
  assert.equal(command.data.valid_days, 7);
  assert.equal(command.data.version, 2);
  assert.equal(command.data.stock_delta, 5);
  assert.equal(command.data.remaining, undefined);
});

test('copy creates a distinct inactive coupon and toggle keeps existing benefits without issuing a coupon', async (t) => {
  const f = fixture();
  t.after(f.dispose);
  await f.module.refresh();
  f.calls.length = 0;
  await f.click('copy', 'coupon-live');
  const draft = f.module.readDraft();
  assert.notEqual(draft.id, 'coupon-live');
  assert.equal(draft.active, false);
  assert.equal(draft.version, 0);
  assert.equal(draft.claimed_count, 0);
  assert.equal(draft.remaining, '100');
  await f.field('amount', '22.10');
  await f.click('back');
  await f.click('toggle', 'coupon-live');
  const write = f.calls.find((row) => row.action === 'template_upsert');
  assert.equal(write.command.id, 'coupon-live');
  assert.equal(write.command.data.active, false);
  assert.equal(write.command.data.amount_cents, 1000);
  assert.equal(write.command.data.stock_delta, 0);
  assert.ok(!f.calls.some((row) => ['claim', 'reserve', 'confirm'].includes(row.action)));
});

test('invalid amounts, eligibility and dates cannot submit a management write', async (t) => {
  const f = fixture();
  t.after(f.dispose);
  await f.module.refresh();
  await f.click('new');
  f.calls.length = 0;
  await f.field('name', '测试券');
  await f.field('amount', '1.005');
  await f.submit();
  assert.match(f.module.render(), /最多两位小数/);
  await f.field('amount', '1.00');
  await f.field('min_points', '-1');
  await f.submit();
  assert.match(f.module.render(), /积分门槛超出/);
  await f.field('min_points', '0');
  await f.field('expires_at', 'invalid');
  await f.submit();
  assert.match(f.module.render(), /正确的领取时间/);
  assert.equal(f.calls.length, 0);
  assert.notEqual(f.module.readDraft(), null);
});

test('authorization, hidden panels and stale read/write replies cannot expose private coupon configuration', async (t) => {
  const f = fixture();
  t.after(f.dispose);
  const wait = deferred();
  f.setRead(() => wait.promise);
  const pending = f.module.refresh();
  f.changeScope('admin-b');
  wait.resolve({ items: [{ ...record(), name: '旧账号私有券' }], total: 1 });
  await pending;
  assert.doesNotMatch(f.module.render(), /旧账号私有券/);
  assert.equal(await f.module.refresh(), false);
  const hidden = fixture();
  t.after(hidden.dispose);
  await hidden.module.refresh();
  hidden.hide();
  hidden.calls.length = 0;
  await hidden.click('new');
  assert.equal(hidden.module.readDraft(), null);
  assert.equal(hidden.calls.length, 0);
  const stale = fixture();
  t.after(stale.dispose);
  await stale.module.refresh();
  await stale.click('new');
  await stale.field('name', '等待保存');
  const saving = deferred();
  stale.setWrite(() => saving.promise);
  const save = stale.submit();
  stale.changeScope('admin-other');
  saving.resolve(record());
  await save;
  assert.doesNotMatch(stale.module.render(), /等待保存|满百减十/);
  assert.equal(stale.notices.length, 0);
});

test('a rejected save retains the draft and a failed list refresh retains the last real rows', async (t) => {
  const f = fixture();
  t.after(f.dispose);
  await f.module.refresh();
  f.setRead(() => {
    throw Error('列表读取失败');
  });
  await f.module.refresh();
  assert.match(f.module.render(), /满百减十/);
  assert.match(f.module.render(), /列表读取失败/);
  f.setRead(null);
  await f.click('edit', 'coupon-live');
  await f.field('name', '保留我的编辑');
  f.setWrite(() => {
    throw Error('版本冲突，请刷新');
  });
  await f.submit();
  assert.equal(f.module.readDraft().name, '保留我的编辑');
  assert.match(f.module.render(), /版本冲突/);
});
