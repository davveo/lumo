(function () {
  'use strict';
  const C = window.LumoConstants;
  const cache = new Map(), imported = new Map(), borrowedIDs = new Set();
  let page = 0, currentKey = '', revision = 0, owner = '', home = null;
  let catalogIDs = new Set(products.map(p => p.id));
  const scope = () => JSON.stringify([window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '', window.LumoAPI.getBase?.() || location.origin, window.LumoAPI.getToken?.() || '']);
  const text = (value, length = 100) => typeof value === 'string' ? value.trim().slice(0, length) : '';
  const redraw = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  function reset() {
    revision++; cache.clear(); home = null; page = 0; currentKey = ''; owner = scope();
    for (let index = products.length - 1; index >= 0; index--) if (borrowedIDs.has(products[index].id) && !catalogIDs.has(products[index].id)) products.splice(index, 1);
    imported.clear(); borrowedIDs.clear();
  }
  function ensureScope() { if (owner !== scope()) reset(); }
  function catalogChanged() {
    // refreshCatalog has already replaced the array with its authoritative page.
    imported.clear(); borrowedIDs.clear(); catalogIDs = new Set(products.map(p => p.id)); reset();
  }
  function productFor(raw) {
    const id = text(raw?.product_id || raw?.id, 150);
    if (raw?.active === false || raw?.status === C.CatalogStatus.Disabled) return null;
    const existing = products.find(p => p.id === id || (p.skuId || p.sku_id) === raw?.id);
    if (existing) return existing;
    if (!id || !text(raw?.name || raw?.attributes?.name) || !window.LumoCommerce?.normalizeProduct) return null;
    return window.LumoCommerce.normalizeProduct(raw);
  }
  function mergeProducts(scene, rows) {
    // Keep only the current home, similar and order pools, rather than appending
    // every recommendation seen during a long browsing session.
    imported.set(scene, new Map(rows.slice(0, scene === C.ProductPool.Search ? 100 : 20).filter(row => !catalogIDs.has(row.product.id)).map(row => [row.product.id, row.product])));
    const keep = new Map([...imported.values()].flatMap(values => [...values]));
    const route = (location.hash || '#/').slice(2).split('/');
    const pinned = new Set((state.cart || []).slice(0, 20).map(item => item.id));
    if (route[0] === 'product') pinned.add(route[1]);
    for (let index = products.length - 1; index >= 0; index--) {
      const p = products[index];
      if (borrowedIDs.has(p.id) && !catalogIDs.has(p.id) && !keep.has(p.id) && !pinned.has(p.id)) { products.splice(index, 1); borrowedIDs.delete(p.id); }
    }
    for (const [id, p] of keep) { if (!products.some(existing => existing.id === id)) products.push(p); borrowedIDs.add(id); }
  }
  function mapItems(data, scene, excluded = [], ticket) {
    ensureScope();
    const own = new Set(excluded.filter(Boolean)), seen = new Set();
    const rows = (Array.isArray(data?.items) ? data.items : []).filter(raw => raw && typeof raw === 'object').slice(0, scene === C.ProductPool.Search ? 100 : 20).map(raw => ({
      product: productFor(raw), reason: text(raw.recommendation_reason, 100) || '为你精选的可售好物',
      reasons: Array.isArray(raw.recommendation_reasons) ? raw.recommendation_reasons.filter(code => typeof code === 'string').slice(0, 8) : []
    })).filter(row => row.product && row.product.visible !== false && !own.has(row.product.id) && !seen.has(row.product.id) && seen.add(row.product.id));
    window.LumoInventory?.prime?.(data.inventory_items || [], ticket);
    mergeProducts(scene, rows);
    return rows;
  }
  function stockTicket() { return window.LumoInventory?.hydrationTicket?.(); }
  function restoreItems(scene, rows) { ensureScope(); mergeProducts(scene, rows); return rows; }
  function keyFor(order) { return scope() + ':order:' + String(order?.id || ''); }
  function rank(order) {
    ensureScope();
    const rows = cache.get(keyFor(order))?.rows || [];
    if (rows.length) mergeProducts(C.RecommendationScene.PostPurchase, rows);
    return rows;
  }
  async function refreshHome() {
    ensureScope();
    if (home?.promise) return home.promise;
    const context = owner, generation = revision, ticket = stockTicket();
    const entry = {rows: home?.rows || [], loading: true, error: '', personalized: false, degraded: false}; home = entry;
    entry.promise = window.LumoAPI.call(C.Service.Recommendation, C.Action.List, {data: {scene: C.RecommendationScene.Home, limit: 20}}, {auth: !!window.LumoAuth?.isLoggedIn()}).then(data => {
      if (context !== scope() || generation !== revision || home !== entry) return [];
      entry.rows = mapItems(data, C.RecommendationScene.Home, [], ticket); entry.personalized = data.personalized === true; entry.degraded = data.degraded === true; entry.strategy = text(data.strategy); return entry.rows;
    }).catch(error => {
      if (context !== scope() || generation !== revision || home !== entry) return [];
      entry.error = error.message || '推荐暂不可用'; throw error;
    }).finally(() => { entry.loading = false; entry.promise = null; });
    return entry.promise;
  }
  function homeModel() {
    ensureScope();
    return {ready: !!home && !home.loading && !home.error, loading: !home || home.loading, loaded: !!home && !home.loading && !home.error, error: home?.error || '', personalized: home?.personalized === true, degraded: home?.degraded === true, count: home?.rows.length || 0};
  }
  function orderHome(list) {
    ensureScope();
    if (!home?.rows.length) return list;
    mergeProducts(C.RecommendationScene.Home, home.rows);
    const positions = new Map(home.rows.map((row, index) => [row.product.id, index]));
    return list.map((product, index) => ({product, index})).sort((a, b) => (positions.get(a.product.id) ?? Infinity) - (positions.get(b.product.id) ?? Infinity) || a.index - b.index).map(row => row.product);
  }
  function homeCard(product) {
    ensureScope();
    const row = home?.rows.find(row => row.product.id === product.id);
    if (!row) return productCard(product);
    return `<div class="home-recommendation-card">${productCard(product)}<div class="purchase-recommendation-reason">${icon('sparkles')}<span>${esc(row.reason)}</span></div></div>`;
  }
  async function refresh(order) {
    ensureScope();
    if (!order) { if (!/^#\/success(?:\/|$)/.test(location.hash)) return []; order = (state.orders || [])[0] || state.pending; }
    if (!order?.id) return [];
    const key = keyFor(order), prior = cache.get(key);
    if (prior?.promise) return prior.promise;
    const context = owner, generation = revision, ticket = stockTicket();
    const entry = {rows: prior?.rows || [], loading: true, error: '', strategy: ''};
    cache.set(key, entry); while (cache.size > 8) cache.delete(cache.keys().next().value);
    entry.promise = window.LumoAPI.call(C.Service.Recommendation, C.Action.List, {data: {scene: C.RecommendationScene.PostPurchase, order_id: order.id, limit: 20}}).then(data => {
      if (context !== scope() || generation !== revision || cache.get(key) !== entry) return [];
      entry.rows = mapItems(data, C.RecommendationScene.PostPurchase, (order.items || []).flatMap(item => [item.id, item.product_id, item.sku_id]), ticket);
      entry.strategy = text(data.strategy); entry.personalized = data.personalized === true; return entry.rows;
    }).catch(error => {
      if (context !== scope() || generation !== revision || cache.get(key) !== entry) return [];
      entry.error = error.message || '推荐暂不可用'; throw error;
    }).finally(() => { entry.loading = false; entry.promise = null; });
    return entry.promise;
  }
  function renderRecommendations(order) {
    if (!order) return '';
    ensureScope();
    const key = keyFor(order);
    if (currentKey !== key) { currentKey = key; page = 0; }
    let entry = cache.get(key);
    if (!entry) { void refresh(order).then(redraw).catch(redraw); entry = cache.get(key) || {rows: [], loading: true}; }
    const rows = rank(order), pages = Math.ceil(rows.length / 4);
    page = pages ? page % pages : 0;
    const visible = rows.slice(page * 4, page * 4 + 4);
    const controls = pages > 1 ? `<button class="purchase-recommendations-refresh" data-recommendation-action="next" aria-label="换一组推荐商品">${icon('return')}换一组<span>${page + 1} / ${pages}</span></button>` : '';
    return `<section class="purchase-recommendations" aria-labelledby="purchase-recommendations-title"><div class="purchase-recommendations-head"><div><span class="purchase-recommendations-eyebrow">CURATED FOR YOU</span><h2 id="purchase-recommendations-title">为你推荐</h2><p>结合本次购买与商品特征，发现更多合心好物</p></div>${controls}</div>${entry.error && visible.length ? `<p role="alert">${esc(entry.error)}</p>` : ''}${entry.loading && !visible.length ? (window.LumoSkeleton?.render('products',{count:4,label:'正在加载推荐商品',className:'sk-post-purchase'})||'') : entry.error && !visible.length ? `<div class="purchase-recommendations-empty"><p>${esc(entry.error)}</p><button class="outline" data-recommendation-action="retry" data-order="${esc(order.id)}">重新加载</button></div>` : visible.length ? `<div class="purchase-recommendations-grid">${visible.map(row => `<div class="purchase-recommendation-card">${productCard(row.product)}<div class="purchase-recommendation-reason">${icon('sparkles')}<span>${esc(row.reason)}</span></div></div>`).join('')}</div>` : `<div class="purchase-recommendations-empty">${icon('bag')}<h3>当前暂无更多可售推荐</h3><p>继续逛逛，发现更多好物。</p><a class="outline" href="#/">浏览全部商品 ${icon('arrow')}</a></div>`}</section>`;
  }
  document.addEventListener('click', event => {
    const control = event.target.closest('[data-recommendation-action]'); if (!control) return;
    if (control.dataset.recommendationAction === 'next') { page++; redraw(); return; }
    const order = (state.orders || []).find(o => o.id === control.dataset.order);
    if (order) { control.disabled = true; refresh(order).catch(() => {}).finally(redraw); }
  });
  for (const event of ['lumo:login', 'lumo:logout', 'lumo:session-expired']) window.addEventListener(event, reset);
  window.LumoRecommendations = {render: renderRecommendations, rank, refresh, refreshHome, homeModel, orderHome, homeCard, mapItems, restoreItems, catalogChanged};
})();
