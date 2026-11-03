(function () {
  'use strict';
  const C = window.LumoConstants;
  const U = window.LumoAdminUI,
    kinds = { late_pickup: '超时未揽收', stalled: '轨迹长时间未更新', carrier_exception: '渠道配送异常' },
    names = {
      CREATED: '运单创建',
      PICKED_UP: '已揽收',
      IN_TRANSIT: '运输中',
      OUT_FOR_DELIVERY: '派送中',
      DELIVERED: '已签收',
      EXCEPTION: '配送异常',
    };
  const m = U.controller(
    'shipments-admin-root',
    () => ({
      data: null,
      tracking: null,
      trackingID: '',
      status: C.DeliveryExceptionStatus.Open,
      page: 1,
      busy: false,
      error: '',
    }),
    async (module) => ({
      data: await module.adapter.call('logistics', 'exceptions', {
        data: { status: module.state.status, page: module.state.page, page_size: 20 },
      }),
      ...(module.state.trackingID
        ? { tracking: await module.adapter.call('logistics', 'tracking', { id: module.state.trackingID }) }
        : {}),
    }),
    (module) => {
      if (!module.state.data)
        return U.heading('正在读取管理数据', '') + window.LumoSkeleton.render('rows', { count: 4 });
      const s = module.state;
      return (
        U.heading('物流轨迹与异常件', '查看配送进展，跟进超时揽收、停滞与渠道异常。') +
        `<section class="admin-section"><form id="shipment-query-form" class="admin-query-form"><label class="field">运单、订单或履约编号<input name="tracking_ref" required></label><button class="primary">查询轨迹</button></form>${(s.tracking?.shipments || []).map((row) => `<h3>${U.esc(row.tracking_id)} · ${names[row.status] || U.esc(row.status)}</h3><ol>${row.events.map((e) => `<li><strong>${names[e.status]}</strong> ${U.esc(e.occurred_at)} ${U.esc(e.location)} ${U.esc(e.description)} · ${U.esc(e.source)}</li>`).join('')}</ol>`).join('')}</section><section class="admin-section"><h2>异常件</h2><div class="admin-list-toolbar shipment-filters"><label class="admin-status-filter">状态<select data-shipment-filter><option value="${C.DeliveryExceptionStatus.Open}" ${s.status === C.DeliveryExceptionStatus.Open ? 'selected' : ''}>待处理</option><option value="${C.DeliveryExceptionStatus.Resolved}" ${s.status === C.DeliveryExceptionStatus.Resolved ? 'selected' : ''}>已处理</option><option value="" ${!s.status ? 'selected' : ''}>全部</option></select></label></div><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>运单 / 异常</th><th>内容</th><th>处理结果</th><th>操作</th></tr></thead><tbody>${(s.data?.items || []).map((row) => `<tr><td>${U.esc(row.tracking_id)}<br>${kinds[row.kind] || U.esc(row.kind)}</td><td>${U.esc(row.details)}</td><td>${U.esc(row.resolution || '待处理')}<br>${U.esc(row.resolved_by)}</td><td>${row.status === C.DeliveryExceptionStatus.Open ? `<button class="outline" data-shipment-resolve="${U.esc(row.id)}">登记处理结果</button>` : ''}</td></tr>`).join('')}</tbody></table></div><div class="admin-pagination"><button class="outline" data-shipment-page="${s.page - 1}" ${s.page <= 1 ? 'disabled' : ''}>上一页</button><span>共 ${s.data?.total || 0} 条</span><button class="outline" data-shipment-page="${s.page + 1}" ${s.page * 20 >= Number(s.data?.total || 0) ? 'disabled' : ''}>下一页</button></div></section><section class="admin-section"><h2>人工记录轨迹</h2><form id="shipment-update-form" class="admin-form"><label class="field">运单号<input name="tracking_id" required maxlength="100"></label><label class="field">状态<select name="status">${Object.entries(
          names,
        )
          .map(([key, label]) => `<option value="${key}">${label}</option>`)
          .join(
            '',
          )}</select></label><label class="field">发生时间<input name="occurred_at" type="datetime-local" step="1" required></label><label class="field">所在地<input name="location" maxlength="160"></label><label class="field">进展说明<input name="description" required maxlength="500"></label><button class="primary">保存人工记录</button></form></section>`
      );
    },
  );
  document.addEventListener('change', async (event) => {
    if (event.target.matches('[data-shipment-filter]') && m.active()) {
      m.state.status = event.target.value;
      m.state.page = 1;
      await m.refresh();
    }
  });
  document.addEventListener('click', async (event) => {
    if (!m.active()) return;
    const b = event.target.closest('[data-shipment-resolve],[data-shipment-page]');
    if (!b) return;
    if (b.dataset.shipmentPage) {
      m.state.page = Number(b.dataset.shipmentPage);
      await m.refresh();
      return;
    }
    const row = m.state.data.items.find((r) => r.id === b.dataset.shipmentResolve),
      resolution = prompt('请填写已经采取的处理措施');
    if (!row || !resolution) return;
    const ok = await m.run(() =>
      m.adapter.call('logistics', 'exception_resolve', {
        id: row.id,
        data: { resolution, expected_version: row.version },
      }),
    );
    if (ok) await m.refresh();
  });
  document.addEventListener('submit', async (event) => {
    if (!m.active() || !['shipment-query-form', 'shipment-update-form'].includes(event.target.id)) return;
    event.preventDefault();
    const form = event.target,
      data = Object.fromEntries(new FormData(form));
    if (form.id === 'shipment-query-form') {
      m.state.trackingID = data.tracking_ref;
      await m.refresh();
      return;
    }
    const ok = await m.run(async () => {
      form.dataset.key ||= crypto.randomUUID();
      data.occurred_at = new Date(data.occurred_at).toISOString();
      await m.adapter.call('logistics', 'tracking_update', { id: form.dataset.key, data });
      m.state.trackingID = data.tracking_id;
    });
    if (ok) await m.refresh();
  });
  m.openDetail = async (id) => {
    m.state.trackingID = id;
    await m.refresh();
  };
  window.LumoShipmentsAdmin = m;
})();
