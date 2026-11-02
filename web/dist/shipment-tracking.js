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
  async function open(id, offset = 0) {
    const current = ++ticket,
      scope = window.LumoAPI.getBase() + '|' + window.LumoAPI.getToken();
    try {
      const data = await window.LumoAPI.call('logistics', 'tracking', { id, data: { event_offset: offset } });
      if (current !== ticket || scope !== window.LumoAPI.getBase() + '|' + window.LumoAPI.getToken()) return;
      modal(
        '物流轨迹',
        data.shipments
          .map(
            (row) =>
              `<section class="tracking-shipment"><h3>${esc(names[row.status] || row.status)}</h3><p>运单 ${esc(row.tracking_id)} · ${esc(row.provider)}</p>${row.provider === 'local-demo-carrier' ? '<p class="demo-note">当前为演示物流，轨迹由授权人员录入。</p>' : ''}<ol class="tracking-timeline">${
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
  window.LumoShipmentTracking = { open };
})();
