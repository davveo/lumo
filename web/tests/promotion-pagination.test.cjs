'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');

const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const types = ['reduction', 'discount', 'quantity_discount', 'direct'];
const idFor = (index) => 'paging-product-' + String(index + 1).padStart(2, '0');

function setup(count = 14) {
  let user = 'buyer',
    token = 'session-a',
    base = 'http://paging.test';
  const calls = [],
    products = Array.from({ length: count }, (_, index) => ({
      id: idFor(index),
      skuId: idFor(index),
      name: '活动好物 ' + (index + 1),
      sub: '真实加载的活动商品',
      category: '手机数码',
      image: 'headphones.jpg',
      price: 100 + index,
      old: 140 + index,
      colors: ['黑色'],
      options: ['标准版'],
      defaultOption: '标准版',
      visible: true,
    }));
  const activities = types.map((kind) => ({
    id: 'activity-' + kind,
    kind,
    name: kind + ' 活动',
    description: '按订单计算的活动优惠',
    active: true,
    quota: 100,
    held: 0,
    used: 0,
    threshold_cents: 0,
    min_quantity: 1,
    discount_cents: 2000,
    pay_rate_bps: 9000,
  }));
  const offers = (type) =>
    products.map((product, index) => ({
      id: type + '-' + product.id,
      activityId: 'activity-' + type,
      productId: product.id,
      product,
      skuId: product.id,
      type,
      status: 'active',
      remaining: 20,
      stock: 30,
      limit: 1,
      limitReached: false,
      price: product.price - 20,
      regularPrice: product.price,
      discount: 20,
      startAt: 0,
      endAt: 0,
      teamSize: 2,
      index,
    }));
  const rows = {
    flash: offers('flash'),
    tomorrow: offers('flash').map((row) => ({ ...row, status: 'upcoming' })),
    group: offers('group'),
    exchange: offers('exchange'),
  };
  const groups = products.map((product, index) => ({
    id: 'team-' + index,
    productId: product.id,
    product,
    offerId: 'group-' + product.id,
    leader: '伙伴' + index,
    mine: true,
    status: 'waiting',
    required: 2,
    joined: 1,
    occupied: 1,
    endAt: Date.now() + 1800000,
  }));
  const app = createHarness(directory, base, {
    globals: {
      LumoAPI: {
        getBase: () => base,
        getToken: () => token,
        async call(service, action, command) {
          calls.push({ service, action, command });
          throw Error('paging must not read a service');
        },
      },
      LumoAuth: {
        user: () => (user ? { id: user } : null),
        isLoggedIn: () => !!user,
        gate: () => true,
        requireLogin: () => !!user,
        afterRender() {},
      },
      LumoInventory: { stockFor: () => 20 },
    },
  });
  app.load('app.js');
  app.ctx.fixtureProducts = products;
  app.evaluate('products.push(...fixtureProducts)');
  app.ctx.LumoPromotions = {
    flashOffers: (session) => rows[session === 'tomorrow' ? 'tomorrow' : 'flash'],
    groupOffers: () => rows.group,
    exchangeOffers: () => rows.exchange,
    teams: () => [],
    myGroups: () => groups,
    campaigns: (kind) => activities.filter((row) => !kind || row.kind === kind),
    campaignRule: () => '每单直减 ¥20',
    exchangeState: () => ({
      selectedId: null,
      eligible: true,
      hasRestricted: false,
      baseAmount: 1000,
      threshold: 1000,
      remaining: 0,
    }),
    status: () => ({ loaded: true, error: '' }),
    afterRender() {},
  };
  app.load('promo-views.js');
  return {
    app,
    calls,
    products,
    rows,
    groups,
    html: () => app.ids.get('app').innerHTML,
    setContext(next) {
      if ('user' in next) user = next.user;
      if ('token' in next) token = next.token;
      if ('base' in next) base = next.base;
    },
  };
}

