'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const escape = (value) =>
  String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
const tick = () => new Promise((resolve) => setImmediate(resolve));
const applicationCategories = () =>
  Array.from(
    vm.runInNewContext(
      fs.readFileSync(path.join(directory, 'app.js'), 'utf8').match(/\bconst\s+categories\s*=\s*(\[[\s\S]*?\])\s*;/)[1],
    ),
  );
const plain = (value) => JSON.parse(JSON.stringify(value));
const defaultConfiguration = () =>
  JSON.parse(
    fs.readFileSync(path.join(__dirname, '../../services/content/internal/usecase/category_navigation.json'), 'utf8'),
  );
const documentRecord = (config) => ({ id: 'storefront-categories', kind: 'storefront', body: JSON.stringify(config) });
const department = (overrides = {}) => ({
  id: 'new-department',
  category: '新分类',
  label: '新分类 / 新品类',
  title: '后台配置的新分类',
  icon: 'layers',
  rows: [
    { title: '细分类', query: '细分类查询', items: [{ label: '指定商品', query: '指定商品查询', featured: true }] },
  ],
  shortcuts: [{ label: '全部新品', query: '' }],
  brands: [{ label: '配置品牌', brand: 'ConfiguredBrand' }],
  ...overrides,
});
const configOf = (...groups) => ({ version: 1, groups });
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

