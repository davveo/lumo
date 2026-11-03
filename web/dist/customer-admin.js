(function () {
  'use strict';
  const U = LumoAdminUI,
    { esc, money, date } = U;
  function profile(data, busy = false) {
    const s = data.sections || {},
      card = s.member || {},
      p = card.profile || {},
      stats = s.stats?.items?.[0],
      account = data.account;
    const cards = [
      ['已采集实付', stats ? money(stats.paid_cents) : s.stats ? money(0) : '—'],
      ['已采集退款', stats ? money(stats.refund_cents) : s.stats ? money(0) : '—'],
      ['净实付', stats ? money(stats.net_cents) : s.stats ? money(0) : '—'],
      ['付款订单', stats?.paid_orders ?? (s.stats ? 0 : '—')],
    ];
    const history = [
      ...(s.aftersales?.items || []).map((r) => ({ ...r, kind: '售后', text: r.reason, tab: 'aftersales' })),
      ...(s.refunds?.items || []).map((r) => ({ ...r, kind: '退款', text: money(r.amount_cents), tab: 'refunds' })),
      ...(s.tickets?.items || []).map((r) => ({ ...r, kind: '客服', text: r.title || r.body, tab: 'tickets' })),
    ]
      .sort((a, b) => Date.parse(b.updated_at || b.created_at) - Date.parse(a.updated_at || a.created_at))
      .slice(0, 20);
    const link = (tab, id, label) =>
      `<button class="admin-text-button" data-customer-tab="${tab}" data-id="${esc(id)}">${esc(label)}</button>`;
    return `<button class="outline" data-ua="back">← 返回用户列表</button><section class="admin-section customer-identity"><div><span class="admin-avatar">${esc((p.display_name || account.username || '客').slice(0, 1))}</span><div><h2>${esc(p.display_name || account.username)}</h2><p>${esc(account.username)} · ${esc(account.id)}</p></div></div><span class="admin-state ${account.active ? 'success' : 'neutral'}">${account.active ? '账号正常' : '已停用'}</span></section>${Object.keys(data.errors || {}).length ? '<p class="admin-error" role="status">部分档案信息暂时不可用，可点击刷新重试；其余信息仍可查看。</p>' : ''}<div class="customer-stats">${cards.map(([name, value]) => `<div><span>${name}</span><strong>${esc(value)}</strong></div>`).join('')}</div>${s.stats ? `<p class="admin-muted customer-scope">${esc(s.stats.scope)} 更新于 ${date(s.stats.generated_at)}</p>` : ''}<div class="customer-grid"><section class="admin-section"><div class="admin-panel-heading"><h2>客户标签与备注</h2><span>内部服务信息</span></div>${s.member ? `<div class="customer-tags">${(card.tags || []).map((tag) => `<span>${esc(tag)}</span>`).join('') || '<small>暂未设置标签</small>'}</div>${data.can_edit ? `<form id="customer-card-form" class="admin-form-stack" data-customer="${esc(account.id)}" data-version="${Number(card.version) || 0}"><label class="field">标签<input name="tags" maxlength="400" value="${esc((card.tags || []).join('，'))}" placeholder="用逗号分隔，如：老客、关注咖啡"><small>最多12个，每个1至24字。</small></label><label class="field">服务备注<textarea name="note" rows="4" maxlength="1000" placeholder="记录需要其他同事接续的服务信息">${esc(card.note || '')}</textarea></label><div class="admin-form-actions"><button class="primary" ${busy ? 'disabled' : ''}>保存标签和备注</button><small>修改人 ${esc(card.updated_by || '—')}</small></div></form>` : `<p>${esc(card.note || '暂无备注')}</p>`}` : '<p class="admin-muted">当前岗位未开放客户标签信息。</p>'}</section><section class="admin-section"><h2>会员与资产</h2><dl class="op-definition">${U.pair('等级', p.level === 'gold' ? '黄金会员' : p.level ? '普通会员' : '—')}${U.pair('成长值', p.growth ?? '—')}${U.pair('可用积分', s.points?.available ?? '—')}${U.pair('冻结积分', s.points?.frozen ?? '—')}${U.pair('可用优惠券', s.coupons?.available ?? '—')}${U.pair('已用优惠券', s.coupons?.used ?? '—')}${U.pair('最近付款', stats?.last_paid_at ? date(stats.last_paid_at) : '—')}</dl></section></div>${s.orders ? `<section class="admin-section"><div class="admin-panel-heading"><h2>最近订单</h2><span>共 ${Number(s.orders.total) || 0} 笔 · 最近10笔</span></div><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>订单</th><th>状态</th><th>订单应付</th><th>创建时间</th></tr></thead><tbody>${(s.orders.items || []).map((r) => `<tr><td>${link('orders', r.id, r.id)}</td><td>${esc(r.status)}</td><td>${money(r.payable_cents ?? r.payable)}</td><td>${date(r.created_at)}</td></tr>`).join('') || '<tr><td colspan="4">暂无订单</td></tr>'}</tbody></table></div></section>` : ''}<section class="admin-section"><div class="admin-panel-heading"><h2>售后与客服时间线</h2><span>已授权来源的最近记录</span></div><ol class="customer-timeline">${history.map((r) => `<li><span>${r.kind}</span><div>${link(r.tab, r.id, r.text || r.id)}<small>${esc(r.status)} · ${date(r.updated_at || r.created_at)}</small></div></li>`).join('') || '<li class="admin-muted">暂无可显示的服务记录</li>'}</ol></section>`;
  }
  const m = U.controller(
    'customer-segments-root',
    () => ({
      items: [],
      total: 0,
      page: 1,
      preview: null,
      previewPage: 1,
      draft: { name: '', min_orders: 1, min_net: 0, inactive_days: 0, version: 0 },
      busy: false,
      error: '',
    }),
    async (module) => {
      const data = await module.adapter.call('member', 'segment_list', {
        data: { page: module.state.page, page_size: 20 },
      });
      return { items: data.items || [], total: Number(data.total) || 0 };
    },
    (module) => {
      const s = module.state,
        d = s.draft;
      const field = (name, label, value) =>
        `<label class="field">${label}<input name="${name}" data-segment-field="${name}" type="${name === 'name' ? 'text' : 'number'}" min="${name === 'min_orders' ? 1 : 0}" step="${name === 'min_net' ? '0.01' : 1}" maxlength="80" value="${esc(value)}" ${s.busy ? 'disabled' : ''}></label>`;
      return (
        U.heading('客户分群', '基于已采集交易记录，预览和保存客户筛选规则。') +
        `<div class="admin-inline-note">仅覆盖已有付款事实的顾客；净实付为付款减退款，历史交易未同步时不代表完整历史。</div><div class="customer-grid"><section class="admin-section"><h2>分群规则</h2><form id="customer-segment-form" class="admin-form-stack">${field('name', '分群名称', d.name)}${field('min_orders', '最少付款订单数', d.min_orders)}${field('min_net', '最低净实付（元）', d.min_net)}${field('inactive_days', '至少多少天未再购买（0不限）', d.inactive_days)}<div class="admin-form-actions"><button name="intent" value="preview" class="primary" ${s.busy ? 'disabled' : ''}>预览人群</button><button name="intent" value="save" class="outline" ${s.busy ? 'disabled' : ''}>保存规则</button><button type="button" class="outline" data-segment-new>新建</button></div></form></section><section class="admin-section"><h2>已保存的分群</h2><div class="customer-segment-list">${s.items.map((row) => `<button class="outline" data-segment-id="${esc(row.id)}"><strong>${esc(row.name)}</strong><small>购买 ≥ ${row.min_orders} 笔 · 净实付 ≥ ${money(row.min_net_cents)}</small></button>`).join('') || '<p class="admin-muted">暂无已保存的规则</p>'}</div>${U.pager(s.total, s.page, 20, 'segment', s.busy)}</section></div>${s.preview ? `<section class="admin-section"><div class="admin-panel-heading"><h2>符合规则的顾客</h2><span>共 ${Number(s.preview.total) || 0} 人 · ${date(s.preview.generated_at)}</span></div><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>会员编号</th><th>付款订单数</th><th>净实付</th><th>最近付款</th></tr></thead><tbody>${(s.preview.items || []).map((row) => `<tr><td><button class="admin-text-button" data-customer-tab="users" data-id="${esc(row.user_id)}">${esc(row.user_id)}</button></td><td>${row.paid_orders}</td><td>${money(row.net_cents)}</td><td>${date(row.last_paid_at)}</td></tr>`).join('') || '<tr><td colspan="4">暂无符合条件的顾客</td></tr>'}</tbody></table></div>${U.pager(s.preview.total, s.previewPage, 20, 'segment-preview', s.busy)}</section>` : ''}`
      );
    },
  );
  const rules = () => {
    const d = m.state.draft;
    return {
      name: d.name,
      min_orders: Number(d.min_orders),
      min_net_cents: Math.round(Number(d.min_net) * 100),
      inactive_days: Number(d.inactive_days),
      version: Number(d.version) || 0,
    };
  };
  document.addEventListener('input', (event) => {
    if (event.target.dataset.segmentField && m.active())
      m.state.draft[event.target.dataset.segmentField] = event.target.value;
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'customer-segment-form' || !m.active()) return;
    event.preventDefault();
    const save = event.submitter?.value === 'save',
      data = rules();
    if (save) {
      const ok = await m.run(async () => {
        const row = await m.adapter.call('member', 'segment_upsert', { id: m.state.draft.id || '', data });
        return { draft: { ...row, min_net: Number(row.min_net_cents) / 100 } };
      });
      if (ok) await m.refresh();
    } else
      await m.run(async () => ({
        preview: await m.adapter.call('member', 'segment_preview', { data: { ...data, page: 1 } }),
        previewPage: 1,
        previewRules: data,
      }));
  });
  document.addEventListener('click', async (event) => {
    const b = event.target.closest(
      '[data-segment-id],[data-segment-page],[data-segment-preview-page],[data-segment-new],[data-customer-tab]',
    );
    if (!b) return;
    if (b.dataset.customerTab && m.active()) {
      await m.adapter.navigate?.(b.dataset.customerTab, { recordId: b.dataset.id });
      return;
    }
    if (!m.active() || m.state.busy) return;
    if (b.dataset.segmentNew != null) {
      m.state.draft = { name: '', min_orders: 1, min_net: 0, inactive_days: 0, version: 0 };
      m.draw();
    }
    if (b.dataset.segmentId) {
      const row = m.state.items.find((r) => r.id === b.dataset.segmentId);
      if (row) {
        m.state.draft = { ...row, min_net: Number(row.min_net_cents) / 100 };
        m.draw();
      }
    }
    if (b.dataset.segmentPage) {
      m.state.page = Number(b.dataset.segmentPage);
      await m.refresh();
    }
    if (b.dataset.segmentPreviewPage) {
      const page = Number(b.dataset.segmentPreviewPage);
      await m.run(async () => ({
        preview: await m.adapter.call('member', 'segment_preview', { data: { ...m.state.previewRules, page } }),
        previewPage: page,
      }));
    }
  });
  window.LumoCustomerProfile = { render: profile };
  window.LumoCustomerSegments = m;
})();
