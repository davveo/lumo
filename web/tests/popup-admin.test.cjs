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
  return { resolve, reject, promise };
};
function campaign() {
  return {
    id: 'new-member',
    version: 3,
    name: '新会员见面礼',
    status: 'PUBLISHED',
    priority: 100,
    starts_at: '',
    ends_at: '',
    placements: ['home'],
    audience: {
      segments: ['new_member'],
      new_member_days: 7,
      min_growth: 0,
      max_growth: 0,
      min_points: 0,
      max_points: 0,
      include_user_ids: [],
      exclude_user_ids: [],
      interest_categories: [],
    },
    frequency: {
      max_per_user: 1,
      max_per_day: 1,
      cooldown_hours: 24,
      close_cooldown_hours: 24,
      once_per_session: true,
      stop_after_claim: true,
    },
    content: {
      title: '欢迎加入 LUMO',
      subtitle: '新会员专享',
      body: '领取你的专属优惠券',
      theme: 'red',
      coupon_template_id: 'welcome-100',
      button_text: '立即领取',
      action_type: 'claim_coupon',
      action_target: '#/coupons',
      auto_close_seconds: 0,
      delay_seconds: 1,
    },
    stats: { impressions: 21, clicks: 12, dismissals: 5, claims: 9 },
  };
}
function setup({ admin = true, items = [campaign()] } = {}) {
  const calls = [],
    messages = [],
    listeners = new Map();
  let session = 'admin-a',
    isAdmin = admin,
    saveHandler = null,
    readHandler = null;
  const fixture = createHarness(directory, 'http://popup-admin.test', { html: 'admin.html' });
  const original = fixture.document.addEventListener.bind(fixture.document);
  fixture.document.addEventListener = (type, fn, ...args) => {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(fn);
    original(type, fn, ...args);
  };
  fixture.load('popup-admin.js');
  const editor = fixture.ctx.LumoPopupAdmin;
  editor.configure({
    isAdmin: () => isAdmin,
    scope: () => session,
    draw() {},
    notify: (message) => messages.push(message),
    async call(service, action, command) {
      calls.push(copy({ service, action, ...(command ? { command } : {}) }));
      if (action === 'popup_list')
        return readHandler
          ? readHandler()
          : { items: copy(items), settings: { enabled: true, max_per_session: 1, min_gap_seconds: 60 } };
      if (action === 'templates_snapshot')
        return {
          items: [
            { id: 'welcome-100', name: '新会员满千减百', amount_cents: 10000, min_cents: 100000, state: 'available' },
          ],
        };
      if (saveHandler) return saveHandler(service, action, command);
      if (action === 'popup_configure') return copy(command.data);
      assert.equal(action, 'popup_upsert');
      return { ...copy(command.data), id: command.id || 'generated-popup', version: 4 };
    },
  });
  async function fire(type, target) {
    for (const fn of listeners.get(type) || []) await fn({ target, preventDefault() {} });
  }
  return {
    fixture,
    editor,
    calls,
    messages,
    dispose() {
      fixture.dispose();
    },
    async load() {
      assert.equal(await editor.refresh(), true);
      calls.length = 0;
    },
    click(action, id) {
      return fixture.click({ paAction: action, ...(id ? { paId: id } : {}) });
    },
    async field(name, value, type) {
      const target = new Element();
      target.dataset = { paField: name };
      target.type = type || (typeof value === 'number' ? 'number' : typeof value === 'boolean' ? 'checkbox' : 'text');
      target.value = String(value);
      target.checked = Boolean(value);
      await fire('input', target);
    },
    async list(name, value) {
      const target = new Element();
      target.dataset = { paList: name };
      target.value = value;
      await fire('input', target);
    },
    async segment(key, checked) {
      const target = new Element();
      target.dataset = { paSegment: key };
      target.checked = checked;
      await fire('change', target);
    },
    async placement(key, checked) {
      const target = new Element();
      target.dataset = { paPlacement: key };
      target.checked = checked;
      await fire('change', target);
    },
    setSave(fn) {
      saveHandler = fn;
    },
    setRead(fn) {
      readHandler = fn;
    },
    changeSession(value, authorized = false) {
      session = value;
      isAdmin = authorized;
      fixture.ctx.dispatchEvent(new fixture.ctx.CustomEvent('lumo:logout'));
    },
  };
}