// A small event DOM checks category interactions. Browser verification owns
// actual hover geometry, panel paint order and mobile widths.
function setup({ config = defaultConfiguration(), globals = {} } = {}) {
  const documentHandlers = new Map(),
    windowHandlers = new Map(),
    timers = new Map(),
    elements = new Map(),
    calls = [];
  let timerID = 0;
  const add = (collection, name, fn) => {
    if (!collection.has(name)) collection.set(name, []);
    collection.get(name).push(fn);
  };
  const classes = () => {
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
      this.classList = classes();
      this.hidden = false;
      this.disabled = false;
      this.style = { setProperty() {} };
      this._html = '';
    }
    get innerHTML() {
      return this._html;
    }
    set innerHTML(value) {
      this._html = String(value);
      this.children = [];
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
        this.children.push(button);
      }
    }
    setAttribute(name, value) {
      this.attributes.set(name, String(value));
      if (name === 'id') this.id = String(value);
    }
    getAttribute(name) {
      return this.attributes.get(name) ?? null;
    }
    hasAttribute(name) {
      return this.attributes.has(name);
    }
    removeAttribute(name) {
      this.attributes.delete(name);
    }
    matches(selector) {
      if (selector.startsWith('#')) return this.id === selector.slice(1);
      if (selector.startsWith('.')) return (this.className || '').split(' ').includes(selector.slice(1));
      if (selector === 'button')
        return (
          this.tagName === 'BUTTON' ||
          this.dataset.categoryGroup !== undefined ||
          this.dataset.categorySelect !== undefined ||
          this.dataset.categoryClose !== undefined
        );
      const attr = selector.match(/^\[data-([a-z-]+)(?:="([^"]*)")?\]$/);
      if (attr) {
        const key = attr[1].replace(/-([a-z])/g, (_, c) => c.toUpperCase());
        return this.dataset[key] !== undefined && (attr[2] === undefined || this.dataset[key] === attr[2]);
      }
      return false;
    }
    closest(selector) {
      for (let current = this; current; current = current.parentElement)
        if (selector.split(',').some((s) => current.matches(s.trim()))) return current;
      return null;
    }
    contains(other) {
      for (let current = other; current; current = current.parentElement) if (current === this) return true;
      return false;
    }
    querySelector(selector) {
      return this.querySelectorAll(selector)[0] || null;
    }
    querySelectorAll(selector) {
      return this.children.flatMap((child) => [
        ...(selector.split(',').some((s) => child.matches(s.trim())) ? [child] : []),
        ...child.querySelectorAll(selector),
      ]);
    }
    addEventListener(name, fn) {
      add(documentHandlers, name, fn);
    }
    focus() {
      document.activeElement = this;
    }
    scrollIntoView() {}
    getBoundingClientRect() {
      return { left: 40, right: 280, top: 200, bottom: 720, width: 240, height: 520 };
    }
  }
  const sidebar = new Element('category-directory'),
    panel = new Element('category-mega-panel');
  sidebar.className = 'category-directory';
  panel.className = 'category-mega-panel';
  panel.hidden = true;
  panel.parentElement = sidebar;
  sidebar.children.push(panel);
  elements.set(panel.id, panel);
  const document = {
    activeElement: null,
    body: new Element('body'),
    documentElement: { clientWidth: 1280 },
    addEventListener: (name, fn) => add(documentHandlers, name, fn),
    querySelector(selector) {
      if (selector === '.category-directory') return sidebar;
      return elements.get(selector.slice(1)) || null;
    },
    querySelectorAll(selector) {
      return sidebar.querySelectorAll(selector);
    },
    getElementById: (id) => elements.get(id) || null,
  };
  const context = {
    document,
    console,
    Map,
    Set,
    Date,
    location: { hash: '#/', origin: 'http://categories.test' },
    innerWidth: 1280,
    innerHeight: 900,
    matchMedia: () => ({ matches: false }),
    categories: applicationCategories(),
    products: [
      { id: 'fridge', name: '复古迷你冰箱', category: '家用电器', brand: 'RealBrand', visible: true },
      { id: 'hidden', name: '隐藏商品', category: '家用电器', brand: 'HiddenBrand', visible: false },
    ],
    ui: { category: '全部好物', query: '旧搜索', onlyFavorites: true, sort: '价格低', min: '50', max: '5000' },
    state: { discovery: { filters: { brand: 'all', inStock: false } } },
    esc: escape,
    icon: () => '',
    asset: String,
    money: String,
    save() {},
    toast() {},
    go(route) {
      calls.push({ kind: 'go', route });
    },
    renderKeepScroll() {
      calls.push({ kind: 'render' });
    },
    addEventListener: (name, fn) => add(windowHandlers, name, fn),
    setTimeout(fn) {
      const id = ++timerID;
      timers.set(id, fn);
      return id;
    },
    clearTimeout(id) {
      timers.delete(id);
    },
  };
  Object.assign(context, globals);
  context.window = context;
  const ctx = vm.createContext(context);
  require('./load-constants.cjs')(ctx, directory);
  vm.runInContext(fs.readFileSync(path.join(directory, 'skeleton.js'), 'utf8'), ctx);
  vm.runInContext(fs.readFileSync(path.join(directory, 'categories.js'), 'utf8'), ctx);
  if (config) ctx.LumoCategories.apply(config);
  const groups = ctx.LumoCategories.groups;
  for (const group of groups) {
    const button = new Element();
    button.tagName = 'BUTTON';
    button.className = 'category-directory-item';
    button.dataset.categoryGroup = group.id;
    button.setAttribute('aria-expanded', 'false');
    button.parentElement = sidebar;
    sidebar.children.unshift(button);
  }
  // Preserve catalogue order for directional-key checks.
  sidebar.children = sidebar.children
    .filter((item) => item !== panel)
    .reverse()
    .concat(panel);
  ctx.LumoCategories.afterRender();
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
  return {
    ctx,
    groups,
    panel,
    sidebar,
    calls,
    document,
    Element,
    groupButton: (index) => sidebar.children[index],
    event: (name, target, extra = {}) => fire(documentHandlers, name, target, extra),
    windowEvent: (name, extra = {}) => fire(windowHandlers, name, document, extra),
    async flush() {
      for (const fn of [...timers.values()]) fn();
      timers.clear();
      await tick();
    },
  };
}

test('unloaded navigation has no local business categories or hardcoded taxonomy fallback', () => {
  const f = setup({ config: null });
  assert.deepEqual(applicationCategories(), ['全部好物']);
  assert.deepEqual(plain(f.ctx.LumoCategories.configuration), { version: 1, groups: [] });
  assert.deepEqual(Array.from(f.ctx.LumoCategories.categories), ['全部好物']);
  assert.equal(f.ctx.LumoCategories.loaded, false);
  assert.match(f.ctx.LumoCategories.directory(), /正在加载商品分类/);
  assert.doesNotMatch(
    fs.readFileSync(path.join(directory, 'categories.js'), 'utf8'),
    /冰洗 \/ 空调|多门冰箱|智能手环|全部手机数码/,
  );
});

