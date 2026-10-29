(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc, money, date, parse, pair } = U;
  const specs = {
    orders: {
      name: '订单管理',
      service: C.Service.Order,
      note: '核对订单金额、支付、商品快照与收货信息。',
      columns: [
        ['来源', 'source'],
        ['商品件数', 'items', 'quantity'],
        ['支付单', 'payment_id'],
      ],
      fields: [
        ['订单来源', 'source'],
        ['支付单', 'payment_id'],
        ['累计退款', 'refunded_total_cents', 'money'],
        ['支付截止', 'expires_at', 'date'],
        ['订单备注', 'note'],
      ],
    },
    payments: {
      name: '支付管理',
      service: C.Service.Payment,
      note: '核对支付申请、渠道回执与实际入账。',
      columns: [
        ['渠道', 'provider'],
        ['渠道回执', 'receipt'],
        ['支付时间', 'paid_at', 'date'],
      ],
      fields: [
        ['渠道', 'provider'],
        ['渠道回执', 'receipt'],
        ['实际支付时间', 'paid_at', 'date'],
      ],
    },
    fulfillment: {
      name: '仓储履约',
      service: C.Service.Fulfillment,
      note: '跟进商品出库、物流签收及退货验收。',
      columns: [
        ['履约类型', 'kind'],
        ['商品件数', 'lines', 'quantity'],
        ['物流单号', 'tracking_id'],
      ],
      fields: [
        ['履约类型', 'kind'],
        ['预占单号', 'reservation_id'],
        ['物流单号', 'tracking_id'],
      ],
    },
    aftersales: {
      name: '售后管理',
      service: C.Service.Aftersale,
      note: '查看申请原因、商品、退货凭证与退款进度。',
      columns: [
        ['售后类型', 'type'],
        ['申请原因', 'reason'],
        ['退货单号', 'return_tracking'],
      ],
      fields: [
        ['申请类型', 'type'],
        ['申请原因', 'reason'],
        ['支付单', 'payment_id'],
        ['退款单', 'refund_id'],
        ['退货单号', 'return_tracking'],
        ['验收品质', 'condition'],
        ['换货 SKU', 'replacement_sku'],
        ['换货履约单', 'replacement_id'],
      ],
    },
    refunds: {
      name: '退款管理',
      service: C.Service.Refund,
      note: '核对退款金额、原支付单及关联售后。',
      columns: [
        ['原支付单', 'payment_id'],
        ['退款原因', 'reason'],
        ['退款渠道', 'provider'],
      ],
      fields: [
        ['原支付单', 'payment_id'],
        ['关联售后', 'aftersale_id'],
        ['退款原因', 'reason'],
        ['渠道', 'provider'],
      ],
    },
    invoices: {
      name: '发票管理',
      service: C.Service.Invoice,
      note: '管理开票申请、已开金额与退款红冲。',
      columns: [
        ['发票抬头', 'title'],
        ['发票号码', 'number'],
        ['已开 / 已退', 'invoice', 'invoice'],
      ],
      fields: [
        ['发票类型', 'kind'],
        ['抬头', 'title'],
        ['税号', 'tax_id'],
        ['发票号码', 'number'],
        ['已开金额', 'issued_cents', 'money'],
        ['已退款金额', 'refunded_cents', 'money'],
        ['渠道', 'provider'],
      ],
    },
  };
  const names = {
    cart: '购物车',
    buy_now: '立即购买',
    direct: '直接购买',
    refund: '仅退款',
    return: '退货退款',
    exchange: '换货',
    order: '订单出库',
    sellable: '可售',
    damaged: '破损',
    unsellable: '不可售',
  };
  const statuses = {
    orders: {
      [C.OrderStatus.Creating]: '创建中',
      [C.OrderStatus.Created]: '已创建',
      [C.OrderStatus.CreateFailed]: '创建失败',
      [C.OrderStatus.AwaitingPayment]: '等待支付',
      [C.OrderStatus.PendingPayment]: '待支付',
      [C.OrderStatus.AwaitingGroup]: '等待成团',
      [C.OrderStatus.WaitingGroup]: '等待成团',
      [C.OrderStatus.PaidWaitGroup]: '已支付待成团',
      [C.OrderStatus.Paid]: '已支付',
      [C.OrderStatus.PaidConfirming]: '收款确认中',
      [C.OrderStatus.ReadyToFulfill]: '待履约',
      [C.OrderStatus.Fulfilling]: '履约中',
      [C.OrderStatus.Shipped]: '已发货',
      [C.OrderStatus.Delivered]: '已送达',
      [C.OrderStatus.Completed]: '已完成',
      [C.OrderStatus.Closing]: '关闭中',
      [C.OrderStatus.Closed]: '已关闭',
      [C.OrderStatus.Cancelled]: '已取消',
      [C.OrderStatus.Terminated]: '已终止',
      [C.OrderStatus.Refunded]: '已退款',
      [C.OrderStatus.PartiallyRefunded]: '部分退款',
    },
    payments: {
      [C.PaymentStatus.Pending]: '待确认收款',
      [C.PaymentStatus.Succeeded]: '支付成功',
      [C.PaymentStatus.Closed]: '已关闭',
    },
    fulfillment: {
      [C.FulfillmentStatus.Ready]: '待出库',
      [C.FulfillmentStatus.Dispatching]: '出库中',
      [C.FulfillmentStatus.Dispatched]: '已发货',
      [C.FulfillmentStatus.CancelRequested]: '申请拦截',
      [C.FulfillmentStatus.WarehouseCancelConfirmed]: '仓库已确认拦截',
      [C.FulfillmentStatus.Cancelled]: '已取消',
      [C.FulfillmentStatus.Inspecting]: '验收中',
      [C.FulfillmentStatus.Inspected]: '已验收',
    },
    aftersales: {
      [C.AftersaleStatus.Requested]: '待审核',
      [C.AftersaleStatus.Approved]: '已批准',
      [C.AftersaleStatus.Returning]: '退货中',
      [C.AftersaleStatus.Inspecting]: '验收中',
      [C.AftersaleStatus.Inspected]: '已验收',
      [C.AftersaleStatus.Rejected]: '已驳回',
      [C.AftersaleStatus.RefundRequested]: '已申请退款',
      [C.AftersaleStatus.Refunding]: '退款中',
      [C.AftersaleStatus.Completed]: '售后已完成',
      [C.AftersaleStatus.ExchangePreparing]: '换货准备中',
      [C.AftersaleStatus.Exchanging]: '换货中',
      [C.AftersaleStatus.Exchanged]: '已换货',
      [C.AftersaleStatus.Cancelled]: '已取消',
    },
    refunds: {
      [C.RefundStatus.Pending]: '待退款',
      [C.RefundStatus.Processing]: '退款处理中',
      [C.RefundStatus.Succeeded]: '退款成功',
    },
    invoices: {
      [C.InvoiceStatus.Requested]: '待开票',
      [C.InvoiceStatus.Issued]: '已开票',
      [C.InvoiceStatus.Adjusted]: '已红冲',
      [C.InvoiceStatus.AdjustedRequest]: '已调整开票申请',
      [C.InvoiceStatus.ReversalPending]: '待红冲',
      [C.InvoiceStatus.Cancelled]: '已取消',
      [C.InvoiceStatus.Void]: '已作废',
    },
  };
  const label = (value) => names[value] || value || '—';
  const statusText = (key, value) => statuses[key]?.[value] || value || '—';
  const badge = (key, value) =>
    `<span class="admin-state ${[C.PaymentStatus.Pending, C.FulfillmentStatus.Ready, C.AftersaleStatus.Requested].includes(value) ? 'pending' : 'neutral'}">${esc(statusText(key, value))}</span>`;
  function lines(row) {
    const value = parse(row.lines_json) || row.lines || row.items;
    return Array.isArray(value) ? value : [];
  }
  function cell(row, key, type) {
    if (type === 'date') return esc(date(row[key]));
    if (type === 'quantity') return String(lines(row).reduce((sum, item) => sum + Number(item.quantity || 0), 0));
    if (type === 'invoice') return `${money(row.issued_cents)}<small>已退 ${money(row.refunded_cents)}</small>`;
    const text = String(label(row[key]));
    return text.length > 96
      ? `<span class="admin-copy-preview">${esc(text.slice(0, 96))}…</span><small>完整内容见详情</small>`
      : esc(text);
  }
  function available(key, row) {
    if (key === 'payments' && row.status === C.PaymentStatus.Pending) return [[C.Action.ConfirmDemo, '确认演示收款']];
    if (key === 'fulfillment') {
      if (row.status === C.FulfillmentStatus.Ready)
        return [
          [C.Action.Dispatch, '确认出库'],
          [C.Action.Cancel, '拦截取消'],
        ];
      if (row.status === C.FulfillmentStatus.Dispatched) return [[C.Action.Deliver, '确认演示签收']];
    }
    if (key === 'aftersales') {
      if (row.status === C.AftersaleStatus.Requested)
        return [
          [C.Action.Approve, '批准申请'],
          [C.Action.Reject, '驳回申请'],
        ];
      if (row.status === C.AftersaleStatus.Returning) return [[C.Action.Inspect, '验收退货']];
      if (
        (row.type === C.AftersaleType.Refund && row.status === C.AftersaleStatus.Approved) ||
        (row.type === C.AftersaleType.Return && row.status === C.AftersaleStatus.Inspected)
      )
        return [[C.Action.Refund, '发起退款']];
      if (row.type === C.AftersaleType.Exchange && row.status === C.AftersaleStatus.Inspected)
        return [[C.Action.Exchange, '创建换货履约']];
    }
    if (key === 'refunds' && [C.RefundStatus.Pending, C.RefundStatus.Processing].includes(row.status))
      return [[C.Action.ConfirmDemo, '确认演示退款']];
    if (key === 'invoices')
      return [
        ...(row.status === C.InvoiceStatus.Requested ? [[C.Action.IssueDemo, '演示开票']] : []),
        [C.Action.AdjustDemo, '处理红冲'],
      ];
    return [];
  }
  function create(key) {
    const spec = specs[key],
      prefix = 'op-' + key;
    async function fetch(module) {
      const s = module.state;
      const data = await module.adapter.call(spec.service, C.Action.AdminList, {
        data: { ...s.filters, page: s.page, page_size: 20 },
      });
      if (!Array.isArray(data.items) || !Number.isSafeInteger(Number(data.total))) throw Error('业务列表返回格式无效');
      return { items: data.items, total: Number(data.total), ready: true };
    }
    const m = U.controller(
      prefix,
      () => ({
        items: [],
        total: 0,
        page: 1,
        filters: { keyword: '', status: '', from: '', to: '' },
        ready: false,
        detail: null,
        order: null,
        action: null,
        busy: false,
        error: '',
      }),
      fetch,
      view,
    );
    function view(module) {
      const s = module.state,
        heading = U.heading(
          spec.name,
          spec.note,
          `<button class="outline" data-op="reload" ${s.busy ? 'disabled' : ''}>刷新</button>`,
        );
      if (s.detail) return heading + detailView(s);
      const stateValues = [
        ...new Set(
          Object.values(
            C[
              key === 'orders'
                ? 'OrderStatus'
                : key === 'payments'
                  ? 'PaymentStatus'
                  : key === 'fulfillment'
                    ? 'FulfillmentStatus'
                    : key === 'aftersales'
                      ? 'AftersaleStatus'
                      : key === 'refunds'
                        ? 'RefundStatus'
                        : 'InvoiceStatus'
            ],
          ).filter((x) => typeof x === 'string'),
        ),
      ];
      return (
        heading +
        `<section class="admin-list-panel"><form data-op-filter class="op-filters"><label class="field">搜索记录<input name="keyword" type="search" value="${esc(s.filters.keyword)}" placeholder="订单号、用户或业务编号"></label><label class="field">状态<select name="status"><option value="">全部状态</option>${stateValues.map((v) => `<option value="${esc(v)}" ${s.filters.status === v ? 'selected' : ''}>${esc(statusText(key, v))}</option>`).join('')}</select></label><label class="field">开始日期<input name="from" type="date" value="${esc(s.filters.from)}"></label><label class="field">结束日期<input name="to" type="date" value="${esc(s.filters.to)}"></label><button class="primary" ${s.busy ? 'disabled' : ''}>查询</button><button class="outline" type="button" data-op="reset">重置</button></form><div class="admin-list-summary"><span>共 ${s.total.toLocaleString('zh-CN')} 条记录</span><span>按创建时间倒序 · 业务时区 UTC+8</span></div>${!s.ready ? (s.error && !s.busy ? U.empty('数据读取失败', '请点击上方重试。') : window.LumoSkeleton.render('table', { count: 6 })) : !s.items.length ? U.empty('暂无符合条件的业务记录') : `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table op-table"><thead><tr><th>业务单 / 订单</th><th>用户</th>${key !== 'fulfillment' ? '<th>金额</th>' : ''}${spec.columns.map(([title]) => `<th>${esc(title)}</th>`).join('')}<th>状态 / 创建时间</th><th>操作</th></tr></thead><tbody>${s.items.map((row) => `<tr><td><strong>${esc(row.id)}</strong><small>订单 ${esc(row.order_id || '—')}</small></td><td>${esc(row.user_id || '—')}</td>${key !== 'fulfillment' ? `<td class="admin-amount">${money(row.amount_cents ?? row.payable_cents)}</td>` : ''}${spec.columns.map(([, field, type]) => `<td>${cell(row, field, type)}</td>`).join('')}<td>${badge(key, row.status)}<small>${esc(date(row.created_at))}</small></td><td><button class="outline" data-op="detail" data-id="${esc(row.id)}">查看详情</button></td></tr>`).join('')}</tbody></table></div>${U.pager(s.total, s.page, 20, 'op', s.busy)}`}</section>`
      );
    }
    function detailView(s) {
      const row = s.detail;
      const order = s.order || {};
      const address = parse(order.address) || order.address || {};
      const quote = parse(order.quote) || order.quote || {};
      const items = Array.isArray(quote.items) ? quote.items : Array.isArray(quote.lines) ? quote.lines : lines(row);
      return `<div class="op-detail-heading"><button class="outline" data-op="back">← 返回列表</button><span>${badge(key, row.status)}</span></div><div class="op-detail-grid"><section class="admin-section"><div class="admin-panel-heading"><h2>业务单据</h2><span>${esc(row.id)}</span></div><dl class="op-definition">${pair('业务单号', row.id)}${pair('关联订单', row.order_id || (key === 'orders' ? row.id : '—'))}${pair('用户编号', row.user_id)}${key !== 'fulfillment' ? pair(key === 'orders' ? '订单应付' : '单据金额', money(row.amount_cents ?? row.payable_cents)) : ''}${spec.fields.map(([name, field, type]) => pair(name, type === 'money' ? money(row[field]) : type === 'date' ? date(row[field]) : label(row[field]))).join('')}${pair('创建时间', date(row.created_at))}${pair('更新时间', date(row.updated_at))}</dl></section><section class="admin-section"><div class="admin-panel-heading"><h2>关联订单与收货信息</h2><span>读取订单快照</span></div>${s.orderError ? `<p class="admin-inline-note">${esc(s.orderError)}</p>` : s.order ? `<dl class="op-definition">${pair('订单状态', statusText('orders', order.status))}${pair('订单应付', money(order.payable_cents))}${pair('累计退款', money(order.refunded_total_cents))}${pair('商品金额', money(quote.subtotal_cents))}${pair('活动优惠', money(quote.campaign_discount_cents))}${pair('优惠券抵扣', money(quote.coupon_discount_cents))}${pair('积分抵扣', money(quote.points))}${pair('订单来源', label(order.source))}${pair('备注', order.note || '—')}${pair('收货人', address.name || address.recipient || address.receiver)}${pair('联系电话', address.phone)}${pair('收货地址', [address.region, address.province, address.city, address.district, address.detail, address.address].filter(Boolean).join(' '))}</dl>` : U.empty('暂无关联订单快照', '此单据未关联订单或订单尚未读取。')}</section><section class="admin-section op-wide"><div class="admin-panel-heading"><h2>商品与数量</h2><span>${items.length} 行</span></div>${items.length ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table"><thead><tr><th>商品 / SKU</th><th>数量</th><th>单价</th></tr></thead><tbody>${items.map((line) => `<tr><td><strong>${esc(line.name || line.title || line.sku_id)}</strong><small>${esc(line.sku_id)}</small></td><td>${esc(line.quantity)}</td><td>${line.unit_price_cents != null ? money(line.unit_price_cents) : line.price_cents != null ? money(line.price_cents) : '—'}</td></tr>`).join('')}</tbody></table></div>` : U.empty('此单据没有商品行', '可通过关联订单核对商品。')}</section><section class="admin-section op-wide"><div class="admin-panel-heading"><h2>处理操作</h2><span>仅展示当前状态允许的操作</span></div><div class="admin-actions">${
        available(key, row)
          .map(
            ([action, name]) =>
              `<button class="outline" data-op="prepare" data-operation="${esc(action)}">${esc(name)}</button>`,
          )
          .join('') || '<span class="admin-muted">当前无需人工处理</span>'
      }${key === 'invoices' ? '<button class="outline" data-op="reversals">查看红冲记录</button>' : ''}</div>${s.action ? `<form data-op-confirm class="op-confirm"><h3>${esc(s.action.name)}</h3><p>业务单 ${esc(row.id)} · ${money(row.amount_cents ?? row.payable_cents)}。提交后将更新实际业务状态。</p>${s.action.operation === C.Action.Inspect ? '<label class="field">退货品质<select name="condition"><option value="sellable">可售</option><option value="damaged">破损</option><option value="unsellable">不可售</option></select></label>' : ''}<div class="admin-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>${s.busy ? '正在处理…' : '确认提交'}</button><button type="button" class="outline" data-op="cancel-action" ${s.busy ? 'disabled' : ''}>取消</button></div></form>` : ''}${s.reversals ? `<dl class="op-definition">${s.reversals.map((item) => pair(item.id, `${money(item.amount_cents)} · ${item.status}`)).join('') || '<p>暂无红冲记录</p>'}</dl>` : ''}</section></div>`;
    }
    document.addEventListener('click', async (event) => {
      const el = event.target.closest('[data-op],[data-op-page]');
      if (!el || !m.active() || !el.closest('#' + prefix) || m.state.busy) return;
      const s = m.state,
        action = el.dataset.op;
      if (el.dataset.opPage) {
        s.page = Number(el.dataset.opPage);
        await m.refresh();
        return;
      }
      if (action === 'reload') {
        s.detail = null;
        await m.refresh();
      }
      if (action === 'reset') {
        s.filters = { keyword: '', status: '', from: '', to: '' };
        s.page = 1;
        await m.refresh();
      }
      if (action === 'back') {
        s.detail = null;
        s.action = null;
        s.order = null;
        m.draw();
      }
      if (action === 'cancel-action') {
        s.action = null;
        m.draw();
      }
      if (action === 'prepare') {
        const item = available(key, s.detail).find(([a]) => a === el.dataset.operation);
        if (item) {
          s.action = { operation: item[0], name: item[1] };
          m.draw();
        }
      }
      if (action === 'detail') {
        const row = s.items.find((r) => r.id === el.dataset.id);
        if (!row) return;
        await m.run(async () => {
          const detail = await m.adapter.call(spec.service, C.Action.Get, { id: row.id });
          let order = null,
            orderError = '';
          if (key === 'orders') {
            order = detail;
          } else if (detail.order_id) {
            try {
              order = await m.adapter.call(C.Service.Order, C.Action.Get, { id: detail.order_id });
            } catch (e) {
              orderError = '关联订单暂不可用：' + e.message;
            }
          }
          return { detail, order, orderError, action: null, reversals: null };
        });
      }
      if (action === 'reversals')
        await m.run(async () => ({
          reversals: (await m.adapter.call(C.Service.Invoice, C.Action.Reversals, { id: s.detail.id })).items || [],
        }));
    });
    document.addEventListener('submit', async (event) => {
      if (!m.active() || !event.target.closest('#' + prefix)) return;
      if (event.target.matches('[data-op-filter]')) {
        event.preventDefault();
        if (m.state.busy) return;
        const form = new FormData(event.target);
        m.state.filters = Object.fromEntries(
          ['keyword', 'status', 'from', 'to'].map((key) => [key, String(form.get(key) || '')]),
        );
        m.state.page = 1;
        await m.refresh();
      }
      if (event.target.matches('[data-op-confirm]')) {
        event.preventDefault();
        const s = m.state;
        if (s.busy || !s.action || !s.detail) return;
        const form = new FormData(event.target),
          operation = s.action.operation,
          id = s.detail.id;
        await m.run(async () => {
          await m.adapter.call(operation === C.Action.Deliver ? C.Service.Logistics : spec.service, operation, {
            id,
            idempotency_key: crypto.randomUUID(),
            data: operation === C.Action.Inspect ? { condition: String(form.get('condition')) } : {},
          });
          const detail = await m.adapter.call(spec.service, C.Action.Get, { id });
          return { detail, action: null, ready: false };
        });
        if (!m.state.error) {
          m.adapter.notify?.('处理成功，业务状态已更新');
          await m.refresh();
        }
      }
    });
    return m;
  }
  window.LumoOperationsAdmin = { create };
})();
