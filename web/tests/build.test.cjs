'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const vm = require('node:vm');
const zlib = require('node:zlib');
const { build } = require('../build.cjs');

test('build preserves script order, hashes immutable bundles and compresses exact bytes', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'lumo-assets-'));
  try {
    const source = path.join(root, 'source'),
      output = path.join(root, 'output');
    fs.mkdirSync(source);
    fs.writeFileSync(
      path.join(source, 'index.html'),
      '<link rel="stylesheet" href="a.css"><link rel="stylesheet" href="b.css"><script src="a.js" defer></script><script src="b.js" defer></script>',
    );
    fs.writeFileSync(path.join(source, 'a.js'), 'window.order=[1];');
    fs.writeFileSync(path.join(source, 'b.js'), 'window.order.push(2);');
    fs.writeFileSync(path.join(source, 'a.css'), 'a{color:inherit}');
    fs.writeFileSync(path.join(source, 'b.css'), 'b{color:inherit}');
    const first = build(source, output),
      html = fs.readFileSync(path.join(output, 'index.html'), 'utf8');
    const scripts = [...html.matchAll(/src="([^"]+)"/g)].map((row) => row[1]),
      styles = [...html.matchAll(/href="([^"]+)"/g)].map((row) => row[1]);
    assert.equal(scripts.length, 1);
    assert.equal(styles.length, 1);
    assert.match(scripts[0], /\.[0-9a-f]{12}\.js$/);
    const context = { window: {} };
    vm.runInNewContext(fs.readFileSync(path.join(output, scripts[0]), 'utf8'), context);
    assert.deepEqual(Array.from(context.window.order), [1, 2]);
    assert.equal(context.window.LumoAssets['a.js'], first['a.js']);
    const original = fs.readFileSync(path.join(output, scripts[0]));
    assert.deepEqual(zlib.gunzipSync(fs.readFileSync(path.join(output, scripts[0] + '.gz'))), original);
    build(source, output);
    assert.equal(fs.readFileSync(path.join(output, 'index.html'), 'utf8'), html);
    fs.appendFileSync(path.join(source, 'a.js'), '\nwindow.changed=true;');
    build(source, output);
    assert.notEqual(fs.readFileSync(path.join(output, 'index.html'), 'utf8'), html);
    assert.equal(fs.readFileSync(path.join(source, 'b.js'), 'utf8'), 'window.order.push(2);');
    assert.throws(() => build(source, source), /outside/);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test('editor resources load once, in dependency order, and are restricted to admin entry', async () => {
  const appended = [];
  let role = 'user';
  const context = {
    window: {
      LumoConstants: { Role: { Admin: 'admin' } },
      LumoAuth: { user: () => ({ role }) },
      LumoAssets: { 'editor.js': 'editor.123456789abc.js' },
    },
    document: {
      createElement: (tag) => ({ tag, remove() {} }),
      head: {
        appendChild(element) {
          appended.push(element);
          queueMicrotask(() => element.onload());
        },
      },
    },
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../dist/route-assets.js'), 'utf8'), context);
  await assert.rejects(context.window.LumoRouteAssets.ensureEditor(), /管理员/);
  assert.equal(appended.length, 0);
  role = 'admin';
  await Promise.all([context.window.LumoRouteAssets.ensureEditor(), context.window.LumoRouteAssets.ensureEditor()]);
  assert.equal(appended.length, 4);
  assert.deepEqual(
    appended.filter((row) => row.tag === 'script').map((row) => row.src),
    ['category-editor.js', 'editor.123456789abc.js'],
  );
});
