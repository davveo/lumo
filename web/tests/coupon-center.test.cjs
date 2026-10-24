const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(
  path.join(process.env.FRONTEND_DIR || path.resolve(__dirname, '../dist'), 'coupon-center.js'),
  'utf8',
);
const esc = (value) =>
  String(value ?? '').replace(
    /[&<>"']/g,
    (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
  );
const flush = () => new Promise((resolve) => setImmediate(resolve));
function setup() {
  const env = {
    user: '',
    token: '',
    base: '',
    now: Date.now(),
    calls: [],
    messages: [],
    loginRoutes: [],
    renders: 0,
    walletRefreshes: 0,
    timers: new Map(),
    countdowns: [],
  };
  const listeners = {},
    claimed = new Set();
  let timerID = 0;
  const sample = (id, extra = {}) => ({
    id,
    name: `优惠券 ${id}`,
    active: true,
    amount_cents: 1000,
    min_cents: 10000,
    valid_days: 14,
    remaining: 100,
    expires_at: new Date(env.now + 86400000).toISOString(),
    claim_status: env.user ? 'available' : 'login_required',
    claimable: !!env.user,
    ...extra,
  });
  env.rows = [
    sample('daily'),
    sample('points', {
      min_points: 5000,
      claim_status: 'conditions_unmet',
      claimable: false,
      claim_reason: '可用积分不足 5000',
    }),
  ];
  env.list = (command) => ({
    items: env.rows.map((row) =>
      claimed.has(row.id)
        ? { ...row, claimed: true, claim_status: 'claimed', claimable: false }
        : {
            ...row,
            ...(!env.user && row.claim_status === 'available'
              ? { claim_status: 'login_required', claimable: false }
              : {}),
          },
    ),
    has_more: false,
    server_time: new Date(env.now).toISOString(),
  });
  env.submit = async (id) => {
    claimed.add(id);
    return { id: 'coupon-' + id, already_claimed: false };
  };
  const on = (name, handler) => (listeners[name] ||= []).push(handler);
  const window = {
    addEventListener: on,
    LumoAuth: {
      isLoggedIn: () => !!env.user,
      user: () => (env.user ? { id: env.user } : null),
      requireLogin: (route) => {
        env.loginRoutes.push(route);
        return !!env.user;
      },
    },
    LumoAPI: {
      getBase: () => env.base,
      getToken: () => env.token,
      call: async (service, action, command, options) => {
        env.calls.push({ service, action, command, options });
        if (action === 'templates' || action === 'templates_snapshot') return env.list(command);
        if (action === 'claim') return env.submit(command.data.template_id);
        throw new Error('Unexpected ' + action);
      },
    },
    LumoMember: {
      refresh: async () => {
        env.walletRefreshes++;
      },
    },
  };
  class TestDate extends Date {
    constructor(...args) {
      super(...(args.length ? args : [env.now]));
    }
    static now() {
      return env.now;
    }
  }
  const context = vm.createContext({
    window,
    document: { addEventListener: on, querySelectorAll: () => env.countdowns },
    location: { hash: '#/coupons', origin: 'http://localhost' },
    Date: TestDate,
    setInterval: (callback) => {
      env.timers.set(++timerID, callback);
      return timerID;
    },
    clearInterval: (id) => env.timers.delete(id),
    esc,
    money: (value) => Number(value).toLocaleString('zh-CN', { maximumFractionDigits: 2 }),
    icon: (name) => `<svg data-icon="${name}"></svg>`,
    header: () => '<header></header>',
    footer: () => '<footer></footer>',
    renderKeepScroll: () => {
      env.renders++;
    },
    toast: (message) => env.messages.push(message),
  });
  require('./load-constants.cjs')(context);
  vm.runInContext(source, context, { filename: 'coupon-center.js' });
  env.center = window.LumoCouponCenter;
  env.event = (name) => {
    for (const handler of listeners[name] || []) handler();
  };
  env.click = async (dataset) => {
    for (const handler of listeners.click || [])
      await handler({
        preventDefault() {},
        target: { closest: (selector) => (selector === '[data-cc-action]' ? { dataset, disabled: false } : null) },
      });
  };
  env.login = (user = 'demo', token = 'session-1') => {
    env.user = user;
    env.token = token;
    env.event('lumo:login');
  };
  env.sample = sample;
  env.claimed = claimed;
  env.context = context;
  return env;
}
(async () => {
  const guest = setup();
  const future = new Date(guest.now + 3600000).toISOString();
  guest.rows = [
    guest.sample('daily'),
    guest.sample('timed', { starts_at: future, claim_status: 'upcoming' }),
    guest.sample('points', { min_points: 5000 }),
    guest.sample('growth', { min_growth: 1000 }),
    guest.sample('expired', { expires_at: new Date(guest.now - 1000).toISOString(), claim_status: 'ended' }),
    guest.sample('empty', { remaining: 0, claim_status: 'exhausted' }),
    guest.sample('unsafe\"<', { name: '<img src=x onerror=alert(1)>' }),
  ];
  await guest.center.refresh();
  assert.equal(guest.calls[0].options.auth, false, 'guest browsing must not require authentication');
  const firstGuestPage = guest.center.page();
  assert.equal((firstGuestPage.match(/class="cc-pagination is-bottom"/g) || []).length, 1);
  assert(!firstGuestPage.includes('cc-pagination is-top'));
  await guest.click({ ccAction: 'page', value: '2' });
  const html = firstGuestPage + guest.center.page();
  assert(html.includes('登录领取'));
  assert(html.includes('即将开放'));
  assert(html.includes('领取已结束'));
  assert(html.includes('已领完'));
  assert(html.includes('成长值满 1,000'));
  assert(html.includes('仅核验，不扣积分'));
  assert(!html.includes('<img src=x'));
  assert(html.includes('&lt;img src=x'));
  assert(html.includes('data-id="unsafe&quot;&lt;"'));
  await guest.center.claim('daily');
  assert.equal(guest.loginRoutes.at(-1), '#/coupons');
  assert.equal(guest.calls.filter((call) => call.action === 'claim').length, 0);
  await guest.click({ ccAction: 'filter', value: 'timed' });
  assert.equal(guest.center.model().filter, 'timed');
  assert(guest.center.page().includes('当前分类 1 种'));
  await guest.click({ ccAction: 'filter', value: 'conditional' });
  assert(guest.center.page().includes('当前分类 2 种'));
  assert(guest.center.home().includes('#/coupons'));
  assert.equal((guest.center.home().match(/class="cc-home-ticket"/g) || []).length, 3);
  assert.equal((guest.center.home({ limit: 2 }).match(/class="cc-home-ticket"/g) || []).length, 2);

  const pages = setup();
  pages.login();
  pages.list = (command) =>
    command.data.page === 1
      ? { items: [pages.sample('first')], has_more: true }
      : { items: [pages.sample('second'), pages.sample('first')], has_more: false };
  await pages.center.refresh();
  assert.equal(pages.center.model().items.length, 2);
  assert.equal(pages.calls.length, 2);
  assert.equal(pages.calls[0].command.data.page_size, 5000);
  assert.equal(pages.calls[1].command.data.page, 2);
  pages.center.reset();
  pages.list = () => ({ items: [], has_more: true });
  await assert.rejects(pages.center.refresh(), /加载不完整/);
  assert.equal(pages.center.model().loaded, false);
  pages.center.reset();
  pages.list = () => ({ items: [pages.sample('repeated-page')], has_more: true });
  const beforeRepeated = pages.calls.length;
  await assert.rejects(pages.center.refresh(), /加载不完整/);
  assert.equal(
    pages.calls.length,
    beforeRepeated + 2,
    'nonadvancing pagination must stop without thousands of duplicate requests',
  );

  // Fetch every metadata page for global eligibility filters, while bounding
  // the rendered list independently of the size of the live coupon catalogue.
  const large = setup();
  large.login();
  large.rows = Array.from({ length: 2000 }, (_, i) =>
    large.sample(`old-${String(i).padStart(4, '0')}`, { created_at: '2025-01-01T00:00:00Z' }),
  );
  large.rows.push(
    large.sample('fresh-timed', {
      created_at: new Date(large.now).toISOString(),
      starts_at: new Date(large.now - 3600000).toISOString(),
    }),
  );
  large.rows.push(
    large.sample('fresh-points', { created_at: new Date(large.now - 1000).toISOString(), min_points: 100 }),
  );
  large.rows.push(
    large.sample('future-special', {
      created_at: new Date(large.now + 1000).toISOString(),
      starts_at: new Date(large.now + 3600000).toISOString(),
      claim_status: 'upcoming',
      claimable: false,
    }),
  );
  large.rows.push(
    large.sample('locked-special', {
      created_at: new Date(large.now + 2000).toISOString(),
      min_growth: 1000,
      claim_status: 'conditions_unmet',
      claimable: false,
    }),
  );
  large.rows.push(
    large.sample('claimed-special', {
      created_at: new Date(large.now + 3000).toISOString(),
      claimed: true,
      claim_status: 'claimed',
      claimable: false,
    }),
  );
  large.rows.push(
    large.sample('empty-special', {
      created_at: new Date(large.now + 4000).toISOString(),
      remaining: 0,
      claim_status: 'exhausted',
      claimable: false,
    }),
  );
  large.rows.push(
    large.sample('expired-special', {
      created_at: new Date(large.now + 5000).toISOString(),
      expires_at: new Date(large.now - 1000).toISOString(),
      claim_status: 'ended',
      claimable: false,
    }),
  );
  large.list = (command) => {
    const start = (command.data.page - 1) * command.data.page_size;
    return {
      items: large.rows.slice(start, start + command.data.page_size),
      has_more: start + command.data.page_size < large.rows.length,
    };
  };
  await large.center.refresh();
  assert.equal(large.center.model().items.length, 2007);
  assert.equal(large.calls.length, 1);
  const largeHTML = large.center.page(),
    cardCount = (html) => (html.match(/<article class="cc-card /g) || []).length;
  const cardIDs = (html) => [...html.matchAll(/data-cc-action="claim" data-id="([^"]+)"/g)].map((row) => row[1]);
  assert.equal(cardCount(largeHTML), 6, 'a large live catalogue must render only six coupons per page');
  assert(largeHTML.includes('当前显示 1–6 种'));
  assert(largeHTML.includes('第 <strong>1</strong> / 335 页'));
  assert.deepEqual(cardIDs(largeHTML), ['fresh-timed', 'fresh-points', 'old-0000', 'old-0001', 'old-0002', 'old-0003']);
  assert(
    largeHTML.indexOf('data-id="fresh-timed"') < largeHTML.indexOf('data-id="old-0000"'),
    'newly created currently available timed coupons must appear ahead of old seed rows',
  );
  assert(
    large.center.home().indexOf('优惠券 fresh-timed') < large.center.home().indexOf('优惠券 old-0000'),
    'home previews use the same newest-within-state order',
  );
  assert(
    !largeHTML.includes('data-id="future-special"'),
    'upcoming rows must not outrank claimable coupons regardless of creation time',
  );
  assert(largeHTML.includes('aria-label="下一页优惠券"'));
  const beforePaging = large.calls.length;
  await large.click({ ccAction: 'page', value: '2' });
  assert.equal(large.center.model().display_page, 2);
  assert.equal(cardCount(large.center.page()), 6);
  assert(large.center.page().includes('当前显示 7–12 种'));
  assert.deepEqual(cardIDs(large.center.page()), [
    'old-0004',
    'old-0005',
    'old-0006',
    'old-0007',
    'old-0008',
    'old-0009',
  ]);
  await large.click({ ccAction: 'page', value: '1' });
  assert.equal(large.center.model().display_page, 1);
  assert(large.center.page().includes('当前显示 1–6 种'));
  assert.deepEqual(
    cardIDs(large.center.page()),
    cardIDs(largeHTML),
    'returning to the previous page retains the original sorted coupons',
  );
  await large.click({ ccAction: 'page', value: '2' });
  assert.equal(
    large.calls.length,
    beforePaging,
    'next and previous pages reuse the complete metadata snapshot without an API request',
  );
  await large.center.refresh(true);
  assert.equal(large.center.model().display_page, 2, 'ordinary refresh preserves the visible page');
  const beforeLastPages = large.calls.length;
  await large.click({ ccAction: 'page', value: '335' });
  assert.equal(cardCount(large.center.page()), 3);
  assert(large.center.page().includes('当前显示 2005–2007 种'));
  assert.deepEqual(cardIDs(large.center.page()), ['claimed-special', 'empty-special', 'expired-special']);
  await large.click({ ccAction: 'page', value: '334' });
  assert.equal(cardCount(large.center.page()), 6);
  assert(large.center.page().includes('当前显示 1999–2004 种'));
  await large.click({ ccAction: 'page', value: '335' });
  assert.equal(cardCount(large.center.page()), 3);
  assert.equal(large.calls.length, beforeLastPages, 'moving between the final pages must not refetch coupon templates');
  await large.click({ ccAction: 'filter', value: 'conditional' });
  assert.equal(large.center.model().display_page, 1);
  assert.equal(cardCount(large.center.page()), 2);
  assert(
    large.center.page().includes('data-id="locked-special"'),
    'global condition filters must find templates fetched from the final API page',
  );
  await large.click({ ccAction: 'filter', value: 'timed' });
  assert.equal(cardCount(large.center.page()), 2);
  assert(large.center.page().includes('data-id="future-special"'));
  await large.click({ ccAction: 'filter', value: 'all' });
  await large.click({ ccAction: 'page', value: '335' });
  large.rows = large.rows.slice(0, 2);
  await large.center.refresh(true);
  assert.equal(large.center.model().display_page, 1);
  assert.equal(cardCount(large.center.page()), 2, 'refresh clamps the page after catalogue changes');

  const preview = setup();
  preview.context.location.hash = '#/';
  preview.rows = Array.from({ length: 2007 }, (_, index) => preview.sample('preview-' + index));
  preview.list = (command) => {
    const start = (command.data.page - 1) * command.data.page_size;
    return {
      items: preview.rows.slice(start, start + command.data.page_size),
      total: 2007,
      has_more: start + command.data.page_size < 2007,
    };
  };
  await preview.center.refresh();
  assert.equal(preview.calls.length, 1, 'home previews never drain every coupon metadata page');
  assert.equal(preview.calls[0].action, 'templates');
  assert.equal(preview.center.model().items.length, 100);
  assert.equal(preview.center.model().complete, false);
  preview.context.location.hash = '#/coupons';
  await preview.center.refresh();
  assert.equal(preview.calls[1].action, 'templates_snapshot');
  assert.equal(preview.center.model().items.length, 2007);
  assert.equal(preview.center.model().complete, true);

  const stale = setup();
  let release;
  stale.list = () =>
    new Promise((resolve) => {
      release = resolve;
    });
  const old = stale.center.refresh();
  stale.login('next', 'next-session');
  release({ items: [stale.sample('old-private-coupon')], has_more: false });
  await old;
  assert.equal(stale.center.model().items.length, 0, 'prior principal responses must not populate a new account');
  stale.list = () => ({ items: [stale.sample('new-account')], has_more: false });
  await stale.center.refresh();
  stale.base = 'http://other-gateway';
  assert.equal(stale.center.model().items.length, 0, 'gateway changes clear center state');
  stale.list = () => ({ items: [stale.sample('new-gateway')], has_more: false });
  await stale.center.refresh();
  stale.token = 'rotated-session';
  assert.equal(stale.center.model().items.length, 0, 'a new session for the same account must clear state');

  const buyer = setup();
  buyer.login();
  buyer.rows = [
    buyer.sample('daily'),
    buyer.sample('second'),
    buyer.sample('locked', { claim_status: 'conditions_unmet', claimable: false, min_points: 9999 }),
  ];
  await buyer.center.refresh();
  await buyer.center.claim('locked');
  assert.equal(
    buyer.calls.filter((call) => call.action === 'claim').length,
    0,
    'ineligible cards cannot submit claims',
  );
  let finishClaim;
  buyer.submit = (id) =>
    new Promise((resolve) => {
      finishClaim = () => {
        buyer.claimed.add(id);
        resolve({ id: 'coupon-' + id });
      };
    });
  const pending = buyer.center.claim('daily');
  await buyer.center.claim('daily');
  assert.equal(
    buyer.calls.filter((call) => call.action === 'claim').length,
    1,
    'double clicks must share a single claim',
  );
  assert(buyer.center.page().includes('正在领取…'));
  // A list refresh can finish while the claim RPC is pending. Its revision must
  // never hide the subsequent successful claim or skip refreshing the wallet.
  await buyer.center.refresh(true);
  finishClaim();
  await pending;
  assert.equal(buyer.walletRefreshes, 1);
  assert(buyer.center.model().items.find((row) => row.id === 'daily').claimed);
  await buyer.center.claim('daily');
  assert.equal(buyer.context.location.hash, '#/member/coupons');
  assert.equal(
    buyer.calls.filter((call) => call.action === 'claim').length,
    1,
    'already claimed rows should open the wallet',
  );

  const multi = setup();
  multi.login();
  multi.rows = [multi.sample('one'), multi.sample('two')];
  await multi.center.refresh();
  const claims = new Map();
  multi.submit = (id) =>
    new Promise((resolve) =>
      claims.set(id, () => {
        multi.claimed.add(id);
        resolve({ id: 'coupon-' + id });
      }),
    );
  const one = multi.center.claim('one'),
    two = multi.center.claim('two');
  claims.get('one')();
  await one;
  claims.get('two')();
  await two;
  assert.equal(multi.walletRefreshes, 2);
  assert(
    multi.center.model().items.every((row) => row.claimed),
    'concurrent independent claims must both be visible',
  );

  const changed = setup();
  changed.login();
  changed.rows = [changed.sample('one')];
  await changed.center.refresh();
  changed.submit = () =>
    new Promise((resolve) => {
      release = resolve;
    });
  const oldClaim = changed.center.claim('one');
  changed.login('other', 'other-session');
  release({ id: 'private-old-coupon' });
  await oldClaim;
  assert.equal(changed.walletRefreshes, 0);
  assert.equal(changed.messages.length, 0);
  assert.equal(changed.center.model().items.length, 0);

  const rejected = setup();
  rejected.login();
  rejected.rows = [rejected.sample('last')];
  await rejected.center.refresh();
  rejected.submit = async () => {
    rejected.rows[0] = rejected.sample('last', { remaining: 0, claim_status: 'exhausted', claimable: false });
    throw new Error('coupon quota exhausted');
  };
  await rejected.center.claim('last');
  assert.equal(rejected.center.model().items[0].claim_status, 'exhausted');
  assert(rejected.messages.some((message) => message.includes('quota exhausted')));

  const committed = setup();
  committed.login();
  committed.rows = [committed.sample('committed')];
  await committed.center.refresh();
  committed.submit = async () => {
    committed.list = () => {
      throw new Error('list offline after commit');
    };
    return { id: 'previously-issued', already_claimed: true };
  };
  await committed.center.claim('committed');
  assert.equal(
    committed.center.model().items[0].claim_status,
    'claimed',
    'a followup refresh failure must not undo the committed claim',
  );
  assert.equal(committed.center.model().items[0].coupon_id, 'previously-issued');
  assert.equal(committed.walletRefreshes, 1);
  assert(committed.messages.some((message) => message.includes('这张券已领取')));

  const countdown = setup();
  countdown.login();
  const serverStart = new Date(countdown.now + 65000).toISOString();
  countdown.rows = [countdown.sample('future', { starts_at: serverStart, claim_status: 'upcoming', claimable: false })];
  countdown.list = () => ({
    items: countdown.rows,
    has_more: false,
    server_time: new Date(countdown.now + 60000).toISOString(),
  });
  await countdown.center.refresh();
  const element = { dataset: { ccCountdown: serverStart }, textContent: '' };
  countdown.countdowns = [element];
  countdown.center.afterRender();
  assert.equal(element.textContent, '00:00:05', 'countdown uses server time, not the browser clock');
  const renders = countdown.renders,
    tick = [...countdown.timers.values()][0];
  countdown.now += 1000;
  tick();
  assert.equal(element.textContent, '00:00:04');
  assert.equal(countdown.renders, renders, 'countdown ticks must not redraw the page');
  const beforeBoundary = countdown.calls.length;
  countdown.now += 5000;
  countdown.list = () => {
    throw new Error('temporarily offline');
  };
  tick();
  await flush();
  tick();
  await flush();
  assert.equal(countdown.calls.length, beforeBoundary + 1, 'a failed boundary refresh must not create a request storm');
  countdown.center.reset();
  assert.equal(countdown.timers.size, 0);
  console.log(
    'PASS coupon center public pagination, filters, conditions, escaping, claim retries, concurrent refresh/claims, wallet synchronization, account isolation and server countdown.',
  );
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
