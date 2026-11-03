(function () {
  'use strict';
  let result = null,
    signature = '',
    busy = false,
    error = '';
  const owner = () => JSON.stringify([LumoAPI.getBase(), LumoAPI.getToken(), LumoCommerce.quoteData()]);
  const cents = (v) => (Number(v || 0) / 100).toFixed(2);
  function view() {
    if (signature && signature !== owner()) result = null;
    return `<div class="offer-optimizer"><button class="outline" data-offer="compare" ${busy ? 'disabled' : ''}>${busy ? '正在比较优惠…' : '比较最优优惠方案'}</button><small>比较已配置且可参与的活动；积分由你选择是否使用。</small>${error ? `<p role="alert">${esc(error)}</p>` : ''}${result ? `<p>推荐实付 <strong>¥${cents(result.quote.payable_cents)}</strong> · 可再省 ¥${cents(result.savings_cents)}</p><button class="primary" data-offer="apply">应用此方案</button>${result.topups?.length ? `<h3>按差额选一件</h3>${result.topups.map((r, i) => `<p>${esc(r.name)} · 商品 ¥${cents(r.price_cents)} · 预计新增实付 ¥${cents(r.additional_payable_cents)} <button class="outline" data-offer="topup" data-index="${i}">加购并去购物车</button></p>`).join('')}<small>凑单是额外购买，优惠和库存以重新报价为准。</small>` : ''}` : ''}</div>`;
  }
  document.addEventListener('click', async (event) => {
    const el = event.target.closest('[data-offer]');
    if (!el || busy) return;
    const ticket = owner();
    busy = true;
    error = '';
    renderKeepScroll();
    try {
      if (el.dataset.offer === 'compare') {
        const value = await LumoAPI.call('pricing', 'optimize', { data: LumoCommerce.quoteData() });
        if (ticket !== owner()) return;
        result = value;
        signature = ticket;
      } else if (result && signature === ticket && el.dataset.offer === 'apply') {
        const q = result.quote;
        if (q.activity_id) {
          if (
            !(await LumoPromotions.selectCampaign(
              q.activity_id,
              selectedItems(),
              checkoutMode === CommerceEnums.OrderSource.Direct,
            ))
          )
            return;
        } else LumoPromotions.clearCampaign();
        if (ticket !== signature || !LumoAuth.isLoggedIn()) return;
        LumoMember.selectCoupon(q.coupon_id || '0');
        LumoCommerce.invalidateQuote();
        result = null;
        await LumoCommerce.ensureQuote(true);
      } else if (result && signature === ticket && el.dataset.offer === 'topup') {
        const r = result.topups[Number(el.dataset.index)];
        if (!r) return;
        await LumoAPI.call('cart', 'add', { data: { sku_id: r.sku_id, quantity: 1 } });
        if (ticket !== owner()) return;
        result = null;
        await LumoCommerce.refreshCart();
        LumoCommerce.invalidateQuote();
        checkoutMode = CommerceEnums.OrderSource.Cart;
        directItem = null;
        go('cart');
      }
    } catch (e) {
      if (ticket === owner()) error = e.message;
    } finally {
      busy = false;
      renderKeepScroll();
    }
  });
  function reset() {
    result = null;
    signature = '';
    error = '';
    busy = false;
  }
  window.addEventListener('lumo:login', reset);
  window.addEventListener('lumo:logout', reset);
  window.addEventListener('lumo:api-base-changed', reset);
  window.LumoOfferOptimizer = { view };
})();
