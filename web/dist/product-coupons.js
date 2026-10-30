(function () {
  'use strict';
  const C = window.LumoConstants;
  const cache = new Map();
  let owner = '',
    revision = 0,
    pending = null,
    timer = null;
  const scope = () =>
    JSON.stringify([
      window.LumoAuth?.user()?.id || '',
      window.LumoAPI.getBase?.() || '',
      window.LumoAPI.getToken?.() || '',
    ]);
  const redraw = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  function reset() {
    owner = scope();
    revision++;
    pending = null;
    cache.clear();
    clearTimeout(timer);
  }
  function selection() {
    if (owner !== scope()) reset();
    if (!/^#\/product\//.test(location.hash)) return null;
    try {
      const sku = skuForItem(detail),
        quantity = Number(detail.qty);
      if (!sku || !Number.isInteger(quantity) || quantity < 1 || quantity > 10) return null;
      return { sku, quantity, key: JSON.stringify([sku, quantity]) };
    } catch (_) {
      return null;
    }
  }
  function summary() {
    if (!window.LumoAuth?.isLoggedIn())
      return `<button type="button" data-auth-action="login" data-return="${esc(location.hash)}">${icon('ticket')}登录查看最佳优惠券 ${icon('chevron')}</button>`;
    const chosen = selection(),
      row = chosen && cache.get(chosen.key);
    if (!chosen) return '<span class="pdp-coupon-status">请先选择商品规格</span>';
    if (!row || row.loading) return '<span class="pdp-coupon-status" role="status">正在计算最佳优惠券…</span>';
    if (row.error)
      return `<button type="button" data-product-coupon="retry">优惠计算失败，重试 ${icon('chevron')}</button>`;
    const quote = row.value,
      selected = (quote.coupon_options || []).find((item) => item.selected && item.eligible);
    if (!selected)
      return `<div class="pdp-coupon-result"><span>暂无适用优惠券</span><a href="#/coupons">去领券 ${icon('chevron')}</a></div>`;
    const goods = Number(quote.subtotal_cents) - Number(quote.discount_cents);
    return `<div class="pdp-coupon-result"><button type="button" data-product-coupon="show">${icon('ticket')}<strong>${esc(selected.name || '最佳优惠券')}</strong><span>可减 ¥${money(Number(selected.discount_cents) / 100)}</span>${icon('chevron')}</button><small>券后商品合计 ¥${money(goods / 100)}${chosen.quantity > 1 ? ` · ${chosen.quantity} 件` : ''}</small></div>`;
  }
  async function calculate(chosen, force = false) {
    const saved = cache.get(chosen.key);
    if (!force && saved && (saved.loading || Date.now() - saved.at < 30000)) return;
    const ticket = ++revision,
      principal = owner;
    if (cache.size >= 100 && !cache.has(chosen.key)) cache.delete(cache.keys().next().value);
    cache.set(chosen.key, { loading: true, at: Date.now() });
    redraw();
    try {
      // Coupon.Calculate delegates to Pricing.Quote, sharing checkout rules.
      const value = await window.LumoAPI.call(C.Service.Coupon, C.Action.Calculate, {
        data: { items: [{ sku_id: chosen.sku, quantity: chosen.quantity }], coupon_mode: 'auto', points: 0 },
      });
      if (ticket !== revision || principal !== scope()) return;
      if (!Array.isArray(value.coupon_options) || !Number.isSafeInteger(Number(value.subtotal_cents)))
        throw Error('优惠计算结果无效');
      cache.set(chosen.key, { value, at: Date.now(), loading: false });
    } catch (error) {
      if (ticket !== revision || principal !== scope()) return;
      cache.set(chosen.key, { error: error.message, at: Date.now(), loading: false });
    } finally {
      if (ticket === revision && principal === scope()) redraw();
    }
  }
  function afterRender() {
    const chosen = selection();
    if (!chosen || !window.LumoAuth?.isLoggedIn()) {
      clearTimeout(timer);
      pending = null;
      return;
    }
    const saved = cache.get(chosen.key);
    if (pending === chosen.key && (!saved || saved.loading || Date.now() - saved.at < 30000)) return;
    pending = chosen.key;
    clearTimeout(timer);
    // Changing a SKU or quantity invalidates the previous response immediately.
    revision++;
    for (const [key, row] of cache) if (row.loading) cache.delete(key);
    timer = setTimeout(() => {
      void calculate(chosen);
    }, 200);
  }
  document.addEventListener('click', (event) => {
    const trigger = event.target.closest('[data-product-coupon]');
    if (!trigger) return;
    const chosen = selection();
    if (!chosen || !window.LumoAuth?.isLoggedIn()) return;
    if (trigger.dataset.productCoupon === 'retry') {
      void calculate(chosen, true);
      return;
    }
    const row = cache.get(chosen.key);
    if (!row?.value) return;
    const items = (row.value.coupon_options || []).filter((item) => item.eligible);
    modal(
      '当前商品可用优惠券',
      `${items.map((item) => `<div class="coupon-card"><span class="price">减 ¥${money(Number(item.discount_cents) / 100)}</span><div><strong>${esc(item.name || '优惠券')}${item.selected ? ' · 最佳优惠' : ''}</strong></div></div>`).join('')}<a class="text-button" href="#/coupons" data-action="close-modal">领券中心 ${icon('chevron')}</a>`,
    );
  });
  for (const event of ['lumo:login', 'lumo:logout', 'lumo:session-expired', 'lumo:ready'])
    window.addEventListener(event, () => {
      reset();
      afterRender();
    });
  window.addEventListener('hashchange', () => {
    pending = null;
    clearTimeout(timer);
    revision++;
    for (const [key, row] of cache) if (row.loading) cache.delete(key);
  });
  window.LumoProductCoupons = { summary, afterRender, reset };
})();