test('backend category configuration supplies broad departments and meaningful detail selections', () => {
  const f = setup(),
    canonical = Array.from(f.ctx.LumoCategories.categories);
  assert.ok(f.groups.length >= 20, 'directory should expose the expanded department range');
  assert.equal(new Set(f.groups.map((group) => group.id)).size, f.groups.length, 'department ids are unique');
  assert.equal(canonical[0], '全部好物');
  let leaves = 0;
  for (const group of f.groups) {
    assert.ok(group.label && group.title && canonical.includes(group.category));
    assert.ok(group.rows.length >= 3, 'each department has actual subcategory rows');
    for (const row of group.rows) {
      assert.ok(row.title);
      for (const item of row.items) {
        assert.equal(typeof item.label, 'string');
        assert.equal(typeof item.query, 'string');
        assert.ok(item.label && item.query);
        leaves++;
      }
    }
  }
  assert.ok(leaves >= 200, 'expanded departments should provide detailed leaf choices');
  assert.ok(canonical.includes('家用电器') && canonical.includes('母婴玩具') && canonical.includes('宠物生活'));
  const html = f.ctx.LumoCategories.directory();
  assert.equal((html.match(/data-category-group=/g) || []).length, f.groups.length);
});

test('refresh reads one public fixed content document and renders configured labels, queries and brands', async () => {
  const calls = [],
    config = configOf(department());
  const f = setup({
    config: null,
    globals: {
      LumoAPI: {
        getBase: () => '',
        getToken: () => '',
        async call(...args) {
          calls.push(args);
          return documentRecord(config);
        },
      },
    },
  });
  await f.ctx.LumoCategories.refresh();
  assert.deepEqual(plain(calls), [['content', 'get', { id: 'storefront-categories' }, { auth: false }]]);
  assert.equal(f.ctx.LumoCategories.loaded, true);
  assert.equal(f.ctx.LumoCategories.error, null);
  assert.deepEqual(Array.from(f.ctx.LumoCategories.categories), ['全部好物', '新分类']);
  assert.match(f.ctx.LumoCategories.directory(), /新分类 \/ 新品类/);
  const button = new f.Element();
  button.dataset.categoryGroup = 'new-department';
  button.parentElement = f.sidebar;
  await f.event('mouseover', button);
  assert.match(f.panel.innerHTML, /后台配置的新分类/);
  assert.match(f.panel.innerHTML, /指定商品查询/);
  assert.match(f.panel.innerHTML, /category-leaf featured/);
  assert.match(f.panel.innerHTML, /ConfiguredBrand/);
  assert.equal(calls.length, 1, 'opening configured navigation never fetches another document');
});

test('apply keeps the canonical array reference, hides disabled groups and clears removed category state', () => {
  const f = setup();
  const reference = f.ctx.categories;
  f.ctx.ui.category = '家用电器';
  f.ctx.ui.query = '旧查询';
  f.ctx.ui.categoryLabel = '旧细类';
  f.ctx.state.discovery.filters = { brand: 'RealBrand', inStock: true };
  const disabled = department({ id: 'disabled-category', category: '家用电器', enabled: false });
  assert.equal(f.ctx.LumoCategories.apply(configOf(disabled, department())), true);
  assert.equal(f.ctx.categories, reference);
  assert.deepEqual(Array.from(reference), ['全部好物', '新分类']);
  assert.deepEqual(
    Array.from(f.ctx.LumoCategories.configuration.groups, (group) => group.enabled),
    [false, true],
  );
  assert.deepEqual(
    Array.from(f.ctx.LumoCategories.groups, (group) => group.id),
    ['new-department'],
  );
  assert.equal(f.ctx.ui.category, '全部好物');
  assert.equal(f.ctx.ui.query, '');
  assert.equal(f.ctx.ui.categoryLabel, '');
  assert.deepEqual(plain(f.ctx.state.discovery.filters), { brand: 'all', inStock: false });
  assert.doesNotMatch(f.ctx.LumoCategories.directory(), /data-category-group="disabled-category"/);
});

