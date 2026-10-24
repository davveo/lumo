'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const clone = (value) => JSON.parse(JSON.stringify(value));
const tick = () => new Promise((resolve) => setImmediate(resolve));
const escape = (value) =>
  String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');

// These tests exercise module contracts and asynchronous event handlers. Real
// browser verification covers geometry, paint order and mobile layout.
function setup(handler = async () => ({ items: [] }), initial = {}) {
  const docHandlers = new Map(),
    winHandlers = new Map(),
    calls = [],
    timers = new Map(),
    elements = new Map();
  let user = initial.user === undefined ? 'buyer' : initial.user,
    token = 'session-a',
    base = 'http://shop.test',
    timerID = 0;
  const classList = () => {
    const values = new Set();
    return {
      add: (...names) => names.forEach((name) => values.add(name)),
      remove: (...names) => names.forEach((name) => values.delete(name)),
      contains: (name) => values.has(name),
      toggle(name, force) {
        const on = force === undefined ? !values.has(name) : force;
        on ? values.add(name) : values.delete(name);
        return on;
      },
    };
  };
  class Element {
    constructor(id = '') {
      this.id = id;
      this.parentElement = null;
      this.children = [];
      this.dataset = {};
      this.attributes = new Map();
      this.classList = classList();
      this.value = '';
      this.hidden = false;
      this.disabled = false;
      this.isConnected = true;
      this.clientLeft = 1;
      this.clientTop = 1;
      this.style = { setProperty() {}, removeProperty() {} };
      this._html = '';
      this.options = [];
    }
    get innerHTML() {
      return this._html;
    }
    set innerHTML(html) {
      this._html = String(html);
      this.options = [];
      for (const match of this._html.matchAll(/<button\b([^>]*)>([\s\S]*?)<\/button>/g)) {
        const button = new Element();
        button.parentElement = this;
        button.textContent = match[2].replace(/<[^>]*>/g, '');
        for (const attr of match[1].matchAll(/([\w-]+)="([^"]*)"/g)) {
          button.setAttribute(attr[1], attr[2]);
          if (attr[1].startsWith('data-'))
            button.dataset[attr[1].slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = attr[2];
          if (attr[1] === 'class') button.className = attr[2];
        }
        button.disabled = /\bdisabled\b/.test(match[1]);
        this.options.push(button);
      }
    }
    setAttribute(name, value) {
      this.attributes.set(name, String(value));
      if (name === 'id') this.id = String(value);
    }
    getAttribute(name) {
      return this.attributes.get(name) ?? null;
    }
    removeAttribute(name) {
      this.attributes.delete(name);
    }
    append(child) {
      child.parentElement = this;
      this.children.push(child);
      if (child.id) elements.set(child.id, child);
    }
    getBoundingClientRect() {
      return { top: 40, left: 300, right: 840, bottom: 84, width: 540, height: 44 };
    }
    matches(selector) {
      if (selector.startsWith('#')) return this.id === selector.slice(1);
      if (selector.startsWith('.')) return (this.className || '').split(' ').includes(selector.slice(1));
      if (selector === '[data-discovery-action]') return this.dataset.discoveryAction !== undefined;
      if (selector === '[role="option"]') return this.getAttribute('role') === 'option';
      return false;
    }
    closest(selector) {
      for (let current = this; current; current = current.parentElement)
        if (selector.split(',').some((value) => current.matches(value.trim()))) return current;
      return null;
    }
    querySelector(selector) {
      return (
        this.children.find((child) => child.matches(selector)) ||
        this.options.find((option) => option.matches(selector)) ||
        null
      );
    }
    querySelectorAll(selector) {
      return this.options.filter((option) => option.matches(selector));
    }
    focus() {
      document.activeElement = this;
    }
    scrollIntoView() {
      this.scrolled = true;
    }
    click() {
      return fire(docHandlers, 'click', this);
    }
  }
  const form = new Element('search-form'),
    input = new Element('search-input');
  form.append(input);
  elements.set(form.id, form);
  const add = (collection, name, fn) => {
    if (!collection.has(name)) collection.set(name, []);
    collection.get(name).push(fn);
  };
  const document = {
    readyState: 'complete',
    activeElement: null,
    body: new Element('body'),
    documentElement: { clientWidth: 1280 },
    addEventListener: (name, fn) => add(docHandlers, name, fn),
    querySelector: (selector) => (selector.startsWith('#') ? elements.get(selector.slice(1)) || null : null),
    getElementById: (id) => elements.get(id) || null,
    createElement: () => new Element(),
  };
  const products = Array.from({ length: 18 }, (_, index) => ({
    id: 'item-' + index,
    name: '商城好物 ' + index,
    category: index % 2 ? '食品生鲜' : '手机数码',
    brand: 'LUMO',
    price: index + 10,
    image: 'product.jpg',
    available: 10,
    visible: true,
  }));
  const context = {
    document,
    products,
    state: {},
    ui: { query: '', category: '全部好物', sort: '综合' },
    location: { hash: '#/', origin: base, pathname: '/' },
    console,
    Map,
    Set,
    Date,
    crypto: { randomUUID: () => 'search-' + calls.length },
    esc: escape,
    asset: String,
    money: String,
    icon: () => '',
    price: String,
    header: () => '',
    footer: () => '',
    save() {},
    toast() {},
    go() {},
    renderKeepScroll() {},
    LumoConnection: { ready: false },
    LumoAuth: { user: () => (user ? { id: user } : null), isLoggedIn: () => !!user, requireLogin: () => !!user },
    LumoAPI: {
      getToken: () => token,
      getBase: () => base,
      async call(service, action, command = {}, options = {}) {
        calls.push({ service, action, command: clone(command), options: clone(options) });
        return handler(service, action, command, options);
      },
    },
    addEventListener: (name, fn) => add(winHandlers, name, fn),
    innerHeight: 900,
    setTimeout(fn) {
      const id = ++timerID;
      timers.set(id, fn);
      return id;
    },
    clearTimeout(id) {
      timers.delete(id);
    },
  };
  context.window = context;
  const ctx = vm.createContext(context);
  require('./load-constants.cjs')(ctx, directory);
  vm.runInContext(fs.readFileSync(path.join(directory, 'discovery.js'), 'utf8'), ctx);
  async function fire(collection, name, target, extra = {}) {
    const event = {
      target,
      relatedTarget: null,
      defaultPrevented: false,
      preventDefault() {
        this.defaultPrevented = true;
      },
      ...extra,
    };
    for (const fn of collection.get(name) || []) await fn(event);
    return event;
  }
  ctx.LumoDiscovery.afterRender();
  return {
    ctx,
    input,
    form,
    document,
    calls,
    products,
    panel: () => elements.get('disc-search-panel'),
    setContext(next) {
      if ('user' in next) user = next.user;
      if ('token' in next) token = next.token;
      if ('base' in next) base = next.base;
    },
    async focus() {
      document.activeElement = input;
      return fire(docHandlers, 'focusin', input);
    },
    async type(value) {
      input.value = value;
      document.activeElement = input;
      return fire(docHandlers, 'input', input);
    },
    async click(action, value) {
      const button = new Element();
      button.parentElement = elements.get('disc-search-panel');
      button.dataset = { discoveryAction: action, value };
      return fire(docHandlers, 'click', button);
    },
    async event(name, extra = {}) {
      return fire(docHandlers, name, input, extra);
    },
    async outside() {
      return fire(docHandlers, 'pointerdown', new Element());
    },
    async windowEvent(name) {
      return fire(winHandlers, name, document);
    },
    async flushTimers() {
      const work = [...timers.values()];
      timers.clear();
      await Promise.all(work.map((fn) => fn()));
      await tick();
    },
    refresh: () => ctx.LumoDiscovery.refresh(),
  };
}