function shownProducts(html) {
  return [...html.matchAll(/<a\b(?=[^>]*\bclass="[^"]*\bpromo-card-title\b")[^>]*\bhref="#\/product\/([^"]+)"/g)].map(
    (row) => row[1],
  );
}
function pagers(html) {
  return [...html.matchAll(/<nav\b[^>]*\baria-label="活动商品分页（(顶部|底部)）"[^>]*>([\s\S]*?)<\/nav>/g)].map(
    ([, position, body]) => ({ position, body }),
  );
}
function assertNoReads(fixture) {
  assert.equal(fixture.calls.length, 0, 'paging and rerendering must use the loaded activity snapshot');
  assert.deepEqual(fixture.app.failures, []);
}

for (const section of ['flash', 'group', 'exchange', ...types]) {
  test(section + ' shows six activities per page and keeps the final partial page reachable', async () => {
    const f = setup();
    try {
      let html = f.app.navigate('promotions/' + section);
      assert.deepEqual(
        shownProducts(html),
        Array.from({ length: 6 }, (_, index) => idFor(index)),
      );
      assert.match(html, /共\s*14\s*件好物/);
      assert.match(html, /当前显示\s*1\s*[–-]\s*6\s*件/);
      const controls = pagers(html);
      assert.deepEqual(
        controls.map((row) => row.position),
        ['底部'],
      );
      assert.match(
        controls[0].body,
        /aria-label="上一页活动商品"[^>]*disabled|disabled[^>]*aria-label="上一页活动商品"/,
      );
      assert.match(controls[0].body, /aria-label="下一页活动商品"/);
      assert.match(controls[0].body, /aria-label="第 3 页活动商品"/);

      await f.app.click({ promoPage: '2' });
      html = f.html();
      assert.deepEqual(
        shownProducts(html),
        Array.from({ length: 6 }, (_, index) => idFor(index + 6)),
      );
      assert.match(html, /当前显示\s*7\s*[–-]\s*12\s*件/);
      f.app.evaluate('render()');
      assert.deepEqual(
        shownProducts(f.html()),
        shownProducts(html),
        'ordinary rerenders must retain the selected activity page',
      );

      await f.app.click({ promoPage: '3' });
      html = f.html();
      assert.deepEqual(
        shownProducts(html),
        [idFor(12), idFor(13)],
        'the former twelve-product cap must not hide the final items',
      );
      assert.match(html, /当前显示\s*13\s*[–-]\s*14\s*件/);
      assert.match(
        pagers(html)[0].body,
        /aria-label="下一页活动商品"[^>]*disabled|disabled[^>]*aria-label="下一页活动商品"/,
      );
      assertNoReads(f);
    } finally {
      f.app.dispose();
    }
  });
}

test('ordinary activities exclude invisible products before calculating the page count', async () => {
  const f = setup(15);
  try {
    f.app.evaluate('products[1].visible=false;products[14].visible=false');
    f.app.navigate('promotions/direct');
    await f.app.click({ promoPage: '3' });
    assert.deepEqual(shownProducts(f.html()), [idFor(13)]);
    assert.match(f.html(), /共\s*13\s*件好物/);
    assert.doesNotMatch(f.html(), /href="#\/product\/paging-product-15"/);
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});

test('empty and single-page lists have useful counts without duplicated pagination controls', () => {
  for (const count of [0, 1, 6]) {
    const f = setup(count);
    try {
      for (const section of ['flash', 'group', 'exchange', ...types]) {
        const html = f.app.navigate('promotions/' + section);
        assert.equal(shownProducts(html).length, count, section + ' count=' + count);
        assert.equal(pagers(html).length, 0, section + ' should not show page controls for a single page');
        assert.match(html, new RegExp('共\\s*' + count + '\\s*件好物'));
      }
      assertNoReads(f);
    } finally {
      f.app.dispose();
    }
  }
});

test('changing the activity tab, flash session or invite resets the visible page', async () => {
  const f = setup();
  try {
    f.app.navigate('promotions/group');
    await f.app.click({ promoPage: '2' });
    assert.equal(shownProducts(f.html())[0], idFor(6));
    assert.equal(shownProducts(f.app.navigate('promotions/flash'))[0], idFor(0));
    await f.app.click({ promoPage: '2' });
    assert.equal(shownProducts(f.app.navigate('promotions/flash/tomorrow'))[0], idFor(0));
    await f.app.click({ promoPage: '2' });
    assert.equal(shownProducts(f.app.navigate('promotions/flash'))[0], idFor(0));
    f.app.navigate('promotions/group');
    await f.app.click({ promoPage: '2' });
    assert.equal(shownProducts(f.app.navigate('promotions/group/team-0'))[0], idFor(0));
    await f.app.click({ promoPage: '2' });
    assert.equal(shownProducts(f.app.navigate('promotions/group/team-1'))[0], idFor(0));
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});

test('leaving activities clears paging state before returning to the same activity', async () => {
  const f = setup();
  try {
    f.app.navigate('promotions/group');
    await f.app.click({ promoPage: '3' });
    f.app.navigate('');
    f.app.ctx.dispatchEvent(new f.app.ctx.CustomEvent('hashchange'));
    const html = f.app.navigate('promotions/group');
    assert.equal(shownProducts(html)[0], idFor(0));
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});

test('user, token and gateway changes reset paging even on the same route', async () => {
  for (const next of [{ user: 'another' }, { token: 'session-b' }, { base: 'http://another.test' }]) {
    const f = setup();
    try {
      f.app.navigate('promotions/group');
      await f.app.click({ promoPage: '3' });
      f.setContext(next);
      assert.equal(shownProducts(f.app.navigate('promotions/group'))[0], idFor(0));
      assertNoReads(f);
    } finally {
      f.app.dispose();
    }
  }
});

test('a stale pagination control cannot move a newly selected activity', async () => {
  const f = setup();
  try {
    const oldHTML = f.app.navigate('promotions/group');
    const oldRevision = /data-promo-page-revision="(\d+)"/.exec(oldHTML)?.[1];
    assert.ok(oldRevision);
    f.app.navigate('promotions/flash');
    await f.app.click({ promoPage: '3', promoPageRevision: oldRevision });
    assert.equal(shownProducts(f.html())[0], idFor(0));
    f.setContext({ token: 'session-b' });
    await f.app.click({ promoPage: '3' });
    assert.equal(
      shownProducts(f.html())[0],
      idFor(0),
      'an old visible button must not transfer the old page into a new session',
    );
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});

test('session lifecycle events clear the same-route pagination without network reads', async () => {
  for (const event of ['lumo:login', 'lumo:logout', 'lumo:session-expired']) {
    const f = setup();
    try {
      f.app.navigate('promotions/group');
      await f.app.click({ promoPage: '3' });
      f.app.ctx.dispatchEvent(new f.app.ctx.CustomEvent(event));
      assert.equal(shownProducts(f.app.navigate('promotions/group'))[0], idFor(0), event);
      assertNoReads(f);
    } finally {
      f.app.dispose();
    }
  }
});

test('a shorter activity snapshot clamps the selected page instead of rendering a blank grid', async () => {
  const f = setup();
  try {
    f.app.navigate('promotions/flash');
    await f.app.click({ promoPage: '3' });
    f.rows.flash.splice(8);
    f.app.evaluate('render()');
    assert.deepEqual(shownProducts(f.html()), [idFor(6), idFor(7)]);
    assert.match(f.html(), /当前显示\s*7\s*[–-]\s*8\s*件/);
    f.rows.flash.splice(2);
    f.app.evaluate('render()');
    assert.deepEqual(shownProducts(f.html()), [idFor(0), idFor(1)]);
    assert.equal(pagers(f.html()).length, 0);
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});

test('out-of-range page numbers clamp safely while malformed page values are ignored', async () => {
  const f = setup();
  try {
    f.app.navigate('promotions/group');
    await f.app.click({ promoPage: '999' });
    assert.deepEqual(shownProducts(f.html()), [idFor(12), idFor(13)]);
    for (const value of ['NaN', 'not-a-page', '1.5', 'Infinity']) {
      await f.app.click({ promoPage: value });
      assert.deepEqual(
        shownProducts(f.html()),
        [idFor(12), idFor(13)],
        'malformed value ' + value + ' must not change the current page',
      );
    }
    await f.app.click({ promoPage: '0' });
    assert.equal(shownProducts(f.html())[0], idFor(0));
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});

test('my groups use the same bounded pager without truncating invitation panels', async () => {
  const f = setup();
  try {
    let html = f.app.navigate('promotions/mygroups');
    assert.equal((html.match(/class="promo-order-panel /g) || []).length, 6);
    assert.equal(pagers(html).length, 2);
    await f.app.click({ promoPage: '3' });
    html = f.html();
    assert.equal((html.match(/class="promo-order-panel /g) || []).length, 2);
    assert.match(html, /活动好物 13/);
    assert.match(html, /活动好物 14/);
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});

test('cart and checkout exchange selectors remain inline and independent of the activity page', async () => {
  const f = setup();
  try {
    f.app.navigate('promotions/exchange');
    await f.app.click({ promoPage: '3' });
    for (const placement of ['cart', 'checkout']) {
      const html = f.app.ctx.LumoPromoView.exchange([], placement);
      assert.equal(shownProducts(html).length, 14, placement + ' must retain the original complete inline selector');
      assert.equal(pagers(html).length, 0);
      assert.doesNotMatch(html, /data-promo-page=/);
    }
    assertNoReads(f);
  } finally {
    f.app.dispose();
  }
});
