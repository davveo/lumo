'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const copy = (value) => JSON.parse(JSON.stringify(value));
const deferred = () => {
  let resolve;
  const promise = new Promise((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
};
function setup(
  initial = { brand: '原品牌', heroImage: 'hero.jpg', extra_layout: { columns: 6 }, heroFileId: 'private-file' },
) {
  const app = createHarness(directory, 'http://settings-admin.test', { html: 'admin.html' });
  const listeners = new Map(),
    calls = [],
    messages = [];
  let owner = 'a',
    admin = true,
    server = copy(initial),
    readHandler = null,
    saveHandler = null;
  const add = app.document.addEventListener.bind(app.document);
  app.document.addEventListener = (type, fn, ...rest) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    add(type, fn, ...rest);
  };
  app.ids.set('store-settings-admin-root', new Element('store-settings-admin-root'));
  app.load('skeleton.js');
  app.load('storefront-layout.js');
  app.load('store-settings-admin.js');
  const module = app.ctx.LumoStoreSettingsAdmin;
  module.configure({
    isAdmin: () => admin,
    scope: () => owner,
    draw() {},
    notify: (value) => messages.push(value),
    async call(service, action, command) {
      calls.push(copy({ service, action, command }));
      assert.equal(service, 'content');
      if (action === 'get') {
        if (readHandler) return readHandler();
        return { id: 'storefront-main', body: JSON.stringify(server) };
      }
      assert.equal(action, 'upsert');
      if (saveHandler) return saveHandler(command);
      server = JSON.parse(command.data.body);
      return { id: command.id, ...command.data };
    },
  });
  return {
    app,
    module,
    calls,
    messages,
    get server() {
      return server;
    },
    set server(value) {
      server = copy(value);
    },
    setRead(value) {
      readHandler = value;
    },
    setSave(value) {
      saveHandler = value;
    },
    owner(value, allowed = true) {
      owner = value;
      admin = allowed;
    },
    async input(key, value) {
      const element = new Element();
      element.dataset = { ssField: key };
      element.value = value;
      for (const fn of listeners.get('input') || []) await fn({ target: element });
    },
    save: () => app.submit('store-settings-admin-form'),
  };
}
test('settings read before editing and merge only edited fields into the latest server document', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  assert.match(f.module.render(), /sk-loading|正在读取/);
  assert.equal(await f.module.refresh(), true);
  await f.input('brand', '新品牌');
  f.server = { ...f.server, sectionTitle: '另一位管理员的标题', extra_layout: { columns: 8, color: 'blue' } };
  await f.save();
  assert.equal(f.server.brand, '新品牌');
  assert.equal(f.server.sectionTitle, '另一位管理员的标题');
  assert.deepEqual(f.server.extra_layout, { columns: 8, color: 'blue' });
  assert.equal(f.server.heroFileId, 'private-file');
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 1);
  assert.equal(f.calls.at(-1).action, 'get');
  assert.equal(f.messages.length, 1);
});
test('unsafe image URLs and invalid colors cannot publish, and failed writes retain edits', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.input('heroImage', 'javascript:alert(1)');
  await f.save();
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 0);
  assert.match(f.module.render(), /HTTPS/);
  await f.input('heroImage', 'https://images.example.test/hero.jpg');
  await f.input('accent', 'red;bad');
  await f.save();
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 0);
  await f.input('accent', '#112233');
  await f.input('brand', '<新品牌>');
  f.setSave(() => {
    throw Error('保存暂不可用');
  });
  await f.save();
  assert.match(f.module.render(), /保存暂不可用/);
  assert.match(f.module.render(), /&lt;新品牌&gt;/);
  assert.equal(f.messages.length, 0);
});
test('404 permits explicit first configuration but read failure and malformed data do not invent saved settings', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  f.setRead(() => {
    throw Object.assign(Error('missing'), { status: 404 });
  });
  await f.module.refresh();
  assert.match(f.module.render(), /尚未建立首页配置/);
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 0);
  f.module.reset();
  f.setRead(() => ({ body: '[]' }));
  assert.equal(await f.module.refresh(), false);
  assert.match(f.module.render(), /商城配置应为对象/);
  assert.doesNotMatch(f.module.render(), /name="brand"/);
});
test('late settings reads and saves cannot restore another administrative session', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  const pending = deferred();
  f.setRead(() => pending.promise);
  const loading = f.module.refresh();
  f.owner('b', false);
  f.module.reset();
  pending.resolve({ body: '{"brand":"private-old"}' });
  await loading;
  assert.equal(f.module.render(), '');
  f.owner('b');
  f.setRead(null);
  await f.module.refresh();
  await f.input('brand', 'old-write');
  const saving = deferred();
  f.setSave(() => saving.promise);
  const save = f.save();
  await new Promise((resolve) => setImmediate(resolve));
  f.owner('c', false);
  f.module.reset();
  saving.resolve({ body: '{}' });
  await save;
  assert.equal(f.module.render(), '');
  assert.equal(f.messages.length, 0);
});

test('same-session refresh while settings are being saved joins the write without restoring a dirty-state error', async (t) => {
  const f = setup();
  t.after(() => f.app.dispose());
  await f.module.refresh();
  await f.input('brand', '待确认的新品牌');
  const gate = deferred();
  f.setSave(async (command) => {
    await gate.promise;
    f.server = JSON.parse(command.data.body);
    return { id: command.id, ...command.data };
  });
  const saving = f.save();
  await new Promise((resolve) => setImmediate(resolve));
  let refreshed = false;
  const refresh = f.module.refresh().then((result) => {
    refreshed = true;
    return result;
  });
  await new Promise((resolve) => setImmediate(resolve));
  const returnedBeforeWrite = refreshed;
  gate.resolve();
  await saving;
  assert.equal(await refresh, true);
  assert.equal(returnedBeforeWrite, false);
  assert.equal(f.server.brand, '待确认的新品牌');
  assert.match(f.module.render(), /已与后端配置同步/);
  assert.doesNotMatch(f.module.render(), /有未保存的修改|仍有未保存/);
  assert.equal(f.messages.length, 1);
  assert.equal(f.calls.filter((row) => row.action === 'upsert').length, 1);
});
