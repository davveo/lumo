'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { createHarness } = require('./dom-harness.cjs');
const loadConstants = require('./load-constants.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const copy = (value) => JSON.parse(JSON.stringify(value));

test('generated business enums preserve protocol values and are deeply immutable', () => {
  const context = { window: {} };
  loadConstants(context, directory);
  const enums = context.window.LumoConstants;
  assert.equal(enums.OrderStatus.Creating, 'CREATING');
  assert.equal(enums.OrderStatus.Created, 'CREATED');
  assert.equal(enums.OrderStatus.AwaitingPayment, 'AWAITING_PAYMENT');
  assert.equal(enums.OrderStatus.Delivered, 'DELIVERED');
  assert.equal(enums.CouponStatus.Available, 'AVAILABLE');
  assert.equal(enums.CouponStatus.AvailableLegacy, 'available');
  assert.equal(enums.CouponClaimStatus.Available, 'available');
  assert.equal(enums.CatalogSort.Default, '');
  assert.equal(enums.Service.Order, 'order');
  assert.equal(enums.Action.List, 'list');
  assert.equal(enums.Event.OrderPaymentAccepted, 'OrderPaymentAccepted');
  assert.equal(enums.ApiErrorCode.SessionChanged, 'SessionChanged');
  assert.equal(enums.ApiErrorCode.NotFound, 'NotFound');
  assert.ok(Object.isFrozen(enums));
  for (const group of Object.values(enums)) assert.ok(Object.isFrozen(group));
  assert.throws(() => {
    enums.OrderStatus.Creating = 'changed';
  }, TypeError);
  const first = enums;
  loadConstants(context, directory);
  assert.equal(context.window.LumoConstants, first);
});

test('equal-valued domains can evolve independently without changing completed-order actions or payment success', async (t) => {
  const context = { window: {} };
  loadConstants(context, directory);
  const enums = copy(context.window.LumoConstants);
  // Deliberately diverge unrelated domains so equal bytes cannot conceal an
  // accidental dependency on an aftersale, flash, claim or RPC-action enum.
  enums.AftersaleViewStatus.Completed = 'aftersale-only';
  enums.FlashRequestStatus.Succeeded = 'flash-only';
  enums.CouponClaimStatus.Available = 'claim-only';
  enums.RefundStatus.Processing = 'refund-only';
  enums.CouponFilter.All = 'coupon-filter-only';
  enums.Action.Request = 'rpc-action-only';
  const item = { sku_id: 'test-sku', name: '商品', quantity: 1, unit_price_cents: 1200 };
  const order = {
    id: 'order',
    status: 'COMPLETED',
    payment_id: 'payment',
    created_at: '2026-10-01T00:00:00Z',
    items: [item],
    quote: { items: [item], subtotal_cents: 1200, payable_cents: 1200 },
    address: {},
  };
  const app = createHarness(directory, 'http://enum-domains.test', {
    globals: {
      LumoConstants: enums,
      LumoAuth: {
        user: () => ({ id: 'buyer' }),
        isLoggedIn: () => true,
        gate: () => true,
        requireLogin: () => true,
        afterRender() {},
      },
      LumoAPI: {
        getBase: () => '',
        getToken: () => 'session',
        async call(service, action) {
          if (service === 'order') return action === 'list' ? { items: [order] } : order;
          if (service === 'interaction')
            return { items: [{ id: 'review', order_id: 'order', sku_id: 'test-sku', status: 'PUBLISHED' }] };
          if (service === 'coupon')
            return { items: [{ id: 'coupon', status: 'AVAILABLE', expires_at: '2099-01-01T00:00:00Z' }] };
          if (service === 'invoice') return { items: [{ id: 'invoice', status: 'REQUESTED' }] };
          if (service === 'notification')
            return { items: [{ id: 'message', event_type: 'OrderCompleted', status: 'UNREAD' }] };
          return { items: [] };
        },
      },
    },
  });
  t.after(() => app.dispose());
  // Use the real modules with the isolated enum fixture instead of loading a
  // second copy of the generated frozen object through the normal harness.
  for (const file of ['app.js', 'member.js', 'member-views.js', 'services.js'])
    app.evaluate(fs.readFileSync(path.join(directory, file), 'utf8'));
  await app.ctx.LumoCommerce.refreshOrders();
  await app.ctx.LumoMember.refresh();
  await app.ctx.LumoServices.refresh();
  const html = app.ctx.LumoMemberView.page('orders', 'completed');
  assert.match(html, /查看评价/);
  assert.match(html, /申请售后/);
  assert.equal(app.ctx.LumoMember.coupons()[0].status, 'available');
  assert.equal(app.ctx.LumoServices.model().invoices[0].status, 'processing');
  assert.match(app.ctx.LumoServices.content('messages', 'all'), /订单已完成/);
  app.evaluate(
    'state.pending=normalizeOrder({...' +
      JSON.stringify(order) +
      ",status:'PENDING_PAYMENT'});commerceState.payment={id:'payment',order_id:'order',provider:'demo',status:'SUCCEEDED'}",
  );
  assert.match(app.navigate('payment'), /模拟支付已成功，正在确认订单/);
  await app.ctx.LumoCommerce.submitDemoPayment();
  assert.equal(app.ctx.location.hash, '#/success');
  app.evaluate(fs.readFileSync(path.join(directory, 'api-client.js'), 'utf8'));
  assert.match(app.ctx.LumoAPI.key(), /^request-/);
});

test('every production enum reference resolves to a generated member and both entry pages load it first', () => {
  const context = { window: {} };
  loadConstants(context, directory);
  const enums = context.window.LumoConstants;
  const wireStatuses = new Set(
    Object.values(enums)
      .flatMap((group) => Object.values(group))
      .filter((value) => /^[A-Z][A-Z0-9_]+$/.test(value)),
  );
  for (const filename of fs.readdirSync(directory).filter((name) => name.endsWith('.js') && name !== 'constants.js')) {
    const source = fs.readFileSync(path.join(directory, filename), 'utf8');
    for (const [, group, member] of source.matchAll(/\b(?:C|CommerceEnums|LumoConstants)\.([A-Z]\w*)\.([A-Z]\w*)\b/g))
      assert.equal(
        typeof enums[group]?.[member],
        'string',
        filename + ' references missing enum ' + group + '.' + member,
      );
    for (const [, , value] of source.matchAll(/(['"`])([A-Z][A-Z0-9_]+)\1/g))
      assert.ok(!wireStatuses.has(value), filename + ' must use its domain constant for ' + value);
  }
  for (const filename of ['index.html', 'admin.html']) {
    const scripts = [
      ...fs.readFileSync(path.join(directory, filename), 'utf8').matchAll(/<script\b[^>]*\bsrc="([^"]+)"/g),
    ].map((row) => row[1]);
    assert.equal(scripts.filter((name) => name === 'constants.js').length, 1);
    assert.ok(
      scripts.indexOf('constants.js') < scripts.indexOf('api-client.js'),
      filename + ' must load enums before API/domain code',
    );
  }
});

test('wire and view enums keep order compatibility, coupon states and notification projections distinct', async (t) => {
  const future = '2099-01-01T00:00:00Z';
  const app = createHarness(directory, 'http://constants.test', {
    globals: {
      LumoAuth: {
        user: () => ({ id: 'buyer' }),
        isLoggedIn: () => true,
        gate: () => true,
        requireLogin: () => true,
        afterRender() {},
      },
      LumoAPI: {
        getBase: () => '',
        getToken: () => 'session',
        async call(service) {
          if (service === 'coupon')
            return {
              items: [
                { id: 'available', status: 'AVAILABLE', expires_at: future },
                { id: 'held', status: 'HELD', expires_at: future },
                { id: 'reserved', status: 'RESERVED', expires_at: future },
                { id: 'used', status: 'USED', expires_at: future },
                { id: 'confirmed', status: 'CONFIRMED', expires_at: future },
                { id: 'expired', status: 'EXPIRED', expires_at: future },
              ],
            };
          if (service === 'notification')
            return {
              items: [
                { id: 'order', event_type: 'OrderCompleted', status: 'UNREAD' },
                { id: 'group', event_type: 'GroupFormed', status: 'READ' },
                { id: 'ticket', event_type: 'TicketReplied', status: 'UNREAD' },
              ],
            };
          if (service === 'invoice')
            return {
              items: [
                { id: 'issued', status: 'ISSUED', number: 'invoice-1' },
                { id: 'request', status: 'REQUESTED' },
              ],
            };
          if (service === 'helpdesk')
            return { items: [{ id: 'ticket', status: 'REPLIED', messages: [{ role: 'admin', body: '回复' }] }] };
          return { items: [] };
        },
      },
    },
  });
  t.after(() => app.dispose());
  for (const file of ['app.js', 'member.js', 'services.js']) app.load(file);
  const member = app.ctx.LumoMember;
  for (const wire of ['CREATING', 'CREATED', 'AWAITING_PAYMENT', 'PENDING_PAYMENT'])
    assert.equal(member.orderState({ status: wire }), 'pending', wire);
  assert.equal(member.orderState({ status: 'DELIVERED' }), 'review');
  assert.equal(member.orderState({ status: 'PAID_WAIT_GROUP' }), 'group');
  assert.equal(member.orderState({ status: 'REFUNDED' }), 'refunded');
  await member.refresh();
  assert.deepEqual(
    copy(member.coupons()).map((row) => row.status),
    ['available', 'frozen', 'frozen', 'used', 'used', 'expired'],
  );
  await app.ctx.LumoServices.refresh();
  const model = copy(app.ctx.LumoServices.model());
  assert.deepEqual(
    model.messages.map((row) => row.type),
    ['order', 'activity', 'service'],
  );
  assert.deepEqual(
    model.messages.map((row) => row.read),
    [false, true, false],
  );
  assert.deepEqual(
    model.invoices.map((row) => row.status),
    ['issued', 'processing'],
  );
  assert.equal(model.tickets[0].status, 'answered');
  assert.equal(model.tickets[0].messages[1].role, 'assistant');
});
