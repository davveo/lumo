'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((resolve) => setImmediate(resolve));

function setup({ missing = [], failure = null, incomplete = false } = {}) {
  const product = {
    id: 'phone',
    skuId: 'sku-a',
    name: 'Variant Phone',
    category: '手机数码',
    price: 100,
    old: 200,
    stock: 50,
    image: 'phone.jpg',
    visible: true,
    colors: ['黑色'],
    options: ['128GB', '256GB', '512GB'],
    defaultOption: '128GB',
    variants: [
      { sku_id: 'sku-a', color: '黑色', option: '128GB', price_cents: 10000 },
      { sku_id: 'sku-b', color: '黑色', option: '256GB', price_cents: 20000 },
      { sku_id: 'sku-c', color: '黑色', option: '512GB', price_cents: 30000 },
    ],
  };
  const calls = [],
    messages = [];
  let modal = '',
    principal = { id: 'admin', role: 'admin' };
  const app = createHarness(directory, 'http://editor.test', {
    globals: {
      products: [product],
      presentationProducts: [product],
      categories: ['全部好物', '手机数码'],
      categoryImages: [],
      LumoAuth: { isLoggedIn: () => !!principal, user: () => principal, requireLogin: () => !!principal },
      LumoAPI: {
        async call(service, action, command = {}) {
          calls.push({ service, action, command });
          if (service === 'inventory' && action === 'batch_get') {
            if (failure) throw failure;
            return {
              items: command.data.ids
                .filter((id) => !missing.includes(id))
                .map((id) => ({ sku_id: id, on_hand: id === 'sku-b' ? 12 : 7, available: 5 })),
              missing_ids: incomplete ? [] : missing,
            };
          }
          if (service === 'catalog' && action === 'upsert') return { id: 'phone', variants: product.variants };
          if (service === 'catalog' && action === 'list') return { items: [product] };
          if (service === 'content' && action === 'list') return { items: [] };
          if (service === 'inventory' && action === 'get')
            throw new Error('per-variant inventory requests are forbidden');
          return {};
        },
      },
      LumoCommerce: { async refreshCatalog() {}, normalizeProduct: (row) => row },
      LumoInventory: { async refresh() {}, stockFor: () => 5 },
      LumoDiscovery: { async refresh() {} },
      esc: (value) =>
        String(value ?? '').replace(
          /[&<>"']/g,
          (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
        ),
      money: String,
      icon: () => '',
      header: () => '',
      footer: () => '',
      render() {},
      toast: (message) => messages.push(message),
      modal: (_title, body) => {
        modal = body;
      },
      closeModal() {
        modal = '';
      },
    },
  });
  app.load('editor.js');
  return {
    app,
    calls,
    messages,
    get modal() {
      return modal;
    },
    setUser(value) {
      principal = value;
    },
  };
}

test('opening the editor reads every variant stock in one fresh batch', async () => {
  const fixture = setup();
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({ editorAction: 'edit-product', id: 'phone' });
    await tick();
    const stock = fixture.calls.filter((c) => c.service === 'inventory');
    assert.equal(stock.length, 1);
    assert.equal(stock[0].action, 'batch_get');
    assert.deepEqual([...stock[0].command.data.ids], ['sku-a', 'sku-b', 'sku-c']);
    assert.match(fixture.modal, /name="stock_variant_0"[^>]*value="12"/);
    assert.match(fixture.modal, /name="stock_variant_1"[^>]*value="7"/);
  } finally {
    fixture.app.dispose();
  }
});

test('editing fails visibly when the batch is incomplete and does not invent stock', async () => {
  const fixture = setup({ missing: ['sku-b'] });
  try {
    await fixture.app.click({ editorAction: 'edit-product', id: 'phone' });
    await tick();
    assert.equal(fixture.modal, '');
    assert.match(fixture.messages.join(' '), /库存缺失/);
    assert.equal(fixture.calls.filter((c) => c.service === 'inventory' && c.action === 'adjust').length, 0);
  } finally {
    fixture.app.dispose();
  }
});

test('publishing changes checks stock once and initializes only missing variants', async () => {
  const fixture = setup({ missing: ['sku-b'] });
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({ editorAction: 'toggle-product', id: 'phone' });
    const stock = fixture.calls.filter((c) => c.service === 'inventory');
    assert.deepEqual(
      stock.map((c) => c.action),
      ['batch_get', 'adjust'],
    );
    assert.equal(stock[1].command.id, 'sku-b');
    assert.equal(stock[1].command.data.on_hand, 50);
    assert.ok(fixture.calls.some((c) => c.service === 'content' && c.action === 'upsert'));
    assert.equal(fixture.app.failures.length, 0);
  } finally {
    fixture.app.dispose();
  }
});

test('failed stock batch never falls back to per-SKU reads or resets existing stock', async () => {
  const fixture = setup({ failure: new Error('inventory offline') });
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({ editorAction: 'toggle-product', id: 'phone' });
    assert.deepEqual(
      fixture.calls.filter((c) => c.service === 'inventory').map((c) => c.action),
      ['batch_get'],
    );
    assert.match(fixture.messages.join(' '), /库存更新未完成/);
    assert.ok(!fixture.calls.some((c) => c.service === 'content' && c.action === 'upsert'));
  } finally {
    fixture.app.dispose();
  }
});

