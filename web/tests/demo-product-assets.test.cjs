'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const navigation = JSON.parse(
  fs.readFileSync(path.join(__dirname, '../../services/content/internal/usecase/category_navigation.json')),
);

test('all demo category covers exist as self-contained images and survive product editing', (t) => {
  const app = createHarness(directory, 'http://demo-images.test', { globals: { products: [] } });
  t.after(() => app.dispose());
  app.load('editor.js');
  for (const group of navigation.groups) {
    const image = 'assets/demo-' + group.id + '.svg';
    const body = fs.readFileSync(path.join(directory, image), 'utf8');
    assert.equal(app.ctx.LumoStudio.safeImage(image), image);
    assert.equal(app.ctx.LumoStudio.asset(image), image);
    assert.match(body, /<svg\b/);
    assert.ok(body.includes(group.category));
    assert.doesNotMatch(body, /<(?:script|foreignObject|image)\b|\bon\w+\s*=|\b(?:href|xlink:href)\s*=/i);
  }
});

test('local demo image support does not accept SVG uploads, path traversal, or arbitrary local SVG', (t) => {
  const app = createHarness(directory, 'http://demo-images.test', { globals: { products: [] } });
  t.after(() => app.dispose());
  app.load('editor.js');
  for (const image of [
    'data:image/svg+xml,<svg onload=alert(1)>',
    'assets/upload.svg',
    '../assets/demo-food.svg',
    'assets/demo-food.svg?x=1',
    'javascript:alert(1)',
  ]) {
    assert.equal(app.ctx.LumoStudio.safeImage(image, ''), '');
  }
});
