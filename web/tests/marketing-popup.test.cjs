'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(
  path.join(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'marketing-popup.js'),
  'utf8',
);
const client = fs.readFileSync(
  path.join(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'api-client.js'),
  'utf8',
);
const flush = async () => {
  for (let index = 0; index < 8; index++) await Promise.resolve();
};
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
function popup(overrides = {}) {
  return {
    delivery_id: 'delivery-one',
    campaign_id: 'welcome',
    expires_at: new Date(Date.now() + 240000).toISOString(),
    content: {
      title: '欢迎领取专属优惠券',
      subtitle: '把喜欢的好物带回家',
      body: '为你准备的生活好礼',
      theme: 'red',
      action_type: 'claim_coupon',
      auto_close_seconds: 0,
    },
    coupon: {
      id: 'everyday-10',
      name: '日常十元券',
      amount_cents: 1000,
      min_cents: 10000,
      valid_days: 7,
      expires_at: new Date(Date.now() + 86400000).toISOString(),
    },
    ...overrides,
  };
}
function setup({ visibility = 'visible', member = false, handler } = {}) {
  let now = Date.now(),
    seq = 0,
    token = member ? 'member-token' : '',
    base = '',
    user = member ? { id: 'buyer' } : null;
  const timers = new Map(),
    calls = [],
    windowListeners = new Map(),
    documentListeners = new Map(),
    ids = new Map(),
    logins = [],
    refreshed = [];
  const add = (map, name, callback, capture) => {
    if (!map.has(name)) map.set(name, []);
    map.get(name).push({ callback, capture: !!capture });
  };
  const clock = {
    setTimeout(callback, delay = 0) {
      const id = ++seq;
      timers.set(id, { callback, at: now + delay });
      return id;
    },
    setInterval(callback, delay) {
      const id = ++seq;
      timers.set(id, { callback, at: now + delay, interval: delay });
      return id;
    },
    clear(id) {
      timers.delete(id);
    },
    advance(ms) {
      const end = now + ms;
      let guard = 0;
      while (true) {
        const next = [...timers.entries()].sort((a, b) => a[1].at - b[1].at).find(([, task]) => task.at <= end);
        if (!next) break;
        if (++guard > 1000) throw Error('timer loop');
        const [id, task] = next;
        now = task.at;
        timers.delete(id);
        if (task.interval) timers.set(id, { ...task, at: now + task.interval });
        task.callback();
      }
      now = end;
    },
    get pending() {
      return timers.size;
    },
  };
  const document = {
    visibilityState: visibility,
    activeElement: null,
    addEventListener: (name, fn, capture) => add(documentListeners, name, fn, capture),
    getElementById: (id) => ids.get(id) || null,
    querySelectorAll: () => [],
    createElement: (tag) => new Element(tag),
  };
  class Element {
    constructor(name) {
      this.name = name;
      this.id = '';
      this.dataset = {};
      this.parent = null;
      this.children = [];
      this.style = {};
      this.isConnected = true;
      this.disabled = false;
      this.html = '';
    }
    set innerHTML(value) {
      this.html = value;
      if (this.id !== 'lumo-marketing-popup') return;
      this.primary = new Element('primary');
      this.primary.parent = this;
      this.primary.dataset.popupAction = 'primary';
      this.primary.disabled = /data-popup-action="primary" disabled/.test(value);
      this.close = new Element('close');
      this.close.parent = this;
      this.close.dataset.popupAction = 'close';
      this.countdown = new Element('countdown');
      this.countdown.parent = this;
    }
    get innerHTML() {
      return this.html;
    }
    appendChild(child) {
      child.parent = this;
      this.children.push(child);
      if (child.id) ids.set(child.id, child);
      return child;
    }
    contains(element) {
      return !!element && (element === this || element.parent === this);
    }
    closest(selector) {
      return selector.includes('[data-popup-action]') && this.dataset.popupAction ? this : null;
    }
    querySelector(selector) {
      if (selector.includes('primary')) return this.primary;
      if (selector.includes('countdown')) return this.countdown;
      return null;
    }
    querySelectorAll() {
      return [this.primary, this.close].filter((item) => item && !item.disabled);
    }
    remove() {
      ids.delete(this.id);
      this.isConnected = false;
      this.parent = null;
    }
    focus() {
      document.activeElement = this;
    }
  }
  document.body = new Element('body');
  document.body.style.overflow = '';
  const modal = new Element('modal-root');
  ids.set('modal-root', modal);
  const original = new Element('original');
  original.focus();
  const location = { origin: 'http://shop.test', hash: '#/' };
  const context = {
    document,
    location,
    AbortController,
    URL,
    Uint8Array,
    console,
    Date: class extends Date {
      static now() {
        return now;
      }
    },
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clear,
    setInterval: clock.setInterval,
    clearInterval: clock.clear,
    LumoAPI: {
      getBase: () => base,
      getToken: () => token,
      key: (prefix) => prefix + '-' + ++seq,
      async call(service, action, command, options = {}) {
        const call = { service, action, command, options };
        calls.push(call);
        return handler
          ? handler(call)
          : action === 'popup_event'
            ? { allowed: true }
            : action === 'popup'
              ? { popup: null }
              : {
                  coupon: {
                    id: 'issued',
                    amount_cents: 1000,
                    min_cents: 10000,
                    expires_at: new Date(now + 604800000).toISOString(),
                  },
                };
      },
    },
    LumoAuth: {
      isLoggedIn: () => !!user,
      user: () => user,
      requireLogin: (hash) => {
        logins.push(hash);
        location.hash = '#/login';
        return false;
      },
    },
    LumoMember: {
      async refresh() {
        refreshed.push('wallet');
      },
    },
    LumoCouponCenter: {
      async refresh() {
        refreshed.push('coupons');
      },
    },
    LumoConnection: { refreshing: false },
    addEventListener: (name, fn) => add(windowListeners, name, fn),
    dispatchEvent(event) {
      for (const { callback } of windowListeners.get(event.type) || []) callback(event);
    },
  };
  context.window = context;
  require('./load-constants.cjs')(context);
  vm.runInNewContext(source, context, { filename: 'marketing-popup.js' });
  const dispatchDoc = (name, target, extra = {}) => {
    const event = {
      target,
      preventDefault() {
        this.prevented = true;
      },
      stopImmediatePropagation() {
        this.stopped = true;
      },
      ...extra,
    };
    for (const { callback } of [...(documentListeners.get(name) || [])].sort(
      (a, b) => Number(b.capture) - Number(a.capture),
    )) {
      callback(event);
      if (event.stopped) break;
    }
    return event;
  };
  const emit = (type, detail = {}) => context.dispatchEvent({ type, detail });
  return {
    api: context.LumoMarketing,
    context,
    document,
    location,
    clock,
    calls,
    modal,
    logins,
    refreshed,
    original,
    get root() {
      return ids.get('lumo-marketing-popup');
    },
    async bootstrap(value) {
      const ticket = context.LumoMarketing.beginBootstrap();
      context.LumoMarketing.hydrate(value, ticket);
      emit('lumo:ready', { ready: true });
      await flush();
      return ticket;
    },
    async click(action) {
      const target = action === 'close' ? this.root?.close : this.root?.primary;
      if (target) dispatchDoc('click', target);
      await flush();
    },
    key(key, shiftKey = false) {
      return dispatchDoc('keydown', document.activeElement, { key, shiftKey });
    },
    async visibility(value) {
      document.visibilityState = value;
      dispatchDoc('visibilitychange', document.body);
      await flush();
    },
    async navigate(hash) {
      location.hash = hash;
      emit('hashchange');
      clock.advance(0);
      await flush();
    },
    changeScope(value = 'other-token') {
      token = value;
      user = value ? { id: value } : null;
      emit('lumo:api-scope-changed');
    },
    setBase(value) {
      base = value;
      emit('lumo:api-scope-changed');
    },
    emit,
    dispose() {
      context.LumoMarketing.reset();
    },
  };
}

