'use strict';
// Real HTTP -> explicit gRPC methods -> isolated domain databases.
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const base = process.env.API_BASE || 'http://127.0.0.1:18180';
const suffix = crypto.randomUUID();
let checks = 0;
const same = (actual, expected) => {
  assert.deepEqual(actual, expected);
  checks++;
};
const check = (value, message) => {
  assert.ok(value, message);
  checks++;
};
async function request(service, action, command = {}, token = '') {
  const response = await fetch(base + '/api/v1/' + service + '/' + action, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
    body: JSON.stringify(command),
  });
  const body = await response.json();
  check(response.headers.get('x-trace-id'), 'diagnostic trace header is present');
  check(!Object.hasOwn(body, 'trace_id'), 'trace remains outside JSON responses');
  return { ok: response.ok, status: response.status, data: body.data, error: body.error };
}
async function call(service, action, command, token) {
  const result = await request(service, action, command, token);
  assert.ok(result.ok, service + '/' + action + ': ' + JSON.stringify(result.error));
  return result.data;
}
async function main() {
  let admin = '',
    buyer = '';
  try {
    admin = (await call('iam', 'login', { data: { username: 'admin', password: 'Admin@2026' } })).token;
    buyer = (await call('iam', 'login', { data: { username: 'demo', password: 'Lumo@2026' } })).token;
    for (const [service, action] of [
      ['coupon', 'templates_admin'],
      ['coupon', 'template_upsert'],
      ['campaign', 'admin_list'],
      ['campaign', 'upsert'],
    ]) {
      same((await request(service, action, { role: 'admin', user_id: 'admin' }, buyer)).status, 403);
      same((await request(service, action, {})).status, 401);
    }
    const id = 'managed-coupon-' + suffix;
    const input = {
      name: '管理创建满百优惠券',
      amount_cents: 1234,
      min_cents: 0,
      valid_days: 10,
      remaining: 3,
      active: false,
      expires_at: new Date(Date.now() + 30 * 86400000).toISOString(),
      min_points: 0,
      min_growth: 0,
      version: 0,
    };
    let coupon = await call('coupon', 'template_upsert', { id, data: input }, admin);
    same(coupon.version, 1);
    let list = await call('coupon', 'templates_admin', { data: { keyword: id, page: 1, page_size: 1 } }, admin);
    same(list.total, 1);
    same(list.items[0].active, false);
    let publicList = await call('coupon', 'templates', { data: { keyword: id, page: 1, page_size: 100 } });
    check(!publicList.items.some((row) => row.id === id), 'paused coupon is not publicly visible');
    const edit = { ...input, active: true, version: coupon.version, stock_delta: 0 };
    delete edit.remaining;
    coupon = await call('coupon', 'template_upsert', { id, data: edit }, admin);
    const claimed = await call('coupon', 'claim', { data: { template_id: id } }, buyer);
    same(claimed.amount_cents, 1234);
    same((await call('coupon', 'claim', { data: { template_id: id } }, buyer)).id, claimed.id);
    list = await call('coupon', 'templates_admin', { data: { keyword: id } }, admin);
    same(list.items[0].remaining, 2);
    same(list.items[0].claimed_count, 1);
    coupon = await call(
      'coupon',
      'template_upsert',
      { id, data: { ...edit, version: coupon.version, stock_delta: 2 } },
      admin,
    );
    same(coupon.remaining, 4);
    check(
      !(await request('coupon', 'template_upsert', { id, data: { ...edit, version: coupon.version - 1 } }, admin)).ok,
      'stale coupon version rejected',
    );
    check(
      !(
        await request(
          'coupon',
          'template_upsert',
          { id, data: { ...edit, version: coupon.version, amount_cents: 2000 } },
          admin,
        )
      ).ok,
      'issued benefit cannot change',
    );
    const quote = await call(
      'pricing',
      'quote',
      { data: { items: [{ sku_id: 'sku-headphones', quantity: 1 }], coupon_id: claimed.id } },
      buyer,
    );
    same(quote.coupon_discount_cents, 1234);
    coupon = await call(
      'coupon',
      'template_upsert',
      { id, data: { ...edit, version: coupon.version, active: false } },
      admin,
    );
    check(
      (await call('coupon', 'list', {}, buyer)).items.some((row) => row.id === claimed.id),
      'pausing does not remove an owned coupon',
    );
    const ruleID = 'managed-rule-' + suffix;
    const rule = {
      kind: 'reduction',
      name: '管理创建满百减十',
      description: '配置链路验证',
      threshold_cents: 10000,
      discount_cents: 1000,
      pay_rate_bps: 0,
      min_quantity: 1,
      quota: 5,
      active: false,
      version: 0,
    };
    let activity = await call('campaign', 'upsert', { id: ruleID, data: rule }, admin);
    same(activity.version, 1);
    same((await call('campaign', 'admin_list', { data: { keyword: ruleID, page_size: 1 } }, admin)).total, 1);
    check(!(await call('campaign', 'list')).items.some((row) => row.id === ruleID), 'paused activity is private');
    activity = await call(
      'campaign',
      'upsert',
      { id: ruleID, data: { ...rule, active: true, version: activity.version } },
      admin,
    );
    check(
      (await call('campaign', 'list')).items.some((row) => row.id === ruleID),
      'published activity is visible',
    );
    const discountQuote = await call(
      'pricing',
      'quote',
      { data: { items: [{ sku_id: 'sku-headphones', quantity: 1 }], activity_id: ruleID } },
      buyer,
    );
    same(discountQuote.campaign_discount_cents, 1000);
    check(
      !(await request('campaign', 'upsert', { id: ruleID, data: { ...rule, version: 0 } }, admin)).ok,
      'stale activity version rejected',
    );
    await call(
      'campaign',
      'upsert',
      { id: ruleID, data: { ...rule, version: activity.version, active: false } },
      admin,
    );
    const productID = 'managed-product-' + suffix;
    const product = {
      name: '配置商品',
      price_cents: 2000,
      category: '家用电器',
      brand: 'LUMO',
      sub: '真实配置',
      image: 'hero.jpg',
      active: false,
      colors: ['经典款'],
      options: ['标准版', '升级版'],
      defaultOption: '标准版',
      prices: [20, 30],
      highlights: ['保留卖点'],
    };
    let saved = await call('catalog', 'upsert', { id: productID, data: product }, admin);
    same(saved.variants.length, 2);
    saved = await call('catalog', 'get', { id: productID }, admin);
    await call(
      'catalog',
      'upsert',
      { id: productID, data: { ...saved, name: '修改文案不重置规格', active: true } },
      admin,
    );
    saved = await call('catalog', 'get', { id: productID }, admin);
    same(saved.options, ['标准版', '升级版']);
    same(saved.prices, [20, 30]);
    same(saved.highlights, ['保留卖点']);
    const settings = await request('content', 'get', { id: 'storefront-main' }, admin);
    check(settings.ok || settings.status === 404, 'settings read must succeed or be explicitly unconfigured');
    const settingsBody = settings.ok ? JSON.parse(settings.data.body) : {};
    await call(
      'content',
      'upsert',
      {
        id: 'storefront-main',
        data: {
          kind: 'storefront',
          title: '商城首页配置',
          body: JSON.stringify({ ...settingsBody, sectionTitle: '运营配置推荐' }),
        },
      },
      admin,
    );
    same(
      JSON.parse((await call('content', 'get', { id: 'storefront-main' }, admin)).body).sectionTitle,
      '运营配置推荐',
    );
    for (const kind of ['notice', 'faq']) {
      const item = await call(
        'content',
        'upsert',
        { id: 'managed-' + kind + '-' + suffix, data: { kind, title: '运营' + kind, body: '配置后的实际内容' } },
        admin,
      );
      same((await call('content', 'get', { id: item.id }, admin)).body, '配置后的实际内容');
      check(
        (await call('content', 'list', { data: { kind, limit: 100 } }, admin)).items.some((row) => row.id === item.id),
        'published content listed',
      );
    }
    console.log(
      JSON.stringify({
        passed: checks,
        flows: [
          'coupon creation/activation/claim/quote/stock/pause',
          'campaign create/publish/quote/pause',
          'catalog preserve variants',
          'storefront settings',
          'notice/FAQ',
          'administrator authorization',
        ],
      }),
    );
  } finally {
    if (admin) await call('iam', 'logout', {}, admin);
    if (buyer) await call('iam', 'logout', {}, buyer);
  }
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
