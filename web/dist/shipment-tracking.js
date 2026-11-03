(function () {
  'use strict';
  const C = window.LumoConstants;
  let ticket = 0;
  const names = {
    CREATED: '运单已创建',
    PICKED_UP: '已揽收',
    IN_TRANSIT: '运输中',
    OUT_FOR_DELIVERY: '派送中',
    DELIVERED: '已签收',
    EXCEPTION: '配送异常',
  };
  function summary(plan, items = []) {
    if (!plan?.lines?.length) return '';
    const title = (sku) => items.find((i) => (i.sku_id || i.skuId) === sku)?.name || sku;
    return `<section class="parcel-progress-summary"><div class="parcel-progress-heading"><h3>商品配送进度</h3><span>${plan.parcels?.filter((p) => ![C.FulfillmentStatus.Cancelled, C.FulfillmentStatus.Invalidated].includes(p.status)).length || 0} 个有效包裹</span></div><div class="parcel-progress-lines">${plan.lines.map((l) => `<article><div><strong>${esc(title(l.sku_id))}</strong><small>购买 ${l.quantity} 件${l.cancelled ? ' · 已取消 ' + l.cancelled + ' 件' : ''}</small></div><div class="parcel-progress-counts"><span>已发货 <b>${l.shipped}</b></span><span>待发货 <b>${Math.max(0, l.quantity - l.cancelled - l.shipped)}</b></span>${l.held ? `<span>售后拦截 ${l.held}</span>` : ''}</div></article>`).join('')}</div><p class="secure-note">各包裹独立更新轨迹，全部待发商品完成发货后才能整单确认收货。</p></section>`;
  }
  const lines = (raw) => {
    try {
      return JSON.parse(raw || '[]');
    } catch (_) {
      return [];
    }
  };
  async function open(id, offset = 0) {
    const current = ++ticket,
      scope = window.LumoAPI.getBase() + '|' + window.LumoAPI.getToken();
    try {
      const [data, plan] = await Promise.all([
        window.LumoAPI.call('logistics', 'tracking', { id, data: { event_offset: offset } }).catch((e) => {
          if (e.status === 404 || e.code === 'NOT_FOUND' || e.code === 'NotFound') return { shipments: [] };
          throw e;
        }),
        window.LumoAPI.call('fulfillment', 'line_state', { id }).catch(() => null),
      ]);
      const items = window.LumoCommerce?.allOrders?.().find?.((o) => o.id === id)?.items || [];
      if (current !== ticket || scope !== window.LumoAPI.getBase() + '|' + window.LumoAPI.getToken()) return;
      modal(
        '物流轨迹',
        summary(plan, items) +
          (data.shipments.length ? '' : '<div class="tracking-empty">所选商品尚未生成运单，发货后可在此查看。</div>') +
          data.shipments
            .map(
              (row) =>
                `<section class="tracking-shipment"><div class="parcel-progress-heading"><strong>包裹 ${data.shipments.indexOf(row) + 1}</strong><span>${esc(row.tracking_id)}</span></div><h3>${esc(names[row.status] || row.status)}</h3><p>运单 ${esc(row.tracking_id)} · ${esc(row.provider)}</p>${
                  lines(row.lines_json).length
                    ? `<ul class="tracking-parcel-items">${lines(row.lines_json)
                        .map(
                          (l) =>
                            `<li>${esc(items.find((i) => i.sku_id === l.sku_id)?.name || l.sku_id)}<strong>× ${l.quantity}</strong></li>`,
                        )
                        .join('')}</ul>`
                    : ''
                }${row.provider === 'local-demo-carrier' ? '<p class="demo-note">当前为演示物流，轨迹由授权人员录入。</p>' : ''}<ol class="tracking-timeline">${
                  [...row.events]
                    .sort((a, b) => Date.parse(b.occurred_at) - Date.parse(a.occurred_at))
                    .map(
                      (event) =>
                        `<li><strong>${esc(names[event.status] || event.status)}</strong><time>${esc(new Date(event.occurred_at).toLocaleString('zh-CN'))}</time><p>${esc(event.location)} ${esc(event.description)}</p><small>${event.source === 'staff' ? '人工记录' : event.source === 'carrier' ? '物流渠道同步' : '运单记录'}</small></li>`,
                    )
                    .join('') || '<li>运单已登记，等待物流渠道更新。</li>'
                }</ol><p>共 ${row.events_total} 条轨迹，本页最多100条。</p>${row.has_more_events ? `<button class="outline" data-track-more="${esc(row.id)}" data-offset="${row.next_event_offset}">查看更早轨迹</button>` : ''}${offset > 0 ? `<button class="outline" data-track-more="${esc(row.id)}" data-offset="0">返回最新轨迹</button>` : ''}</section>`,
            )
            .join(''),
      );
    } catch (error) {
      toast(error.message);
    }
  }
  document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-track-more]');
    if (button) void open(button.dataset.trackMore, Number(button.dataset.offset));
  });
  window.addEventListener('lumo:logout', () => ticket++);
  window.LumoShipmentTracking = { open, summary };
})();