test('eligible bootstrap confirms one impression before rendering, opening never issues a coupon', async () => {
  const gate = deferred();
  const app = setup({ handler: (call) => (call.action === 'popup_event' ? gate.promise : {}) });
  await app.bootstrap(popup());
  assert.equal(app.root, undefined);
  assert.equal(app.calls.length, 1);
  assert.equal(app.calls[0].command.data.event, 'impression');
  gate.resolve({ allowed: true });
  await flush();
  assert.equal(app.api.model().visible, true);
  assert.match(app.root.innerHTML, /满 ¥100 可用/);
  assert.match(app.root.innerHTML, /登录领取/);
  assert.equal(app.document.body.style.overflow, 'hidden');
  assert.equal(
    app.calls.some((call) => call.action === 'popup_claim'),
    false,
  );
  app.clock.advance(0);
  await flush();
  assert.equal(app.calls.length, 1, 'ready does not fetch a second decision');
  app.dispose();
});

test('unmatched, denied and expired candidates never render or claim', async () => {
  for (const value of [null, popup({ expires_at: '2000-01-01T00:00:00Z' }), popup()]) {
    const app = setup({ handler: async () => ({ allowed: false }) });
    await app.bootstrap(value);
    assert.equal(app.api.model().visible, false);
    assert.equal(app.root, undefined);
    assert.equal(app.calls.length, value?.expires_at && Date.parse(value.expires_at) > Date.now() ? 1 : 0);
    app.dispose();
  }
});