test('configuration snapshots cannot mutate navigation and empty configuration is a supported backend value', () => {
  const config = configOf(
    department({
      rows: [{ title: '默认查询', items: [{ label: '默认商品' }] }],
      shortcuts: undefined,
      brands: undefined,
    }),
  );
  const f = setup({ config });
  config.groups[0].title = '外部变更';
  const snapshot = f.ctx.LumoCategories.configuration;
  snapshot.groups[0].category = '外部变更';
  snapshot.groups[0].rows[0].items[0].label = '外部变更';
  assert.equal(f.ctx.LumoCategories.groups[0].title, '后台配置的新分类');
  assert.equal(f.ctx.LumoCategories.groups[0].category, '新分类');
  assert.equal(f.ctx.LumoCategories.groups[0].rows[0].query, '默认查询');
  assert.equal(f.ctx.LumoCategories.groups[0].rows[0].items[0].query, '默认商品');
  assert.equal(f.ctx.LumoCategories.groups[0].rows[0].items[0].featured, false);
  f.ctx.LumoCategories.apply({ version: 1, groups: [] });
  assert.equal(f.ctx.LumoCategories.loaded, true);
  assert.equal(f.ctx.LumoCategories.groups.length, 0);
  assert.match(f.ctx.LumoCategories.directory(), /暂无商品分类/);
  assert.doesNotMatch(f.ctx.LumoCategories.directory(), /正在加载/);
});

test('invalid backend documents are rejected without replacing the current configuration', () => {
  const f = setup({ config: configOf(department()) });
  const before = plain(f.ctx.LumoCategories.configuration);
  const invalid = [
    { version: 2, groups: [] },
    configOf(department({ id: 'x" onclick="alert(1)' })),
    configOf(department({ icon: 'untrusted' })),
    configOf(department({ enabled: null })),
    configOf(department({ category: '全部好物' })),
    configOf(department({ category: '重'.repeat(81) })),
    configOf(department({ rows: [] }), department({ id: 'duplicate-category' })),
    configOf(
      department({
        rows: [
          { title: '同名', items: [] },
          { title: '同名', items: [] },
        ],
      }),
    ),
    configOf(department({ rows: [{ title: '分类', items: [{ label: '同名' }, { label: '同名' }] }] })),
    configOf(department({ rows: [{ title: '分类', items: [{ label: '商品', featured: null }] }] })),
    configOf(department({ rows: [{ title: '分类', query: false, items: [] }] })),
    configOf(
      department({
        brands: [
          { label: '品牌A', brand: 'same' },
          { label: '品牌B', brand: 'same' },
        ],
      }),
    ),
    { id: 'wrong-id', body: JSON.stringify(before) },
    { id: 'storefront-categories', body: 'invalid json' },
  ];
  for (const config of invalid) {
    assert.throws(() => f.ctx.LumoCategories.apply(config), /商品分类配置格式不正确/);
    assert.deepEqual(plain(f.ctx.LumoCategories.configuration), before);
  }
});

test('refresh coalesces concurrent readers and recovers from a synchronous API failure', async () => {
  const response = deferred();
  let calls = 0;
  const f = setup({
    config: null,
    globals: {
      LumoAPI: {
        call() {
          calls++;
          if (calls === 1) throw Error('连接失败');
          return response.promise;
        },
      },
    },
  });
  await assert.rejects(f.ctx.LumoCategories.refresh(), /连接失败/);
  assert.equal(f.ctx.LumoCategories.loaded, false);
  assert.equal(f.ctx.LumoCategories.groups.length, 0);
  assert.match(f.ctx.LumoCategories.directory(), /role="alert"/);
  assert.match(f.ctx.LumoCategories.directory(), /品类配置加载失败/);
  const first = f.ctx.LumoCategories.refresh(),
    second = f.ctx.LumoCategories.refresh();
  assert.equal(first, second);
  assert.equal(calls, 2);
  response.resolve(documentRecord(configOf(department())));
  await first;
  assert.equal(f.ctx.LumoCategories.loaded, true);
  assert.equal(f.ctx.LumoCategories.error, null);
});

