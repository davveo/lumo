'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const {createHarness} = require('./dom-harness.cjs');

const directory = process.env.FRONTEND_DIR || path.join(__dirname, '..', 'dist');
const tick = () => new Promise(resolve => setImmediate(resolve));
function deferred() {
  let resolve;
  const promise = new Promise(yes => { resolve = yes; });
  return {promise, resolve};
}
async function until(predicate, description) {
  for (let attempt = 0; attempt < 50; attempt++) {
    if (predicate()) return;
    await tick();
  }
  assert.fail('Did not observe ' + description);
}
function order(id = 'order-a', user = 'buyer-a') {
  const items = [{sku_id: 'sku-test', name: 'Payment test product', quantity: 1, unit_price_cents: 1200}];
  return {id, user_id: user, status: 'PENDING_PAYMENT', payable_cents: 1200,
    created_at: new Date().toISOString(), expires_at: new Date(Date.now() + 60000).toISOString(),
    quote: {quote_id: 'quote-' + id, subtotal_cents: 1200, payable_cents: 1200, items}, items, address: {}};
}
function setup(t) {
  const app = createHarness(directory, 'http://localhost:8080');
  t.after(() => app.dispose());
  let user = 'buyer-a';
  app.ctx.LumoAuth = {user: () => ({id: user, role: 'user'}), isLoggedIn: () => true,
    requireLogin: () => true, gate: () => true, afterRender() {}};
  app.load('api-client.js');
  const server = {order: order(), payment: null};
  const calls = [];
  const respond = async (service, action, command) => {
    if (service === 'payment' && action === 'create') {
      server.payment = {id: 'pay-' + command.id, order_id: command.id, user_id: user,
        provider: 'demo', status: 'PENDING', amount_cents: 1200};
      return {...server.payment};
    }
    if (service === 'payment' && action === 'submit_demo') {
      server.payment = {...server.payment, status: 'SUCCEEDED'};
      server.order = {...server.order, status: 'READY_TO_FULFILL', payment_id: command.id, paid_at: new Date().toISOString()};
      return {...server.payment};
    }
    if (service === 'payment' && action === 'get') return {...server.payment};
    if (service === 'order' && action === 'get') return {...server.order};
    if (service === 'order' && action === 'list') return {items: [{...server.order}]};
    if (service === 'cart' && action === 'list') return {items: []};
    throw new Error('Unexpected API request: ' + service + '/' + action);
  };
  let handler = respond;
  app.ctx.LumoAPI.call = async (service, action, command = {}) => {
    calls.push({route: service + '/' + action, command: JSON.parse(JSON.stringify(command))});
    return handler(service, action, command);
  };
  app.load('app.js');
  const setPending = value => app.evaluate('state.pending=normalizeOrder(' + JSON.stringify(value) + ')');
  setPending(server.order);
  app.navigate('payment');
  return {app, server, calls, respond, setPending,
    handle(fn) { handler = fn; },
    switchSession(nextUser = user, base) {
      user = nextUser;
      if (base) app.ctx.LumoAPI.setBase(base);
      app.ctx.dispatchEvent(new app.ctx.CustomEvent('lumo:login'));
    }};
}

test('creating a payment request alone remains pending and never reports success', async t => {
  const {app, calls} = setup(t);
  await app.ctx.LumoCommerce.createPayment();
  assert.equal(app.ctx.LumoCommerce.state.payment.status, 'PENDING');
  assert.equal(app.json('state.pending').backendStatus, 'PENDING_PAYMENT');
  assert.equal(app.ctx.location.hash, '#/payment');
  assert.equal(calls.filter(call => call.route === 'payment/submit_demo').length, 0);
});

test('the pay button submits the demo receipt and waits for server order acceptance', async t => {
  const fixture = setup(t), {app, calls, server} = fixture;
  const acceptedOrder = deferred();
  fixture.handle((service, action, command) => service === 'order' && action === 'get'
    ? acceptedOrder.promise : fixture.respond(service, action, command));
  await app.click({action: 'pay'});
  await until(() => calls.some(call => call.route === 'order/get'), 'the server order confirmation request');
  assert.deepEqual(calls.slice(0, 2).map(call => call.route), ['payment/create', 'payment/submit_demo']);
  assert.equal(app.ctx.LumoCommerce.state.payment.status, 'SUCCEEDED');
  assert.equal(app.ctx.location.hash, '#/payment', 'a successful provider receipt is insufficient for success navigation');
  assert.equal(app.json('state.pending').backendStatus, 'PENDING_PAYMENT');
  acceptedOrder.resolve({...server.order});
  await until(() => app.ctx.location.hash === '#/success' && !app.ctx.LumoCommerce.state.paying, 'server-confirmed success');
  assert.equal(app.ctx.LumoCommerce.allOrders().find(row => row.id === server.order.id).payment_id, server.payment.id);
  assert.equal(calls.filter(call => call.route === 'cart/list').length, 1, 'accepted payment refreshes the backend cart');
  assert.equal(app.failures.length, 0);
});

