'use strict';
const test = require('node:test'),
  assert = require('node:assert/strict'),
  path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise((resolve) => setImmediate(resolve));
function deferred() {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
}
function setup(t) {
  const h = createHarness(directory, 'http://shop.test');
  t.after(() => h.dispose());
  const requests = [],
    snapshots = [],
    timers = new Map();
  let token = 'old-token',
    expired = 0,
    sequence = 0;
  h.ctx.document.hidden = false;
  h.ctx.LumoAPI = { getBase: () => 'http://shop.test', getToken: () => token };
  h.ctx.LumoAuth = { isLoggedIn: () => true };
  h.ctx.LumoServices = { applyPush: (value) => snapshots.push(value) };
  h.ctx.fetch = () => {
    const request = deferred();
    requests.push(request);
    return request.promise;
  };
  h.ctx.setTimeout = (fn) => {
    timers.set(++sequence, fn);
    return sequence;
  };
  h.ctx.clearTimeout = (id) => timers.delete(id);
  h.ctx.addEventListener('lumo:session-expired', () => expired++);
  h.load('realtime-messages.js');
  return {
    h,
    requests,
    snapshots,
    timers,
    setToken(next) {
      token = next;
    },
    expired: () => expired,
  };
}
test('a stale stream HTTP 401 cannot expire a newly logged-in session', async (t) => {
  const s = setup(t);
  s.h.ctx.LumoRealtime.connect();
  s.setToken('new-token');
  s.h.evaluate("window.dispatchEvent(new CustomEvent('lumo:login'))");
  assert.equal(s.requests.length, 2);
  s.requests[0].resolve({ status: 401 });
  await tick();
  assert.equal(s.expired(), 0);
  assert.equal(s.timers.size, 0);
});
test('stopped stream cannot apply late frames or schedule reconnect over its replacement', async (t) => {
  const s = setup(t),
    read = deferred();
  s.h.ctx.LumoRealtime.connect();
  s.requests[0].resolve({ status: 200, ok: true, body: { getReader: () => ({ read: () => read.promise }) } });
  await tick();
  s.h.ctx.LumoRealtime.stop();
  s.h.ctx.LumoRealtime.connect();
  read.resolve({
    done: false,
    value: new TextEncoder().encode(
      'event: notifications\ndata: {"items":[],"unread_total":99}\n\nevent: auth_required\ndata: {}\n\n',
    ),
  });
  await tick();
  assert.equal(s.snapshots.length, 0);
  assert.equal(s.expired(), 0);
  assert.equal(s.timers.size, 0);
});
test('active stream still applies notifications and reconnects after a normal close', async (t) => {
  const s = setup(t);
  s.h.ctx.LumoRealtime.connect();
  let reads = 0;
  s.requests[0].resolve({
    status: 200,
    ok: true,
    body: {
      getReader: () => ({
        async read() {
          return reads++
            ? { done: true }
            : {
                done: false,
                value: new TextEncoder().encode('event: notifications\ndata: {"items":[],"unread_total":3}\n\n'),
              };
        },
      }),
    },
  });
  await tick();
  assert.equal(s.snapshots.length, 1);
  assert.equal(s.snapshots[0].unread_total, 3);
  assert.equal(s.timers.size, 1);
});