for (const changed of ['base', 'token', 'user'])
  test('late navigation responses cannot cross a changed ' + changed, async () => {
    const oldResponse = deferred(),
      newResponse = deferred();
    let base = '',
      token = '',
      user = 'first',
      calls = 0;
    const f = setup({
      config: null,
      globals: {
        LumoAPI: {
          getBase: () => base,
          getToken: () => token,
          call() {
            return ++calls === 1 ? oldResponse.promise : newResponse.promise;
          },
        },
        LumoAuth: { user: () => ({ id: user }) },
      },
    });
    const old = f.ctx.LumoCategories.refresh();
    if (changed === 'base') base = 'http://new-api.test';
    else if (changed === 'token') token = 'new-token';
    else user = 'second';
    const current = f.ctx.LumoCategories.refresh();
    newResponse.resolve(documentRecord(configOf(department({ category: '当前分类' }))));
    await current;
    oldResponse.resolve(documentRecord(defaultConfiguration()));
    await old;
    assert.equal(calls, 2);
    assert.deepEqual(Array.from(f.ctx.LumoCategories.categories), ['全部好物', '当前分类']);
    assert.equal(f.ctx.LumoCategories.error, null);
  });

test('an explicit saved apply wins over an older refresh and a later read failure clears stale navigation', async () => {
  const response = deferred();
  let calls = 0;
  const f = setup({
    globals: {
      LumoAPI: {
        call() {
          calls++;
          return calls === 1 ? response.promise : Promise.reject(Error('服务不可用'));
        },
      },
    },
  });
  const old = f.ctx.LumoCategories.refresh();
  f.ctx.LumoCategories.apply(configOf(department()));
  response.resolve(documentRecord(defaultConfiguration()));
  await old;
  assert.deepEqual(Array.from(f.ctx.LumoCategories.categories), ['全部好物', '新分类']);
  await assert.rejects(f.ctx.LumoCategories.refresh(), /服务不可用/);
  assert.deepEqual(Array.from(f.ctx.LumoCategories.categories), ['全部好物']);
  assert.equal(f.ctx.LumoCategories.loaded, false);
  assert.match(f.ctx.LumoCategories.directory(), /品类配置加载失败/);
  assert.doesNotMatch(f.ctx.LumoCategories.directory(), /新分类|家用电器/);
});

test('saved backend categories immediately update the horizontal navigation and disabled ones disappear', () => {
  const app = createHarness(directory, 'http://new-category-nav.test');
  try {
    app.load('app.js');
    app.load('categories.js');
    app.ctx.LumoCategories.apply(configOf(department({ category: '新品 & 优选' })));
    const html = app.navigate('');
    assert.match(html, /data-value="新品 &amp; 优选"/);
    assert.doesNotMatch(html, /data-value="家用电器"/);
    app.ctx.LumoCategories.select('新品 & 优选', '指定商品查询', '指定商品');
    assert.equal(app.json('ui.category'), '新品 & 优选');
    assert.equal(app.json('ui.query'), '指定商品查询');
    app.ctx.LumoCategories.apply(configOf(department({ category: '新品 & 优选', enabled: false })));
    const hidden = app.navigate('');
    assert.doesNotMatch(hidden, /data-value="新品 &amp; 优选"/);
    assert.equal(app.json('ui.category'), '全部好物');
    assert.equal(app.failures.length, 0);
  } finally {
    app.dispose();
  }
});

test('hover switches local department content without remote reads or selection side effects', async () => {
  const f = setup();
  f.ctx.LumoAPI = {
    call() {
      f.calls.push({ kind: 'unexpected-api' });
      throw Error('opening a department must stay local');
    },
  };
  const before = plain(f.ctx.ui);
  await f.event('mouseover', f.groupButton(0));
  assert.equal(f.panel.hidden, false);
  assert.match(f.panel.innerHTML, new RegExp(f.groups[0].title));
  assert.equal(f.groupButton(0).getAttribute('aria-expanded'), 'true');
  await f.event('mouseover', f.groupButton(1));
  assert.match(f.panel.innerHTML, new RegExp(f.groups[1].title));
  assert.equal(f.groupButton(0).getAttribute('aria-expanded'), 'false');
  assert.deepEqual(plain(f.ctx.ui), before);
  assert.equal(f.calls.length, 0);
});

test('keyboard opens departments and moves focus into real detail choices', async () => {
  const f = setup();
  const first = f.groupButton(0);
  f.document.activeElement = first;
  await f.event('focusin', first);
  assert.equal(f.panel.hidden, false);
  const down = await f.event('keydown', first, { key: 'ArrowDown' });
  assert.equal(down.defaultPrevented, true);
  assert.equal(f.document.activeElement, f.groupButton(1));
  await f.event('keydown', f.groupButton(1), { key: 'ArrowRight' });
  assert.ok(f.panel.contains(f.document.activeElement), 'right arrow focuses a real expanded choice');
  await f.event('keydown', f.document.activeElement, { key: 'Escape' });
  assert.equal(f.panel.hidden, true);
});

