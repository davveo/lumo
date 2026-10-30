(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc } = U,
    modules = new Map();
  const ticketLabels = {
    [C.HelpdeskStatus.Open]: '待客服回复',
    [C.HelpdeskStatus.Replied]: '客服已回复',
    [C.HelpdeskStatus.Closed]: '已关闭',
  };
  const actionLabels = {
    [C.AnalyticsBehavior.View]: '浏览商品',
    [C.AnalyticsBehavior.Search]: '搜索商品',
    [C.AnalyticsBehavior.Click]: '点击商品',
    [C.AnalyticsBehavior.Impression]: '商品曝光',
    [C.Event.OrderCompleted]: '完成订单',
    [C.Event.RefundSucceeded]: '退款成功',
  };
  const sources = {
    product_detail: '商品详情',
    search: '商城搜索',
    homepage: '商城首页',
    recommendation: '推荐位',
    catalog: '商品列表',
  };
  function create(key) {
    if (modules.has(key)) return modules.get(key);
    const ticket = key === 'tickets',
      service = ticket ? C.Service.Helpdesk : C.Service.Analytics;
    const m = U.controller(
      'service-admin-' + key,
      () => ({
        ready: false,
        busy: false,
        error: '',
        items: [],
        total: 0,
        page: 1,
        keyword: '',
        status: '',
        type: '',
        from: '',
        to: '',
        selected: '',
        conversation: null,
        messageOffset: 0,
        draft: '',
        replyKey: '',
        confirmClose: false,
      }),
      async (module) => {
        const s = module.state,
          r = await module.adapter.call(service, C.Action.AdminList, {
            data: {
              page: s.page,
              page_size: 10,
              keyword: s.keyword,
              status: s.status,
              event_type: s.type,
              from: s.from,
              to: s.to,
            },
          });
        return {
          ready: true,
          items: r.items || [],
          total: Number(r.total) || 0,
          catalogError: !!r.catalog_unavailable,
        };
      },
      (module) => {
        const s = module.state;
        return (
          U.heading(
            ticket ? '客服工单' : '行为记录',
            ticket
              ? '按时间查看顾客和客服对话，直接回复并跟进处理。'
              : '查看登录会员的浏览、搜索及交易结果；记录实际发生的行为。',
            `<button class="outline" data-svc-admin="refresh" ${s.busy ? 'disabled' : ''}>刷新</button>`,
          ) +
          (!ticket
            ? '<details class="admin-section behavior-explainer"><summary>这些记录代表什么？</summary><p>浏览商品、搜索词来自商城埋点；订单完成和退款成功来自服务端事件。点击、曝光只在对应页面实际接入埋点后出现。它们用于了解购物行为，与“账号操作审计”分开。</p></details>'
            : '') +
          `<section class="admin-section"><form data-svc-filter class="op-filters"><label class="field">关键词<input name="keyword" maxlength="120" value="${esc(s.keyword)}" placeholder="${ticket ? '工单、会员、订单号或内容' : '商品、会员、订单号或搜索词'}"></label><label class="field">${ticket ? '工单状态' : '行为类型'}<select name="${ticket ? 'status' : 'type'}"><option value="">全部</option>${Object.entries(
            ticket ? ticketLabels : actionLabels,
          )
            .map(
              ([v, l]) => `<option value="${v}" ${v === (ticket ? s.status : s.type) ? 'selected' : ''}>${l}</option>`,
            )
            .join(
              '',
            )}</select></label><label class="field">开始日期<input name="from" type="date" value="${esc(s.from)}"></label><label class="field">结束日期<input name="to" type="date" value="${esc(s.to)}"></label><button class="outline" ${s.busy ? 'disabled' : ''}>查询</button></form>${s.catalogError ? '<p class="admin-error">商品名称暂时读取失败，当前显示商品编号；可刷新重试。</p>' : ''}${!s.ready && !s.error ? window.LumoSkeleton.render('table', { count: 5 }) : s.items.length ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="${ticket ? '客服工单' : '行为记录'}列表"><table class="admin-table svc-admin-table"><thead><tr>${ticket ? '<th>工单 / 顾客</th><th>问题摘要</th><th>状态 / 时间</th><th>操作</th>' : '<th>行为</th><th>商品 / 搜索词 / 关联单据</th><th>会员</th><th>时间 / 来源</th>'}</tr></thead><tbody>${s.items.map((r) => (ticket ? `<tr><td><strong>${esc(r.title || '服务工单')}</strong><small>工单 ${esc(r.id)}</small><small>顾客 ${esc(r.user_id)}</small></td><td><span class="svc-admin-preview">${esc(r.body)}</span>${r.order_id ? `<small>订单 ${esc(r.order_id)}</small>` : ''}</td><td><strong>${esc(ticketLabels[r.status] || '待处理')}</strong><small>${U.date(r.created_at)}</small></td><td><button class="outline" data-svc-admin="open" data-id="${esc(r.id)}">查看会话</button></td></tr>` : `<tr><td><strong>${esc(r.action_label || actionLabels[r.type] || '其他行为')}</strong><small>${esc(r.record_category || '访问互动')}</small></td><td><span>${esc(r.product_name || r.query || (r.refund_id ? '退款单 ' + r.refund_id : '') || r.order_id || r.sku_id || '历史记录缺少对象')}</span>${r.sku_id ? `<small>商品 ${esc(r.sku_id)}</small>` : ''}<details class="svc-admin-identifiers"><summary>关联编号</summary><p>记录：${esc(r.id)}</p>${r.order_id ? `<p>订单：${esc(r.order_id)}</p>` : ''}${r.refund_id ? `<p>退款单：${esc(r.refund_id)}</p>` : ''}${r.page ? `<p>页面：${esc(r.page)}</p>` : ''}</details></td><td><span>${esc(r.user_id || '历史会员')}</span></td><td>${U.date(r.created_at)}<small>${esc(sources[r.source] || (/Order|Refund/.test(r.type) ? '服务端交易事件' : '历史记录'))}</small></td></tr>`)).join('')}</tbody></table></div>` : U.empty(ticket ? '暂无符合条件的工单' : '暂无符合条件的行为记录', ticket ? '顾客提交问题后会显示在这里。' : '登录会员浏览商品、搜索或完成交易后会产生对应记录。')}${U.pager(s.total, s.page, 10, 'svc-admin', s.busy)}</section>${ticket ? conversation(module) : ''}`
        );
      },
    );
    modules.set(key, m);
    async function open(id, offset = 0, focus = false) {
      const s = m.state;
      if (id !== s.selected) {
        s.selected = id;
        s.conversation = null;
        s.draft = '';
        s.replyKey = '';
        s.confirmClose = false;
      }
      s.messageOffset = offset;
      await m.run(
        () => m.adapter.call(service, C.Action.Get, { id, data: { message_limit: 50, message_offset: offset } }),
        (r) => {
          s.conversation = r;
        },
      );
      if (focus && s.conversation && m.active())
        document.querySelector('.svc-conversation [data-svc-admin="close-detail"]')?.focus();
    }
    document.addEventListener('click', async (event) => {
      if (!m.active() || m.state.busy) return;
      const el = event.target.closest('[data-svc-admin],[data-svc-admin-page]');
      if (!el) return;
      if (el.dataset.svcAdminPage !== undefined) {
        const old = m.state.page;
        m.state.page = Math.max(1, Number(el.dataset.svcAdminPage) || 1);
        if (!(await m.refresh())) m.state.page = old;
        m.draw();
        return;
      }
      const action = el.dataset.svcAdmin,
        s = m.state;
      if (action === 'refresh') await m.refresh();
      if (action === 'open') await open(el.dataset.id, 0, true);
      if (action === 'close-detail') {
        const id = s.selected;
        s.selected = '';
        s.conversation = null;
        m.draw();
        Array.from(document.querySelectorAll('[data-svc-admin="open"]'))
          .find((b) => b.dataset.id === id)
          ?.focus();
      }
      if (action === 'messages') await open(s.selected, Math.max(0, Number(el.dataset.offset) || 0));
      if (action === 'close-ticket') {
        s.confirmClose = true;
        m.draw();
      }
      if (action === 'cancel-close') {
        s.confirmClose = false;
        m.draw();
      }
      if (action === 'confirm-close') {
        if (
          await m.run(
            () => m.adapter.call(service, C.Action.Close, { id: s.selected }),
            () => {},
          )
        ) {
          s.confirmClose = false;
          await open(s.selected, s.messageOffset);
          await m.refresh();
          m.adapter.notify?.('工单已关闭');
        }
      }
    });
    document.addEventListener('keydown', (event) => {
      if (!m.active() || !m.state.selected) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        m.state.selected = '';
        m.state.conversation = null;
        m.draw();
        return;
      }
      if (event.key === 'Tab') {
        const panel = document.querySelector('.svc-conversation');
        if (!panel) return;
        const controls = Array.from(
          panel.querySelectorAll('button:not([disabled]),textarea:not([disabled]),input:not([disabled]),a[href]'),
        );
        const first = controls[0],
          last = controls[controls.length - 1];
        if (first && !panel.contains(document.activeElement)) {
          event.preventDefault();
          first.focus();
          return;
        }
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    });
    document.addEventListener('input', (event) => {
      if (!m.active()) return;
      if (event.target.matches('[data-svc-draft]')) {
        m.state.draft = event.target.value;
        m.state.replyKey = '';
      }
    });
    document.addEventListener('submit', async (event) => {
      if (!m.active() || m.state.busy) return;
      const form = event.target;
      if (!form.matches('[data-svc-filter],[data-svc-reply]')) return;
      event.preventDefault();
      const f = new FormData(form),
        s = m.state;
      if (form.matches('[data-svc-filter]')) {
        for (const k of ['keyword', 'status', 'type', 'from', 'to']) s[k] = String(f.get(k) || '').trim();
        s.page = 1;
        await m.refresh();
        return;
      }
      const body = String(f.get('body') || s.draft).trim();
      if (!body) {
        s.error = '请填写回复内容';
        m.draw();
        return;
      }
      s.draft = body;
      s.replyKey = s.replyKey || crypto.randomUUID();
      if (
        await m.run(
          () =>
            m.adapter.call(service, C.Action.Reply, { id: s.selected, idempotency_key: s.replyKey, data: { body } }),
          () => {},
        )
      ) {
        s.draft = '';
        s.replyKey = '';
        const count = Number(s.conversation?.message_total) || 0;
        await open(s.selected, Math.floor(count / 50) * 50);
        await m.refresh();
        const messages = document.querySelector('.svc-conversation-messages');
        if (messages) messages.scrollTop = messages.scrollHeight;
        document.querySelector('[data-svc-draft]')?.focus();
        m.adapter.notify?.('回复已发送');
      }
    });
    return m;
  }
  function conversation(m) {
    const s = m.state,
      r = s.conversation;
    if (!s.selected) return '';
    if (!r)
      return `<section class="admin-section">${s.busy ? window.LumoSkeleton.render('rows', { count: 3 }) : '<p>会话加载失败，可重新打开工单重试。</p>'}</section>`;
    const messages = [
      ...(s.messageOffset === 0
        ? [{ id: r.id, role: C.Role.User, body: r.body, created_at: r.created_at, initial: true }]
        : []),
      ...(r.messages || []),
    ];
    return `<button type="button" class="svc-conversation-backdrop" tabindex="-1" data-svc-admin="close-detail" aria-label="关闭工单会话"></button><section class="admin-section svc-conversation" role="dialog" aria-modal="true" aria-labelledby="svc-conversation-title"><div class="svc-conversation-heading"><div><h2 id="svc-conversation-title">${esc(r.title || '工单会话')}</h2><p>${esc(ticketLabels[r.status])} · 顾客 ${esc(r.user_id)}</p><small>工单 ${esc(r.id)}${r.order_id ? ' · 订单 ' + esc(r.order_id) : ''}</small></div><button class="outline" data-svc-admin="close-detail">收起会话</button></div><ol class="svc-conversation-messages" aria-label="工单对话">${messages.map((msg) => `<li class="${msg.role === C.Role.Admin ? 'is-staff' : 'is-customer'}"><div><strong>${msg.role === C.Role.Admin ? '商城客服' : '顾客'}${msg.initial ? ' · 发起问题' : ''}</strong><time>${U.date(msg.created_at)}</time></div><p>${esc(msg.body)}</p></li>`).join('')}</ol><div class="admin-pagination"><span>回复 ${Math.min(s.messageOffset + 1, Number(r.message_total) || 0)}–${Math.min(s.messageOffset + 50, Number(r.message_total) || 0)} / ${Number(r.message_total) || 0}</span><button class="outline" data-svc-admin="messages" data-offset="${s.messageOffset - 50}" ${s.messageOffset <= 0 || s.busy ? 'disabled' : ''}>较早消息</button><button class="outline" data-svc-admin="messages" data-offset="${s.messageOffset + 50}" ${s.messageOffset + 50 >= Number(r.message_total) || s.busy ? 'disabled' : ''}>较新消息</button></div>${r.status === C.HelpdeskStatus.Closed ? '<p class="svc-conversation-closed">工单已关闭，会话记录保留。</p>' : `<form data-svc-reply><label class="field">客服回复<textarea data-svc-draft name="body" maxlength="10000" rows="4" required placeholder="输入给顾客的回复…">${esc(s.draft)}</textarea></label><div class="admin-form-footer"><button type="button" class="outline" data-svc-admin="close-ticket" ${s.busy ? 'disabled' : ''}>关闭工单</button><button class="primary" ${s.busy ? 'disabled' : ''}>发送回复</button></div></form>${s.confirmClose ? '<div class="svc-close-confirm"><p>关闭后将停止新增回复，已有记录保留。</p><button class="primary" data-svc-admin="confirm-close">确认关闭</button><button class="outline" data-svc-admin="cancel-close">继续跟进</button></div>' : ''}`}</section>`;
  }
  window.LumoServiceAdmin = { create };
})();
