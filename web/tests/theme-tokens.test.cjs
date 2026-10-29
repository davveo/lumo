'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const source = (file) => fs.readFileSync(path.join(directory, file), 'utf8');
// These are stylesheet contracts, not a replacement for browser layout checks.
// Resolve custom-property dependencies to ensure changing the configured brand
// reaches the actual component declarations, rather than just the root token.
const rules = (file) =>
  [...source(file).matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((match) => ({
    selector: match[1]
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .trim()
      .replace(/\s+/g, ' '),
    body: match[2],
  }));
const block = (file, selector) => {
  const found = rules(file).find((rule) => rule.selector === selector);
  assert.ok(found, file + ': missing ' + selector);
  return found.body;
};
const declarations = (body) =>
  Object.fromEntries([...body.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)].map((match) => [match[1], match[2]]));
const tokens = declarations(block('styles.css', ':root'));
function expand(value, overrides = {}, visited = new Set()) {
  return value.replace(/var\((--[\w-]+)\)/g, (_, key) => {
    assert.ok(!visited.has(key), 'circular custom property: ' + key);
    assert.ok(key in overrides || key in tokens, 'unresolved custom property: ' + key);
    return expand(overrides[key] || tokens[key], overrides, new Set([...visited, key]));
  });
}
function branded(file, selector) {
  const body = block(file, selector);
  const original = expand(body),
    blue = expand(body, { '--red': '#337bbf' });
  assert.notEqual(original, blue, file + ': ' + selector + ' ignores the configured colour');
  assert.match(blue, /#337bbf/, file + ': ' + selector + ' has no dependency on the live brand token');
}

test('brand colour reaches primary buttons, page headers, search feedback and product/cart surfaces', () => {
  for (const [file, selector] of [
    ['styles.css', '.primary'],
    ['styles.css', '.secondary'],
    ['styles.css', '.primary:hover'],
    ['styles.css', '.shipping-option.active'],
    ['styles.css', '.payment-method.active'],
    ['styles.css', 'input:focus, textarea:focus, select:focus'],
    ['homepage.css', 'body:has(.storefront-page) .header'],
    ['product-detail.css', '.pdp-purchase-heading'],
    ['product-detail.css', '.pdp-price'],
    ['product-detail.css', '.pdp-option.active'],
    ['cart-page.css', '.cart-benefits-strip'],
    ['auth-views.css', '.auth-input-wrap:focus-within'],
    ['discovery.css', '.disc-compare-button:hover, .disc-compare-button.selected'],
    ['member-views.css', '.mem-choice-address.active'],
    ['services.css', '.svc-support-nav a.active'],
  ])
    branded(file, selector);
});

test('brand tints, borders and focus rings derive from one colour while fixed semantic and benefit themes remain distinct', () => {
  for (const key of [
    '--red-dark',
    '--pale',
    '--brand-border',
    '--brand-border-strong',
    '--brand-ring',
    '--brand-fill',
    '--brand-shadow',
  ]) {
    assert.match(expand(tokens[key], { '--red': '#138f82' }), /#138f82/, key);
    assert.notEqual(expand(tokens[key]), expand(tokens[key], { '--red': '#138f82' }), key);
  }
  assert.match(block('auth-views.css', '.auth-error'), /#fff2f0/);
  assert.match(block('homepage.css', '.home-activity-card.is-amber'), /#fff5df/);
  assert.match(block('member-views.css', '.mem-coupon-value'), /#fff4ec/);
  assert.match(source('marketing-popup.css'), /\.mp-theme-plum|\.mp-theme-purple|\.mp-theme/);
});

test('phone controls keep explicit touch heights without increasing the desktop component defaults', () => {
  const styles = source('styles.css'),
    cart = source('cart-page.css'),
    member = source('member-views.css');
  assert.match(block('styles.css', '.quantity'), /height:\s*32px/);
  assert.match(block('cart-page.css', '.cart-page .cart-table-row .quantity'), /height:\s*29px/);
  assert.match(styles, /@media \(max-width: 680px\)[\s\S]*\.quantity[\s\S]*min-height:\s*44px/);
  assert.match(cart, /@media \(max-width: 760px\)[\s\S]*\.cart-page \.cart-table-row \.quantity\s*\{\s*height:\s*44px/);
  assert.match(member, /@media \(max-width: 680px\)[\s\S]*\.mem-primary[\s\S]*min-height:\s*44px/);
  assert.match(member, /\.mem-toggle::before\s*\{[^}]*height:\s*20px/);
});
