(function () {
  'use strict';
  let quote = null,
    key = '',
    busy = false,
    error = '';
  const owner = () =>
    JSON.stringify([
      LumoAPI.getBase(),
      LumoAPI.getToken(),
      location.hash,
      detail,
      state.address?.id,
      state.address?.region,
    ]);
  function summary(value) {
    const p = value?.promise;
    if (!p?.available) return esc(p?.reason || '配送时效暂未配置');
    return `${p.warehouse ? esc(p.warehouse) + '发货 · ' : ''}预计 ${esc(p.dispatch_by)} 前发出，${esc(p.delivery_from)} 至 ${esc(p.delivery_to)} 送达。${esc(p.basis)}`;
  }
  function service(p) {
    const s = p?.service_policy;
    if (!s?.configured)
      return esc(typeof p?.warranty === 'string' ? p.warranty : '商家暂未配置专属退换与保修说明，请咨询客服。');
    return `${s.return_days > 0 ? `收货后 ${Number(s.return_days)} 天可按说明申请退换` : '退换条件见下方说明'}；退货运费：${esc({ buyer: '买家承担', seller: '商家承担', case_by_case: '按售后责任认定' }[s.return_shipping] || '按售后责任认定')}；${s.warranty_months > 0 ? `保修 ${Number(s.warranty_months)} 个月` : '保修条件见说明'}。${esc(s.explanation)}`;
  }
  function view() {
    if (key !== owner()) quote = null;
    return `<button type="button" class="text-button" data-delivery-estimate ${busy ? 'disabled' : ''}>${busy ? '查询中…' : '查看运费与送达预估'}</button><small role="status">${error ? esc(error) : quote ? summary(quote) : '选择地址后查询；时效按商家已配置规则估算。'}</small>`;
  }
  document.addEventListener('click', async (e) => {
    if (!e.target.closest('[data-delivery-estimate]') || busy) return;
    if (!state.address) {
      LumoAuth.requireLogin(location.hash) && document.querySelector('[data-action="address"]')?.click();
      return;
    }
    const ticket = owner(),
      p = actualProduct(detail.id),
      v = p?.variants?.find((v) => v.color === detail.color && v.option === detail.option);
    busy = true;
    error = '';
    renderKeepScroll();
    try {
      const r = await LumoAPI.call(
        'logistics',
        'quote',
        {
          data: {
            address: { region: state.address.region },
            subtotal_cents: Number(v?.price_cents || p?.price_cents) * Number(detail.qty || 1),
            quantity: Number(detail.qty || 1),
          },
        },
        { auth: false },
      );
      if (ticket === owner()) {
        quote = r;
        key = ticket;
      }
    } catch (err) {
      if (ticket === owner()) error = err.message;
    } finally {
      busy = false;
      renderKeepScroll();
    }
  });
  window.LumoDelivery = { view, summary, service };
})();