test('outside actions, route changes and explicit close hide the department panel', async () => {
  const f = setup();
  await f.event('click', f.groupButton(0));
  assert.equal(f.panel.hidden, false);
  await f.event('pointerdown', new f.Element());
  assert.equal(f.panel.hidden, true);
  await f.event('click', f.groupButton(0));
  await f.windowEvent('hashchange');
  assert.equal(f.panel.hidden, true);
  await f.event('click', f.groupButton(0));
  f.ctx.LumoCategories.close();
  assert.equal(f.panel.hidden, true);
});

test('fallback category selection clears stale query/favorites while retaining explicit price and sort', () => {
  const f = setup();
  f.ctx.LumoCategories.select('家用电器', '冰箱', '冰箱');
  assert.equal(f.ctx.ui.category, '家用电器');
  assert.equal(f.ctx.ui.query, '冰箱');
  assert.equal(f.ctx.ui.onlyFavorites, false);
  assert.equal(f.ctx.ui.sort, '价格低');
  assert.equal(f.ctx.ui.min, '50');
  assert.equal(f.ctx.ui.max, '5000');
  assert.equal(f.panel.hidden, true);
  assert.equal(f.ctx.LumoCategories.selection().label, '冰箱');
});

test('a category and leaf dispatch one structured search through discovery', async () => {
  const calls = [];
  const app = createHarness(directory, 'http://category-filter.test', {
    globals: {
      LumoAPI: {
        getBase: () => '',
        getToken: () => '',
        async call(service, action, command = {}, options = {}) {
          calls.push({ service, action, command, options });
          return { items: [] };
        },
      },
    },
  });
  try {
    for (const script of ['app.js', 'discovery.js', 'categories.js']) app.load(script);
    app.ctx.LumoCategories.apply(defaultConfiguration());
    app.evaluate(
      "products.push(...presentationProducts);ui.query='旧搜索';ui.onlyFavorites=true;ui.sort='销量';ui.min='100';ui.max='2000'",
    );
    await app.ctx.LumoCategories.select('家用电器', '冰箱', '冰箱');
    await tick();
    await tick();
    const selected = app.json('ui');
    assert.equal(selected.category, '家用电器');
    assert.equal(selected.query, '冰箱');
    assert.equal(selected.onlyFavorites, false);
    assert.equal(selected.sort, '销量');
    assert.equal(selected.min, '100');
    assert.equal(selected.max, '2000');
    const queries = calls.filter((call) => call.service === 'search' && call.action === 'query');
    assert.equal(queries.length, 1, 'one leaf click performs a single structured search');
    assert.equal(queries[0].command.data.category, '家用电器');
    assert.equal(queries[0].command.data.query, '冰箱');
    assert.equal(queries[0].options.auth, false);
    assert.equal(app.failures.length, 0);
  } finally {
    app.dispose();
  }
});

test('configured category queries preserve the full 120 character backend limit', async () => {
  const calls = [],
    query = '精'.repeat(120);
  const app = createHarness(directory, 'http://long-category-query.test', {
    globals: {
      LumoAPI: {
        getBase: () => '',
        getToken: () => '',
        async call(service, action, command) {
          calls.push({ service, action, command });
          return { items: [] };
        },
      },
    },
  });
  try {
    for (const script of ['app.js', 'discovery.js', 'categories.js']) app.load(script);
    app.ctx.LumoCategories.apply(
      configOf(department({ rows: [{ title: '长查询', items: [{ label: '商品', query }] }] })),
    );
    await app.ctx.LumoCategories.select('新分类', query, '商品');
    assert.equal(app.json('ui.query'), query);
    assert.equal(calls.find((row) => row.service === 'search').command.data.query, query);
    assert.equal(app.failures.length, 0);
  } finally {
    app.dispose();
  }
});

