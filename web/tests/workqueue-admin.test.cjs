'use strict';
const test = require('node:test'),
  assert = require('node:assert/strict'),
  path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
function setup() {
  const h = createHarness(process.env.FRONTEND_DIR || path.join(__dirname, '../dist'), 'http://workqueue.test', {
    html: 'admin.html',
  });
  h.load('skeleton.js');
  h.load('admin-ui.js');
  h.load('workqueue-admin.js');
  h.ids.set('workqueue-admin-root', new Element('workqueue-admin-root'));
  return h;
}
test('work queue paginates the merged window and preserves failed batch items for correction', async (t) => {
  const h = setup();
  t.after(() => h.dispose());
  const calls = [],
    m = h.ctx.LumoWorkQueueAdmin;
  const data = {
    actor_id: 'admin',
    actor_name: '管理员',
    can_assign_others: true,
    allowed_services: ['fulfillment'],
    views: [],
    policy: { version: 0 },
    notice: '按最早100条合并',
    items: Array.from({ length: 43 }, (_, i) => ({
      work_id: 'fulfillment:job-' + i,
      resource_id: 'job-' + i,
      service: 'fulfillment',
      order_id: 'order-' + i,
      status: 'READY',
      assignment: { version: 7 },
      waiting_hours: 24,
      overdue: true,
      due_at: '2026-11-01T00:00:00Z',
    })),
  };
  m.configure({
    isAdmin: () => true,
    scope: () => 'admin',
    draw() {},
    async call(service, action, request) {
      calls.push({ service, action, request });
      if (action === 'staff_list') return { items: [{ id: 'warehouse', username: '仓库同事', active: true }] };
      if (action === 'assign_work')
        return {
          items: [
            { service: 'fulfillment', id: 'job-0', ok: true, version: 8 },
            { service: 'fulfillment', id: 'job-1', ok: false, error: '负责人已变化，请刷新后重试' },
          ],
        };
      return data;
    },
  });
  await m.refresh();
  let html = m.render();
  assert.equal((html.match(/data-wq="detail"/g) || []).length, 20);
  assert.match(html, /第 1 \/ 3 页/);
  assert.match(html, /仓库同事/);
  assert.equal(m.state.assignTo, 'admin');
  await h.click({ wqPage: '3' });
  html = m.render();
  assert.equal((html.match(/data-wq="detail"/g) || []).length, 3);
  m.state.selected.add('fulfillment:job-0');
  m.state.selected.add('fulfillment:job-1');
  await h.submit('workqueue-assign', { assignee_id: 'warehouse', reason: '分派出库' });
  const submitted = calls.find((c) => c.action === 'assign_work');
  assert.equal(submitted.request.data.items.length, 2);
  assert.equal(submitted.request.data.items[0].expected_version, 7);
  assert.ok(submitted.request.idempotency_key);
  assert.equal(m.state.selected.has('fulfillment:job-0'), false);
  assert.equal(m.state.selected.has('fulfillment:job-1'), true);
  assert.match(m.render(), /负责人已变化/);
  assert.doesNotMatch(m.render(), /data-operation="dispatch"/);
});
test('saved work views send owned filters with their current version', async (t) => {
  const h = setup();
  t.after(() => h.dispose());
  const m = h.ctx.LumoWorkQueueAdmin,
    calls = [];
  m.configure({
    isAdmin: () => true,
    scope: () => 'staff',
    draw() {},
    async call(service, action, request) {
      calls.push({ action, request });
      return {
        actor_id: 'staff',
        allowed_services: ['helpdesk'],
        can_assign_others: false,
        items: [],
        views: [
          {
            id: 'personal-view',
            name: '我的客服',
            version: 4,
            filters_json: '{"service":"helpdesk","assignee":"mine"}',
          },
        ],
        policy: {},
      };
    },
  });
  await m.refresh();
  m.state.filters = { service: 'helpdesk', assignee: 'mine', overdue_only: true, keyword: '' };
  await h.submit('workqueue-save-view', { name: '我的客服' });
  const save = calls.find((c) => c.action === 'save_view');
  assert.equal(save.request.data.version, 4);
  assert.equal(save.request.data.filters.overdue_only, true);
  assert.equal(save.request.user_id, undefined);
});
