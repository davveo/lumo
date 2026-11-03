(function () {
  'use strict';
  const C = LumoConstants,
    U = LumoAdminUI,
    { esc, money, date } = U,
    size = 20;
  const labels = {
    fulfillment: '仓库履约',
    aftersale: '售后处理',
    payment: '支付跟进',
    refund: '退款跟进',
    helpdesk: '客服工单',
  };
  const menus = {
    fulfillment: 'fulfillment',
    aftersale: 'aftersales',
    payment: 'payments',
    refund: 'refunds',
    helpdesk: 'tickets',
  };
  const states = {
    PENDING: '待确认',
    PROCESSING: '处理中',
    READY: '待出库',
    DISPATCHING: '出库中',
    REQUESTED: '待审核',
    APPROVED: '已批准',
    INSPECTED: '已验收',
    REFUND_REQUESTED: '待退款',
    REFUNDING: '退款中',
    OPEN: '待回复',
  };
  const defaults = () => ({ keyword: '', service: '', assignee: 'all', overdue_only: false });
  const m = U.controller(
    'workqueue-admin-root',
    () => ({
      data: null,
      filters: defaults(),
      window: 1,
      page: 1,
      selected: new Set(),
      detail: null,
      saveView: false,
      viewName: '',
      assignTo: null,
      reason: '分派业务跟进',
      results: [],
      staff: [],
      staffReady: false,
      policyEditor: false,
      busy: false,
      error: '',
    }),
    async (module) => {
      const s = module.state,
        data = await module.adapter.call(C.Service.Helpdesk, C.Action.WorkQueue, {
          data: { ...s.filters, window: s.window },
        });
      let staff = s.staff;
      if (data.can_assign_others && !s.staffReady) {
        const directory = await module.adapter.call(C.Service.IAM, C.Action.StaffList, {
          data: { page: 1, page_size: 100 },
        });
        staff = (directory.items || []).filter((r) => r.active);
      }
      const valid = new Set((data.items || []).map((r) => r.work_id));
      return {
        data,
        staff,
        staffReady: true,
        assignTo: s.assignTo ?? data.actor_id,
        page: Math.min(s.page, Math.max(1, Math.ceil((data.items || []).length / size))),
        selected: new Set([...s.selected].filter((id) => valid.has(id))),
      };
    },
    (module) => view(module),
  );
  function head(s) {
    return U.heading(
      '待办工作台',
      '按跟进目标处理待办，查看关联业务并记录负责人。',
      `<button class="outline" data-wq="reload" ${s.busy ? 'disabled' : ''}>刷新</button>${s.data?.can_assign_others ? '<button class="outline" data-wq="policy">内部跟进目标</button>' : ''}`,
    );
  }
  function policyEditor(s) {
    const p = s.data.policy || {};
    return `<form id="workqueue-policy" class="admin-section"><div class="admin-panel-heading"><h2>内部跟进目标</h2><button type="button" class="outline" data-wq="close-policy">返回待办</button></div><p class="admin-muted">用于团队待办排序；订单中已记录的发货日期和审核目标优先使用。</p><div class="admin-form">${[
      ['payment_hours', '支付跟进'],
      ['refund_hours', '退款跟进'],
      ['shipping_hours', '仓库履约'],
      ['aftersale_hours', '售后审核'],
      ['support_hours', '客服回复'],
    ]
      .map(
        ([key, name]) =>
          `<label class="field">${name}（小时，0 为不设目标）<input name="${key}" type="number" min="0" max="720" step="1" required value="${Number(p[key] || 0)}"></label>`,
      )
      .join(
        '',
      )}<label class="field wide">修改原因<input name="reason" maxlength="240" required></label><div class="admin-form-footer"><button class="primary" ${s.busy ? 'disabled' : ''}>保存跟进目标</button></div></div></form>`;
  }
  function detailView(s) {
    const d = s.detail,
      subject = d.subject || {},
      address = d.order?.address || {};
    const related = (key, title) =>
      d[key] ? `<section class="admin-section"><h2>${title}</h2>${LumoAdminDetails.render(d[key])}</section>` : '';
    return `${head(s)}<div class="admin-actions wq-detail-actions"><button class="outline" data-wq="close-detail">← 返回待办</button><button class="primary" data-wq="handle" data-service="${esc(s.detailService)}" data-id="${esc(subject.id)}">进入${esc(labels[s.detailService])}</button></div>${Object.keys(d.errors || {}).length ? '<p class="admin-inline-note">部分关联业务暂不可读取，可刷新后重试。</p>' : ''}<div class="op-detail-grid"><section class="admin-section"><h2>当前待办</h2><dl class="op-definition">${U.pair('业务类型', labels[s.detailService])}${U.pair('编号', subject.id)}${U.pair('状态', states[subject.status] || subject.status)}${U.pair('负责人', d.assignment?.assignee_name || '未分派')}${U.pair('创建时间', date(subject.created_at))}${U.pair('关联订单', subject.order_id || '—')}</dl></section>${d.order ? `<section class="admin-section"><h2>订单与收货信息</h2><dl class="op-definition">${U.pair('订单状态', d.order.status)}${d.order.payable_cents != null ? U.pair('订单应付', money(d.order.payable_cents)) : ''}${U.pair('收货人', address.recipient || address.name || '—')}${U.pair('联系电话', address.phone || '—')}${U.pair('地址', [address.province, address.city, address.district, address.region, address.address].filter(Boolean).join(' '))}</dl></section>` : ''}</div><div class="wq-context-grid">${related('payment', '支付记录')}${related('fulfillment', '仓库履约')}${related('logistics', '物流进展')}${related('aftersales', '售后申请')}${related('tickets', '客服记录')}</div><section class="admin-section"><h2>负责人变更记录</h2>${(d.assignment_history || []).length ? `<ol class="mg-notes">${d.assignment_history.map((h) => `<li><strong>${esc(h.actor_id)} → ${esc(h.after || '解除分派')}</strong><p>${esc(h.reason)}</p><small>${date(h.created_at)}</small></li>`).join('')}</ol>` : U.empty('尚未分派此待办', '可返回列表领取或分派负责人。')}</section>`;
  }
  function view(module) {
    const s = module.state;
    if (!s.data) return head(s) + LumoSkeleton.render('table', { count: 6 });
    if (s.detail) return detailView(s);
    if (s.policyEditor) return head(s) + policyEditor(s);
    const d = s.data,
      rows = d.items || [],
      pageRows = rows.slice((s.page - 1) * size, s.page * size),
      all = pageRows.length && pageRows.every((r) => s.selected.has(r.work_id));
    return `${head(s)}<section class="admin-section wq-saved"><label class="admin-status-filter">已保存视图<select data-wq-view><option value="">选择工作视图</option>${(d.views || []).map((v) => `<option value="${esc(v.id)}" ${v.name === s.viewName ? 'selected' : ''}>${esc(v.name)}</option>`).join('')}</select></label><button class="outline" data-wq="save-view">保存当前筛选</button>${s.viewName ? '<button class="outline" data-wq="delete-view">删除此视图</button>' : ''}${s.saveView ? `<form id="workqueue-save-view" class="admin-query-form"><label class="field">视图名称<input name="name" value="${esc(s.viewName)}" required maxlength="60" placeholder="如：我负责的超时售后"></label><button class="primary" ${s.busy ? 'disabled' : ''}>保存视图</button></form>` : ''}</section><section class="admin-list-panel"><form id="workqueue-filter" class="op-filters"><label class="field">搜索待办<input name="keyword" type="search" value="${esc(s.filters.keyword)}" maxlength="120" placeholder="业务编号、订单或关键词"></label><label class="field">业务队列<select name="service"><option value="">全部可处理队列</option>${(d.allowed_services || []).map((v) => `<option value="${v}" ${s.filters.service === v ? 'selected' : ''}>${labels[v]}</option>`).join('')}</select></label><label class="field">负责人<select name="assignee">${[
      ['all', '全部待办'],
      ['mine', '我负责的'],
      ['unassigned', '未分派'],
    ]
      .map(([v, n]) => `<option value="${v}" ${s.filters.assignee === v ? 'selected' : ''}>${n}</option>`)
      .join(
        '',
      )}</select></label><label class="wq-check"><input type="checkbox" name="overdue_only" ${s.filters.overdue_only ? 'checked' : ''}>仅看超出目标</label><button class="primary" ${s.busy ? 'disabled' : ''}>查询</button><button class="outline" type="button" data-wq="reset">重置</button></form><p class="admin-inline-note">${esc(d.notice)}${Object.keys(d.errors || {}).length ? ' 部分队列暂不可读取。' : ''}</p><form id="workqueue-assign" class="wq-batch"><span>已选 ${s.selected.size} 项 · 每次最多50项</span>${d.can_assign_others ? `<label class="admin-status-filter">分派给<select name="assignee_id" data-wq-target><option value="${esc(d.actor_id)}" ${s.assignTo === d.actor_id ? 'selected' : ''}>我（${esc(d.actor_name || d.actor_id)}）</option>${s.staff.map((u) => `<option value="${esc(u.id)}" ${s.assignTo === u.id ? 'selected' : ''}>${esc(u.username)} · ${esc(u.role)}</option>`).join('')}<option value="" ${s.assignTo === '' ? 'selected' : ''}>解除负责人</option></select></label>` : `<label class="admin-status-filter">操作<select name="assignee_id"><option value="${esc(d.actor_id)}">领取为我负责</option><option value="">解除我的负责</option></select></label>`}<label class="field">操作原因<input name="reason" required maxlength="240" value="${esc(s.reason)}"></label><button class="primary" ${s.busy || !s.selected.size ? 'disabled' : ''}>更新所选负责人</button></form>${rows.length ? `<div class="admin-table-wrap"><table class="admin-table wq-table"><thead><tr><th><input type="checkbox" data-wq-all aria-label="选择本页待办" ${all ? 'checked' : ''}></th><th>业务 / 订单</th><th>当前状态</th><th>跟进目标</th><th>负责人</th><th>操作</th></tr></thead><tbody>${pageRows.map((r) => `<tr><td><input type="checkbox" data-wq-select="${esc(r.work_id)}" aria-label="选择 ${esc(r.resource_id)}" ${s.selected.has(r.work_id) ? 'checked' : ''}></td><td><strong>${labels[r.service]}</strong><small>${esc(r.title || r.reason || r.resource_id)}</small><small>${esc(r.resource_id)}</small><small>订单 ${esc(r.order_id || '—')}</small></td><td>${esc(states[r.status] || r.status)}<small>已等待 ${r.waiting_hours == null ? '未知' : Number(r.waiting_hours).toFixed(1) + ' 小时'}</small></td><td><span class="admin-state ${r.overdue ? 'danger' : 'neutral'}">${r.overdue ? '已超出目标' : r.due_at ? '目标内' : '未设目标'}</span><small>${r.due_at ? date(r.due_at) : '按等待时间排列'}</small><small>${esc(r.target_label)}</small></td><td>${esc(r.assignment?.assignee_name || r.assignment?.assignee_id || '未分派')}</td><td><button class="outline" data-wq="detail" data-service="${r.service}" data-id="${esc(r.resource_id)}">关联详情</button></td></tr>`).join('')}</tbody></table></div>${U.pager(rows.length, s.page, size, 'wq', s.busy)}` : U.empty('当前批次暂无符合筛选的待办', d.has_more_windows ? '还有更多记录，可继续读取下一批。' : '可调整筛选或稍后刷新。')}<div class="admin-pagination"><span>业务队列第 ${s.window} 批 · 当前匹配 ${rows.length} 项</span><div><button class="outline" data-wq-window="${s.window - 1}" ${s.busy || s.window <= 1 ? 'disabled' : ''}>上一批</button><button class="outline" data-wq-window="${s.window + 1}" ${s.busy || !d.has_more_windows ? 'disabled' : ''}>下一批</button></div></div></section>${s.results.length ? `<section class="admin-section"><h2>分派结果</h2><ul class="wq-results">${s.results.map((r) => `<li class="${r.ok ? 'is-success' : 'is-error'}"><strong>${r.ok ? '已更新' : '未更新'}</strong><span>${labels[r.service]} · ${esc(r.id)}</span>${r.error ? `<p>${esc(r.error)}</p>` : ''}</li>`).join('')}</ul></section>` : ''}`;
  }
  document.addEventListener('change', async (e) => {
    if (!m.active() || m.state.busy) return;
    const s = m.state;
    if (e.target.matches('[data-wq-select]')) {
      if (e.target.checked && s.selected.size >= 50) {
        m.adapter.notify?.('每次最多分派50项');
        e.target.checked = false;
        return;
      }
      e.target.checked ? s.selected.add(e.target.dataset.wqSelect) : s.selected.delete(e.target.dataset.wqSelect);
      m.draw();
    }
    if (e.target.matches('[data-wq-all]')) {
      for (const r of (s.data.items || []).slice((s.page - 1) * size, s.page * size))
        e.target.checked ? s.selected.size < 50 && s.selected.add(r.work_id) : s.selected.delete(r.work_id);
      m.draw();
    }
    if (e.target.matches('[data-wq-view]')) {
      const v = s.data.views.find((v) => v.id === e.target.value);
      if (v) {
        s.filters = U.parse(v.filters_json) || defaults();
        s.viewName = v.name;
        s.window = 1;
        s.page = 1;
        s.selected.clear();
        await m.refresh();
      }
    }
    if (e.target.matches('[data-wq-target]')) s.assignTo = e.target.value;
  });
  document.addEventListener('click', async (e) => {
    if (!m.active() || m.state.busy) return;
    const b = e.target.closest('[data-wq],[data-wq-page],[data-wq-window]');
    if (!b || b.disabled) return;
    const s = m.state,
      a = b.dataset.wq;
    if (b.dataset.wqPage) {
      s.page = Number(b.dataset.wqPage);
      m.draw();
      return;
    }
    if (b.dataset.wqWindow) {
      s.window = Number(b.dataset.wqWindow);
      s.page = 1;
      s.selected.clear();
      await m.refresh();
      return;
    }
    if (a === 'reload') {
      await m.refresh();
      return;
    }
    if (a === 'reset') {
      s.filters = defaults();
      s.window = s.page = 1;
      s.viewName = '';
      s.selected.clear();
      await m.refresh();
      return;
    }
    if (a === 'save-view') {
      s.saveView = !s.saveView;
      m.draw();
      return;
    }
    if (a === 'delete-view') {
      const v = s.data.views.find((v) => v.name === s.viewName);
      if (v) {
        const ok = await m.run(() =>
          m.adapter.call(C.Service.Helpdesk, C.Action.SaveView, {
            data: { name: v.name, version: v.version, delete: true, filters: s.filters },
          }),
        );
        if (ok) {
          s.viewName = '';
          await m.refresh();
        }
      }
      return;
    }
    if (a === 'policy' || a === 'close-policy') {
      s.policyEditor = a === 'policy';
      m.draw();
      return;
    }
    if (a === 'close-detail') {
      s.detail = null;
      m.draw();
      return;
    }
    if (a === 'handle') {
      await m.adapter.navigate?.(menus[b.dataset.service], { recordId: b.dataset.id });
      return;
    }
    if (a === 'detail') {
      const ok = await m.run(
        () =>
          m.adapter.call(C.Service.Helpdesk, C.Action.WorkDetail, {
            id: b.dataset.id,
            data: { service: b.dataset.service },
          }),
        (r) => {
          s.detail = r;
          s.detailService = b.dataset.service;
        },
      );
      if (ok) window.scrollTo(0, 0);
    }
  });
  document.addEventListener('submit', async (e) => {
    if (
      !m.active() ||
      !['workqueue-filter', 'workqueue-save-view', 'workqueue-assign', 'workqueue-policy'].includes(e.target.id)
    )
      return;
    e.preventDefault();
    if (m.state.busy) return;
    const s = m.state,
      form = e.target,
      data = new FormData(form);
    if (form.id === 'workqueue-filter') {
      s.filters = {
        keyword: String(data.get('keyword') || '').trim(),
        service: String(data.get('service') || ''),
        assignee: String(data.get('assignee') || 'all'),
        overdue_only: data.get('overdue_only') === 'on',
      };
      s.window = s.page = 1;
      s.selected.clear();
      await m.refresh();
      return;
    }
    if (form.id === 'workqueue-save-view') {
      const name = String(data.get('name') || '').trim(),
        old = s.data.views.find((v) => v.name === name);
      const ok = await m.run(() =>
        m.adapter.call(C.Service.Helpdesk, C.Action.SaveView, {
          data: { name, filters: s.filters, version: old?.version || 0 },
        }),
      );
      if (ok) {
        s.viewName = name;
        s.saveView = false;
        await m.refresh();
      }
      return;
    }
    if (form.id === 'workqueue-policy') {
      const config = {};
      for (const key of ['payment_hours', 'refund_hours', 'shipping_hours', 'aftersale_hours', 'support_hours'])
        config[key] = Number(data.get(key));
      const ok = await m.run(() =>
        m.adapter.call(C.Service.Helpdesk, C.Action.PolicyUpsert, {
          data: { config, version: s.data.policy.version, reason: String(data.get('reason') || '') },
        }),
      );
      if (ok) {
        s.policyEditor = false;
        await m.refresh();
      }
      return;
    }
    const items = s.data.items
      .filter((r) => s.selected.has(r.work_id))
      .map((r) => ({ service: r.service, id: r.resource_id, expected_version: r.assignment?.version || 0 }));
    if (!items.length) return;
    s.assignTo = String(data.get('assignee_id') || '');
    s.reason = String(data.get('reason') || '').trim();
    const signature = JSON.stringify([items, s.assignTo, s.reason]);
    if (s.batchSignature !== signature) {
      s.batchSignature = signature;
      s.batchKey = crypto.randomUUID();
    }
    const ok = await m.run(
      () =>
        m.adapter.call(C.Service.Helpdesk, C.Action.AssignWork, {
          idempotency_key: s.batchKey,
          data: { items, assignee_id: s.assignTo, reason: s.reason },
        }),
      (r) => {
        s.results = r.items || [];
        s.selected = new Set(s.results.filter((v) => !v.ok).map((v) => v.service + ':' + v.id));
      },
    );
    if (ok) await m.refresh();
  });
  window.LumoWorkQueueAdmin = m;
})();