test('detail leaves and brands come only from configured metadata and escape labels', async () => {
  const config = defaultConfiguration();
  config.groups[0].brands = [
    { label: '后台品牌', brand: 'ConfiguredBrand' },
    { label: '<img src=x onerror=alert(1)>', brand: 'EscapedBrand' },
  ];
  const f = setup({ config });
  await f.event('click', f.groupButton(0));
  assert.match(f.panel.innerHTML, /后台品牌/);
  assert.doesNotMatch(
    f.panel.innerHTML,
    /RealBrand|HiddenBrand/,
    'visible catalogue products never invent navigation brands',
  );
  assert.match(f.panel.innerHTML, /&lt;img/);
  assert.doesNotMatch(f.panel.innerHTML, /<img src=x/);
  const leaf = f.panel.querySelector('.category-leaf');
  await f.event('click', leaf);
  assert.equal(f.ctx.ui.category, f.groups[0].category);
  assert.equal(f.ctx.ui.query, leaf.dataset.categoryQuery);
  assert.equal(f.ctx.LumoCategories.selection().label, leaf.dataset.categoryLabel);
  assert.equal(f.panel.hidden, true);
  assert.deepEqual(
    f.calls.map((call) => call.kind),
    ['go'],
  );
  f.ctx.LumoDiscovery = {
    selectCategory(...args) {
      f.calls.push({ kind: 'select', args });
    },
  };
  await f.event('click', f.groupButton(0));
  await f.event('click', f.panel.querySelector('.category-brand'));
  assert.deepEqual(f.calls.at(-1).args, [f.groups[0].category, '', '后台品牌', 'ConfiguredBrand']);
});

function product(id, category, name) {
  return {
    id,
    product_id: id,
    name,
    category,
    brand: 'RealBrand',
    price_cents: 109900,
    active: true,
    image: 'appliance.jpg',
    attributes: { category, brand: 'RealBrand', catalog_visible: true },
  };
}
function searchApp(backend) {
  const calls = [],
    primed = [];
  const app = createHarness(directory, 'http://category-full-catalog.test', {
    globals: {
      LumoAPI: {
        getBase: () => '',
        getToken: () => '',
        async call(service, action, command = {}, options = {}) {
          calls.push({ service, action, command, options });
          return backend(service, action, command);
        },
      },
      LumoInventory: {
        hydrationTicket: () => 1,
        prime(rows, ticket) {
          primed.push({ rows: plain(rows), ticket });
        },
        stockFor: () => 20,
      },
    },
  });
  app.load('app.js');
  app.evaluate('products.push(...presentationProducts)');
  for (const script of ['discovery.js', 'recommendations.js', 'categories.js']) app.load(script);
  app.ctx.LumoCategories.apply(defaultConfiguration());
  return { app, calls, primed };
}

test('category search hydrates a full result outside the initial catalogue without per-item reads', async () => {
  const remote = Array.from({ length: 65 }, (_, index) =>
    product('remote-fridge-' + index, '家用电器', '全目录迷你冰箱 ' + index),
  );
  const { app, calls, primed } = searchApp(async (service, action) => ({
    items: service === 'search' ? remote : [],
    inventory_items: remote.map((row) => ({ sku_id: row.id, available: 20 })),
  }));
  try {
    await app.ctx.LumoCategories.select('家用电器', '冰箱', '冰箱');
    await tick();
    assert.equal(
      app.json("products.filter(p=>p.id.startsWith('remote-fridge-')).length"),
      65,
      'all supported search results survive metadata hydration',
    );
    assert.equal(app.json("LumoDiscovery.filter(products).filter(p=>p.id.startsWith('remote-fridge-')).length"), 65);
    assert.equal(primed.length, 1, 'the authoritative search stock snapshot is primed once');
    assert.equal(primed[0].rows.length, 65);
    assert.equal(primed[0].ticket, 1);
    assert.equal(calls.filter((call) => call.service === 'search').length, 1);
    assert.equal(
      calls.filter((call) => call.service === 'catalog' || call.service === 'inventory').length,
      0,
      'hydrating results does not cause an HTTP request per product',
    );
    assert.match(app.ids.get('app').innerHTML, /全目录迷你冰箱/);
    assert.equal(app.failures.length, 0);
  } finally {
    app.dispose();
  }
});

