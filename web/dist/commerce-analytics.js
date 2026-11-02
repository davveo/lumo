(function () {
  'use strict';
  let previous = '';
  const track = (type, sku = '') => {
    if (!window.LumoAuth?.isLoggedIn()) return;
    void window.LumoAPI.call('analytics', 'record', {
      idempotency_key: crypto.randomUUID(),
      data: { type, sku_id: sku, page: location.hash.slice(0, 240), source: 'storefront' },
    }).catch(() => {});
  };
  window.addEventListener('lumo:cart-added', (event) => track('add_cart', event.detail?.sku_id));
  const route = () => {
    const next = location.hash + '|' + (window.LumoAuth?.user()?.id || '');
    if (next === previous) return;
    previous = next;
    if (location.hash === '#/checkout') track('checkout');
  };
  window.addEventListener('hashchange', route);
  window.addEventListener('lumo:login', route);
})();