test('failed demo submission keeps the payment for retry and does not fabricate success', async t => {
  const fixture = setup(t), {app, calls} = fixture;
  let fail = true;
  fixture.handle((service, action, command) => {
    if (service === 'payment' && action === 'submit_demo' && fail) throw new Error('模拟渠道暂时不可用');
    return fixture.respond(service, action, command);
  });
  await app.ctx.LumoCommerce.submitDemoPayment();
  assert.equal(app.ctx.location.hash, '#/payment');
  assert.equal(app.ctx.LumoCommerce.state.payment.status, 'PENDING');
  assert.equal(app.ctx.LumoCommerce.state.paying, false);
  assert.match(app.ctx.LumoCommerce.state.paymentError, /模拟渠道暂时不可用/);
  assert.equal(calls.filter(call => call.route === 'order/list').length, 0);
  const paymentID = app.ctx.LumoCommerce.state.payment.id;
  fail = false;
  await app.ctx.LumoCommerce.submitDemoPayment();
  assert.equal(app.ctx.location.hash, '#/success');
  assert.equal(app.ctx.LumoCommerce.state.paymentError, '');
  assert.equal(calls.filter(call => call.route === 'payment/create').length, 1);
  assert.deepEqual(calls.filter(call => call.route === 'payment/submit_demo').map(call => call.command.id), [paymentID, paymentID]);
});

test('repeated pay clicks share the in-flight operation instead of submitting twice', async t => {
  const fixture = setup(t), {app, calls} = fixture;
  const created = deferred();
  fixture.handle((service, action, command) => service === 'payment' && action === 'create'
    ? created.promise : fixture.respond(service, action, command));
  const first = app.ctx.LumoCommerce.submitDemoPayment();
  await app.ctx.LumoCommerce.submitDemoPayment();
  assert.equal(calls.filter(call => call.route === 'payment/create').length, 1);
  created.resolve(await fixture.respond('payment', 'create', {id: fixture.server.order.id}));
  await first;
  assert.equal(calls.filter(call => call.route === 'payment/submit_demo').length, 1);
  assert.equal(app.ctx.location.hash, '#/success');
});

for (const mode of ['different buyer', 'same buyer new session', 'different gateway']) {
  test('late payment creation cannot continue after ' + mode, async t => {
    const fixture = setup(t), {app, calls} = fixture;
    const created = deferred();
    fixture.handle((service, action, command) => service === 'payment' && action === 'create'
      ? created.promise : fixture.respond(service, action, command));
    const oldOperation = app.ctx.LumoCommerce.submitDemoPayment();
    const newUser = mode === 'different buyer' ? 'buyer-b' : 'buyer-a';
    fixture.switchSession(newUser, mode === 'different gateway' ? 'https://another.example.test' : undefined);
    fixture.setPending(order('order-b', newUser));
    created.resolve({id: 'old-payment', order_id: 'order-a', user_id: 'buyer-a', provider: 'demo', status: 'PENDING'});
    await oldOperation;
    assert.equal(calls.filter(call => call.route === 'payment/submit_demo').length, 0);
    assert.equal(calls.filter(call => call.route === 'order/get').length, 0);
    assert.equal(app.ctx.LumoCommerce.state.payment, null);
    assert.equal(app.ctx.LumoCommerce.state.paymentError, '');
    assert.equal(app.ctx.LumoCommerce.state.paying, false);
    assert.equal(app.json('state.pending').id, 'order-b');
    assert.equal(app.ctx.location.hash, '#/payment');
  });
}

test('late successful receipt cannot replace a new buyer payment state', async t => {
  const fixture = setup(t), {app, calls} = fixture;
  const submitted = deferred();
  fixture.handle((service, action, command) => service === 'payment' && action === 'submit_demo'
    ? submitted.promise : fixture.respond(service, action, command));
  const oldOperation = app.ctx.LumoCommerce.submitDemoPayment();
  await until(() => calls.some(call => call.route === 'payment/submit_demo'), 'demo receipt submission');
  fixture.switchSession('buyer-b');
  fixture.setPending(order('order-b', 'buyer-b'));
  app.ctx.LumoCommerce.state.payment = {id: 'pay-order-b', order_id: 'order-b', status: 'PENDING', provider: 'demo'};
  submitted.resolve({id: 'pay-order-a', order_id: 'order-a', status: 'SUCCEEDED', provider: 'demo'});
  await oldOperation;
  assert.equal(app.ctx.LumoCommerce.state.payment.id, 'pay-order-b');
  assert.equal(app.ctx.LumoCommerce.state.payment.status, 'PENDING');
  assert.equal(calls.filter(call => call.route === 'order/get').length, 0);
  assert.equal(app.ctx.location.hash, '#/payment');
});

test('resuming another order never submits a payment retained from the previous order', async t => {
  const {app, calls} = setup(t);
  app.ctx.LumoCommerce.state.payment = {id: 'other-payment', order_id: 'other-order', status: 'PENDING', provider: 'demo'};
  const view = app.navigate('payment');
  assert.equal(view.includes('other-payment'), false);
  await app.ctx.LumoCommerce.submitDemoPayment();
  assert.deepEqual(calls.filter(call => call.route === 'payment/create').map(call => call.command.id), ['order-a']);
  assert.deepEqual(calls.filter(call => call.route === 'payment/submit_demo').map(call => call.command.id), ['pay-order-a']);
  assert.equal(calls.some(call => call.command.id === 'other-payment'), false);
  assert.equal(app.ctx.location.hash, '#/success');
});
