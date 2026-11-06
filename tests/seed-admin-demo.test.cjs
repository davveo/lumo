'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { Seeder, options } = require('../scripts/seed-admin-demo.cjs');

test('demo seeding accepts only an explicit local origin and a bounded namespace', () => {
  assert.equal(options([]).apply, false);
  for (const base of [
    'https://example.com',
    'http://127.0.0.1.evil.test',
    'http://user:secret@localhost',
    'http://localhost/api',
    'file:///tmp/demo',
  ])
    assert.throws(() => options(['--base-url', base]));
  assert.throws(() => options(['--namespace', '../another-run']));
  assert.equal(options(['--apply', '--base-url', 'http://localhost:8080']).apply, true);
});
test('completed seed steps survive restart and do not replay business writes', async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'lumo-demo-seed-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const config = { base: 'http://localhost:8080', namespace: 'test-seed-v1', manifest: path.join(dir, 'run.json') };
  const first = new Seeder(config);
  let writes = 0;
  const original = await first.once('checkout', async () => {
    writes++;
    return { order_id: 'owned-demo-order' };
  });
  const resumed = new Seeder(config);
  const replay = await resumed.once('checkout', async () => {
    writes++;
    throw Error('must not repeat checkout');
  });
  assert.deepEqual(original, replay);
  assert.equal(writes, 1);
  assert.equal(fs.statSync(config.manifest).mode & 0o777, 0o600);
  assert.throws(() => new Seeder({ ...config, base: 'http://localhost:9090' }));
});
test('the durable manifest rejects credential-bearing API responses', async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'lumo-demo-private-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const config = { base: 'http://localhost:8080', namespace: 'test-seed-v1', manifest: path.join(dir, 'run.json') };
  const seed = new Seeder(config);
  await assert.rejects(
    seed.once('session', async () => ({ token: 'sensitive-session' })),
    /Credentials/,
  );
  assert.equal(fs.existsSync(config.manifest), false);
});