test('background tab and existing form wait without consuming an impression', async () => {
  const app = setup({ visibility: 'hidden' });
  app.modal.innerHTML = '<section role="dialog">收货地址</section>';
  await app.bootstrap(popup());
  app.clock.advance(2000);
  await flush();
  assert.equal(app.calls.length, 0);
  await app.visibility('visible');
  assert.equal(app.calls.length, 0);
  app.modal.innerHTML = '';
  app.clock.advance(300);
  await flush();
  assert.equal(app.calls.length, 1);
  assert.equal(app.api.model().visible, true);
  app.dispose();
});

test('configured delay and countdown start with the actual display, closing restores focus and scroll', async () => {
  const app = setup({ visibility: 'hidden' });
  const item = popup();
  item.content.delay_seconds = 2;
  item.content.auto_close_seconds = 5;
  await app.bootstrap(item);
  app.clock.advance(10000);
  await flush();
  assert.equal(app.calls.length, 0);
  await app.visibility('visible');
  app.clock.advance(1999);
  await flush();
  assert.equal(app.calls.length, 0);
  app.clock.advance(1);
  await flush();
  assert.equal(app.api.model().visible, true);
  assert.equal(app.root.countdown.textContent, '5 秒后自动关闭');
  app.clock.advance(4999);
  await flush();
  assert.equal(app.api.model().visible, true);
  app.clock.advance(1);
  await flush();
  assert.equal(app.api.model().visible, false);
  assert.equal(app.document.body.style.overflow, '');
  assert.equal(app.document.activeElement, app.original);
  assert.equal(app.calls.filter((call) => call.command.data.event === 'auto_close').length, 1);
  app.clock.advance(10000);
  await flush();
  assert.equal(app.calls.filter((call) => call.action === 'popup').length, 0);
  app.dispose();
});

test('guest CTA goes through login and never calls the claim endpoint', async () => {
  const app = setup();
  await app.bootstrap(popup());
  await app.click('primary');
  assert.deepEqual(app.logins, ['#/coupons']);
  assert.equal(app.location.hash, '#/login');
  assert.equal(app.api.model().visible, false);
  assert.equal(
    app.calls.some((call) => call.action === 'popup_claim'),
    false,
  );
  app.dispose();
});

test('member claims only the bound delivery, refreshes wallet, and uses the issued coupon without a second claim', async () => {
  const app = setup({ member: true });
  await app.bootstrap(popup());
  await app.click('primary');
  const claims = app.calls.filter((call) => call.action === 'popup_claim');
  assert.equal(claims.length, 1);
  assert.deepEqual(Object.keys(claims[0].command.data).sort(), ['delivery_id', 'view_id']);
  assert.equal(claims[0].command.data.delivery_id, 'delivery-one');
  assert.equal(app.api.model().claimed, true);
  assert.match(app.root.innerHTML, /立即使用/);
  assert.match(app.root.innerHTML, /日常十元券/);
  assert.deepEqual(app.refreshed.sort(), ['coupons', 'wallet']);
  await app.click('primary');
  assert.equal(app.location.hash, '#/coupons');
  assert.equal(app.calls.filter((call) => call.action === 'popup_claim').length, 1);
  app.dispose();
});

