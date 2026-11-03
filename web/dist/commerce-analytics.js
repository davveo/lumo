(function () {
  'use strict';
  const key = 'lumo.journey.v1';
  let session = '',
    visitor = '',
    source = 'direct',
    query = '',
    previous = '';
  const clean = (q) =>
    String(q || '')
      .replace(/[^\s@]+@[^\s@]+\.[^\s@]+/g, '')
      .replace(/\d{7,}/g, '')
      .trim()
      .slice(0, 100);
  function reset() {
    session = crypto.randomUUID();
    visitor = crypto.randomUUID();
    query = '';
    source = 'direct';
    previous = '';
    try {
      sessionStorage.setItem(key, JSON.stringify({ session, visitor }));
    } catch (_) {}
  }
  try {
    const old = JSON.parse(sessionStorage.getItem(key) || '{}');
    if (/^[a-zA-Z0-9_-]{16,80}$/.test(old.session || '') && /^[a-zA-Z0-9_-]{16,80}$/.test(old.visitor || '')) {
      session = old.session;
      visitor = old.visitor;
    } else reset();
  } catch (_) {
    reset();
  }
  const context = () => ({ session_id: session, visitor_id: visitor, source, query });
  function track(type, sku = '', extra = {}) {
    if (!window.LumoAPI || !session) return;
    void window.LumoAPI.call(
      'analytics',
      'track',
      {
        idempotency_key: crypto.randomUUID(),
        data: { ...context(), type, sku_id: String(sku || '').slice(0, 150), result_count: -1, ...extra },
      },
      { auth: window.LumoAuth?.isLoggedIn() === true },
    ).catch(() => {});
  }
  const search = (q, count) => {
    query = clean(q);
    source = 'search';
    track('search', '', { query, result_count: Number.isFinite(count) ? count : -1 });
  };
  window.addEventListener('lumo:cart-added', (e) => track('add_cart', e.detail?.sku_id));
  function route() {
    const next = location.hash + '|' + (window.LumoAuth?.user()?.id || '');
    if (next === previous) return;
    previous = next;
    if (location.hash.startsWith('#/category/')) source = 'category';
    if (location.hash.startsWith('#/product/')) {
      const id = decodeURIComponent(location.hash.split('/')[2] || ''),
        p = typeof products !== 'undefined' ? products.find((r) => r.id === id) : null;
      track('view', p?.skuId || p?.sku_id || id);
    }
    if (location.hash === '#/checkout') track('checkout');
  }
  window.addEventListener('hashchange', route);
  window.addEventListener('lumo:ready', route);
  window.addEventListener('lumo:login', () => {
    reset();
    route();
  });
  window.addEventListener('lumo:logout', () => {
    reset();
    route();
  });
  window.addEventListener('lumo:api-base-changed', () => {
    reset();
    route();
  });
  document.addEventListener('click', (e) => {
    const a = e.target.closest('a[href^="#/product/"]');
    if (a && a.closest('.recommendation-section,[data-recommendations]')) source = 'recommendation';
    if (a && a.closest('.promo-section')) source = 'campaign';
  });
  window.LumoJourney = { context, track, search };
})();
