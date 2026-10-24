'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const { createHarness } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
function fixture(extra = {}) {
  const app = createHarness(directory, 'http://detail.test', {
    globals: {
      LumoAPI: {
        async call() {
          return { items: [] };
        },
        getBase: () => '',
        getToken: () => '',
      },
      LumoProductExtras: {
        afterRender() {},
        readState: () => ({ loaded: true }),
        reviewCount: () => 2,
        questionCount: () => 1,
        reviews: () => '<section>真实购买评价</section>',
        questions: () => '<section>真实商品问答</section>',
        related: () => '<section>真实相似商品</section>',
        detailActions: () => '<button data-action="favorite">收藏商品</button>',
      },
    },
  });
  app.load('app.js');
  const product = {
    id: 'outside-bootstrap',
    name: '真实目录商品',
    category: '家用电器',
    brand: '实际品牌',
    price_cents: 9900,
    image: 'fridge.jpg',
    colors: ['深蓝', '白色'],
    options: ['标准', '大容量'],
    variants: [
      { sku_id: 'blue-standard', color: '深蓝', option: '标准', price_cents: 9900 },
      { sku_id: 'white-large', color: '白色', option: '大容量', price_cents: 15900 },
    ],
    ...extra,
  };
  app.evaluate(`products.push(normalizeProduct(${JSON.stringify(product)})); commerceState.catalogLoaded=true;`);
  return { app, render: () => app.navigate('product/outside-bootstrap'), html: () => app.ids.get('app').innerHTML };
}

test('detail separates media, real SKU purchase choices and backend content tabs without invented video/store claims', (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  const html = f.render();
  assert.match(html, /class="pdp-gallery"/);
  assert.match(html, /class="pdp-media"/);
  assert.match(html, /class="pdp-purchase" aria-label="商品选购"/);
  assert.match(html, /商品编号：blue-standard/);
  assert.match(html, /真实购买评价/);
  assert.match(html, /data-action="detail-add"/);
  assert.match(html, /data-action="buy-now"/);
  assert.match(html, /data-action="option" data-value="大容量"[^>]*disabled/);
  assert.equal(
    (html.match(/class="pdp-thumbnail /g) || []).length,
    1,
    'one real image must not become three invented gallery images',
  );
  const purchase = html.match(/<aside class="pdp-purchase"[\s\S]*?<\/aside>/)[0];
  assert.doesNotMatch(purchase, /<video|门店|现货充足|7天无理由|全国联保|官方正品|已售/);
  for (const title of ['用户评价', '商品详情', '规格参数', '服务保障', '推荐好物', '商品问答'])
    assert.ok(html.includes(title), title);
});

test('sparse SKU combinations choose a real variant and reject invalid option clicks', async (t) => {
  const f = fixture();
  t.after(() => f.app.dispose());
  f.render();
  await f.app.click({ action: 'option', value: '大容量' });
  assert.equal(f.app.evaluate('detail.option'), '标准');
  await f.app.click({ action: 'color', value: '白色' });
  assert.equal(f.app.evaluate('detail.color'), '白色');
  assert.equal(f.app.evaluate('detail.option'), '大容量');
  assert.equal(f.app.evaluate('skuForItem(detail)'), 'white-large');
  assert.match(f.html(), /商品编号：white-large/);
  assert.match(f.html(), /¥<\/small>159/);
  assert.match(f.html(), /data-action="option" data-value="标准"[^>]*disabled/);
});

test('tabs render their actual backend sections and video appears only with a supplied valid media URL', async (t) => {
  const f = fixture({ images: ['fridge-side.jpg'], video_url: 'https://media.example.test/product.mp4' });
  t.after(() => f.app.dispose());
  f.render();
  assert.equal((f.html().match(/class="pdp-thumbnail /g) || []).length, 3);
  assert.doesNotMatch(f.html(), /<video/);
  await f.app.click({ action: 'thumb', value: '2' });
  assert.match(
    f.html(),
    /<video src="https:\/\/media.example.test\/product.mp4"[^>]*controls playsinline preload="metadata"/,
  );
  await f.app.click({ action: 'detail-tab', value: '3' });
  assert.match(f.html(), /真实商品问答/);
  await f.app.click({ action: 'detail-tab', value: '5' });
  assert.match(f.html(), /真实相似商品/);
  await f.app.click({ action: 'detail-tab', value: '4' });
  assert.match(f.html(), /服务与售后/);
  await f.app.click({ action: 'detail-tab', value: '1' });
  assert.match(f.html(), /实际品牌/);
  const invalid = fixture({ video_url: 'javascript:alert(1)' });
  t.after(() => invalid.app.dispose());
  assert.doesNotMatch(invalid.render(), /javascript:alert|<video/);
});

test('detail stylesheet provides a desktop media/purchase grid and mobile reading order with no forced minimum width', () => {
  const css = fs.readFileSync(path.join(directory, 'product-detail.css'), 'utf8');
  assert.match(css, /\.pdp-layout\s*\{[^}]*grid-template-columns:/);
  assert.match(css, /@media \(max-width: 760px\)/);
  assert.match(css, /\.pdp-main\s*\{\s*display: contents;/);
  assert.match(css, /\.pdp-purchase\s*\{\s*order: 1;\s*position: static;/);
  assert.match(css, /\.pdp-details\.details-bottom\s*\{\s*order: 2;/);
  assert.match(css, /\.pdp-tabs\.detail-tabs\s*\{[^}]*overflow-x: auto;/);
});
