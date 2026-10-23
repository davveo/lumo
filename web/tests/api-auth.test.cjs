const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const crypto = require('node:crypto').webcrypto;
const path = require('node:path');
const scripts = (process.env.FRONTEND_DIR || path.join(__dirname, '..', 'dist')) + path.sep;
const future = new Date(Date.now() + 86400000).toISOString();
const tokenA = 'a'.repeat(64), tokenB = 'b'.repeat(64);
function storage() {
  const values = new Map();
  return { getItem: k => values.get(k) ?? null, setItem: (k, v) => values.set(k, String(v)), removeItem: k => values.delete(k), values };
}
function environment() {
  const listeners = new Map();
  const ctx = { URL, AbortController, Uint8Array, setTimeout, clearTimeout, console, crypto,
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options?.detail; } },
    location: { origin: 'http://localhost:8080', hash: '#/', reload() {} },
    document: { addEventListener() {}, querySelector() { return null; }, getElementById() { return null; } },
    localStorage: storage(), sessionStorage: storage(), state: { cart: [], orders: [], favorites: [] },
    renderKeepScroll() {}, toast() {},
    addEventListener(type, fn) { if (!listeners.has(type)) listeners.set(type, []); listeners.get(type).push(fn); },
    dispatchEvent(event) { for (const fn of listeners.get(event.type) || []) fn(event); },
    async fetch() { throw new Error('fetch not stubbed'); }
  };
  ctx.window = ctx;
  return vm.createContext(ctx);
}
function load(ctx, filename) { vm.runInContext(fs.readFileSync(scripts + filename, 'utf8'), ctx, { filename }); }
function response(status, body, trace = 'c'.repeat(32)) { return { status, ok: status < 400, headers: { get: () => trace }, async json() { return body; } }; }
let assertions = 0;
async function main() {
  const ctx = environment(); load(ctx, 'api-client.js');
  let received;
  ctx.LumoAPI.setToken(tokenA);
  ctx.fetch = async (url, options) => { received = { url, options }; return response(200, { data: { id: 'sku-coffee' }, trace_id: 'c'.repeat(32) }); };
  assert.equal((await ctx.LumoAPI.call('catalog', 'get', { id: 'sku-coffee', user_id: 'attacker', role: 'admin' }, { auth: false })).id, 'sku-coffee'); assertions++;
  assert.equal(received.url, '/api/v1/catalog/get'); assertions++;
  assert.equal(received.options.headers.Authorization, undefined); assertions++;
  assert.equal(received.options.credentials, 'omit'); assertions++;
  assert.equal(JSON.parse(received.options.body).role, undefined); assertions++;
  assert.match(received.options.headers.traceparent, /^00-[a-f0-9]{32}-[a-f0-9]{16}-01$/); assertions++;
  await ctx.LumoAPI.call('checkout', 'submit', { idempotency_key: 'stable-001', data: { quote_id: 'q' } });
  assert.equal(received.options.headers.Authorization, 'Bearer ' + tokenA); assertions++;
  assert.equal(received.options.headers['Idempotency-Key'], 'stable-001'); assertions++;
  ctx.fetch = async () => response(401, { error: { code: 'Unauthenticated', message: 'session revoked or expired' }, trace_id: 'c'.repeat(32) });
  await assert.rejects(ctx.LumoAPI.call('reconciliation', 'list', {}, { token: tokenB }), error => error.status === 401 && error.traceId.length === 32); assertions++;
  assert.equal(ctx.LumoAPI.getToken(), tokenA); assertions++;
  await assert.rejects(ctx.LumoAPI.call('order', 'list'), /登录状态已失效/); assertions++;
  assert.equal(ctx.LumoAPI.getToken(), ''); assertions++;
  assert.equal(ctx.LumoAPI.requests()[0].ok, false); assertions++;
  assert.equal(JSON.stringify(ctx.LumoAPI.requests()).includes(tokenA), false); assertions++;
  ctx.fetch = async () => ({ status: 200, ok: true, headers: { get() {} }, async json() { throw new Error('html'); } });
  await assert.rejects(ctx.LumoAPI.call('catalog', 'list'), /未提供有效接口/); assertions++;
  assert.throws(() => ctx.LumoAPI.setBase('https://name:secret@example.com'), /格式不正确/); assertions++;

  ctx.LumoAPI.setToken(tokenA);
  let resolveOld;
  ctx.fetch = () => new Promise(resolve => { resolveOld = resolve; });
  const oldRequest = ctx.LumoAPI.call('order', 'list');
  ctx.LumoAPI.setToken(tokenB);
  resolveOld(response(200, {data:{items:[{id:'old-user-private-order'}]}}));
  await assert.rejects(oldRequest, error => error.code === 'SessionChanged'); assertions++;
  assert.equal(ctx.LumoAPI.getToken(), tokenB); assertions++;

  const auth = environment();
  auth.localStorage.setItem('lumo-api-session-v2', JSON.stringify({ token: tokenA, userId: 'forged', role: 'admin', expiresAt: Date.now() + 86400000, issuedAt: Date.now(), remember: true }));
  auth.localStorage.setItem('lumo-account-demo::lumo-mall-v1', JSON.stringify({ orders: [{ id: 'fake-order' }] }));
  load(auth, 'api-client.js'); load(auth, 'auth.js');
  assert.equal(auth.LumoAuth.isLoggedIn(), false); assertions++;
  assert.equal(auth.LumoAuth.getItem('lumo-mall-v1'), null); assertions++;
  auth.fetch = async (url, options) => { received = { url, options }; return response(200, { data: { user_id: 'demo', role: 'user', expires_at: future, session_valid: true } }); };
  await auth.LumoAuth.refresh();
  assert.equal(auth.LumoAuth.user().id, 'demo'); assertions++;
  assert.equal(auth.LumoAuth.user().role, 'user'); assertions++;
  assert.equal(received.url, '/api/v1/iam/current'); assertions++;
  assert.equal(received.options.headers.Authorization, 'Bearer ' + tokenA); assertions++;
  auth.fetch = async () => { throw new Error('offline'); };
  await auth.LumoAuth.confirmLogout();
  assert.equal(auth.LumoAuth.isLoggedIn(), true, 'failed server revoke must not pretend logout succeeded'); assertions++;
  auth.fetch = async (url, options) => { received = { url, options }; return response(200, { data: { revoked: true } }); };
  await auth.LumoAuth.confirmLogout();
  assert.equal(received.url, '/api/v1/iam/logout'); assertions++;
  assert.equal(auth.LumoAuth.isLoggedIn(), false); assertions++;
  assert.equal(auth.LumoAPI.getToken(), ''); assertions++;
  auth.fetch = async () => response(401, { error: { message: 'invalid credentials', code: 'Unauthenticated' } });
  await assert.rejects(auth.LumoAuth.login('demo', 'wrong'), /账号或密码不正确/); assertions++;
  assert.equal(auth.LumoAuth.isLoggedIn(), false); assertions++;
  auth.fetch = async (url, options) => { received = { url, options }; return response(200, { data: { token: tokenB, user_id: 'admin', role: 'admin', expires_at: future } }); };
  await auth.LumoAuth.login('admin', 'TypedPassword', true);
  assert.equal(JSON.parse(received.options.body).data.password, 'TypedPassword'); assertions++;
  assert.equal(auth.LumoAuth.user().role, 'admin'); assertions++;
  assert.equal(auth.localStorage.getItem('lumo-api-session-v2').includes('TypedPassword'), false); assertions++;
  auth.LumoAuth.setItem('lumo-mall-v1', JSON.stringify({ orders: [{ id: 'transient' }] }));
  auth.LumoAPI.setBase('https://api.example.com');
  assert.equal(auth.LumoAuth.isLoggedIn(), false); assertions++;
  assert.equal(auth.LumoAuth.getItem('lumo-mall-v1'), null); assertions++;
  auth.LumoAuth.requireLogin('#/promotions/group/group%3Ademo%3Ainvite');
  assert.equal(auth.LumoAuth.model().returnTo, '#/promotions/group/group%3Ademo%3Ainvite'); assertions++;
  console.log('PASS frontend API/auth: ' + assertions + ' assertions');
}
main().catch(error => { console.error(error); process.exitCode = 1; });