test('an older response for the same category cannot overwrite newer results or stock', async () => {
  let release,
    requests = 0;
  const oldResponse = new Promise((resolve) => (release = resolve));
  const newest = product('newest-fridge', '家用电器', '最新冰箱');
  const { app, primed } = searchApp(async (service) => {
    if (service !== 'search') return { items: [] };
    if (++requests === 1) return oldResponse;
    return { items: [newest], inventory_items: [{ sku_id: newest.id, available: 8 }] };
  });
  try {
    const old = app.ctx.LumoCategories.select('家用电器', '冰箱', '冰箱');
    await app.ctx.LumoCategories.select('家用电器', '冰箱', '冰箱');
    release({
      items: [product('old-fridge', '家用电器', '旧冰箱')],
      inventory_items: [{ sku_id: 'old-fridge', available: 99 }],
    });
    await old;
    await tick();
    assert.deepEqual(app.json('LumoDiscovery.filter(products).map(p=>p.id)'), ['newest-fridge']);
    assert.equal(app.json("products.some(p=>p.id==='old-fridge')"), false);
    assert.ok(
      primed.every((entry) => entry.rows.every((row) => row.sku_id !== 'old-fridge')),
      'stale stock is not primed',
    );
    assert.equal(app.failures.length, 0);
  } finally {
    app.dispose();
  }
});

test('switching departments replaces the search product pool while preserving a cart product', async () => {
  const fridges = Array.from({ length: 65 }, (_, index) =>
    product('fridge-pool-' + index, '家用电器', '冰箱 ' + index),
  );
  const books = Array.from({ length: 70 }, (_, index) => product('book-pool-' + index, '图书文娱', '小说 ' + index));
  const { app } = searchApp(async (service, action, command) => ({
    items: service === 'search' ? (command.data.category === '家用电器' ? fridges : books) : [],
    inventory_items: [],
  }));
  try {
    await app.ctx.LumoCategories.select('家用电器', '冰箱', '冰箱');
    app.evaluate("state.cart=[{id:'fridge-pool-0',qty:1,price:1099}]");
    await app.ctx.LumoCategories.select('图书文娱', '小说', '小说');
    await tick();
    assert.deepEqual(
      app.json("products.filter(p=>p.id.startsWith('fridge-pool-')).map(p=>p.id)"),
      ['fridge-pool-0'],
      'only a cart-pinned product survives the old search pool',
    );
    assert.equal(app.json("products.filter(p=>p.id.startsWith('book-pool-')).length"), 70);
    assert.equal(app.json('LumoDiscovery.filter(products).length'), 70);
    assert.equal(app.json('products.length'), 83, '12 initial products + 70 current results + one pinned cart product');
    assert.equal(app.failures.length, 0);
  } finally {
    app.dispose();
  }
});

for (const scenario of ['category', 'brand', 'base'])
  test('late category results cannot cross a changed ' + scenario, async () => {
    let release,
      requests = 0;
    const pending = new Promise((resolve) => (release = resolve));
    const expected = product(
      'expected-result',
      scenario === 'category' ? '图书文娱' : '家用电器',
      scenario === 'category' ? '新小说' : '新冰箱',
    );
    const { app, primed } = searchApp(async (service) => {
      if (service !== 'search') return { items: [] };
      if (++requests === 1) return pending;
      return { items: [expected], inventory_items: [{ sku_id: expected.id, available: 8 }] };
    });
    try {
      const first = app.ctx.LumoCategories.select('家用电器', '冰箱', '冰箱');
      if (scenario === 'base') {
        app.ctx.LumoAPI.getBase = () => 'http://other-category.test';
        app.ctx.LumoCategories.apply(defaultConfiguration());
      }
      await app.ctx.LumoCategories.select(
        scenario === 'category' ? '图书文娱' : '家用电器',
        scenario === 'category' ? '小说' : '冰箱',
        scenario === 'category' ? '小说' : '冰箱',
        scenario === 'brand' ? 'RealBrand' : '',
      );
      release({
        items: [product('stale-result', '家用电器', '旧冰箱')],
        inventory_items: [{ sku_id: 'stale-result', available: 99 }],
      });
      await first;
      await tick();
      assert.deepEqual(app.json('LumoDiscovery.filter(products).map(p=>p.id)'), ['expected-result']);
      assert.equal(app.json("products.some(p=>p.id==='stale-result')"), false);
      assert.ok(primed.every((entry) => entry.rows.every((row) => row.sku_id !== 'stale-result')));
      assert.equal(app.failures.length, 0);
    } finally {
      app.dispose();
    }
  });
