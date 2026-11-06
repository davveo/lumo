const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function preview() {
  const context = {
    console,
    URL,
    Date,
    sessionStorage: { getItem: () => 'true' },
    document: { addEventListener() {} },
  };
  context.window = context;
  vm.createContext(context);
  for (const file of ['constants.js', 'admin-demo-data.js', 'admin-demo.js'])
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../dist', file), 'utf8'), context);
  return context.LumoAdminDemo;
}
test('preview lists paginate, filter, and return isolated copies', async () => {
  const demo = preview();
  const first = await demo.call('order', 'admin_list', { data: { page: 1, page_size: 20 } });
  const last = await demo.call('order', 'admin_list', { data: { page: 2, page_size: 20 } });
  assert.equal(first.total, 28);
  assert.equal(first.items.length, 20);
  assert.equal(last.items.length, 8);
  assert.ok(!first.items.some((a) => last.items.some((b) => a.id === b.id)));
  first.items[0].status = 'modified';
  const again = await demo.call('order', 'admin_list', { data: { keyword: 'PREVIEW_ORD_10001' } });
  assert.equal(again.items.length, 1);
  assert.notEqual(again.items[0].status, 'modified');
});
test('preview rejects business mutations and unregistered actions without a network fallback', async () => {
  const demo = preview();
  for (const [service, action] of [
    ['iam', 'staff_upsert'],
    ['order', 'cancel'],
    ['payment', 'create'],
    ['catalog', 'update'],
    ['helpdesk', 'assign_work'],
    ['unknown', 'list'],
  ])
    await assert.rejects(
      demo.call(service, action, { data: { name: 'must not persist' } }),
      (error) => error.code === 'preview_read_only',
    );
  const policy = await demo.call('iam', 'policy_get');
  assert.equal(policy.config.wechat_enabled, true);
  policy.config.password_enabled = false;
  assert.equal((await demo.call('iam', 'policy_get')).config.password_enabled, true);
});
