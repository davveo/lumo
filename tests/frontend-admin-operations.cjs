'use strict';
// All writes run against the isolated devstack started by test-frontend.sh.
const assert = require('node:assert/strict');
const base = process.env.API_BASE || 'http://127.0.0.1:18181';
const suffix = crypto.randomUUID().slice(0, 8);
let checks = 0;
const equal = (a, b) => {
  assert.deepEqual(a, b);
  checks++;
};
async function request(service, action, command = {}, token = '') {
  const response = await fetch(base + '/api/v1/' + service + '/' + action, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
    body: JSON.stringify(command),
  });
  const body = await response.json();
  assert.ok(response.headers.get('x-trace-id'));
  assert.ok(!('trace_id' in body));
  checks += 2;
  return { status: response.status, data: body.data, error: body.error };
}
async function call(service, action, command, token) {
  const value = await request(service, action, command, token);
  assert.equal(value.status, 200, service + '/' + action + ': ' + JSON.stringify(value.error));
  checks++;
  return value.data;
}
async function main() {
  let admin = '',
    buyer = '';
  try {
    admin = (await call('iam', 'login', { data: { username: 'admin', password: 'Admin@2026' } })).token;
    buyer = (await call('iam', 'login', { data: { username: 'demo', password: 'Lumo@2026' } })).token;
    for (const service of ['order', 'payment', 'fulfillment', 'aftersale', 'refund', 'invoice', 'iam']) {
      equal(
        (await request(service, 'admin_list', { role: 'admin', data: { page: 1, page_size: 20 } }, buyer)).status,
        403,
      );
      const page = await call(service, 'admin_list', { data: { page: 1, page_size: 20 } }, admin);
      assert.ok(Array.isArray(page.items));
      assert.ok(Number.isInteger(page.total));
      checks += 2;
    }
    equal((await request('management', 'dashboard', {}, buyer)).status, 403);
    for (const days of [7, 30]) {
      const stats = await call('management', 'dashboard', { data: { days } }, admin);
      equal(stats.degraded, false);
      equal(stats.days, days);
      equal(stats.timezone, 'Asia/Shanghai');
      for (const key of ['order', 'payment', 'refund', 'fulfillment', 'aftersale', 'invoice', 'users'])
        assert.ok(stats[key]);
    }
    equal((await request('management', 'dashboard', { data: { days: 365 } }, admin)).status, 400);
    const productID = 'ops-product-' + suffix;
    const product = {
      name: '运营测试耳机',
      price_cents: 10000,
      category: '手机数码',
      brand: 'LUMO',
      image: 'hero.jpg',
      active: true,
      colors: ['白色', '黑色'],
      options: ['标准版', '升级版'],
      defaultOption: '标准版',
      prices: [100, 150],
    };
    const saved = await call('catalog', 'upsert', { id: productID, data: product }, admin);
    equal(saved.variants.length, 4);
    await call(
      'inventory',
      'adjust',
      { id: productID, data: { on_hand: 30, expected_on_hand: 0, reason: '采购入库' } },
      admin,
    );
    const variant = saved.variants.find((v) => v.sku_id !== productID);
    await call(
      'inventory',
      'adjust',
      { id: variant.sku_id, data: { on_hand: 9, expected_on_hand: 0, reason: '规格备货' } },
      admin,
    );
    const stocks = await call('inventory', 'batch_get', { data: { ids: [productID, variant.sku_id] } }, admin);
    equal(stocks.items.length, 2);
    equal(stocks.items.find((row) => row.sku_id === variant.sku_id).available, 9);
    equal(
      (
        await request(
          'inventory',
          'adjust',
          { id: productID, data: { on_hand: 40, expected_on_hand: 0, reason: '旧页面提交' } },
          admin,
        )
      ).status,
      409,
    );
    const history = await call('inventory', 'adjustments', { id: productID }, admin);
    equal(history.items.length, 1);
    equal(history.items[0].before, 0);
    equal(history.items[0].after, 30);
    equal(history.items[0].reason, '采购入库');
    equal(history.items[0].actor_id, 'admin');
    const address = await call(
      'address',
      'create',
      {
        data: {
          recipient: '运营测试',
          phone: '13800138000',
          address: '测试街道 101',
          province: '广东省',
          city: '深圳市',
          district: '南山区',
        },
      },
      buyer,
    );
    for (const [index, type, amount, min, rate, cap, want] of [
      [0, 'reduction', 1000, 10000, 0, 0, 1000],
      [1, 'cash', 500, 0, 0, 0, 500],
      [2, 'discount', 0, 0, 8500, 0, 1500],
      [3, 'threshold_discount', 0, 5000, 7500, 1500, 1500],
    ]) {
      const id = 'ops-coupon-' + suffix + '-' + index;
      const input = {
        name: '运营' + type,
        coupon_type: type,
        amount_cents: amount,
        min_cents: min,
        discount_bps: rate,
        max_discount_cents: cap,
        scope_type: index === 3 ? 'category' : 'all',
        scope_values: index === 3 ? ['手机数码'] : [],
        valid_days: 7,
        remaining: 10,
        active: true,
        min_points: 0,
        min_growth: 0,
        starts_at: null,
        expires_at: new Date(Date.now() + 86400000).toISOString(),
        version: 0,
      };
      const template = await call('coupon', 'template_upsert', { id, data: input }, admin);
      equal(template.coupon_type, type);
      const coupon = await call('coupon', 'claim', { data: { template_id: id } }, buyer);
      equal(coupon.coupon_type, type);
      const quote = await call(
        'pricing',
        'quote',
        { data: { items: [{ sku_id: productID, quantity: 1 }], coupon_id: coupon.id } },
        buyer,
      );
      equal(quote.coupon_discount_cents, want);
      equal(quote.payable_cents, 10000 - want);
      const checkout = await call(
        'checkout',
        'submit',
        {
          idempotency_key: 'ops-submit-' + suffix + '-' + index,
          data: { quote_id: quote.quote_id, address_id: address.id },
        },
        buyer,
      );
      assert.ok(checkout.order_id);
      checks++;
      const order = await call('order', 'get', { id: checkout.order_id }, buyer);
      equal(order.payable_cents, 10000 - want);
      if (index < 3) {
        const payment = await call('payment', 'create', { id: checkout.order_id }, buyer);
        if (index !== 1) {
          await call('payment', 'confirm_demo', { id: payment.id }, admin);
          for (let attempt = 0; attempt < 50; attempt++) {
            const current = await call('order', 'get', { id: checkout.order_id }, buyer);
            if (current.status === 'READY_TO_FULFILL') break;
            await new Promise((resolve) => setTimeout(resolve, 100));
          }
          if (index === 0)
            await call(
              'aftersale',
              'create',
              {
                idempotency_key: 'ops-after-' + suffix,
                data: {
                  order_id: checkout.order_id,
                  type: 'refund',
                  reason: '运营验收申请',
                  lines: [{ sku_id: productID, quantity: 1 }],
                },
              },
              buyer,
            );
        }
      }
      const edit = { ...input, version: template.version, stock_delta: 0 };
      delete edit.remaining;
      if (index === 2) {
        edit.discount_bps = 7500;
        equal((await request('coupon', 'template_upsert', { id, data: edit }, admin)).status, 409);
      }
    }

    const config = {
      brand: 'OPS',
      heroInterval: 4,
      heroSlides: [
        {
          title: '周末好物',
          subtitle: '精选好物',
          eyebrow: 'WEEKEND',
          image: 'hero.jpg',
          button: '前往领券',
          link: '#/coupons',
        },
      ],
      showPicks: false,
      announcement: '周末福利上线',
      announcementLink: '#/coupons',
      footerText: '运营测试商城',
      contactPhone: '400-000-0000',
      contactHours: '09:00–18:00',
    };
    await call(
      'content',
      'upsert',
      { id: 'storefront-main', data: { kind: 'storefront', title: '首页配置', body: JSON.stringify(config) } },
      admin,
    );
    const stored = await call('content', 'get', { id: 'storefront-main' }, admin);
    equal(JSON.parse(stored.body).heroSlides.length, 1);
    equal(
      (
        await request(
          'content',
          'upsert',
          {
            id: 'storefront-main',
            data: {
              kind: 'storefront',
              title: '非法配置',
              body: JSON.stringify({
                ...config,
                heroSlides: [{ ...config.heroSlides[0], link: 'javascript:alert(1)' }],
              }),
            },
          },
          admin,
        )
      ).status,
      400,
    );
    equal(
      (
        await request(
          'iam',
          'set_active',
          { id: 'demo', data: { active: false, expected_active: true, reason: '测试停用' } },
          buyer,
        )
      ).status,
      403,
    );
    await call(
      'iam',
      'set_active',
      { id: 'demo', data: { active: false, expected_active: true, reason: '隔离回归' } },
      admin,
    );
    equal((await request('iam', 'current', {}, buyer)).status, 401);
    buyer = '';
    await call(
      'iam',
      'set_active',
      { id: 'demo', data: { active: true, expected_active: false, reason: '恢复隔离用户' } },
      admin,
    );
    buyer = (await call('iam', 'login', { data: { username: 'demo', password: 'Lumo@2026' } })).token;
  } finally {
    if (buyer) await request('iam', 'logout', {}, buyer);
    if (admin) await request('iam', 'logout', {}, admin);
  }
  console.log('Admin operations checks passed:', checks);
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