test('claim failure remains visible and original delivery can be retried without fake success', async () => {
  let attempts = 0;
  const app = setup({
    member: true,
    handler: async (call) => {
      if (call.action === 'popup_claim') {
        if (++attempts === 1) throw Error('活动配额正在更新');
        return { coupon: { id: 'issued', expires_at: new Date(Date.now() + 86400000).toISOString() } };
      }
      return { allowed: true };
    },
  });
  await app.bootstrap(popup());
  await app.click('primary');
  assert.equal(app.api.model().claimed, false);
  assert.match(app.root.innerHTML, /活动配额正在更新/);
  assert.match(app.root.innerHTML, /立即领取/);
  await app.click('primary');
  assert.equal(app.api.model().claimed, true);
  assert.equal(attempts, 2);
  app.dispose();
});

test('account, gateway and route changes abort and discard late decisions or impressions', async () => {
  for (const change of [
    (app) => app.changeScope(),
    (app) => app.setBase('https://other.test'),
    (app) => app.navigate('#/checkout'),
  ]) {
    const gate = deferred();
    const app = setup({ handler: () => gate.promise });
    await app.bootstrap(popup());
    const signal = app.calls[0].options.signal;
    await change(app);
    assert.equal(signal.aborted, true);
    gate.resolve({ allowed: true });
    await flush();
    assert.equal(app.api.model().visible, false);
    assert.equal(app.root, undefined);
    app.dispose();
  }
});

test('late claim after logout cannot repaint another customer view', async () => {
  const gate = deferred();
  const app = setup({
    member: true,
    handler: (call) => (call.action === 'popup_claim' ? gate.promise : Promise.resolve({ allowed: true })),
  });
  await app.bootstrap(popup());
  await app.click('primary');
  assert.equal(app.api.model().busy, true);
  app.changeScope('');
  gate.resolve({ coupon: { id: 'old-private-coupon' } });
  await flush();
  assert.equal(app.root, undefined);
  assert.equal(app.api.model().claimed, false);
  assert.equal(app.refreshed.length, 0);
  app.dispose();
});

test('route entry makes one decision per entry, excluded checkout and login routes never request one', async () => {
  const app = setup();
  await app.bootstrap(null);
  app.clock.advance(0);
  await flush();
  assert.equal(app.calls.length, 0);
  await app.navigate('#/product/coffee');
  assert.equal(app.calls.length, 1);
  assert.equal(app.calls[0].command.data.placement, 'product');
  app.emit('lumo:ready', { ready: true });
  app.clock.advance(0);
  await flush();
  assert.equal(app.calls.length, 1);
  await app.navigate('#/checkout');
  await app.navigate('#/login');
  assert.equal(app.calls.length, 1);
  await app.navigate('#/coupons');
  assert.equal(app.calls.length, 2);
  app.dispose();
});

test('stale bootstrap route is discarded then current route is decided after readiness', async () => {
  const app = setup();
  const ticket = app.api.beginBootstrap();
  await app.navigate('#/product/coffee');
  assert.equal(app.api.hydrate(popup(), ticket), false);
  app.emit('lumo:ready', { ready: true });
  app.clock.advance(0);
  await flush();
  assert.equal(app.calls.length, 1);
  assert.equal(app.calls[0].action, 'popup');
  assert.equal(app.calls[0].command.data.placement, 'product');
  app.dispose();
});

test('Escape and Tab remain inside the popup without touching the application modal', async () => {
  const app = setup();
  await app.bootstrap(popup());
  const root = app.root;
  root.close.focus();
  const tab = app.key('Tab');
  assert.equal(tab.prevented, true);
  assert.equal(app.document.activeElement, root.primary);
  const reverse = app.key('Tab', true);
  assert.equal(reverse.prevented, true);
  assert.equal(app.document.activeElement, root.close);
  const escape = app.key('Escape');
  assert.equal(escape.stopped, true);
  assert.equal(app.api.model().visible, false);
  assert.equal(app.document.body.style.overflow, '');
  app.dispose();
});