function words(fixture, className) {
  return fixture
    .panel()
    .options.filter((option) => (option.className || '').split(' ').includes(className))
    .map((option) => option.dataset.value);
}

test('empty focus opens actual catalog discovery without any HTTP or invented history', async () => {
  const f = setup(undefined, { user: null });
  await f.focus();
  assert.equal(f.panel().hidden, false);
  assert.equal(f.panel().getAttribute('role'), 'dialog');
  assert.equal(f.input.getAttribute('aria-expanded'), 'true');
  assert.match(f.panel().innerHTML, /搜索历史/);
  assert.match(f.panel().innerHTML, /暂无搜索历史/);
  assert.match(f.panel().innerHTML, /搜索发现/);
  const discover = words(f, 'disc-search-discover-item');
  assert.ok(discover.length > 0);
  assert.ok(discover.every((value) => f.products.some((product) => product.name === value)));
  await f.flushTimers();
  assert.equal(f.calls.length, 0);
});

test('personal search history is escaped and discovery rotation stays local', async () => {
  const f = setup(async (service, action, command) => ({
    items: service === 'engagement' && command.data.kind === 'search' ? [{ query: '<b>我的历史</b>' }] : [],
  }));
  await f.refresh();
  await f.focus();
  assert.match(f.panel().innerHTML, /&lt;b&gt;我的历史&lt;\/b&gt;/);
  assert.doesNotMatch(f.panel().innerHTML, /<b>我的历史<\/b>/);
  const before = words(f, 'disc-search-discover-item'),
    count = f.calls.length;
  await f.click('refresh-search-discover');
  assert.notDeepEqual(words(f, 'disc-search-discover-item'), before);
  assert.equal(f.calls.length, count);
});

