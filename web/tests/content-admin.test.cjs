'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const copy = (value) => JSON.parse(JSON.stringify(value));
function setup() {
  const app = createHarness(directory, 'http://content-admin.test', { html: 'admin.html' });
  const calls = [],
    messages = [],
    listeners = new Map();
  let owner = 'a',
    admin = true,
    failSave = false,
    handler = null,
    writeGate = null;
  let rows = Array.from({ length: 42 }, (_, index) => ({
    id: 'notice-' + index,
    kind: 'notice',
    title: '公告' + index,
    body: '<正文>' + index,
  }));
  const add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    add(type, fn, ...rest);
  };
  app.ids.set('content-admin-root', new Element('content-admin-root'));
  app.load('skeleton.js');
  app.load('content-admin.js');
  const module = app.ctx.LumoContentAdmin;
  module.configure({
    isAdmin: () => admin,
    scope: () => owner,
    draw() {},
    notify: (message) => messages.push(message),
    async call(service, action, command) {
      calls.push(copy({ service, action, command }));
      if (handler) return handler(service, action, command);
      if (action === 'list')
        return {
          items: copy(
            rows
              .filter((row) => row.kind === command.data.kind)
              .slice(command.data.offset, command.data.offset + command.data.limit),
          ),
        };
      if (action === 'get') return copy(rows.find((row) => row.id === command.id));
      assert.equal(action, 'upsert');
      if (writeGate) await writeGate;
      if (failSave) throw Error('发布失败');
      const row = { id: command.id, ...copy(command.data) };
      rows = [row, ...rows.filter((value) => value.id !== row.id)];
      return copy(row);
    },
  });
  return {
    app,
    module,
    calls,
    messages,
    fail(value) {
      failSave = value;
    },
    gate(value) {
      writeGate = value;
    },
    owner(value, allowed = false) {
      owner = value;
      admin = allowed;
    },
    handler(value) {
      handler = value;
    },
    async field(key, value) {
      const element = new Element();
      element.dataset = { cnField: key };
      element.value = value;
      for (const fn of listeners.get(key === 'kind' ? 'change' : 'input') || []) await fn({ target: element });
    },
    save: () => app.submit('content-admin-form'),
  };
}
test('content pagination reads only its selected server page and distinguishes FAQ from notices', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  assert.deepEqual(f.calls[0].command.data, { kind: 'notice', limit: 21, offset: 0 });
  assert.match(f.module.render(), /公告19/);
  assert.doesNotMatch(f.module.render(), /公告20/);
  assert.match(f.module.render(), /&lt;正文&gt;/);
  await f.app.click({ cnAction: 'next' });
  assert.equal(f.calls.at(-1).command.data.offset, 20);
  assert.match(f.module.render(), /公告20/);
  await f.app.click({ cnKind: 'faq' });
  assert.equal(f.calls.at(-1).command.data.kind, 'faq');
  assert.equal(f.calls.at(-1).command.data.offset, 0);
  assert.match(f.module.render(), /暂无已发布内容/);
  assert.doesNotMatch(f.module.render(), /删除|停用/);
});
test('publishing FAQ uses a stable generated ID, confirms canonical data and reloads its actual collection', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ cnAction: 'new' });
  await f.field('kind', 'faq');
  await f.field('title', '如何退货？');
  await f.field('body', '在订单页提交售后申请。');
  f.fail(true);
  await f.save();
  const first = f.calls.find((row) => row.action === 'upsert');
  assert.match(f.module.render(), /发布失败/);
  assert.match(f.module.render(), /如何退货/);
  f.fail(false);
  await f.save();
  const writes = f.calls.filter((row) => row.action === 'upsert');
  assert.equal(writes.length, 2);
  assert.equal(writes[1].command.id, first.command.id);
  assert.equal(writes[1].command.data.kind, 'faq');
  assert.equal(f.calls.at(-2).action, 'get');
  assert.equal(f.calls.at(-1).command.data.kind, 'faq');
  assert.equal(f.messages.length, 1);
  assert.match(f.module.render(), /如何退货/);
});
test('content editor rejects oversized UTF-8 and keeps existing record kind and identity', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ cnAction: 'edit', cnId: 'notice-0' });
  await f.field('kind', 'faq');
  await f.field('body', '中'.repeat(7000));
  await f.save();
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 0);
  await f.field('body', '更新后的公告');
  await f.save();
  const write = f.calls.find((row) => row.action === 'upsert');
  assert.equal(write.command.id, 'notice-0');
  assert.equal(write.command.data.kind, 'notice');
});
test('late content list replies cannot leak a previous administrative session', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  let resolve;
  const pending = new Promise((yes) => {
    resolve = yes;
  });
  f.handler(() => pending);
  const loading = f.module.refresh();
  f.owner('b');
  f.module.reset();
  resolve({ items: [{ id: 'old', kind: 'notice', title: '旧账号内容' }] });
  await loading;
  assert.equal(f.module.render(), '');
  assert.equal(f.messages.length, 0);
});

test('same-session refresh during content publication waits for canonical confirmation and closes the new draft', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ cnAction: 'new' });
  await f.field('title', '待确认的新公告');
  await f.field('body', '发布流程在切换页面后继续。');
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  f.gate(gate);
  const saving = f.save();
  await new Promise((resolve) => setImmediate(resolve));
  let refreshed = false;
  const refresh = f.module.refresh().then((result) => {
    refreshed = true;
    return result;
  });
  await new Promise((resolve) => setImmediate(resolve));
  const returnedBeforeWrite = refreshed;
  release();
  await saving;
  assert.equal(await refresh, true);
  assert.equal(returnedBeforeWrite, false);
  assert.doesNotMatch(f.module.render(), /id="content-admin-form"/);
  assert.match(f.module.render(), /待确认的新公告/);
  assert.equal(f.messages.length, 1);
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 1);
  assert.equal(f.calls.filter((row) => row.action === 'list').length, 2);
});

test('a previous session content publication does not hold up or repopulate the new session', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.app.click({ cnAction: 'new' });
  await f.field('title', '旧会话未完成发布');
  await f.field('body', '仅旧会话的在途操作');
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  f.gate(gate);
  const saving = f.save();
  await new Promise((resolve) => setImmediate(resolve));
  f.owner('b', true);
  f.module.reset();
  assert.equal(await f.module.refresh(), true);
  release();
  await saving;
  assert.doesNotMatch(f.module.render(), /旧会话未完成发布|id="content-admin-form"/);
  assert.equal(f.messages.length, 0);
});
