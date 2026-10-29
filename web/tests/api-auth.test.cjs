const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const crypto = require('node:crypto').webcrypto;
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const scripts = (process.env.FRONTEND_DIR || path.join(__dirname, '..', 'dist')) + path.sep;
const future = new Date(Date.now() + 86400000).toISOString();
const tokenA = 'a'.repeat(64),
  tokenB = 'b'.repeat(64);
function storage() {
  const values = new Map();
  return {
    getItem: (k) => values.get(k) ?? null,
    setItem: (k, v) => values.set(k, String(v)),
    removeItem: (k) => values.delete(k),
    values,
  };
}
function environment() {
  const listeners = new Map();
  const ctx = {
    URL,
    AbortController,
    Uint8Array,
    setTimeout,
    clearTimeout,
    setInterval(fn, ms) {
      const timer = setInterval(fn, ms);
      timer.unref();
      return timer;
    },
    clearInterval,
    console,
    crypto,
    CustomEvent: class {
      constructor(type, options) {
        this.type = type;
        this.detail = options?.detail;
      }
    },
    location: { origin: 'http://localhost:8080', hash: '#/', reload() {} },
    document: {
      addEventListener() {},
      querySelector() {
        return null;
      },
      getElementById() {
        return null;
      },
    },
    localStorage: storage(),
    sessionStorage: storage(),
    state: { cart: [], orders: [], favorites: [] },
    renderKeepScroll() {},
    toast() {},
    addEventListener(type, fn) {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(fn);
    },
    dispatchEvent(event) {
      for (const fn of listeners.get(event.type) || []) fn(event);
    },
    async fetch() {
      throw new Error('fetch not stubbed');
    },
  };
  ctx.window = ctx;
  return vm.createContext(ctx);
}
function load(ctx, filename) {
  require('./load-constants.cjs')(ctx, scripts);
  vm.runInContext(fs.readFileSync(scripts + filename, 'utf8'), ctx, { filename });
}
function response(status, body, trace = 'c'.repeat(32)) {
  return {
    status,
    ok: status < 400,
    headers: { get: () => trace },
    async json() {
      return body;
    },
  };
}
let assertions = 0;
async function main() {
  const ctx = environment();
  load(ctx, 'api-client.js');
  let received;
  ctx.LumoAPI.setToken(tokenA);
  ctx.fetch = async (url, options) => {
    received = { url, options };
    return response(200, { data: { id: 'sku-coffee' } });
  };
  assert.equal(
    (
      await ctx.LumoAPI.call(
        'catalog',
        'get',
        { id: 'sku-coffee', user_id: 'attacker', role: 'admin' },
        { auth: false },
      )
    ).id,
    'sku-coffee',
  );
  assertions++;
  assert.equal(received.url, '/api/v1/catalog/get');
  assertions++;
  assert.equal(received.options.headers.Authorization, undefined);
  assertions++;
  assert.equal(received.options.credentials, 'same-origin');
  assertions++;
  assert.equal(JSON.parse(received.options.body).role, undefined);
  assertions++;
  assert.match(received.options.headers.traceparent, /^00-[a-f0-9]{32}-[a-f0-9]{16}-01$/);
  assertions++;
  await ctx.LumoAPI.call('checkout', 'submit', { idempotency_key: 'stable-001', data: { quote_id: 'q' } });
  assert.equal(received.options.headers.Authorization, 'Bearer ' + tokenA);
  assertions++;
  assert.equal(received.options.headers['Idempotency-Key'], 'stable-001');
  assertions++;
  ctx.fetch = async () =>
    response(401, {
      error: { code: 'Unauthenticated', message: 'session revoked or expired' },
    });
  await assert.rejects(
    ctx.LumoAPI.call('reconciliation', 'list', {}, { token: tokenB }),
    (error) => error.status === 401 && error.traceId.length === 32,
  );
  assertions++;
  assert.equal(ctx.LumoAPI.getToken(), tokenA);
  assertions++;
  await assert.rejects(ctx.LumoAPI.call('order', 'list'), /登录状态已失效/);
  assertions++;
  assert.equal(ctx.LumoAPI.getToken(), '');
  assertions++;
  assert.equal(ctx.LumoAPI.requests()[0].ok, false);
  assertions++;
  assert.equal(JSON.stringify(ctx.LumoAPI.requests()).includes(tokenA), false);
  assertions++;
  ctx.fetch = async () => ({
    status: 200,
    ok: true,
    headers: { get() {} },
    async json() {
      throw new Error('html');
    },
  });
  await assert.rejects(ctx.LumoAPI.call('catalog', 'list'), /未提供有效接口/);
  assertions++;
  assert.throws(() => ctx.LumoAPI.setBase('https://name:secret@example.com'), /格式不正确/);
  assertions++;

  ctx.LumoAPI.setToken(tokenA);
  let resolveOld;
  ctx.fetch = () =>
    new Promise((resolve) => {
      resolveOld = resolve;
    });
  const oldRequest = ctx.LumoAPI.call('order', 'list');
  ctx.LumoAPI.setToken(tokenB);
  resolveOld(response(200, { data: { items: [{ id: 'old-user-private-order' }] } }));
  await assert.rejects(oldRequest, (error) => error.code === 'SessionChanged');
  assertions++;
  assert.equal(ctx.LumoAPI.getToken(), tokenB);
  assertions++;

  const headerTrace = 'd'.repeat(32);
  ctx.fetch = async () => response(200, { data: { id: 'sku-coffee' }, trace_id: 'legacy-body-field' }, headerTrace);
  await ctx.LumoAPI.call('catalog', 'get', { id: 'sku-coffee' }, { auth: false });
  assert.equal(
    ctx.LumoAPI.requests()[0].traceId,
    headerTrace,
    'a body field cannot override the trace response header',
  );
  assertions++;
  ctx.fetch = async () =>
    response(503, { error: { code: 'Unavailable', message: 'retry later' }, trace_id: 'legacy-body-field' }, '');
  await assert.rejects(
    ctx.LumoAPI.call('catalog', 'get', { id: 'sku-coffee' }, { auth: false }),
    (error) => error.traceId === '',
  );
  assertions++;

  const auth = environment();
  auth.localStorage.setItem(
    'lumo-api-session-v2',
    JSON.stringify({
      token: tokenA,
      userId: 'forged',
      role: 'admin',
      expiresAt: Date.now() + 86400000,
      issuedAt: Date.now(),
      remember: true,
    }),
  );
  auth.localStorage.setItem('lumo-account-demo::lumo-mall-v1', JSON.stringify({ orders: [{ id: 'fake-order' }] }));
  load(auth, 'api-client.js');
  load(auth, 'auth.js');
  assert.equal(auth.LumoAuth.isLoggedIn(), false);
  assertions++;
  assert.equal(auth.LumoAuth.getItem('lumo-mall-v1'), null);
  assertions++;
  auth.fetch = async (url, options) => {
    received = { url, options };
    return response(200, { data: { user_id: 'demo', role: 'user', expires_at: future, session_valid: true } });
  };
  await auth.LumoAuth.refresh();
  assert.equal(auth.LumoAuth.user().id, 'demo');
  assertions++;
  assert.equal(auth.LumoAuth.user().role, 'user');
  assertions++;
  assert.equal(received.url, '/api/v1/iam/current');
  assertions++;
  assert.equal(received.options.headers.Authorization, 'Bearer ' + tokenA);
  assertions++;
  auth.fetch = async () => {
    throw new Error('offline');
  };
  await auth.LumoAuth.confirmLogout();
  assert.equal(auth.LumoAuth.isLoggedIn(), true, 'failed server revoke must not pretend logout succeeded');
  assertions++;
  auth.fetch = async (url, options) => {
    received = { url, options };
    return response(200, { data: { revoked: true } });
  };
  await auth.LumoAuth.confirmLogout();
  assert.equal(received.url, '/api/v1/iam/logout');
  assertions++;
  assert.equal(auth.LumoAuth.isLoggedIn(), false);
  assertions++;
  assert.equal(auth.LumoAPI.getToken(), '');
  assertions++;
  auth.fetch = async () => response(401, { error: { message: 'invalid credentials', code: 'Unauthenticated' } });
  await assert.rejects(auth.LumoAuth.login('demo', 'wrong'), /账号或密码不正确/);
  assertions++;
  assert.equal(auth.LumoAuth.isLoggedIn(), false);
  assertions++;
  auth.fetch = async (url, options) => {
    received = { url, options };
    return response(200, { data: { token: tokenB, user_id: 'admin', role: 'admin', expires_at: future } });
  };
  await auth.LumoAuth.login('admin', 'TypedPassword', true);
  assert.equal(JSON.parse(received.options.body).data.password, 'TypedPassword');
  assertions++;
  assert.equal(auth.LumoAuth.user().role, 'admin');
  assertions++;
  assert.equal(auth.localStorage.getItem('lumo-api-session-v2').includes('TypedPassword'), false);
  assertions++;
  auth.LumoAuth.setItem('lumo-mall-v1', JSON.stringify({ orders: [{ id: 'transient' }] }));
  auth.LumoAPI.setBase('https://api.example.com');
  assert.equal(auth.LumoAuth.isLoggedIn(), false);
  assertions++;
  assert.equal(auth.LumoAuth.getItem('lumo-mall-v1'), null);
  assertions++;
  auth.LumoAuth.requireLogin('#/promotions/group/group%3Ademo%3Ainvite');
  assert.equal(auth.LumoAuth.model().returnTo, '#/promotions/group/group%3Ademo%3Ainvite');
  assertions++;

  for (const oldStatus of [200, 401, 503]) {
    const switched = environment();
    switched.sessionStorage.setItem(
      'lumo-api-session-v2',
      JSON.stringify({
        token: tokenA,
        userId: 'old-user',
        role: 'user',
        expiresAt: Date.now() + 86400000,
        issuedAt: Date.now(),
      }),
    );
    load(switched, 'api-client.js');
    load(switched, 'auth.js');
    let resolveRefresh;
    switched.fetch = async (url) => {
      if (url.endsWith('/iam/current'))
        return new Promise((resolve) => {
          resolveRefresh = resolve;
        });
      return response(200, { data: { token: tokenB, user_id: 'new-user', role: 'admin', expires_at: future } });
    };
    const oldRefresh = switched.LumoAuth.refresh();
    await switched.LumoAuth.login('new-user', 'password', true);
    resolveRefresh(
      response(
        oldStatus,
        oldStatus === 200
          ? { data: { user_id: 'old-user', role: 'user', expires_at: future } }
          : { error: { message: 'old request failed', code: oldStatus === 401 ? 'Unauthenticated' : 'Unavailable' } },
      ),
    );
    assert.equal(await oldRefresh, null, 'a completed refresh for a replaced session is ignored');
    assertions++;
    assert.equal(switched.LumoAuth.isLoggedIn(), true, 'an old response must preserve the new login');
    assertions++;
    assert.equal(switched.LumoAuth.user().id, 'new-user');
    assertions++;
    assert.equal(switched.LumoAuth.user().role, 'admin');
    assertions++;
    assert.equal(switched.LumoAuth.model().error, '');
    assertions++;
    assert.equal(switched.LumoAPI.getToken(), tokenB);
    assertions++;
    assert.equal(JSON.parse(switched.localStorage.getItem('lumo-api-session-v2')).token, tokenB);
    assertions++;
  }

  const checkout = createHarness(scripts, 'http://localhost:8080');
  let checkoutUser = 'demo';
  checkout.ctx.LumoAuth = {
    user: () => ({ id: checkoutUser }),
    isLoggedIn: () => true,
    requireLogin: () => true,
    gate: () => true,
    afterRender() {},
  };
  checkout.load('api-client.js');
  checkout.load('app.js');
  try {
    checkout.ctx.sessionStorage.setItem(
      'lumo-submit-v2::demo',
      JSON.stringify({ data: { quote_id: 'unknown-gateway-quote' } }),
    );
    assert.equal(
      checkout.evaluate('readSubmitAttempt()'),
      null,
      'legacy requests without a gateway must not be replayed',
    );
    assertions++;
    checkout.ctx.LumoAPI.setBase('https://one.example.test/api/v1/');
    checkout.evaluate("storeSubmitAttempt({key:'request-one',data:{quote_id:'quote-one'}})");
    checkout.ctx.LumoAPI.setBase('https://two.example.test');
    assert.equal(
      checkout.evaluate('readSubmitAttempt()'),
      null,
      'a matching user on another gateway has no pending request',
    );
    assertions++;
    await checkout.ctx.LumoCommerce.resumeSubmit();
    assert.equal(
      checkout.ctx.LumoAPI.requests().length,
      0,
      'switching gateway must not replay the old quote or submission',
    );
    assertions++;
    checkout.evaluate("storeSubmitAttempt({key:'request-two',data:{quote_id:'quote-two'}})");
    checkout.ctx.LumoAPI.setBase('https://one.example.test');
    assert.equal(
      checkout.evaluate('readSubmitAttempt().data.quote_id'),
      'quote-one',
      'the original gateway retains its recovery request',
    );
    assertions++;
    checkoutUser = 'another-user';
    assert.equal(
      checkout.evaluate('readSubmitAttempt()'),
      null,
      'another user on the same gateway has no pending request',
    );
    assertions++;
    checkoutUser = 'demo';
    checkout.evaluate('clearSubmitAttempt()');
    checkout.ctx.LumoAPI.setBase('https://two.example.test');
    assert.equal(
      checkout.evaluate('readSubmitAttempt().data.quote_id'),
      'quote-two',
      'clearing one gateway must preserve the other gateway request',
    );
    assertions++;
    checkout.ctx.LumoAPI.setBase('');
    checkout.evaluate("storeSubmitAttempt({key:'same-origin',data:{quote_id:'local-quote'}})");
    checkout.ctx.LumoAPI.setBase(checkout.ctx.location.origin + '/api/v1/');
    assert.equal(
      checkout.evaluate('readSubmitAttempt().key'),
      'same-origin',
      'equivalent same-origin gateway URLs share recovery',
    );
    assertions++;

    const quote = {
      quote_id: 'quote-retry',
      expires_at: future,
      subtotal_cents: 100,
      payable_cents: 100,
      shipping_cents: 0,
      items: [{ sku_id: 'sku-test', quantity: 1, unit_price_cents: 100 }],
    };
    const order = {
      id: 'order-retry',
      status: 'PENDING_PAYMENT',
      payable_cents: 100,
      created_at: new Date().toISOString(),
      expires_at: future,
      quote,
      items: quote.items,
      address: {},
    };
    const submitted = [];
    let accepted = false;
    checkout.ctx.fetch = async (url, init) => {
      if (url.endsWith('/coupon/calculate') || url.endsWith('/pricing/get')) return response(200, { data: quote });
      if (url.endsWith('/logistics/quote')) return response(200, { data: { serviceable: true, shipping_cents: 0 } });
      if (url.endsWith('/checkout/submit')) {
        submitted.push(JSON.parse(init.body));
        if (!accepted) {
          accepted = true;
          const timeout = new Error('response lost after the server accepted the order');
          timeout.name = 'AbortError';
          throw timeout;
        }
        return response(200, { data: { order_id: order.id } });
      }
      if (url.endsWith('/order/get')) return response(200, { data: order });
      if (url.endsWith('/order/list')) return response(200, { data: { items: [order] } });
      throw new Error('Unexpected recovery request: ' + url);
    };
    checkout.evaluate(
      "clearSubmitAttempt();products.push(normalizeProduct({id:'sku-test',name:'Test item',price_cents:100}));state.cart=[{id:'sku-test',sku_id:'sku-test',qty:1,selected:true}];state.address={id:'address-test'}",
    );
    await checkout.ctx.LumoCommerce.submit();
    assert.match(
      checkout.ctx.LumoCommerce.state.quoteError,
      /请求超时/,
      'a timed-out submission must remain recoverable',
    );
    assertions++;
    assert.equal(
      checkout.evaluate('readSubmitAttempt().data.quote_id'),
      quote.quote_id,
      'a lost response must retain the original quote',
    );
    assertions++;
    const retainedKey = checkout.evaluate('readSubmitAttempt().key');
    await checkout.ctx.LumoCommerce.resumeSubmit();
    assert.equal(submitted.length, 2);
    assertions++;
    assert.equal(submitted[0].idempotency_key, retainedKey);
    assertions++;
    assert.deepEqual(submitted[1], submitted[0], 'recovery must send the identical quote and idempotency key');
    assertions++;
    assert.equal(checkout.evaluate('readSubmitAttempt()'), null, 'an observed order clears only its recovery request');
    assertions++;
    assert.equal(checkout.json('state.pending').id, order.id);
    assertions++;
  } finally {
    checkout.dispose();
  }
  console.log('PASS frontend API/auth: ' + assertions + ' assertions');
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
