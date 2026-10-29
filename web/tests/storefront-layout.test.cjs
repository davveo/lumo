'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createHarness } = require('./dom-harness.cjs');
const dir = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
test('both entry points load shared coupon and storefront configuration before their consumers', () => {
  for (const file of ['index.html', 'admin.html']) {
    const html = fs.readFileSync(path.join(dir, file), 'utf8');
    for (const shared of ['coupon-benefits.js', 'storefront-layout.js']) {
      const position = html.indexOf('src="' + shared + '"');
      assert.ok(position > html.indexOf('src="constants.js"'), file + ' missing ' + shared);
      assert.ok(position < html.indexOf('src="' + (file === 'index.html' ? 'app.js' : 'admin-app.js') + '"'));
    }
  }
});
test('storefront uses bounded slides and typed visibility flags while rejecting executable links and images', () => {
  const app = createHarness(dir, 'http://storefront-layout.test');
  app.load('storefront-layout.js');
  const layout = app.ctx.LumoStorefrontLayout;
  const config = layout.normalize({
    showPicks: false,
    showCoupons: 'false',
    heroInterval: 0,
    heroLink: 'javascript:alert(1)',
    heroSlides: [
      { title: 'good', image: 'hero.jpg', button: '领券', link: '#/coupons' },
      { title: 'unsafe', image: 'javascript:alert(1)', link: '#/coupons' },
    ],
  });
  assert.equal(config.showPicks, false);
  assert.equal(config.showCoupons, true);
  assert.equal(config.heroInterval, 0);
  assert.equal(config.heroLink, '#/');
  assert.equal(config.heroSlides.length, 1);
  const html = layout.hero({
    ...config,
    heroTitle: '主视觉',
    heroSubtitle: '精选商品',
    heroEyebrow: 'LUMO',
    heroImage: 'hero.jpg',
  });
  assert.match(html, /data-store-slide="0"/);
  assert.match(html, /data-store-slide="1" hidden/);
  assert.match(html, /切换轮播图/);
  assert.match(html, /href="#\/coupons"/);
  assert.doesNotMatch(html, /javascript:/);
});
