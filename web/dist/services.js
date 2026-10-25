(function () {
  'use strict';
  const C = window.LumoConstants;
  const cleanText = (value, length = 100) => (typeof value === 'string' ? value.trim().slice(0, length) : '');
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const uid = (prefix) =>
    prefix + '-' + (crypto.randomUUID?.() || Date.now().toString(36) + Math.random().toString(36).slice(2));
  const date = (value) =>
    new Date(value).toLocaleString('zh-CN', {
      timeZone: 'Asia/Shanghai',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  const fullDate = (value) => new Date(value).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' });
  const loggedIn = () => !!window.LumoAuth?.isLoggedIn();
  const call = (service, action, command = {}, options) => window.LumoAPI.call(service, action, command, options);
  const owner = () => window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '';
  const ui = { messageFilter: C.MessageFilter.All, faqQuery: '', ticketFilter: 'all', invoiceForm: null };
  const FAQS = [];
  let memory;
  let version = 0,
    inFlight = null,
    faqBase = '',
    faqAt = 0,
    dialogVersion = 0;
  function emptyData() {
    return {
      messages: [],
      unreadTotal: null,
      invoices: [],
      tickets: [],
      chat: [],
      checkout: { enabled: false, kind: C.InvoiceKind.Personal, title: '个人', taxId: '' },
    };
  }
  memory = emptyData();
  const serviceScope = () =>
    JSON.stringify([owner(), window.LumoAPI?.getToken?.() || '', window.LumoAPI?.getBase?.() || location.origin]);
  let memoryScope = serviceScope();
  const emptyReadStates = () =>
    Object.fromEntries(
      ['faq', 'messages', 'invoices', 'tickets'].map((key) => [key, { loading: false, loaded: false, error: '' }]),
    );
  let readStates = emptyReadStates();
  function ensureScope() {
    if (memoryScope !== serviceScope()) reset();
  }
  function readState(resource = '') {
    ensureScope();
    if (resource) return { ...(readStates[resource] || { loading: false, loaded: true, error: '' }) };
    const values = Object.values(readStates);
    return {
      loading: values.some((value) => value.loading),
      loaded: values.every((value) => value.loaded),
      error: values.find((value) => value.error)?.error || '',
    };
  }
  const data = () => {
    ensureScope();
    return memory;
  };
  const redraw = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  function reset() {
    version++;
    dialogVersion++;
    memoryScope = serviceScope();
    inFlight = null;
    memory = emptyData();
    readStates = emptyReadStates();
    ui.invoiceForm = null;
    if (faqBase !== (window.LumoAPI?.getBase?.() || '')) {
      FAQS.splice(0);
      faqAt = 0;
      faqBase = '';
    }
  }
  function normalizeRequest(raw) {
    return {
      kind: raw?.kind === C.InvoiceKind.Company ? C.InvoiceKind.Company : C.InvoiceKind.Personal,
      title: cleanText(raw?.title, 80) || '个人',
      taxId: cleanText(raw?.taxId, 18).toUpperCase(),
    };
  }
  function guard(target) {
    return !!window.LumoAuth?.requireLogin(target || location.hash);
  }
  const at = (value) => (value ? Date.parse(value) || Number(value) || 0 : 0);
  const labels = {
    [C.Event.OrderAwaitingPayment]: '订单待付款',
    [C.Event.PaymentSucceeded]: '支付渠道已确认',
    [C.Event.OrderPaymentAccepted]: '支付已入账',
    [C.Event.OrderReadyToFulfill]: '商品待发货',
    [C.Event.OrderClosed]: '订单已关闭',
    [C.Event.ShipmentDispatched]: '商品已发货',
    [C.Event.DeliveryConfirmed]: '配送已签收',
    [C.Event.OrderCompleted]: '订单已完成',
    [C.Event.RefundSucceeded]: '退款已完成',
    [C.Event.InvoiceIssued]: '电子发票已开具',
    [C.Event.TicketReplied]: '工单有新回复',
    [C.Event.GroupFormed]: '拼团已成团',
    [C.Event.GroupFailed]: '拼团未成团',
  };
  function mapMessage(r) {
    const event = r.event_type || '',
      isTicket = event === C.Event.TicketReplied,
      isInvoice = event === C.Event.InvoiceIssued,
      isService = /Refund|Invoice|Ticket/.test(event);
    return {
      id: r.id,
      type: /Group/.test(event) ? C.MessageType.Activity : isService ? C.MessageType.Service : C.MessageType.Order,
      title: labels[event] || r.title || event,
      body: '业务编号 ' + (r.business_id || ''),
      target: isTicket
        ? '#/member/support/tickets'
        : isInvoice
          ? '#/member/invoices'
          : /Refund/.test(event)
            ? '#/member/aftersales'
            : '#/member/orders',
      at: at(r.created_at),
      read: r.status === C.NotificationStatus.Read,
    };
  }
  function mapInvoice(r) {
    const issued = !!r.number;
    return {
      ...r,
      backendStatus: r.status,
      orderId: r.order_id,
      taxId: r.tax_id || '',
      amount: Number(r.issued_cents || r.amount_cents || 0) / 100,
      refundedAmount: Number(r.refunded_cents || 0) / 100,
      status: issued ? C.InvoiceViewStatus.Issued : C.InvoiceViewStatus.Processing,
      createdAt: at(r.created_at),
      issuedAt: issued ? at(r.updated_at) : null,
      number: r.number || '',
      items: orderById(r.order_id)?.items || [],
      provider: r.provider || C.InvoiceProvider.Unsubmitted,
    };
  }
  function mapTicket(r) {
    const first = { role: C.ConversationRole.User, text: r.body || '', at: at(r.created_at) };
    return {
      id: r.id,
      orderId: r.order_id || '',
      type: '服务工单',
      title: r.title || '',
      detail: r.body || '',
      status:
        r.status === C.HelpdeskStatus.Closed
          ? C.TicketViewStatus.Closed
          : r.status === C.HelpdeskStatus.Replied
            ? C.TicketViewStatus.Answered
            : C.TicketViewStatus.Open,
      at: at(r.created_at),
      updatedAt: at(r.updated_at || r.created_at),
      messages: [
        first,
        ...(r.messages || []).map((m) => ({
          role: m.role === C.Role.Admin ? C.ConversationRole.Assistant : C.ConversationRole.User,
          text: m.body || '',
          at: at(m.created_at),
        })),
      ],
    };
  }
  async function refresh(force = false, resources = ['faq', 'messages', 'invoices', 'tickets']) {
    ensureScope();
    const requested = new Set(resources);
    if (!requested.size) return model();
    if (inFlight) {
      if (!force && [...requested].every((key) => inFlight.resources.has(key))) return inFlight.promise;
      await inFlight.promise.catch(() => {});
      return refresh(force, [...requested]);
    }
    const v = version,
      scope = serviceScope(),
      pending = { resources: requested };
    inFlight = pending;
    const base = window.LumoAPI.getBase?.() || '',
      needFaq = requested.has('faq') && (!faqAt || faqBase !== base || Date.now() - faqAt > 60000),
      authenticated = loggedIn();
    for (const key of ['faq', 'messages', 'invoices', 'tickets']) {
      if (!requested.has(key)) continue;
      const active = key === 'faq' ? needFaq : authenticated;
      readStates[key] = { ...readStates[key], loading: active, error: '', loaded: readStates[key].loaded || !active };
    }
    pending.promise = (async () => {
      const results = await Promise.allSettled([
        needFaq
          ? call(C.Service.Content, C.Action.List, { data: { kind: C.ContentKind.Faq, limit: 100 } }, { auth: false })
          : null,
        authenticated && requested.has('messages')
          ? call(C.Service.Notification, C.Action.List, { data: { limit: 100 } })
          : null,
        authenticated && requested.has('invoices') ? call(C.Service.Invoice, C.Action.List) : null,
        authenticated && requested.has('tickets')
          ? call(C.Service.Helpdesk, C.Action.List, { data: { limit: 100 } })
          : null,
      ]);
      if (v !== version || scope !== serviceScope()) return model();
      const keys = ['faq', 'messages', 'invoices', 'tickets'],
        values = results.map((result, index) => {
          const key = keys[index];
          if (!requested.has(key)) return null;
          readStates[key] = {
            loading: false,
            loaded: readStates[key].loaded || result.status === 'fulfilled',
            error: result.status === 'rejected' ? result.reason?.message || '服务信息暂未加载成功' : '',
          };
          return result.status === 'fulfilled' ? result.value : null;
        });
      const [faq, messages, invoices, tickets] = values;
      if (faq) {
        FAQS.splice(
          0,
          FAQS.length,
          ...(faq.items || []).map((r) => ({
            id: r.id,
            category: '购物帮助',
            title: r.title || '',
            answer: r.body || '',
            keywords: (r.title || '') + ' ' + (r.body || ''),
          })),
        );
        faqBase = base;
        faqAt = Date.now();
      }
      if (!authenticated || !loggedIn()) {
        memory = emptyData();
      } else {
        if (messages) {
          memory.messages = (messages.items || []).map(mapMessage);
          const unread = Number(messages.unread_total);
          memory.unreadTotal =
            messages.unread_total != null && Number.isFinite(unread) && unread >= 0
              ? Math.floor(unread)
              : memory.messages.filter((row) => !row.read).length;
        }
        if (invoices) memory.invoices = (invoices.items || []).map(mapInvoice);
        if (tickets) memory.tickets = (tickets.items || []).map(mapTicket);
      }
      const failed = results.find((result) => result.status === 'rejected');
      if (failed) throw failed.reason;
      return model();
    })().finally(() => {
      if (inFlight === pending) inFlight = null;
    });
    return pending.promise;
  }
  const memberModel = () =>
    window.LumoMember?.model() || { notifications: { order: true, promotion: false }, aftersales: [] };
  const orderStatus = (order) => window.LumoMember?.orderState(order) || '';
  const orderById = (id) => (window.LumoCommerce?.allOrders?.() || state.orders).find((o) => o.id === id);
  const invoiceEligible = (order) =>
    !!order?.paidAt && ![C.OrderViewStatus.Closed, C.OrderViewStatus.Refunded].includes(orderStatus(order));
  function invoiceState(invoice) {
    return [C.InvoiceStatus.Cancelled, C.InvoiceStatus.Void].includes(invoice.backendStatus) && !invoice.number
      ? C.InvoiceViewStatus.Void
      : invoice.status;
  }
  function requestError(r) {
    if (!r || r.enabled === false) return null;
    if (!cleanText(r.title, 80)) return '请填写发票抬头';
    if (r.kind === C.InvoiceKind.Company && !/^[A-Z0-9]{18}$/.test(r.taxId || ''))
      return '企业发票请填写 18 位纳税人识别号';
    return null;
  }
  function notify() {
    return null;
  } // inbox notifications are projected by backend events
  function persist() {} // transient forms and FAQ lookup do not manufacture business records
  async function createInvoice(order, request) {
    if (request?.enabled === false) return;
    const result = await call(C.Service.Invoice, C.Action.Create, {
      data: { order_id: order.id, kind: request.kind, title: request.title, tax_id: request.taxId || '' },
    });
    await refresh(true);
    return result;
  }
  async function onPaid(order) {
    if (order?.invoiceRequest && order.invoiceRequest.enabled !== false)
      return createInvoice(order, order.invoiceRequest);
    return refresh(true);
  }
  function sync() {
    ensureScope();
  }
  function refreshForView() {
    const route = (location.hash || '#/').slice(2).split('/');
    const resources =
      route[0] === 'member'
        ? { messages: ['messages'], invoices: ['invoices'], support: ['faq', 'tickets'] }[route[1]] || []
        : [];
    return refresh(false, resources);
  }
  function unreadCount() {
    ensureScope();
    if (!loggedIn()) return 0;
    if (readStates.messages.loaded)
      return memory.unreadTotal ?? memory.messages.filter((message) => !message.read).length;
    return window.LumoAccountSummary?.model().unread_count ?? 0;
  }
  function afterRender() {
    document.querySelectorAll('[data-service-unread]').forEach((node) => {
      const count = unreadCount();
      node.textContent = count > 99 ? '99+' : count;
      node.hidden = !count;
    });
  }
  function heading(title, subtitle, action = '') {
    return `<div class="mem-pagehead svc-heading"><div><span class="mem-kicker">LUMO MEMBER SERVICES</span><h1>${esc(title)}</h1><p>${esc(subtitle)}</p></div>${action}</div>`;
  }
  const button = (label, action, id = '', primary = false, extra = '') =>
    `<button class="${primary ? 'mem-primary' : 'mem-outline'}" data-service-action="${esc(action)}" ${id ? `data-id="${esc(id)}"` : ''} ${extra}>${esc(label)}</button>`;
  function empty(title, description, symbol = 'info', action = '') {
    return `<div class="svc-empty"><span>${icon(symbol)}</span><h3>${esc(title)}</h3><p>${esc(description)}</p>${action}</div>`;
  }
  function messagesContent(filter) {
    const validFilters = [
      C.MessageFilter.All,
      C.MessageFilter.Unread,
      C.MessageType.Order,
      C.MessageType.Activity,
      C.MessageType.Service,
    ];
    if (!validFilters.includes(filter)) filter = ui.messageFilter;
    if (!validFilters.includes(filter)) filter = C.MessageFilter.All;
    ui.messageFilter = filter;
    const messages = data().messages.filter((row) =>
      filter === C.MessageFilter.All || filter === C.MessageFilter.Unread
        ? filter !== C.MessageFilter.Unread || !row.read
        : row.type === filter,
    );
    return `${heading('消息中心', '好物动态与服务进展，及时为你送达。', button('全部标为已读', 'read-all', '', false, data().messages.some((row) => !row.read) ? '' : 'disabled'))}<section class="mem-panel"><nav class="mem-tabs svc-tabs" aria-label="消息分类">${[
      [C.MessageFilter.All, '全部消息'],
      [C.MessageFilter.Unread, '未读'],
      [C.MessageType.Order, '订单通知'],
      [C.MessageType.Activity, '优惠活动'],
      [C.MessageType.Service, '服务通知'],
    ]
      .map(
        ([id, label]) =>
          `<a href="#/member/messages/${id}" class="${filter === id ? 'active' : ''}" ${filter === id ? 'aria-current="page"' : ''}>${label}${id === C.MessageFilter.Unread ? `<span>${data().messages.filter((row) => !row.read).length}</span>` : ''}</a>`,
      )
      .join(
        '',
      )}</nav>${messages.length ? `<div class="svc-message-list">${messages.map((row) => `<article class="svc-message ${row.read ? '' : 'is-unread'}"><span class="svc-message-icon ${row.type}">${icon(row.type === C.MessageType.Order ? 'bag' : row.type === C.MessageType.Activity ? 'ticket' : 'support')}</span><div class="svc-message-copy"><div><h2>${esc(row.title)}</h2><time>${date(row.at)}</time></div><p>${esc(row.body)}</p><div class="svc-message-actions">${button('查看详情', 'open-message', row.id)}${row.read ? '<span>已读</span>' : button('标为已读', 'read-message', row.id)}</div></div>${row.read ? '' : '<i class="svc-unread-dot" aria-label="未读"></i>'}</article>`).join('')}</div>` : empty(filter === C.MessageFilter.Unread ? '消息已全部读完' : '暂时没有相关消息', '订单和服务进展会记录在这里。', 'info')}</section>`;
  }
  function invoiceName(invoice) {
    return invoice.kind === C.InvoiceKind.Company ? '企业抬头' : '个人抬头';
  }
  function invoicesContent(filter) {
    if (
      ![
        'all',
        C.InvoiceViewStatus.Processing,
        C.InvoiceViewStatus.Issued,
        C.InvoiceViewStatus.Available,
        C.InvoiceViewStatus.Void,
      ].includes(filter)
    )
      filter = 'all';
    const eligible = state.orders
      .filter(invoiceEligible)
      .filter((order) => !data().invoices.some((invoice) => invoice.orderId === order.id));
    const rows = data().invoices.filter((invoice) => filter === 'all' || invoiceState(invoice) === filter);
    return `${heading('发票中心', '管理电子发票，留存每一份购物记录。', button('申请电子发票', 'invoice-picker', '', true))}<section class="mem-panel"><nav class="mem-tabs svc-tabs" aria-label="发票分类">${[
      ['all', '全部发票'],
      [C.InvoiceViewStatus.Processing, '开票中'],
      [C.InvoiceViewStatus.Issued, '已开票'],
      [C.InvoiceViewStatus.Void, '已取消'],
      [C.InvoiceViewStatus.Available, '待申请订单'],
    ]
      .map(
        ([id, label]) =>
          `<a href="#/member/invoices/${id}" class="${filter === id ? 'active' : ''}" ${filter === id ? 'aria-current="page"' : ''}>${label}<span>${id === C.InvoiceViewStatus.Available ? eligible.length : id === 'all' ? data().invoices.length : data().invoices.filter((row) => invoiceState(row) === id).length}</span></a>`,
      )
      .join(
        '',
      )}</nav>${filter === C.InvoiceViewStatus.Available ? (eligible.length ? `<div class="svc-invoice-list">${eligible.map((order) => `<article class="svc-eligible-order"><span>${icon('bag')}</span><div><strong>${esc(order.items[0]?.name || products.find((p) => p.id === order.items[0]?.id)?.name || '商城订单')}${order.items.length > 1 ? ` 等 ${order.items.length} 款` : ''}</strong><p>${esc(order.id)} · ${fullDate(order.paidAt)}</p></div>${price(order.totals.total)}${button('申请发票', 'apply-invoice', order.id)}</article>`).join('')}</div>` : empty('暂无待申请订单', '完成购买并成团后，即可为订单申请发票。', 'edit')) : rows.length ? `<div class="svc-invoice-list">${rows.map((row) => `<article class="svc-invoice-card"><div class="svc-invoice-head"><span class="svc-invoice-icon">${icon('edit')}</span><div><h2>电子普通发票</h2><small>${fullDate(row.createdAt)} · ${esc(row.orderId)}</small></div><span class="mem-badge ${row.status === C.InvoiceViewStatus.Issued ? 'svc-success-badge' : ''}">${invoiceState(row) === C.InvoiceViewStatus.Void ? '已取消' : row.status === C.InvoiceViewStatus.Issued ? '已开票' : '开票中'}</span></div><div class="svc-invoice-info"><div><label>${invoiceName(row)}</label><strong>${esc(row.title)}</strong>${row.kind === C.InvoiceKind.Company ? `<small>税号 ${esc(row.taxId)}</small>` : ''}</div><div><label>开票金额</label>${price(row.amount)}</div></div><div class="svc-invoice-foot"><span>${invoiceState(row) === C.InvoiceViewStatus.Void ? '申请已取消' : row.status === C.InvoiceViewStatus.Issued ? '发票号码 ' + esc(row.number) : '申请已提交，等待服务端开票'}</span><div>${button('查看详情', 'view-invoice', row.id)}${button('导出票据记录', 'download-invoice', row.id)}</div></div></article>`).join('')}</div>` : empty('暂时没有发票记录', '可选择已支付订单申请，或在下单时填写发票信息。', 'edit', button('选择订单', 'invoice-picker'))}</section>`;
  }
  function invoiceFields(form) {
    return `<div class="svc-kind-options" role="group" aria-label="发票抬头类型">${[
      [C.InvoiceKind.Personal, '个人'],
      [C.InvoiceKind.Company, '企业'],
    ]
      .map(
        ([id, label]) =>
          `<button type="button" class="${form.kind === id ? 'active' : ''}" data-service-action="invoice-kind" data-value="${id}" aria-pressed="${form.kind === id}">${label}</button>`,
      )
      .join(
        '',
      )}</div><input type="hidden" name="kind" value="${form.kind}"><label class="field">发票抬头<input name="title" maxlength="80" value="${esc(form.title)}" required placeholder="${form.kind === C.InvoiceKind.Company ? '请输入企业名称' : '个人或姓名'}"></label>${form.kind === C.InvoiceKind.Company ? `<label class="field">纳税人识别号<input name="taxId" maxlength="18" pattern="[A-Za-z0-9]{18}" value="${esc(form.taxId)}" required placeholder="18 位纳税人识别号"></label>` : ''}`;
  }
  function renderInvoiceForm() {
    const form = ui.invoiceForm;
    if (!form) return;
    const checkout = form.mode === C.InvoiceEditorMode.Checkout,
      order = orderById(form.orderId);
    modal(
      checkout ? '发票信息' : '申请电子发票',
      `<form id="${checkout ? 'service-checkout-invoice-form' : 'service-invoice-form'}" data-order="${esc(form.orderId || '')}" novalidate>${checkout ? `<label class="check svc-invoice-switch"><input type="checkbox" name="enabled" ${form.enabled ? 'checked' : ''}>开具电子普通发票</label>` : `<p class="modal-desc">订单 ${esc(form.orderId)} · 开票金额 ¥${money(order?.totals.total || 0)}</p>`}${invoiceFields(form)}<div class="svc-modal-actions"><button type="button" class="outline" data-action="close-modal">取消</button><button class="primary" type="submit">${checkout ? '保存发票信息' : '提交申请'}</button></div></form>`,
    );
  }
  function openInvoice(orderId) {
    if (!guard('#/member/invoices')) return;
    const existing = data().invoices.find((invoice) => invoice.orderId === orderId);
    if (existing) {
      viewInvoice(existing.id);
      return;
    }
    const order = orderById(orderId);
    if (!invoiceEligible(order)) {
      toast('当前订单暂不支持申请发票');
      return;
    }
    ui.invoiceForm = {
      mode: C.InvoiceEditorMode.Order,
      orderId,
      kind: C.InvoiceKind.Personal,
      title: '个人',
      taxId: '',
      enabled: true,
    };
    renderInvoiceForm();
  }
  function invoicePicker() {
    const orders = window.LumoCommerce?.readState?.('orders'),
      invoices = readState('invoices');
    if (!invoices.loaded || (orders && !orders.loaded)) {
      return readDialog(
        '选择开票订单',
        'rows',
        async () => {
          const reads = [];
          if (!invoices.loaded)
            reads.push(
              refresh().catch((error) => {
                if (!readState('invoices').loaded) throw error;
              }),
            );
          if (orders && !orders.loaded) reads.push(window.LumoCommerce?.refreshOrders?.());
          await Promise.all(reads);
          const orderState = window.LumoCommerce?.readState?.('orders');
          if (!readState('invoices').loaded || (orderState && !orderState.loaded))
            throw Error(readState('invoices').error || orderState?.error || '订单信息尚未加载，请重试');
        },
        invoicePicker,
        'invoice-picker',
        '',
      );
    }
    const list = state.orders
      .filter(invoiceEligible)
      .filter((order) => !data().invoices.some((invoice) => invoice.orderId === order.id));
    modal(
      '选择开票订单',
      `<div class="svc-invoice-picker">${list.length ? list.map((order) => `<button class="svc-pick-order" data-service-action="apply-invoice" data-id="${esc(order.id)}"><span>${icon('bag')}</span><div><strong>${esc(order.items[0]?.name || products.find((p) => p.id === order.items[0]?.id)?.name || '商城订单')}</strong><p>${esc(order.id)} · ¥${money(order.totals.total)}</p></div>${icon('chevron')}</button>`).join('') : empty('暂无可开票订单', '已开票、未支付或待成团的订单不会重复出现在这里。', 'edit')}</div>`,
    );
  }
  async function readDialog(title, type, load, show, retryAction, id) {
    ensureScope();
    const scope = serviceScope(),
      ticket = ++dialogVersion,
      marker = uid('service-read');
    modal(
      title,
      `<div data-service-loading="${marker}">${window.LumoSkeleton?.render(type, { count: 2, label: '正在读取' + title }) || ''}</div>`,
    );
    const current = () => {
      const root = document.querySelector('#modal-root');
      return scope === serviceScope() && ticket === dialogVersion && (!root || String(root.innerHTML).includes(marker));
    };
    try {
      const result = await load();
      if (current()) show(result);
    } catch (error) {
      if (current())
        modal(
          title,
          `<div role="alert"><p>${esc(error?.message || '读取失败，请重试')}</p>${button('重新加载', retryAction, id)}</div>`,
        );
    }
  }
  async function viewInvoice(id) {
    return readDialog(
      '电子发票详情',
      'form',
      () =>
        Promise.all([
          call(C.Service.Invoice, C.Action.Get, { id }),
          call(C.Service.Invoice, C.Action.Reversals, { id }),
        ]),
      ([record, reversal]) => {
        const row = mapInvoice(record);
        modal(
          '电子发票详情',
          `<div class="svc-invoice-preview"><span class="svc-invoice-preview-label">${esc(row.provider)} · 票据记录</span><h3>电子普通发票</h3><dl><dt>状态</dt><dd>${esc(row.backendStatus)}</dd><dt>关联订单</dt><dd>${esc(row.orderId)}</dd><dt>抬头</dt><dd>${esc(row.title)}</dd><dt>原票金额</dt><dd>¥${money(row.amount)}</dd><dt>已退款金额</dt><dd>¥${money(row.refundedAmount)}</dd><dt>发票号码</dt><dd>${esc(row.number || '尚未开具')}</dd><dt>申请时间</dt><dd>${date(row.createdAt)}</dd></dl>${(reversal.items || []).length ? `<h4>红冲 / 调整记录</h4>${reversal.items.map((r) => `<p>原票 ${esc(r.original_number)} · ¥${money(Number(r.amount_cents || 0) / 100)} · ${esc(r.status)}</p>`).join('')}` : ''}<p>退款后保留原票并记录调整。DEMO 渠道记录不具备税务或报销效力。</p></div><div class="svc-modal-actions">${button('导出票据记录', 'download-invoice', row.id)}</div>`,
        );
      },
      'view-invoice',
      id,
    );
  }
  async function downloadInvoice(id) {
    const [row, reversal] = await Promise.all([
      call(C.Service.Invoice, C.Action.Get, { id }),
      call(C.Service.Invoice, C.Action.Reversals, { id }),
    ]);
    const url = URL.createObjectURL(
      new Blob([JSON.stringify({ invoice: row, reversals: reversal.items || [] }, null, 2)], {
        type: 'application/json',
      }),
    );
    const link = document.createElement('a');
    link.href = url;
    link.download = 'LUMO-invoice-record-' + row.order_id + '.json';
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    toast('票据记录已导出');
  }
  function checkoutFields() {
    const draft = data().checkout;
    return `<section class="checkout-section svc-checkout-invoice"><h2>发票信息</h2><button class="benefit-row" data-service-action="checkout-invoice">${icon('edit')}电子普通发票<span>${draft.enabled ? esc(draft.title) : '不开发票'}</span>${icon('chevron', 'arrow-icon')}</button></section>`;
  }
  function checkoutMeta() {
    return { invoiceRequest: data().checkout.enabled ? normalizeRequest(data().checkout) : null };
  }
  function validateCheckout(orderOrItems) {
    if (orderOrItems && !Array.isArray(orderOrItems) && Array.isArray(orderOrItems.items))
      return orderOrItems.invoiceRequest ? requestError(orderOrItems.invoiceRequest) : null;
    return data().checkout.enabled ? requestError(data().checkout) : null;
  }
  function orderActions(order) {
    if (!order?.paidAt) return '';
    const existing = data().invoices.find((invoice) => invoice.orderId === order.id);
    if (existing) return button('查看发票', 'view-invoice', existing.id);
    return invoiceEligible(order) ? button('申请发票', 'apply-invoice', order.id) : '';
  }
  function faqAnswer(value) {
    const query = cleanText(value, 300).toLowerCase();
    const match =
      FAQS.find((r) => r.title.toLowerCase() === query) ||
      FAQS.find((r) =>
        [...new Set(query.match(/[\u4e00-\u9fff]{2}|[a-z]{3,}/g) || [])].some((word) =>
          (r.title + ' ' + r.answer).toLowerCase().includes(word),
        ),
      );
    return match ? match.answer : '帮助库暂未找到对应答案。请创建服务工单，商家回复后会同步到工单与消息中心。';
  }
  function appendChat(text) {
    const body = cleanText(text, 300);
    if (!body) return;
    data().chat.push(
      { role: C.ConversationRole.User, text: body, at: Date.now() },
      { role: C.ConversationRole.Assistant, text: faqAnswer(body), at: Date.now() },
    );
    data().chat = data().chat.slice(-60);
    persist();
    redraw();
    document.querySelector('.svc-chat-log')?.scrollTo({ top: 99999 });
  }
  function supportContent(filter) {
    const ticketsTab = filter === 'tickets';
    return `${heading('帮助与客服', '常见问题随时查，服务记录随时看。', button('创建服务工单', 'new-ticket', '', true))}<nav class="svc-support-nav" aria-label="客服服务"><a href="#/member/support/help" class="${ticketsTab ? '' : 'active'}">${icon('support')}在线帮助</a><a href="#/member/support/tickets" class="${ticketsTab ? 'active' : ''}">${icon('layers')}服务工单<span>${data().tickets.length}</span></a></nav>${ticketsTab ? ticketsContent() : helpContent()}`;
  }
  function helpContent() {
    const query = ui.faqQuery.toLowerCase();
    const rows = FAQS.filter((row) => !query || (row.title + row.answer + row.keywords).toLowerCase().includes(query));
    return `<div class="svc-help-layout"><section class="mem-panel svc-faq-panel"><div class="mem-section-head"><h2>常见问题</h2><span>${FAQS.length} 项购物帮助</span></div><form id="service-faq-form" class="svc-faq-search" role="search">${icon('search')}<input name="query" maxlength="100" placeholder="搜索订单、优惠券、发票等问题" aria-label="搜索常见问题" value="${esc(ui.faqQuery)}"><button type="submit">搜索</button>${query ? '<button type="button" data-service-action="reset-faq">重置</button>' : ''}</form><div class="svc-faq-list">${rows.length ? rows.map((row) => `<details><summary><span class="svc-faq-category">${row.category}</span>${esc(row.title)}${icon('chevron')}</summary><p>${esc(row.answer)}</p><button data-service-action="faq-chat" data-id="${row.id}">继续咨询${icon('arrow')}</button></details>`).join('') : empty('没有找到相关问题', '换个关键词试试，或向帮助助手提问。', 'search')}</div></section><section class="mem-panel svc-chat-panel"><div class="svc-chat-heading"><span>${icon('support')}</span><div><h2>购物帮助查询</h2><p>订单 · 配送 · 优惠 · 售后</p></div><button data-service-action="clear-chat" aria-label="清空咨询记录">${icon('trash')}</button></div><div class="svc-chat-log" role="log" aria-label="购物咨询记录" aria-live="polite">${
      data().chat.length
        ? data()
            .chat.map(
              (row) =>
                `<div class="svc-chat-message ${row.role}"><span>${icon(row.role === C.ConversationRole.User ? C.ConversationRole.User : 'support')}</span><div><p>${esc(row.text)}</p><time>${date(row.at)}</time></div></div>`,
            )
            .join('')
        : `<div class="svc-chat-message assistant"><span>${icon('support')}</span><div><p>可以搜索服务端购物帮助内容。需要商家处理的问题，请创建服务工单。</p></div></div><div class="svc-chat-suggestions">${FAQS.slice(
            1,
            4,
          )
            .map((row) => `<button data-service-action="faq-chat" data-id="${row.id}">${esc(row.title)}</button>`)
            .join('')}</div>`
    }</div><form id="service-chat-form" class="svc-chat-form"><input name="message" maxlength="300" required placeholder="例如：如何使用优惠券？" aria-label="咨询内容"><button type="submit" aria-label="发送咨询">${icon('arrow')}</button></form></section></div>`;
  }
  function ticketsContent() {
    const rows = data().tickets.filter((row) => ui.ticketFilter === 'all' || row.status === ui.ticketFilter);
    return `<section class="mem-panel"><div class="svc-local-tabs" role="group" aria-label="工单状态">${[
      ['all', '全部'],
      [C.TicketViewStatus.Open, '待处理'],
      [C.TicketViewStatus.Answered, '已回复'],
      [C.TicketViewStatus.Closed, '已关闭'],
    ]
      .map(
        ([id, label]) =>
          `<button data-service-action="ticket-filter" data-value="${id}" class="${ui.ticketFilter === id ? 'active' : ''}">${label}</button>`,
      )
      .join(
        '',
      )}</div>${rows.length ? `<div class="svc-ticket-list">${rows.map((row) => `<article class="svc-ticket-card"><div class="svc-ticket-heading"><span class="svc-ticket-type">${icon('support')}</span><div><h2>${esc(row.title)}</h2><small>${esc(row.id)} · ${date(row.at)}</small></div><span class="mem-badge ${row.status === C.TicketViewStatus.Closed ? 'svc-muted-badge' : row.status === C.TicketViewStatus.Answered ? 'svc-success-badge' : ''}">${{ open: '待处理', answered: '已回复', closed: '已关闭' }[row.status]}</span></div><p class="svc-ticket-description">${esc(row.detail)}</p><div class="svc-ticket-meta"><span>${esc(row.type)}</span>${row.orderId ? `<span>关联订单 ${esc(row.orderId)}</span>` : '<span>购物咨询</span>'}</div><div class="svc-ticket-actions">${button('查看会话', 'view-ticket', row.id)}${row.status === C.TicketViewStatus.Closed ? '' : `${button('补充说明', 'reply-ticket', row.id)}${button('关闭工单', 'close-ticket', row.id)}`}</div></article>`).join('')}</div>` : empty('暂时没有服务工单', '可以关联具体订单，记录需要帮助的问题。', 'support', button('创建工单', 'new-ticket', '', true))}</section>`;
  }
  function newTicket(feedback = false) {
    const orders = window.LumoMember?.listOrders('all') || state.orders;
    modal(
      feedback ? '意见反馈' : '创建服务工单',
      `<form id="service-ticket-form"><label class="field">问题类型<select name="type">${['订单问题', '配送问题', '商品问题', '发票问题', '售后问题', '其他问题'].map((label) => `<option${feedback && label === '其他问题' ? ' selected' : ''}>${label}</option>`).join('')}</select></label><label class="field">关联订单（选填）<select name="orderId"><option value="">不关联订单</option>${orders.map((order) => `<option value="${esc(order.id)}">${esc(order.id)} · ¥${money(order.totals.total)}</option>`).join('')}</select></label><label class="field">问题标题<input name="title" maxlength="80" required placeholder="一句话描述你的问题"></label><label class="field">详细说明<textarea name="detail" rows="4" maxlength="500" required placeholder="请描述问题和希望获得的帮助"></textarea></label><label class="field">问题附件（选填，最大 10 MiB）<input type="file" name="attachment" accept="image/png,image/jpeg,image/webp,application/pdf,text/plain"></label><div class="svc-modal-actions"><button class="outline" type="button" data-action="close-modal">取消</button><button class="primary" type="submit">提交工单</button></div></form>`,
    );
  }
  async function viewTicket(id, reply = false) {
    return readDialog(
      reply ? '补充工单说明' : '工单会话',
      'rows',
      () => call(C.Service.Helpdesk, C.Action.Get, { id }),
      (raw) => {
        const row = mapTicket(raw);
        const files = [...String(raw.body || '').matchAll(/\[附件:([^|\]]+)\|([^\]]+)\]/g)];
        modal(
          reply ? '补充工单说明' : '工单会话',
          `<p class="modal-desc">${esc(row.title)} · ${esc(row.id)}</p><div class="svc-ticket-chat">${row.messages.map((message) => `<div class="svc-chat-message ${message.role}"><span>${icon(message.role === C.ConversationRole.User ? 'user' : 'support')}</span><div><p>${esc(message.text)}</p><time>${date(message.at)}</time></div></div>`).join('')}</div>${files.map((f) => button('下载附件：' + f[1], 'download-attachment', f[2])).join('')}${reply && row.status !== C.TicketViewStatus.Closed ? `<form id="service-ticket-reply-form" data-id="${esc(row.id)}"><label class="field">补充说明<textarea name="message" rows="3" maxlength="500" required></textarea></label><button class="primary" type="submit">发送补充说明</button></form>` : ''}`,
        );
      },
      reply ? 'reply-ticket' : 'view-ticket',
      id,
    );
  }
  function content(tab, filter = 'all') {
    sync();
    const key =
        tab === 'messages' ? 'messages' : tab === 'invoices' ? 'invoices' : filter === 'tickets' ? 'tickets' : 'faq',
      status = readState(key),
      title = { messages: '消息中心', invoices: '发票中心', tickets: '服务工单', faq: '帮助与客服' }[key];
    const warning = status.error
      ? `<section class="mem-panel" role="alert"><p>${esc(status.error)}</p><button class="mem-outline" data-service-action="retry-read">重新加载</button></section>`
      : '';
    if (!status.loaded)
      return (
        heading(title, '正在同步服务端信息，稍候即可查看。') +
        warning +
        (!status.error
          ? window.LumoSkeleton?.render(key === 'invoices' ? 'table' : key === 'faq' ? 'rows' : 'cards', {
              count: 3,
              label: title + '加载中',
            }) || ''
          : '')
      );
    return (
      warning +
      (tab === 'messages'
        ? messagesContent(filter)
        : tab === 'invoices'
          ? invoicesContent(filter)
          : supportContent(filter))
    );
  }
  function supportLauncher() {
    const route = (location.hash || '#/').slice(2).split('/')[0];
    return ['login', 'editor', 'payment'].includes(route)
      ? ''
      : `<a class="svc-support-launcher" href="#/member/support" aria-label="帮助与客服">${icon('support')}<span>客服</span></a>`;
  }
  function model() {
    return clone(data());
  }

  async function uploadAttachment(file) {
    if (!file || !file.size) return null;
    if (file.size > 10 * 1024 * 1024) throw Error('附件最大为 10 MiB');
    const meta = await call(C.Service.Media, C.Action.Create, {
      data: { file_name: file.name, content_type: file.type || 'application/octet-stream', size: file.size },
    });
    if (meta.upload_supported === false || !meta.upload_url)
      throw Error('附件存储尚未配置，请先移除附件或配置对象存储');
    const multipart = new FormData();
    for (const [key, value] of Object.entries(meta.upload_fields || {})) multipart.append(key, value);
    multipart.append('file', file);
    const result = await fetch(meta.upload_url, { method: meta.upload_method || 'POST', body: multipart });
    if (!result.ok) throw Error('附件上传失败（' + result.status + '）');
    const id = meta.file_id || meta.id;
    await call(C.Service.Media, C.Action.Complete, { id });
    return { id, name: file.name.replace(/[\[\]|]/g, '_') };
  }
  function showError(e) {
    toast(e?.message || '请求未完成，请重试');
  }
  document.addEventListener('click', async (event) => {
    const b = event.target.closest('[data-service-action]');
    if (!b || b.disabled) return;
    event.preventDefault();
    if (!guard(location.hash.startsWith('#/member') ? location.hash : '#/member/support')) return;
    const action = b.dataset.serviceAction,
      id = b.dataset.id,
      value = b.dataset.value;
    b.disabled = true;
    try {
      if (action === 'retry-read') {
        const request = refresh(true);
        redraw();
        await request;
        redraw();
      }
      if (action === 'read-all') {
        await call(C.Service.Notification, C.Action.ReadAll);
        memory.messages.forEach((message) => (message.read = true));
        memory.unreadTotal = 0;
        await refresh(true, ['messages']);
        redraw();
      }
      if (action === 'read-message' || action === 'open-message') {
        const row = memory.messages.find((m) => m.id === id);
        await call(C.Service.Notification, C.Action.Read, { id });
        if (row && !row.read) {
          row.read = true;
          if (memory.unreadTotal != null) memory.unreadTotal = Math.max(0, memory.unreadTotal - 1);
        }
        await refresh(true, ['messages']);
        if (action === 'open-message' && row) go(row.target.slice(2));
        else redraw();
      }
      if (action === 'invoice-picker') await invoicePicker();
      if (action === 'apply-invoice') openInvoice(id);
      if (action === 'checkout-invoice') {
        ui.invoiceForm = { mode: C.InvoiceEditorMode.Checkout, orderId: '', ...clone(memory.checkout) };
        renderInvoiceForm();
      }
      if (action === 'invoice-kind' && ui.invoiceForm) {
        const form = document.querySelector('#service-invoice-form,#service-checkout-invoice-form');
        if (form) {
          const v = Object.fromEntries(new FormData(form));
          ui.invoiceForm = {
            ...ui.invoiceForm,
            title: cleanText(v.title, 80),
            taxId: cleanText(v.taxId, 18),
            enabled: ui.invoiceForm.mode === C.InvoiceEditorMode.Checkout ? v.enabled === 'on' : true,
          };
        }
        ui.invoiceForm.kind = value === C.InvoiceKind.Company ? C.InvoiceKind.Company : C.InvoiceKind.Personal;
        renderInvoiceForm();
      }
      if (action === 'view-invoice') await viewInvoice(id);
      if (action === 'download-invoice') await downloadInvoice(id);
      if (action === 'reset-faq') {
        ui.faqQuery = '';
        redraw();
      }
      if (action === 'faq-chat') {
        const raw = await call(C.Service.Content, C.Action.Get, { id }, { auth: false });
        const row = FAQS.find((r) => r.id === id);
        if (row) {
          row.title = raw.title;
          row.answer = raw.body;
          appendChat(row.title);
        }
      }
      if (action === 'clear-chat') {
        memory.chat = [];
        redraw();
      }
      if (action === 'ticket-filter') {
        ui.ticketFilter = [C.TicketViewStatus.Open, C.TicketViewStatus.Answered, C.TicketViewStatus.Closed].includes(
          value,
        )
          ? value
          : 'all';
        redraw();
      }
      if (action === 'new-ticket') newTicket();
      if (action === 'view-ticket' || action === 'reply-ticket') await viewTicket(id, action === 'reply-ticket');
      if (action === 'close-ticket') {
        await call(C.Service.Helpdesk, C.Action.Close, { id });
        await refresh(true);
        redraw();
        toast('工单已关闭');
      }
      if (action === 'download-attachment') {
        const r = await call(C.Service.Media, C.Action.Download, { id });
        if (!r.download_url) throw Error('附件暂无可用下载地址');
        const a = document.createElement('a');
        a.href = r.download_url;
        a.rel = 'noopener';
        a.target = '_blank';
        a.click();
      }
    } catch (e) {
      showError(e);
    } finally {
      b.disabled = false;
    }
  });
  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (
      ![
        'service-invoice-form',
        'service-checkout-invoice-form',
        'service-faq-form',
        'service-chat-form',
        'service-ticket-form',
        'service-ticket-reply-form',
      ].includes(form.id)
    )
      return;
    event.preventDefault();
    if (!guard(location.hash)) return;
    const values = Object.fromEntries(new FormData(form)),
      submit = form.querySelector('[type=submit],button');
    if (submit) submit.disabled = true;
    try {
      if (form.id === 'service-faq-form') {
        const result = await call(C.Service.Helpdesk, C.Action.Faq, {}, { auth: false });
        FAQS.splice(
          0,
          FAQS.length,
          ...(result.items || []).map((r) => ({
            id: r.id,
            category: '购物帮助',
            title: r.title || '',
            answer: r.body || '',
            keywords: (r.title || '') + ' ' + (r.body || ''),
          })),
        );
        faqBase = window.LumoAPI.getBase?.() || '';
        faqAt = Date.now();
        readStates.faq = { loading: false, loaded: true, error: '' };
        ui.faqQuery = cleanText(values.query, 100);
        redraw();
      }
      if (form.id === 'service-chat-form') appendChat(values.message);
      if (form.id === 'service-invoice-form' || form.id === 'service-checkout-invoice-form') {
        const checkout = form.id === 'service-checkout-invoice-form',
          request = {
            enabled: checkout ? values.enabled === 'on' : true,
            kind: values.kind === C.InvoiceKind.Company ? C.InvoiceKind.Company : C.InvoiceKind.Personal,
            title: cleanText(values.title, 80),
            taxId: cleanText(values.taxId, 18).toUpperCase(),
          };
        const error = requestError(request);
        if (error) throw Error(error);
        if (checkout) memory.checkout = request;
        else await createInvoice(orderById(form.dataset.order), request);
        ui.invoiceForm = null;
        closeModal();
        redraw();
        toast(checkout ? '发票信息已保存' : '发票申请已提交');
      }
      if (form.id === 'service-ticket-form') {
        const title = cleanText(values.title, 80),
          body = cleanText(values.detail, 500);
        if (!title || !body) throw Error('请填写问题标题和说明');
        form.dataset.requestKey ||= uid('ticket');
        let attachment;
        if (values.attachment?.size) {
          attachment = await uploadAttachment(values.attachment);
        }
        await call(C.Service.Helpdesk, C.Action.Create, {
          idempotency_key: form.dataset.requestKey,
          data: {
            title,
            body: body + (attachment ? '\n[附件:' + attachment.name + '|' + attachment.id + ']' : ''),
            order_id: values.orderId || '',
          },
        });
        await refresh(true);
        closeModal();
        go('member/support/tickets');
        toast('工单已提交');
      }
      if (form.id === 'service-ticket-reply-form') {
        const body = cleanText(values.message, 500);
        if (!body) throw Error('请填写补充说明');
        form.dataset.requestKey ||= uid('reply');
        await call(C.Service.Helpdesk, C.Action.Reply, {
          id: form.dataset.id,
          idempotency_key: form.dataset.requestKey,
          data: { body },
        });
        await refresh(true);
        closeModal();
        redraw();
        toast('补充说明已发送');
      }
    } catch (e) {
      showError(e);
    } finally {
      if (submit) submit.disabled = false;
    }
  });
  window.LumoServices = {
    readState,
    openFeedback: () => {
      if (guard('#/member/support')) newTicket(true);
    },
    content,
    refresh,
    refreshForView,
    reset,
    unreadCount,
    afterRender,
    onPaid,
    notify,
    checkoutFields,
    checkoutMeta,
    validateCheckout,
    orderActions,
    openInvoice,
    supportLauncher,
    invoiceEligible,
    invoiceState,
    model,
    uploadAttachment,
  };
  window.addEventListener('lumo:logout', reset);
})();
