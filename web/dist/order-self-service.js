(function () {
  'use strict';
  const C = LumoConstants;
  let revision = 0,
    current = null,
    busy = false,
    error = '',
    editing = false;
  const owner = () => JSON.stringify([LumoAPI.getBase(), LumoAPI.getToken()]);
  const date = (value) =>
    value ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '—';
  const names = {
    CREATED: '订单创建',
    SUCCEEDED: '支付已成功',
    PARTIALLY_DISPATCHED: '部分包裹已发货',
    PARTIALLY_SHIPPED: '部分发货',
    READY: '仓库待出库',
    DISPATCHING: '仓库处理中',
    DISPATCHED: '已出库',
    CANCELLED: '已取消',
    REQUESTED: '售后待审核',
    APPROVED: '售后审核通过',
    RETURNING: '等待退回商品',
    INSPECTED: '退货已验收',
    COMPLETED: '处理已完成',
    OPEN: '工单待处理',
    REPLIED: '客服已回复',
    CLOSED: '工单已关闭',
    PICKED_UP: '快递已揽收',
    IN_TRANSIT: '运输中',
    OUT_FOR_DELIVERY: '正在派送',
    DELIVERED: '已送达',
    EXCEPTION: '配送异常',
    address_changed: '收货地址已修改',
    warehouse_processing: '仓库开始处理',
    shipment_urged: '已提交催发货',
  };
  const source = {
    order: '订单',
    order_service: '订单服务',
    payment: '支付',
    fulfillment: '仓库',
    logistics: '物流',
    aftersale: '售后',
    helpdesk: '客服',
  };
  const active = (ticket, scope) =>
    ticket === revision && scope === owner() && !!document.getElementById('order-service-view');
  function render() {
    if (!current) return;
    const raw = current.order,
      o = LumoCommerce.normalizeOrder(raw),
      modifiable =
        !raw.dispatch_started && [C.OrderStatus.PendingPayment, C.OrderStatus.ReadyToFulfill].includes(raw.status),
      ready = raw.status === C.OrderStatus.ReadyToFulfill;
    const last = Date.parse(raw.last_urged_at),
      urged = Number.isFinite(last) && Date.now() - last < 86400000;
    const addresses = LumoMember.model().addresses || [];
    modal(
      '订单详情',
      `<section id="order-service-view" class="order-service-view"><div class="svc-conversation-header"><h3>${esc(LumoMember.orderLabel(o))}</h3><p>订单 ${esc(o.id)} · ${date(raw.created_at)}</p></div>${orderItems(o.items)}${amountLines(o.totals)}${window.LumoShipmentTracking?.summary(current.fulfillment?.plan, o.items) || ''}${current.fulfillment?.parcelized ? `<button class="outline" data-order-service="parcels">查看各包裹物流</button>` : ''}<dl class="service-definition"><div><dt>收货人</dt><dd>${esc(o.address.name)} · ${esc(o.address.phone.slice(0, 3) + '****' + o.address.phone.slice(-4))}</dd></div><div><dt>收货地址</dt><dd>${esc(o.address.region + ' ' + o.address.detail)}</dd></div></dl><div class="svc-modal-actions">${modifiable ? `<button class="outline" data-order-service="address" ${busy ? 'disabled' : ''}>修改收货地址</button>` : ''}${ready ? `<button class="outline" data-order-service="urge" ${busy || urged ? 'disabled' : ''}>${urged ? '已催发货 · 24 小时内请勿重复' : '催发货'}</button>` : ''}<button class="outline" data-order-service="support" ${busy ? 'disabled' : ''}>联系此订单客服</button><button class="outline" data-order-service="refresh" ${busy ? 'disabled' : ''}>刷新进度</button></div>${editing ? `<form id="order-change-address"><label class="field">选择本次收货地址<select name="address_id" required ${busy ? 'disabled' : ''}>${addresses.map((a) => `<option value="${esc(a.id)}" ${a.id === o.address.id ? 'selected' : ''}>${esc(a.name)} · ${esc(a.region + ' ' + a.detail)}</option>`).join('')}</select></label><p class="secure-note">仅在仓库开始处理前可修改。新地址须可配送，且运费与原订单一致。</p>${addresses.length ? '<button class="primary" ' + (busy ? 'disabled' : '') + '>确认修改地址</button>' : '<a class="outline" href="#/member/addresses" data-action="close-modal">先添加收货地址</a>'}</form>` : ''}${error ? `<p class="api-config-error" role="alert">${esc(error)}</p>` : ''}<h3 class="order-timeline-heading">订单服务进度</h3>${current.partial ? '<p class="secure-note">展示当前可读取的最近进展，部分记录暂不可用或超过展示上限。</p>' : ''}<ol class="service-timeline order-service-timeline" aria-label="订单全流程进度">${(current.events || []).map((e, i, all) => `<li class="done ${i === all.length - 1 ? 'current' : ''}"><strong>${esc(names[e.status] || e.status)}<small> · ${esc(source[e.kind] || '订单')}</small></strong><time>${date(e.at)}</time>${['aftersale', 'helpdesk'].includes(e.kind) ? `<p>${esc(e.id)}</p>` : ''}</li>`).join('')}</ol>${
        o.items.some((i) => i.service_policy?.configured)
          ? `<details><summary>查看下单时的商品服务政策</summary>${o.items
              .filter((i) => i.service_policy?.configured)
              .map((i) => `<p><strong>${esc(i.name)}</strong><br>${LumoDelivery.service(i)}</p>`)
              .join('')}</details>`
          : ''
      }</section>`,
    );
  }
  async function open(id) {
    if (!LumoAuth.requireLogin(location.hash)) return;
    const ticket = ++revision,
      scope = owner();
    current = null;
    busy = false;
    error = '';
    editing = false;
    modal(
      '订单详情',
      `<section id="order-service-view">${LumoSkeleton.render('rows', { count: 5, label: '正在读取订单服务进度' })}</section>`,
    );
    try {
      const r = await LumoAPI.call('checkout', 'timeline', { id });
      if (!active(ticket, scope)) return;
      current = r;
      render();
    } catch (e) {
      if (active(ticket, scope))
        modal(
          '订单详情',
          `<section id="order-service-view"><p role="alert">${esc(e.message)}</p><button class="outline" data-order-service="retry" data-id="${esc(id)}">重新加载</button></section>`,
        );
    }
  }
  document.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-order-service]');
    if (!b || b.disabled) return;
    const action = b.dataset.orderService;
    if (action === 'parcels') {
      await LumoShipmentTracking.open(current.order.id);
      return;
    }
    if (action === 'open' || action === 'retry') {
      await open(b.dataset.id);
      return;
    }
    if (!current || busy) return;
    const id = current.order.id,
      ticket = revision,
      scope = owner();
    if (action === 'support') {
      LumoServices.openOrderTicket(id);
      return;
    }
    if (action === 'refresh') {
      await open(id);
      return;
    }
    busy = true;
    error = '';
    render();
    try {
      if (action === 'address') {
        await LumoMember.refresh(true, ['addresses']);
        if (active(ticket, scope)) editing = true;
      }
      if (action === 'urge') {
        await LumoAPI.call('order', 'urge', { id });
        if (active(ticket, scope)) {
          toast('已通知商家跟进发货');
          await open(id);
        }
      }
    } catch (err) {
      if (active(ticket, scope)) error = err.message;
    } finally {
      if (active(ticket, scope)) {
        busy = false;
        render();
      }
    }
  });
  document.addEventListener('submit', async (e) => {
    if (e.target.id !== 'order-change-address') return;
    e.preventDefault();
    if (!current || busy) return;
    const ticket = revision,
      scope = owner(),
      id = current.order.id,
      data = new FormData(e.target);
    busy = true;
    error = '';
    render();
    try {
      await LumoAPI.call('checkout', 'change_address', {
        id,
        data: { address_id: String(data.get('address_id') || ''), expected_version: current.order.version },
      });
      if (!active(ticket, scope)) return;
      await LumoCommerce.refreshOrders();
      if (active(ticket, scope)) {
        toast('订单收货地址已更新');
        await open(id);
      }
    } catch (err) {
      if (active(ticket, scope)) error = err.message;
    } finally {
      if (active(ticket, scope)) {
        busy = false;
        render();
      }
    }
  });
  window.addEventListener('lumo:logout', () => {
    revision++;
    current = null;
  });
  window.LumoOrderService = { open };
})();
