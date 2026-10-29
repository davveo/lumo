/* Administrative actions always use a separately authenticated server session. */
(function () {
  'use strict';
  const C = window.LumoConstants;
  const API = window.LumoAPI,
    $ = (s) => document.querySelector(s);
  API.enableReadBatching?.();
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
    );
  const tokenKey = 'lumo-admin-session-v1';
  let token = sessionStorage.getItem(tokenKey) || '',
    tab = 'overview',
    rows = [],
    lastTrace = '',
    busy = false,
    epoch = 0;
  const snapshots = new Map();
  const rowSnapshots = new Map();
  const listFilters = new Map();
  const pageSize = 10;
  const icons = {
    overview:
      '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
    users: '<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/>',
    products: '<path d="m3 7 9-4 9 4v10l-9 4-9-4V7Zm0 0 9 4 9-4M12 11v10"/>',
    coupons: '<path d="M3 5h18v5a2 2 0 0 0 0 4v5H3v-5a2 2 0 0 0 0-4V5Zm8 0v14"/>',
    activities: '<path d="m12 3 3 6 6 1-4 5 1 6-6-3-6 3 1-6-4-5 6-1 3-6Z"/>',
    settings: '<path d="M3 4h18v16H3V4Zm0 5h18M8 9v11"/>',
    payments: '<rect x="3" y="5" width="18" height="14" rx="3"/><path d="M3 10h18M7 15h3"/>',
    fulfillment: '<path d="m3 7 9-4 9 4v10l-9 4-9-4V7Zm0 0 9 4 9-4M12 11v10M7 5l10 5"/>',
    aftersales: '<path d="M4 9a8 8 0 1 1 0 7M4 4v5h5M12 8v5l3 2"/>',
    refunds: '<path d="M8 5H4v4M4 5l5 5M5 16a8 8 0 1 0 0-9M10 12h5m-5 3h5"/>',
    invoices: '<path d="M6 3h12v18l-3-2-3 2-3-2-3 2V3Zm3 5h6m-6 4h6"/>',
    interactions: '<path d="M21 11a8 8 0 0 1-8 8H7l-4 3V7a4 4 0 0 1 4-4h6a8 8 0 0 1 8 8ZM7 9h10M7 13h6"/>',
    tickets: '<path d="M4 14v-3a8 8 0 0 1 16 0v3M4 12H2v6h4v-6H4Zm16 0h2v6h-4v-6h2Zm0 6a4 4 0 0 1-4 4h-3"/>',
    reconciliation: '<path d="M6 3h12v18H6V3Zm3 5h6m-6 4h6m-6 4h3"/>',
    inventory: '<path d="M3 9h18v12H3V9Zm2 0V3h14v6M9 3v6m6-6v6M8 14h8"/>',
    categories:
      '<rect x="3" y="3" width="6" height="6" rx="1"/><rect x="15" y="3" width="6" height="6" rx="1"/><rect x="3" y="15" width="6" height="6" rx="1"/><rect x="15" y="15" width="6" height="6" rx="1"/>',
    popups: '<path d="M3 7h18v4a2 2 0 0 0 0 4v4H3v-4a2 2 0 0 0 0-4V7Zm8 0v12M7 3l4 4 5-4"/>',
    content: '<path d="M5 3h10l4 4v14H5V3Zm9 0v5h5M9 12h6m-6 4h6"/>',
    analytics: '<path d="M4 3v18h17M8 17v-5m5 5V7m5 10v-8"/>',
    arrow: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
    search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 5 5"/>',
    refresh: '<path d="M20 10a8 8 0 0 0-14-5L3 8m0-5v5h5M4 14a8 8 0 0 0 14 5l3-3m0 5v-5h-5"/>',
  };
  const icon = (name) =>
    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.overview}</svg>`;
  const groups = [
    ['总览', ['overview']],
    ['运行管理', ['tasks', 'audit']],
    ['系统设置', ['paymentSettings', 'loginSettings']],
    [
      '交易与履约',
      ['orders', 'payments', 'fulfillment', 'aftersales', 'refunds', 'invoices', 'reconciliation', 'shipping'],
    ],
    ['商品与库存', ['products', 'inventory', 'stockwatch', 'productAlerts', 'categories']],
    ['营销与装修', ['coupons', 'activities', 'special', 'recommendation', 'popups', 'settings']],
    ['运营与服务', ['users', 'points', 'content', 'messages', 'interactions', 'tickets', 'analytics']],
  ];
  const descriptions = {
    overview: '查看业务动态，快速进入日常管理。',
    payments: '核对支付记录，处理待确认的演示收款。',
    fulfillment: '跟进仓库出库、配送与签收进度。',
    aftersales: '集中处理退款、退货与换货申请。',
    refunds: '查看退款进度，确认演示渠道退款回执。',
    invoices: '管理开票申请与发票红冲记录。',
    reconciliation: '核对内部交易记录，定位账务差异。',
    inventory: '查询商品库存，管理数量调整与退货入库。',
    content: '维护商城公告、帮助内容与页面配置。',
    interactions: '审核买家评价，回复商品问题。',
    tickets: '查看客户会话，跟进问题处理。',
    analytics: '查看用户行为，了解商城访问与互动。',
  };
  const filterState = () => {
    if (!listFilters.has(tab)) listFilters.set(tab, { keyword: '', status: '', page: 1 });
    return listFilters.get(tab);
  };
  function closeNavigation() {
    document.body.classList.remove('admin-nav-open');
    $('#admin-nav-toggle')?.setAttribute('aria-expanded', 'false');
  }

  const scope = () => API.getBase() + '|' + token;
  const placeholder = (type, count, label) => window.LumoSkeleton?.render(type, { count, label }) || '';
  function loadingPage(name) {
    return (
      title(name) +
      (tab === 'overview'
        ? placeholder('summary', 3, '正在读取工作台统计') + placeholder('rows', 3, '正在读取业务概览')
        : ['categories', 'popups', 'coupons', 'activities', 'settings', 'content', 'products'].includes(tab)
          ? placeholder('form', 6, '正在读取管理配置')
          : placeholder('table', 6, '正在读取' + name))
    );
  }

  const menus = [
    ['overview', '工作台'],
    ['paymentSettings', '支付方式'],
    ['loginSettings', '登录方式'],
    ['productAlerts', '购物提醒'],
    ['special', '秒杀与拼团'],
    ['stockwatch', '库存预警与流水'],
    ['points', '会员与积分'],
    ['shipping', '配送与运费'],
    ['messages', '通知模板'],
    ['recommendation', '推荐与搜索'],
    ['tasks', '异常任务'],
    ['audit', '账号操作审计'],
    ['users', '用户管理'],
    ['products', '商品管理'],
    ['coupons', '优惠券管理'],
    ['activities', '活动管理'],
    ['settings', '商城装修'],
    ['orders', '订单管理'],
    ['payments', '支付管理'],
    ['fulfillment', '仓储履约'],
    ['aftersales', '售后管理'],
    ['refunds', '退款管理'],
    ['invoices', '发票管理'],
    ['interactions', '评价与问答'],
    ['tickets', '客服工单'],
    ['reconciliation', '对账记录'],
    ['inventory', '库存管理'],
    ['categories', '分类导航'],
    ['popups', '营销弹窗'],
    ['content', '内容管理'],
    ['analytics', '行为记录'],
  ];
  const specs = {
    payments: [C.Service.Payment, C.Action.List],
    fulfillment: [C.Service.Fulfillment, C.Action.List],
    aftersales: [C.Service.Aftersale, C.Action.List],
    refunds: [C.Service.Refund, C.Action.List],
    invoices: [C.Service.Invoice, C.Action.List],
    interactions: [C.Service.Interaction, C.Action.List],
    tickets: [C.Service.Helpdesk, C.Action.List],
    reconciliation: [C.Service.Reconciliation, C.Action.List],
    analytics: [C.Service.Analytics, C.Action.List],
  };
  const statusNames = {
    [C.OrderStatus.Creating]: '创建中',
    [C.OrderStatus.Created]: '已创建',
    [C.OrderStatus.CreateFailed]: '创建失败',
    [C.OrderStatus.PendingPayment]: '待支付',
    [C.OrderStatus.AwaitingPayment]: '待支付',
    [C.OrderStatus.PaidConfirming]: '收款确认中',
    [C.OrderStatus.Paid]: '已支付',
    [C.OrderStatus.PaidWaitGroup]: '已支付待成团',
    [C.OrderStatus.AwaitingGroup]: '待成团',
    [C.OrderStatus.WaitingGroup]: '待成团',
    [C.OrderStatus.ReadyToFulfill]: '待履约',
    [C.OrderStatus.Fulfilling]: '履约中',
    [C.OrderStatus.Shipped]: '已发货',
    [C.OrderStatus.Delivered]: '已送达',
    [C.OrderStatus.Completed]: '已完成',
    [C.OrderStatus.Closing]: '关闭中',
    [C.OrderStatus.PartiallyRefunded]: '部分退款',
    [C.OrderStatus.Terminated]: '已终止',
    [C.RefundStatus.Processing]: '处理中',
    [C.PaymentStatus.Pending]: '待确认',
    [C.PaymentStatus.Succeeded]: '已成功',
    [C.FulfillmentStatus.Ready]: '待发货',
    [C.FulfillmentStatus.Dispatched]: '已发货',
    [C.AftersaleStatus.Requested]: '待处理',
    [C.AftersaleStatus.Approved]: '已批准',
    [C.AftersaleStatus.Returning]: '退货中',
    [C.FulfillmentStatus.Inspected]: '已验收',
    [C.AftersaleStatus.Refunding]: '退款中',
    [C.OrderStatus.Refunded]: '已退款',
    [C.AftersaleStatus.Exchanged]: '已换货',
    [C.InvoiceStatus.Issued]: '已开票',
    [C.HelpdeskStatus.Open]: '待回复',
    [C.HelpdeskStatus.Replied]: '已回复',
    [C.PaymentStatus.Closed]: '已关闭',
    [C.ReconciliationStatus.Matched]: '一致',
    [C.ReconciliationStatus.Differences]: '有差异',
    [C.InteractionStatus.Published]: '已公开',
    [C.AftersaleStatus.Rejected]: '已驳回',
    [C.FulfillmentStatus.Cancelled]: '已取消',
  };
  const currency = (n) => '¥' + (Number(n || 0) / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2 });
  const idKey = () =>
    'admin-' + (crypto.randomUUID ? crypto.randomUUID() : Date.now() + '-' + Math.random().toString(16).slice(2));
  const message = (text) => {
    const el = $('#admin-toast');
    el.textContent = text;
    el.style.display = 'block';
    setTimeout(() => {
      el.style.display = 'none';
    }, 3500);
  };
  async function call(service, action, command = {}) {
    const session = token;
    try {
      const result = await API.call(service, action, command, { token: session, auth: true });
      if (session === token) lastTrace = API.requests()[0]?.traceId || '';
      return result;
    } catch (error) {
      if (error.status === 401 && session === token) {
        token = '';
        sessionStorage.removeItem(tokenKey);
        epoch++;
        login();
      }
      throw error;
    }
  }
  const editors = {
    productAlerts: window.LumoAlertsAdmin,
    paymentSettings: window.LumoAccessAdmin?.create('paymentSettings'),
    loginSettings: window.LumoAccessAdmin?.create('loginSettings'),
    overview: window.LumoDashboardAdmin,
    reconciliation: window.LumoManagementAdmin?.create('reconciliation') || window.LumoReconciliationAdmin,
    users: window.LumoUsersAdmin,
    inventory: window.LumoInventoryAdmin,
    ...Object.fromEntries(
      ['orders', 'payments', 'fulfillment', 'aftersales', 'refunds', 'invoices'].map((key) => [
        key,
        window.LumoOperationsAdmin?.create(key),
      ]),
    ),
    categories: window.LumoCategoryEditor,
    popups: window.LumoPopupAdmin,
    coupons: window.LumoCouponAdmin,
    activities: window.LumoPromotionAdmin,
    settings: window.LumoStoreSettingsAdmin,
    content: window.LumoManagementAdmin?.create('content') || window.LumoContentAdmin,
    products: window.LumoCatalogAdmin,
    ...Object.fromEntries(
      ['special', 'stockwatch', 'points', 'shipping', 'messages', 'recommendation', 'tasks', 'audit'].map((key) => [
        key,
        window.LumoManagementAdmin?.create(key),
      ]),
    ),
  };
  async function navigate(next, params = {}) {
    if (!token || !menus.some(([key]) => key === next)) return;
    const origin = scope();
    tab = next;
    closeNavigation();
    await load();
    if (origin !== scope() || tab !== next) return;
    if (next === 'inventory' && params.skuId && window.LumoInventoryAdmin) {
      await window.LumoInventoryAdmin.selectSKU(params.skuId);
    } else if (next === 'inventory' && params.skuId) {
      for (const selector of ['#inventory-query [name="sku_id"]', '#inventory-adjust [name="sku_id"]']) {
        const input = $(selector);
        if (input) input.value = params.skuId;
      }
    }
    window.scrollTo(0, 0);
  }
  for (const [key, editor] of Object.entries(editors))
    editor?.configure({
      isAdmin: () => !!token,
      scope,
      call,
      draw: () => {
        if (tab === key && token) $('#admin-main').innerHTML = editor.render();
      },
      notify: message,
      navigate,
    });
  function errorHTML(error) {
    return `<div class="admin-error">${esc(error.message || '请求失败')}${error.traceID || error.traceId ? `<div class="admin-trace">Trace ID: ${esc(error.traceID || error.traceId)}</div>` : ''}</div>`;
  }
  function nav() {
    document.body.dataset.adminAuth = token ? 'true' : 'false';
    const name = menus.find(([key]) => key === tab)?.[1] || '工作台';
    $('#admin-breadcrumb').textContent = token ? name : '管理员登录';
    $('#admin-session').hidden = !token;
    $('#admin-nav-toggle').hidden = !token;
    $('#admin-nav').innerHTML = token
      ? groups
          .map(
            ([label, keys]) =>
              `<section class="admin-nav-group"><h2>${label}</h2>${keys
                .map((key) => {
                  const name = menus.find(([id]) => id === key)[1];
                  return `<button class="admin-nav-btn ${tab === key ? 'active' : ''}" data-tab="${key}" ${tab === key ? 'aria-current="page"' : ''}>${icon(key)}<span>${name}</span></button>`;
                })
                .join('')}</section>`,
          )
          .join('') +
        `<div class="admin-nav-bottom"><a class="admin-nav-btn" href="index.html#/editor">${icon('content')}<span>商品与页面编辑 ↗</span></a><a class="admin-nav-btn" href="index.html">${icon('arrow')}<span>返回商城</span></a></div>`
      : '<div class="admin-nav-intro">让日常经营<br>更清晰、更有序。<small>商品 · 交易 · 运营 · 服务</small></div>';
  }
  function login() {
    snapshots.clear();
    rowSnapshots.clear();
    listFilters.clear();
    rows = [];
    closeNavigation();
    $('#admin-main').removeAttribute('aria-busy');
    for (const editor of Object.values(editors)) editor?.reset();
    nav();
    $('#admin-main').innerHTML =
      `<div class="admin-login"><div class="admin-login-mark">L</div><span class="admin-eyebrow">LUMO 商城管理</span><h1>登录商城管理</h1><p class="admin-note">使用管理员账号处理支付、履约和售后。管理会话与商城顾客会话分别保存。</p><form id="admin-login"><label class="field">账号<input name="username" autocomplete="username" required></label><label class="field">密码<input type="password" name="password" autocomplete="current-password" required></label><button class="primary" type="submit">登录</button></form><div id="admin-error"></div></div>`;
  }
  function title(text, extra = '') {
    return `<div class="admin-title"><div><h1>${esc(text)}</h1><p>${esc(descriptions[tab] || '')}</p></div><div class="admin-title-actions">${extra}<button class="outline" data-action="reload">${icon('refresh')}刷新</button></div></div>`;
  }
  const button = (action, id, label) => `<button data-operation="${action}" data-id="${esc(id)}">${label}</button>`;
  function copyPreview(value) {
    const text = String(value || '—');
    if (text.length <= 120) return `<span class="admin-copy-preview">${esc(text)}</span>`;
    return `<span class="admin-copy-preview">${esc(text.slice(0, 120))}…</span><details class="admin-copy-details"><summary>展开全文</summary><p>${esc(text)}</p></details>`;
  }
  function rowActions(row) {
    const id = row.id;
    switch (tab) {
      case 'payments':
        return row.status === C.PaymentStatus.Pending
          ? button(C.Service.Payment + '.' + C.Action.ConfirmDemo, id, '确认演示收款')
          : '';
      case 'fulfillment':
        return (
          (row.status === C.FulfillmentStatus.Ready
            ? button(C.Service.Fulfillment + '.' + C.Action.Dispatch, id, '确认出库') +
              button(C.Service.Fulfillment + '.' + C.Action.Cancel, id, '拦截取消')
            : '') +
          (row.status === C.FulfillmentStatus.Dispatched
            ? button(C.Service.Logistics + '.' + C.Action.Deliver, id, '确认演示签收')
            : '')
        );
      case 'aftersales':
        return (
          (row.status === C.AftersaleStatus.Requested
            ? button(C.Service.Aftersale + '.' + C.Action.Approve, id, '批准') +
              button(C.Service.Aftersale + '.' + C.Action.Reject, id, '驳回')
            : '') +
          (row.status === C.AftersaleStatus.Returning
            ? button(C.Service.Aftersale + '.' + C.Action.Inspect, id, '验收退货')
            : '') +
          ((row.type === C.AftersaleType.Refund && row.status === C.AftersaleStatus.Approved) ||
          (row.type === C.AftersaleType.Return && row.status === C.AftersaleStatus.Inspected)
            ? button(C.Service.Aftersale + '.' + C.Action.Refund, id, '发起退款')
            : '') +
          (row.type === C.AftersaleType.Exchange && row.status === C.AftersaleStatus.Inspected
            ? button(C.Service.Aftersale + '.' + C.Action.Exchange, id, '创建换货履约')
            : '')
        );
      case 'refunds':
        return [C.RefundStatus.Pending, C.RefundStatus.Processing].includes(row.status)
          ? button(C.Service.Refund + '.' + C.Action.ConfirmDemo, id, '确认演示退款')
          : '';
      case 'invoices':
        return (
          (row.status === C.InvoiceStatus.Requested
            ? button(C.Service.Invoice + '.' + C.Action.IssueDemo, id, '演示开票')
            : '') +
          button(C.Service.Invoice + '.' + C.Action.AdjustDemo, id, '处理红冲') +
          button(C.Service.Invoice + '.' + C.Action.Reversals, id, '查看红冲记录')
        );
      case 'interactions':
        return (
          button(C.Service.Interaction + '.' + C.Action.Moderate, id, '审核') +
          (row.kind === C.InteractionKind.Question
            ? button(C.Service.Interaction + '.' + C.Action.Answer, id, '回复问题')
            : '')
        );
      case 'tickets':
        return (
          button(C.Service.Helpdesk + '.' + C.Action.Get, id, '查看会话') +
          (row.status !== C.HelpdeskStatus.Closed
            ? button(C.Service.Helpdesk + '.' + C.Action.Reply, id, '客服回复') +
              button(C.Service.Helpdesk + '.' + C.Action.Close, id, '关闭工单')
            : '')
        );
      default:
        return '';
    }
  }
  function stateBadge(status) {
    const tone = [
      C.PaymentStatus.Succeeded,
      C.FulfillmentStatus.Dispatched,
      C.InvoiceStatus.Issued,
      C.ReconciliationStatus.Matched,
      C.InteractionStatus.Published,
    ].includes(status)
      ? 'success'
      : [
            C.PaymentStatus.Pending,
            C.AftersaleStatus.Requested,
            C.HelpdeskStatus.Open,
            C.RefundStatus.Processing,
            C.FulfillmentStatus.Ready,
          ].includes(status)
        ? 'pending'
        : [C.AftersaleStatus.Rejected, C.ReconciliationStatus.Differences].includes(status)
          ? 'danger'
          : 'neutral';
    return `<span class="admin-state ${tone}"><i></i>${esc(statusNames[status] || status || '—')}</span>`;
  }
  function table() {
    const state = filterState();
    const keyword = state.keyword.trim().toLocaleLowerCase();
    const filtered = rows.filter(
      (row) =>
        (!state.status || row.status === state.status) &&
        (!keyword ||
          [
            row.id,
            row.order_id,
            row.user_id,
            row.title,
            row.name,
            row.kind,
            row.type,
            row.body,
            row.reason,
            row.tracking_id,
            row.number,
            row.answer,
            statusNames[row.status],
            row.status,
          ].some((value) =>
            String(value ?? '')
              .toLocaleLowerCase()
              .includes(keyword),
          )),
    );
    const pages = Math.max(1, Math.ceil(filtered.length / pageSize));
    state.page = Math.max(1, Math.min(state.page, pages));
    const visible = filtered.slice((state.page - 1) * pageSize, state.page * pageSize);
    const summary = `<div class="admin-list-summary"><span>匹配 <strong>${filtered.length}</strong> 条记录</span><span>已加载 ${rows.length} 条</span></div>`;
    if (!visible.length)
      return (
        summary +
        `<div class="admin-empty">${icon('search')}<strong>${rows.length ? '没有符合条件的记录' : '暂无记录'}</strong><span>${rows.length ? '调整关键词或状态，重新查找。' : '新的业务记录会显示在这里。'}</span>${rows.length ? '<button class="outline" data-action="reset-filters">清除筛选</button>' : ''}</div>`
      );
    return (
      summary +
      `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table"><thead><tr><th scope="col">业务对象</th><th scope="col">内容 / 金额</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead><tbody>${visible.map((row) => `<tr><td><strong>${esc(row.title || row.name || row.kind || row.type || '业务记录')}</strong><small>${esc(row.id)}</small><small>${row.order_id ? '订单 ' + esc(row.order_id) : esc(row.user_id || '')}</small></td><td>${row.amount_cents != null ? `<strong class="admin-amount">${currency(row.amount_cents)}</strong>` : ''}${copyPreview(row.body || row.reason || row.tracking_id || row.number)}${row.details ? `<details class="admin-record-details"><summary>查看明细</summary><pre>${esc(JSON.stringify(row.details, null, 2))}</pre></details>` : ''}${row.answer ? `<div class="admin-answer">回复：${copyPreview(row.answer)}</div>` : ''}</td><td>${stateBadge(row.status)}</td><td><div class="admin-actions">${rowActions(row) || '<span class="admin-muted">—</span>'}</div></td></tr>`).join('')}</tbody></table></div><div class="admin-pagination"><span>第 ${(state.page - 1) * pageSize + 1}–${Math.min(state.page * pageSize, filtered.length)} 条，共 ${filtered.length} 条</span><div><button class="outline" data-list-page="${state.page - 1}" ${state.page === 1 ? 'disabled' : ''}>上一页</button><span>${state.page} / ${pages}</span><button class="outline" data-list-page="${state.page + 1}" ${state.page === pages ? 'disabled' : ''}>下一页</button></div></div>`
    );
  }
  function listPage(name) {
    const state = filterState();
    const statuses = [...new Set([...rows.map((row) => row.status), state.status].filter(Boolean))];
    return (
      title(
        name,
        tab === 'reconciliation'
          ? '<button class="primary" data-operation="reconciliation.run">执行内部对账</button>'
          : '',
      ) +
      `<section class="admin-list-panel"><div class="admin-list-toolbar"><label class="admin-search">${icon('search')}<input id="admin-list-search" data-list-filter="keyword" value="${esc(state.keyword)}" placeholder="搜索名称、订单号或编号" aria-label="搜索业务记录" type="search"></label><label class="admin-status-filter">状态<select id="admin-list-status" data-list-filter="status"><option value="">全部状态</option>${statuses.map((status) => `<option value="${esc(status)}" ${state.status === status ? 'selected' : ''}>${esc(statusNames[status] || status)}</option>`).join('')}</select></label></div>${tab === 'payments' || tab === 'refunds' ? '<div class="admin-inline-note">演示渠道确认将更新实际业务状态，请核对记录后操作。</div>' : ''}<div id="admin-list">${table()}</div></section><details class="admin-trace-details"><summary>查看最近调用链路</summary><div class="admin-trace">${esc(lastTrace || '暂无链路编号')}</div></details><section id="operation-result"></section>`
    );
  }
  function updateList() {
    const target = $('#admin-list');
    if (target && specs[tab]) {
      target.innerHTML = table();
      snapshots.set(scope() + '|' + tab, listPage(menus.find(([key]) => key === tab)[1]));
    }
  }
  function field(label, name, type = 'text', value = '', extra = '') {
    return `<label class="field ${extra}">${label}<input name="${name}" type="${type}" value="${esc(value)}" ${type === 'number' ? `min="${name === 'quantity' ? 1 : 0}" step="1"` : ''} required></label>`;
  }
  function inventoryForms() {
    return `<div class="admin-inventory-layout"><section class="admin-section"><div class="admin-section-heading">${icon('search')}<div><h2>库存查询</h2><p>输入 SKU 编号，查看当前库存快照。</p></div></div><form id="inventory-query" class="admin-form">${field('SKU 编号', 'sku_id', 'text', '', 'wide')}<div class="admin-form-footer"><button class="outline">查询库存</button></div></form></section><section class="admin-section"><div class="admin-section-heading">${icon('inventory')}<div><h2>调整库存</h2><p>设置在库总量，不得低于已预占与已分配数量。</p></div></div><form id="inventory-adjust" class="admin-form">${field('SKU 编号', 'sku_id')}${field('在库总量', 'on_hand', 'number')}${field('调整原因', 'reason', 'text', '', 'wide')}<div class="admin-form-footer"><button class="primary">提交调整</button></div></form></section><section class="admin-section admin-wide"><div class="admin-section-heading">${icon('fulfillment')}<div><h2>仓库退货入库</h2><p>售后退货请优先在售后处理页验收。独立收货请填写唯一凭证，避免重复入库。</p></div></div><form id="return-inspect" class="admin-form">${field('唯一验收凭证号', 'inspection_id')}${field('SKU 编号', 'sku_id')}${field('实收数量', 'quantity', 'number')}<label class="field">品质<select name="condition"><option value="sellable">可售</option><option value="damaged">破损</option><option value="unsellable">不可售</option></select></label><div class="admin-form-footer"><button class="primary">登记入库</button></div></form></section></div><div id="admin-result"></div>`;
  }
  function overview(all) {
    const orders = all[0].items || [];
    return (
      title('工作台') +
      `<div class="admin-cards">${[
        ['近期订单', orders.length, 'overview', '本次加载的订单记录'],
        [
          '待处理售后',
          (all[1].items || []).filter((row) => row.status === C.AftersaleStatus.Requested).length,
          'aftersales',
          '等待审核的售后申请',
        ],
        [
          '未关闭工单',
          (all[2].items || []).filter((row) => row.status !== C.HelpdeskStatus.Closed).length,
          'tickets',
          '需要继续跟进的客户会话',
        ],
      ]
        .map(
          ([name, count, key, note]) =>
            `<article class="admin-card"><div><span>${name}</span><i>${icon(key)}</i></div><strong>${count}</strong><small>${note}</small>${key !== 'overview' ? `<button data-tab="${key}" aria-label="查看${name}">${icon('arrow')}</button>` : ''}</article>`,
        )
        .join(
          '',
        )}</div><div class="admin-dashboard-grid"><section class="admin-section"><div class="admin-panel-heading"><h2>常用管理</h2><span>日常工作，一步直达</span></div><div class="admin-shortcuts">${[
        ['coupons', '优惠券管理', '创建优惠券与领取条件'],
        ['activities', '活动管理', '配置满减、折扣与额度'],
        ['products', '商品管理', '维护商品信息与上下架'],
        ['settings', '商城装修', '调整品牌、主视觉与主题'],
        ['inventory', '库存管理', '查询和调整库存'],
        ['popups', '营销弹窗', '配置人群与权益'],
      ]
        .map(
          ([key, label, note]) =>
            `<button data-tab="${key}"><i>${icon(key)}</i><span><strong>${label}</strong><small>${note}</small></span>${icon('arrow')}</button>`,
        )
        .join(
          '',
        )}</div></section><section class="admin-section admin-flow"><div class="admin-panel-heading"><h2>交易处理流程</h2><span class="admin-demo-label">演示渠道</span></div><ol>${[
        ['payments', '确认收款', '核对支付单，确认渠道回执'],
        ['fulfillment', '仓库出库', '确认订单资源，安排发货'],
        ['fulfillment', '配送签收', '跟进物流，完成演示签收'],
        ['aftersales', '售后服务', '按申请处理退货、退款或换货'],
      ]
        .map(
          ([key, label, note], index) =>
            `<li><b>${index + 1}</b><div><button data-tab="${key}">${label}${icon('arrow')}</button><p>${note}</p></div></li>`,
        )
        .join(
          '',
        )}</ol></section></div><section class="admin-section admin-recent"><div class="admin-panel-heading"><h2>近期订单</h2><span>服务端实时记录 · 仅展示最近 5 条</span></div>${
        orders.length
          ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table"><thead><tr><th>订单编号</th><th>用户</th><th>应付金额</th><th>状态</th></tr></thead><tbody>${orders
              .slice(0, 5)
              .map(
                (row) =>
                  `<tr><td><strong>${esc(row.id)}</strong></td><td>${esc(row.user_id || '—')}</td><td class="admin-amount">${currency(row.payable_cents ?? row.amount_cents)}</td><td>${stateBadge(row.status)}</td></tr>`,
              )
              .join('')}</tbody></table></div>`
          : '<div class="admin-empty"><strong>暂无订单记录</strong><span>顾客提交订单后，将显示在这里。</span></div>'
      }</section>`
    );
  }
  function renderMain(html) {
    const focused = document.activeElement;
    const focusId = ['admin-list-search', 'admin-list-status'].includes(focused?.id) ? focused.id : '';
    const start = focused?.selectionStart,
      end = focused?.selectionEnd;
    $('#admin-main').innerHTML = html;
    if (focusId) {
      const replacement = $('#' + focusId);
      replacement?.focus({ preventScroll: true });
      if (typeof start === 'number' && typeof end === 'number') replacement?.setSelectionRange(start, end);
    }
  }
  async function load() {
    const revision = ++epoch;
    nav();
    if (!token) {
      login();
      return;
    }
    const name = menus.find((x) => x[0] === tab)?.[1] || '',
      origin = scope(),
      key = origin + '|' + tab,
      main = $('#admin-main'),
      cached = snapshots.get(key);
    rows = rowSnapshots.get(key) || [];
    main.innerHTML = cached || loadingPage(name);
    main.setAttribute('aria-busy', 'true');
    const current = () => revision === epoch && origin === scope() && !!token;
    const show = (html) => {
      if (!current()) return;
      renderMain(html);
      snapshots.set(key, html);
    };
    try {
      if (editors[tab]) {
        const editor = editors[tab],
          pending = editor.refresh();
        if (current()) main.innerHTML = editor.render();
        await pending;
        show(editor.render());
        return;
      }
      if (tab === 'overview') {
        const all = await Promise.all([
          call(C.Service.Order, C.Action.List),
          call(C.Service.Aftersale, C.Action.List),
          call(C.Service.Helpdesk, C.Action.List),
        ]);
        if (!current()) return;
        show(overview(all));
        return;
      }
      if (tab === 'inventory') {
        show(title(name) + inventoryForms());
        return;
      }
      let fetched = [];
      if (tab === 'interactions') {
        const [reviews, questions] = await Promise.all([
          call(C.Service.Interaction, C.Action.List, {
            data: { kind: C.InteractionKind.Review, include_pending: true },
          }),
          call(C.Service.Interaction, C.Action.List, {
            data: { kind: C.InteractionKind.Question, include_pending: true },
          }),
        ]);
        fetched = [...(reviews.items || []), ...(questions.items || [])];
      } else {
        const [service, action] = specs[tab];
        const result = await call(service, action);
        fetched = result.items || [];
      }
      if (!current()) return;
      rows = fetched;
      rowSnapshots.set(key, fetched);
      show(listPage(name));
    } catch (error) {
      if (!current()) return;
      const latest = snapshots.get(key);
      renderMain(latest ? latest + errorHTML(error) : title(name) + errorHTML(error));
    } finally {
      if (current()) main.removeAttribute('aria-busy');
    }
  }
  function showResult(result) {
    const target = $('#operation-result') || $('#admin-result');
    if (target)
      target.innerHTML = `<section class="admin-section"><h2>处理结果</h2><pre>${esc(JSON.stringify(result, null, 2))}</pre><div class="admin-trace">链路编号：${esc(lastTrace)}</div></section>`;
  }
  document.addEventListener('click', async (event) => {
    const target = event.target.closest('[data-tab],[data-action],[data-operation],[data-list-page]');
    if (!target) return;
    if (target.dataset.action === 'toggle-nav') {
      const expanded = $('#admin-nav-toggle').getAttribute('aria-expanded') !== 'true';
      document.body.classList.toggle('admin-nav-open', expanded);
      $('#admin-nav-toggle').setAttribute('aria-expanded', expanded);
      if (expanded) $('#admin-sidebar-close')?.focus();
      return;
    }
    if (target.dataset.action === 'close-nav') {
      closeNavigation();
      $('#admin-nav-toggle')?.focus();
      return;
    }
    if (busy) return;
    if (target.dataset.listPage !== undefined && specs[tab]) {
      filterState().page = Number(target.dataset.listPage) || 1;
      updateList();
      return;
    }
    if (target.dataset.action === 'reset-filters') {
      listFilters.delete(tab);
      const html = listPage(menus.find(([key]) => key === tab)[1]);
      $('#admin-main').innerHTML = html;
      snapshots.set(scope() + '|' + tab, html);
      return;
    }
    if (target.dataset.tab) {
      if (!menus.some(([key]) => key === target.dataset.tab)) return;
      tab = target.dataset.tab;
      closeNavigation();
      load();
      window.scrollTo(0, 0);
      return;
    }
    if (target.dataset.action === 'reload') {
      load();
      return;
    }
    if (target.dataset.action === C.Action.Logout) {
      try {
        await call(C.Service.IAM, C.Action.Logout);
        token = '';
        sessionStorage.removeItem(tokenKey);
        login();
      } catch (error) {
        message(error.message);
      }
      return;
    }
    if (!target.dataset.operation) return;
    const [service, action] = target.dataset.operation.split('.');
    const command = { id: target.dataset.id || '', data: {}, idempotency_key: idKey() };
    if (action === C.Action.Inspect) {
      const condition = prompt('退货品质：sellable / damaged / unsellable', C.StockCondition.Sellable);
      if (condition === null) return;
      if (![C.StockCondition.Sellable, C.StockCondition.Damaged, C.StockCondition.Unsellable].includes(condition)) {
        message('请选择有效品质');
        return;
      }
      command.data.condition = condition;
    }
    if (service === C.Service.Interaction && action === C.Action.Moderate) {
      const approved = confirm('确定公开此内容？点击取消可选择驳回。');
      if (!approved && !confirm('是否驳回此内容？')) return;
      command.data.approved = approved;
    }
    if (action === C.Action.Answer || action === C.Action.Reply) {
      const body = prompt('输入回复内容');
      if (!body?.trim()) return;
      command.data[action === C.Action.Answer ? 'answer' : 'body'] = body.trim();
    }
    busy = true;
    target.disabled = true;
    if (action === C.Action.Get || action === C.Action.Reversals) {
      const resultPanel = $('#operation-result') || $('#admin-result');
      if (resultPanel) resultPanel.innerHTML = placeholder('rows', 3, '正在读取业务详情');
    }
    try {
      const result = await call(service, action, command);
      if (action === C.Action.Get || action === C.Action.Reversals) showResult(result);
      else {
        message('后端已受理');
        await load();
        showResult(result);
      }
    } catch (error) {
      const el = $('#operation-result');
      if (el) el.innerHTML = errorHTML(error);
      else message(error.message);
    } finally {
      busy = false;
      target.disabled = false;
    }
  });
  const filterList = (event) => {
    const key = event.target.dataset.listFilter;
    if (!specs[tab] || !['keyword', 'status'].includes(key)) return;
    const state = filterState();
    state[key] = event.target.value;
    state.page = 1;
    updateList();
  };
  document.addEventListener('input', (event) => {
    if (event.target.dataset.listFilter === 'keyword') filterList(event);
  });
  document.addEventListener('change', (event) => {
    if (event.target.dataset.listFilter === 'status') filterList(event);
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && $('#admin-nav-toggle').getAttribute('aria-expanded') === 'true') {
      closeNavigation();
      $('#admin-nav-toggle').focus();
    }
    if (event.key === 'Tab' && $('#admin-nav-toggle').getAttribute('aria-expanded') === 'true') {
      const focusable = [...$('#admin-sidebar').querySelectorAll('a[href],button:not([disabled])')];
      const first = focusable[0],
        last = focusable[focusable.length - 1];
      if (event.shiftKey && event.target === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && event.target === last) {
        event.preventDefault();
        first?.focus();
      }
    }
  });
  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (!['admin-login', 'inventory-query', 'inventory-adjust', 'return-inspect'].includes(form.id)) return;
    event.preventDefault();
    if (busy) return;
    busy = true;
    const button = form.querySelector('button');
    button.disabled = true;
    const data = Object.fromEntries(new FormData(form));
    try {
      if (form.id === 'admin-login') {
        const session = await API.call(C.Service.IAM, C.Action.Login, { data }, { auth: false, token: '' });
        if (session.role !== C.Role.Admin) {
          await API.call(C.Service.IAM, C.Action.Logout, {}, { token: session.token });
          throw Error('此账号没有管理员权限');
        }
        token = session.token;
        sessionStorage.setItem(tokenKey, token);
        form.reset();
        await load();
        window.scrollTo(0, 0);
      }
      if (form.id === 'inventory-query') {
        $('#admin-result').innerHTML = placeholder('rows', 3, '正在读取库存快照');
        showResult(await call(C.Service.Inventory, C.Action.Get, { id: data.sku_id }));
      }
      if (form.id === 'inventory-adjust')
        showResult(
          await call(C.Service.Inventory, C.Action.Adjust, {
            id: data.sku_id,
            idempotency_key: idKey(),
            data: { on_hand: Number(data.on_hand), reason: data.reason },
          }),
        );
      if (form.id === 'return-inspect')
        showResult(
          await call(C.Service.Fulfillment, C.Action.InspectReturn, {
            id: data.inspection_id,
            data: { sku_id: data.sku_id, quantity: Number(data.quantity), condition: data.condition },
          }),
        );
    } catch (error) {
      const el = $('#admin-error') || $('#admin-result');
      if (el) el.innerHTML = errorHTML(error);
      else message(error.message);
    } finally {
      busy = false;
      button.disabled = false;
    }
  });
  window.addEventListener('resize', () => {
    if (window.innerWidth > 900) closeNavigation();
  });
  window.LumoAdminReady = (async () => {
    if (token) {
      $('#admin-main').innerHTML = placeholder('profile', 1, '正在验证管理员会话');
      try {
        const principal = await call(C.Service.IAM, C.Action.Current);
        if (principal.role !== C.Role.Admin) throw Error('管理员身份无效');
      } catch (_) {
        token = '';
        sessionStorage.removeItem(tokenKey);
      }
    }
    await load();
  })();
})();