test('admin lists canonical campaigns, global controls and actual metrics from two bounded reads', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  assert.equal(await f.editor.refresh(), true);
  assert.deepEqual(
    f.calls.map((call) => [call.service, call.action]),
    [
      ['content', 'popup_list'],
      ['coupon', 'templates_snapshot'],
    ],
  );
  assert.equal(f.calls[1].command.data.page_size, 5000);
  const html = f.editor.render();
  assert.match(html, /新会员见面礼/);
  assert.match(html, /全局展示控制/);
  assert.match(html, /领取成功/);
  assert.match(html, /21/);
  assert.match(html, /已启用/);
});
test('draft audience conditions are combined without server writes until explicit publish', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  await f.load();
  await f.click('edit', 'new-member');
  await f.segment('all', true);
  assert.deepEqual(copy(f.editor.draft.audience.segments), ['all']);
  await f.segment('new_member', true);
  await f.segment('returning_member', true);
  await f.field('audience.min_points', 500);
  await f.field('audience.max_points', 8000);
  await f.field('audience.new_member_days', 14);
  await f.list('audience.include_user_ids', 'user-a\nuser-b,user-a， user-c');
  await f.list('audience.exclude_user_ids', 'user-b');
  await f.list('audience.interest_categories', '手机数码\n家用电器');
  await f.placement('product', true);
  assert.equal(f.editor.dirty, true);
  assert.deepEqual(f.calls, []);
  const snapshot = copy(f.editor.draft);
  snapshot.name = 'outside mutation';
  assert.equal(f.editor.draft.name, '新会员见面礼');
  await f.click('publish');
  assert.equal(f.calls.length, 1);
  const command = f.calls[0].command;
  assert.equal(command.id, 'new-member');
  assert.equal(command.data.version, 3);
  assert.equal(command.data.status, 'PUBLISHED');
  assert.deepEqual(command.data.audience.segments, ['new_member', 'returning_member']);
  assert.deepEqual(command.data.audience.include_user_ids, ['user-a', 'user-b', 'user-c']);
  assert.deepEqual(command.data.audience.exclude_user_ids, ['user-b']);
  assert.deepEqual(command.data.placements, ['home', 'product']);
  assert.equal(command.data.audience.min_points, 500);
  assert.equal(f.editor.dirty, false);
});
test('new and copied campaigns remain drafts and preserve distinct content without old identities', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  await f.load();
  await f.click('duplicate', 'new-member');
  assert.equal(f.editor.draft.id, '');
  assert.equal(f.editor.draft.version, undefined);
  assert.equal(f.editor.draft.stats, undefined);
  assert.equal(f.editor.draft.status, 'DRAFT');
  await f.field('name', '老会员回馈');
  await f.field('content.theme', 'peach');
  await f.click('save-draft');
  assert.equal(f.calls[0].command.id, undefined);
  assert.equal(f.calls[0].command.data.status, 'DRAFT');
  assert.equal(f.calls[0].command.data.content.theme, 'peach');
  await f.click('back');
  await f.click('new');
  assert.deepEqual(copy(f.editor.draft.audience.segments), ['new_member']);
  assert.equal(f.editor.draft.frequency.max_per_user, 1);
});
test('validation blocks empty targeting, mismatched limits and invalid auto-close before publishing', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  await f.load();
  await f.click('edit', 'new-member');
  await f.segment('new_member', false);
  await f.click('publish');
  assert.match(f.editor.render(), /至少选择/);
  assert.equal(f.calls.length, 0);
  await f.segment('new_member', true);
  await f.field('audience.min_growth', 20);
  await f.field('audience.max_growth', 10);
  await f.click('publish');
  assert.match(f.editor.render(), /成长值上限不能低于下限/);
  assert.equal(f.calls.length, 0);
  await f.field('audience.max_growth', 0);
  await f.field('content.auto_close_seconds', 3);
  await f.click('publish');
  assert.match(f.editor.render(), /自动关闭请填写/);
  assert.equal(f.calls.length, 0);
});
test('server failure keeps editable draft and optimistic version, canonical success supports pause', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  await f.load();
  await f.click('edit', 'new-member');
  await f.field('content.title', '保留这份修改');
  f.setSave(() => {
    throw Error('<script>alert(1)</script> 配置已经更新');
  });
  await f.click('publish');
  assert.equal(f.editor.dirty, true);
  assert.equal(f.editor.draft.version, 3);
  assert.equal(f.editor.draft.content.title, '保留这份修改');
  assert.match(f.editor.render(), /&lt;script&gt;/);
  assert.doesNotMatch(f.editor.render(), /<script>/);
  f.setSave(null);
  await f.click('pause');
  assert.equal(f.calls.at(-1).command.data.status, 'PAUSED');
  assert.equal(f.editor.draft.status, 'PAUSED');
  assert.deepEqual(f.messages, ['弹窗已保存并暂停']);
});
test('global disabled and frequency settings round-trip independently of campaigns', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  await f.load();
  await f.field('settings.enabled', false);
  await f.field('settings.max_per_session', 2);
  await f.field('settings.min_gap_seconds', 600);
  await f.click('settings');
  assert.deepEqual(f.calls, [
    {
      service: 'content',
      action: 'popup_configure',
      command: { data: { enabled: false, max_per_session: 2, min_gap_seconds: 600 } },
    },
  ]);
  assert.deepEqual(f.messages, ['全局弹窗设置已保存']);
});
test('unauthorized and stale session requests cannot repopulate private configuration or publish', async (t) => {
  const denied = setup({ admin: false });
  t.after(() => denied.dispose());
  assert.equal(await denied.editor.refresh(), false);
  await denied.click('new');
  await denied.click('publish');
  assert.deepEqual(denied.calls, []);
  assert.match(denied.editor.render(), /管理员账号/);
  const f = setup();
  t.after(() => f.dispose());
  await f.load();
  await f.click('edit', 'new-member');
  await f.field('content.title', '旧会话');
  const pending = deferred();
  f.setSave(() => pending.promise);
  const saving = f.click('publish');
  f.changeSession('anonymous');
  pending.resolve(campaign());
  await saving;
  assert.equal(f.editor.draft, null);
  assert.deepEqual(f.messages, []);
  assert.doesNotMatch(f.editor.render(), /新会员见面礼/);
});
test('late refresh after logout cannot restore campaign user lists', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  const pending = deferred();
  f.setRead(() => pending.promise);
  const loading = f.editor.refresh();
  f.changeSession('next-user');
  pending.resolve({ items: [campaign()], settings: { enabled: true, max_per_session: 1, min_gap_seconds: 60 } });
  assert.equal(await loading, false);
  assert.equal(f.editor.draft, null);
  assert.doesNotMatch(f.editor.render(), /new-member/);
});
test('preview uses authoritative coupon amounts and escapes every authored field', async (t) => {
  const f = setup();
  t.after(() => f.dispose());
  await f.load();
  await f.click('edit', 'new-member');
  assert.match(f.editor.render(), /满 1,000 元可用/);
  assert.match(f.editor.render(), /不发放优惠券/);
  const attack = '\"><img src=x onerror=alert(1)> &';
  for (const key of ['name', 'content.title', 'content.subtitle', 'content.body', 'content.button_text'])
    await f.field(key, attack);
  const html = f.editor.render();
  assert.doesNotMatch(html, /<img\b/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt; &amp;/);
  assert.deepEqual(f.calls, []);
});