test('content is escaped, hostile links fall back to the coupon center, and no countdown is forced', async () => {
  const app = setup();
  const item = popup({ coupon: null });
  item.content = {
    ...item.content,
    title: '<img src=x onerror=alert(1)>',
    action_type: 'link',
    action_target: 'javascript:alert(1)',
    button_text: '去看看',
  };
  await app.bootstrap(item);
  assert.match(app.root.innerHTML, /&lt;img/);
  assert.doesNotMatch(app.root.innerHTML, /<img src=x/);
  assert.equal(app.root.countdown.textContent, '');
  await app.click('primary');
  assert.equal(app.location.hash, '#/coupons');
  app.dispose();
});

test('configured links use the real shopping routes and reject unsupported category or account routes', async () => {
  for (const destination of [
    '#/',
    '#/coupons',
    '#/member/coupons',
    '#/promotions',
    '#/promotions/flash/tomorrow',
    '#/promotions/group',
    '#/promotions/exchange',
    '#/promotions/reduction',
    '#/promotions/discount',
    '#/promotions/quantity_discount',
    '#/promotions/direct',
    '#/product/demo.2026-01',
  ]) {
    const app = setup();
    const item = popup({ coupon: null });
    item.content = { ...item.content, action_type: 'link', action_target: destination };
    await app.bootstrap(item);
    await app.click('primary');
    assert.equal(app.location.hash, destination);
    app.dispose();
  }
  for (const destination of [
    '#/category/phones',
    '#/member',
    '#/payment',
    '#/promotions/mygroups',
    '#/product/%zz',
    '//attacker.invalid',
    '#/product/a\n',
  ]) {
    const app = setup();
    const item = popup({ coupon: null });
    item.content = { ...item.content, action_type: 'link', action_target: destination };
    await app.bootstrap(item);
    await app.click('primary');
    assert.equal(app.location.hash, '#/coupons');
    app.dispose();
  }
});

test('API transports only same-origin visitor cookies and can cancel stale popup calls', async () => {
  let received;
  const events = [];
  const context = {
    URL,
    AbortController,
    Uint8Array,
    setTimeout,
    clearTimeout,
    console,
    location: { origin: 'https://shop.test' },
    CustomEvent: class {
      constructor(type, options) {
        this.type = type;
        this.detail = options?.detail;
      }
    },
    dispatchEvent(event) {
      events.push(event);
    },
    async fetch(url, options) {
      received = { url, options };
      return {
        status: 200,
        ok: true,
        headers: {
          get() {
            return '';
          },
        },
        async json() {
          return { data: { popup: null } };
        },
      };
    },
  };
  context.window = context;
  require('./load-constants.cjs')(context);
  vm.runInNewContext(client, context);
  await context.LumoAPI.call('storefront', 'popup');
  assert.equal(received.options.credentials, 'same-origin');
  context.LumoAPI.setBase('https://gateway.other');
  await context.LumoAPI.call('storefront', 'popup');
  assert.equal(received.options.credentials, 'same-origin', 'cross-origin gateway must never receive browser cookies');
  assert.equal(received.url, 'https://gateway.other/api/v1/storefront/popup');
  context.fetch = (url, options) =>
    new Promise((resolve, reject) =>
      options.signal.addEventListener('abort', () => {
        const error = new Error('aborted');
        error.name = 'AbortError';
        reject(error);
      }),
    );
  const controller = new AbortController();
  const pending = context.LumoAPI.call('storefront', 'popup', {}, { signal: controller.signal });
  controller.abort();
  await assert.rejects(pending);
  const scopeCount = events.filter((event) => event.type === 'lumo:api-scope-changed').length;
  context.LumoAPI.setToken('new-token');
  assert.equal(events.filter((event) => event.type === 'lumo:api-scope-changed').length, scopeCount + 1);
  assert.equal(JSON.stringify(events).includes('new-token'), false);
});
