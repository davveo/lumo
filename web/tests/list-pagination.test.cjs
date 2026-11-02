'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');

test('history reads follow every cursor and stop on identity changes or a repeated cursor', async () => {
  const h = createHarness(directory, 'http://shop.test');
  try {
    h.load('api-config.js');
    h.load('api-client.js');
    const api = h.ctx.LumoAPI,
      calls = [];
    api.call = async (_service, _action, command) => {
      calls.push(command.data.cursor || '');
      return command.data.cursor
        ? { items: [{ id: 'old' }], has_more: false }
        : { items: [{ id: 'new' }], has_more: true, next_cursor: 'page-two' };
    };
    const result = await api.listAll('order');
    assert.deepEqual(
      Array.from(result.items, (row) => row.id),
      ['new', 'old'],
    );
    assert.deepEqual(calls, ['', 'page-two']);
    api.call = async () => ({ items: [], has_more: true, next_cursor: 'repeated' });
    await assert.rejects(api.listAll('invoice'), /分页未推进/);
    api.call = async () => {
      api.setToken('a-different-session');
      return { items: [{ id: 'private' }], has_more: true, next_cursor: 'later' };
    };
    await assert.rejects(api.listAll('aftersale'), /已变化/);
  } finally {
    h.dispose();
  }
});

test('sparse search pages offer continuation without claiming the catalogue is empty', async () => {
  const h = createHarness(directory, 'http://shop.test');
  try {
    await h.loadAll({ bootstrap: false });
    h.evaluate("ui.query='Sparse';ui.category='全部好物'");
    const cursors = [];
    h.ctx.LumoAPI.call = async (service, action, command) => {
      if (service !== 'search') return { items: [] };
      cursors.push(command.data.cursor || '');
      return {
        items: [],
        inventory_items: [],
        has_more: true,
        next_cursor: 'scan-' + cursors.length,
        total: null,
        total_exact: false,
      };
    };
    await h.ctx.LumoDiscovery.refreshSearch(true);
    const html = h.navigate('');
    assert.match(html, /当前批次暂无匹配商品/);
    assert.match(html, /data-discovery-action="load-more"/);
    await h.click({ discoveryAction: 'load-more' });
    assert.deepEqual(cursors.slice(0, 2), ['', 'scan-1']);
  } finally {
    h.dispose();
  }
});