test('history and discovery selections use the real search pipeline and close the panel', async () => {
  const f = setup(async (service) => ({ items: service === 'search' ? [{ id: 'item-2' }] : [] }));
  await f.focus();
  await f.click('search', '商城好物 2');
  await tick();
  assert.equal(f.ctx.ui.query, '商城好物 2');
  assert.equal(f.panel().hidden, true);
  const search = f.calls.find((call) => call.service === 'search');
  assert.equal(search.command.data.query, '商城好物 2');
  assert.equal(search.options.auth, false);
  assert.ok(
    f.calls.some(
      (call) => call.service === 'engagement' && call.action === 'put' && call.command.data.query === '商城好物 2',
    ),
  );
});

test('clearing history preserves typed text and only clears the current user search kind', async () => {
  const f = setup(async (service, action, command) => ({
    items: service === 'engagement' && action === 'list' && command.data.kind === 'search' ? [{ query: '旧搜索' }] : [],
  }));
  await f.refresh();
  await f.type('正在输入');
  await f.click('clear-search');
  assert.equal(f.input.value, '正在输入');
  assert.doesNotMatch(f.panel().innerHTML, />旧搜索</);
  const clear = f.calls.find((call) => call.action === 'clear');
  assert.deepEqual(clear.command, { data: { kind: 'search' } });
});

test('late history clear cannot erase a new session of the same user', async () => {
  let release,
    fresh = false;
  const f = setup(async (service, action, command) => {
    if (action === 'clear')
      return new Promise((resolve) => {
        release = resolve;
      });
    return {
      items:
        service === 'engagement' && command.data.kind === 'search'
          ? [{ query: fresh ? '新会话历史' : '旧会话历史' }]
          : [],
    };
  });
  await f.refresh();
  await f.focus();
  const clearing = f.click('clear-search');
  await tick();
  fresh = true;
  f.setContext({ token: 'session-b' });
  await f.refresh();
  await f.focus();
  release({ cleared: true });
  await clearing;
  assert.match(f.panel().innerHTML, /新会话历史/);
  assert.doesNotMatch(f.panel().innerHTML, /旧会话历史/);
});

test('unmatched suggestions keep useful sections without fabricated result rows', async () => {
  const f = setup(async () => ({ items: [] }), { user: null });
  await f.type('nothing matches');
  await f.flushTimers();
  assert.equal(f.panel().hidden, false);
  assert.match(f.panel().innerHTML, /搜索发现/);
  assert.equal(f.panel().querySelectorAll('[role="option"]').length, 0);
  assert.doesNotMatch(f.panel().innerHTML, /查看全部搜索结果|搜索“nothing matches”/);
});

for (const change of [{ user: 'other' }, { token: 'session-b' }, { base: 'http://other.test' }]) {
  test('late suggestions are discarded after scope changes ' + JSON.stringify(change), async () => {
    let release;
    const f = setup(async (service) =>
      service === 'search'
        ? new Promise((resolve) => {
            release = resolve;
          })
        : { items: [] },
    );
    await f.type('商城');
    const pending = f.flushTimers();
    await tick();
    // Do not dispatch focus/input here: those cancel the suggestion revision
    // independently, which would mask an absent account/session/base fence.
    f.setContext(change);
    release({ items: [{ id: 'item-0' }] });
    await pending;
    assert.equal(f.panel().querySelectorAll('[role="option"]').length, 0);
  });
}

test('late private history refresh cannot populate a new session', async () => {
  let release;
  const f = setup(async (service, action, command) =>
    command.data.kind === 'search'
      ? new Promise((resolve) => {
          release = resolve;
        })
      : { items: [] },
  );
  const refreshing = f.refresh();
  await tick();
  f.setContext({ token: 'session-b' });
  release({ items: [{ query: '旧账号私有搜索' }] });
  await refreshing;
  await f.focus();
  assert.doesNotMatch(f.panel().innerHTML, /旧账号私有搜索/);
  assert.match(f.panel().innerHTML, /暂无搜索历史/);
});

test('keyboard selects only actual matched products and Escape/outside/route/scroll close', async () => {
  const f = setup(async (service) => ({ items: service === 'search' ? [{ id: 'item-0' }] : [] }), { user: null });
  await f.type('商城');
  await f.flushTimers();
  assert.equal(f.panel().querySelectorAll('[role="option"]').length, 1);
  const down = await f.event('keydown', { key: 'ArrowDown' });
  assert.equal(down.defaultPrevented, true);
  const option = f.panel().querySelectorAll('[role="option"]')[0];
  assert.equal(option.getAttribute('aria-selected'), 'true');
  await f.event('keydown', { key: 'Enter' });
  await tick();
  assert.equal(f.ctx.ui.query, '商城好物 0');
  assert.equal(f.panel().hidden, true);
  await f.focus();
  await f.event('keydown', { key: 'Escape' });
  assert.equal(f.panel().hidden, true);
  await f.focus();
  await f.outside();
  assert.equal(f.panel().hidden, true);
  await f.focus();
  await f.windowEvent('hashchange');
  assert.equal(f.panel().hidden, true);
  await f.focus();
  await f.windowEvent('scroll');
  assert.equal(f.panel().hidden, true);
});