test('admin homepage applies public theme without loading disabled catalog; editor entry loads it', async () => {
  const fixture = setup();
  try {
    await fixture.app.ctx.LumoStudio.refresh();
    assert.deepEqual(
      fixture.calls.map((c) => c.service + '/' + c.action),
      ['content/list'],
    );
    fixture.app.navigate('editor');
    await fixture.app.ctx.LumoStudio.refresh();
    const catalogs = fixture.calls.filter((c) => c.service === 'catalog');
    assert.equal(catalogs.length, 1);
    assert.equal(catalogs[0].command.data.include_disabled, true);
    fixture.setUser({ id: 'buyer', role: 'user' });
    await fixture.app.click({ editorAction: 'edit-product', id: 'phone' });
    await tick();
    assert.equal(fixture.calls.filter((c) => c.service === 'inventory').length, 0);
  } finally {
    fixture.app.dispose();
  }
});

test('an incomplete stock response cannot initialize existing or unknown stock rows', async () => {
  const fixture = setup({ missing: ['sku-b'], incomplete: true });
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({ editorAction: 'toggle-product', id: 'phone' });
    assert.deepEqual(
      fixture.calls.filter((c) => c.service === 'inventory').map((c) => c.action),
      ['batch_get'],
    );
    assert.match(fixture.messages.join(' '), /库存查询结果不完整/);
  } finally {
    fixture.app.dispose();
  }
});

test('hidden or deleted navigation categories preserve existing product editing and configured hidden choices', async () => {
  const group = (id, category, enabled = true) => ({
    id,
    category,
    label: category,
    title: category,
    icon: 'layers',
    enabled,
    rows: [],
  });
  for (const status of ['hidden', 'deleted']) {
    const fixture = setup();
    try {
      fixture.app.load('categories.js');
      fixture.app.ctx.LumoCategories.apply({
        version: 1,
        groups: [
          group('phones', '手机数码'),
          group('new-category', '后台新分类'),
          group('hidden-choice', '后台隐藏分类', false),
        ],
      });
      const next = {
        version: 1,
        groups: [group('new-category', '后台新分类'), group('hidden-choice', '后台隐藏分类', false)],
      };
      if (status === 'hidden') next.groups.unshift(group('phones', '手机数码', false));
      fixture.app.ctx.LumoCategories.apply(next);
      assert.deepEqual(
        fixture.app.json('categories'),
        ['全部好物', '后台新分类'],
        'public navigation omits hidden and deleted classifications',
      );
      await fixture.app.click({ editorAction: 'edit-product', id: 'phone' });
      await tick();
      const options = fixture.modal.match(/<select name="category">([\s\S]*?)<\/select>/)?.[1];
      assert.ok(options, 'the real product editor opens after navigation changes');
      assert.match(options, /<option selected>手机数码<\/option>/, 'the original classification stays selected');
      assert.match(
        options,
        />后台隐藏分类<\/option>/,
        'admin choices use the full configuration, including hidden classes',
      );
      assert.equal(
        (options.match(/>手机数码<\/option>/g) || []).length,
        1,
        'retained product categories are not duplicated',
      );
      await fixture.app.submit(
        'studio-product-form',
        {
          name: '修改后的手机',
          sub: '保留原分类',
          category: '手机数码',
          price: '100',
          old: '200',
          stock: '7',
          image: 'phone.jpg',
          visible: 'on',
        },
        { id: 'phone' },
      );
      const saved = fixture.calls.find((call) => call.service === 'catalog' && call.action === 'upsert');
      assert.ok(
        saved,
        'editing an existing product still writes to the catalog after its navigation category is ' + status,
      );
      assert.equal(saved.command.data.category, '手机数码');
      assert.equal(saved.command.data.name, '修改后的手机');
      assert.ok(fixture.messages.includes('商品修改已保存'));
      assert.equal(fixture.modal, '');
      assert.equal(fixture.app.failures.length, 0);
    } finally {
      fixture.app.dispose();
    }
  }
});

test('dynamic category names are escaped in actual product options and storefront design previews', async () => {
  const fixture = setup(),
    category = '</option><img src=x onerror=alert(1)> & "优选"';
  try {
    fixture.app.load('categories.js');
    fixture.app.ctx.LumoCategories.apply({
      version: 1,
      groups: [{ id: 'escaped-category', category, label: category, title: category, icon: 'layers', rows: [] }],
    });
    await fixture.app.click({ editorAction: 'edit-product', id: 'phone' });
    await tick();
    const options = fixture.modal.match(/<select name="category">([\s\S]*?)<\/select>/)?.[1];
    const encoded = fixture.app.ctx.esc(category);
    assert.ok(options.includes(encoded));
    assert.ok(!options.includes(category));
    assert.doesNotMatch(options, /<img\b/, 'category text cannot create an element in the select');
    assert.equal((options.match(/<option\b/g) || []).length, 2, 'untrusted names cannot close or create options');
    await fixture.app.ctx.LumoStudio.refresh();
    await fixture.app.click({ editorAction: 'tab', value: 'design' });
    const html = fixture.app.ctx.LumoStudio.renderEditor();
    const preview = html.match(
      /<div class="studio-preview-categories">([\s\S]*?)<\/div><div class="studio-preview-heading">/,
    )?.[1];
    assert.ok(preview, 'the actual page-decoration view renders configured categories');
    assert.ok(preview.includes(encoded));
    assert.ok(!preview.includes(category));
    assert.equal((preview.match(/<span>/g) || []).length, 1, 'preview follows the dynamic category count');
    assert.equal((preview.match(/<img\b/g) || []).length, 1, 'only the intended preview artwork creates an image');
    assert.doesNotMatch(preview, /undefined/);
    assert.equal(fixture.app.failures.length, 0);
  } finally {
    fixture.app.dispose();
  }
});
