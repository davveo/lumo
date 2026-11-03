(function () {
  'use strict';
  const C = LumoConstants,
    U = LumoAdminUI,
    { esc, money } = U,
    labels = { product: '商品', query: '搜索词', source: '来源', customer: '客群' },
    stage = { view: '浏览', add_cart: '加购', checkout: '结算', paid: '支付' },
    names = {
      unknown: '未记录',
      new: '新付费顾客',
      returning: '老付费顾客',
      anonymous: '游客',
      unpaid: '尚未支付',
      direct: '直接访问',
      search: '搜索',
      category: '分类',
      recommendation: '推荐',
      campaign: '活动',
      external: '外部来源',
    };
  const m = U.controller(
    'diagnostics-admin-root',
    () => ({ days: 7, dimension: 'product', keyword: '', page: 1, data: null, busy: false, error: '' }),
    async (module) => ({
      data: await module.adapter.call(C.Service.Analytics, C.Action.Drilldown, {
        data: {
          days: module.state.days,
          dimension: module.state.dimension,
          keyword: module.state.keyword,
          page: module.state.page,
        },
      }),
    }),
    (module) => view(module.state),
  );
  function view(s) {
    const d = s.data;
    return (
      U.heading(
        '经营明细',
        '按商品、搜索词、来源和客群查看实际行为与交易',
        '<button class="outline" data-diag="refresh">刷新</button>',
      ) +
      `<section class="admin-section"><form id="diagnostics-filter" class="admin-filter-form"><label class="field">统计周期<select name="days">${[7, 30, 90].map((n) => `<option value="${n}" ${n === s.days ? 'selected' : ''}>近 ${n} 天</option>`).join('')}</select></label><label class="field">分析维度<select name="dimension">${Object.entries(
        labels,
      )
        .map(([key, name]) => `<option value="${key}" ${key === s.dimension ? 'selected' : ''}>${name}</option>`)
        .join(
          '',
        )}</select></label><label class="field">筛选明细<input name="keyword" type="search" maxlength="100" value="${esc(s.keyword)}" placeholder="商品、SKU、搜索词或来源"></label><button class="primary" ${s.busy ? 'disabled' : ''}>查询</button><button class="outline" type="button" data-diag="reset">重置</button></form></section>` +
      (!d
        ? '<section class="admin-section">读取统计中…</section>'
        : `<div class="db-metrics">${[
            ['浏览器会话', Number(d.anonymous_sessions) + Number(d.logged_sessions)],
            ['搜索次数', d.searches],
            ['无结果搜索', d.zero_results],
            ['已付款订单', d.paid_orders],
            ['缺少会话归因', d.missing_attribution_orders],
            ['观察满24小时未支付的加购会话', d.abandoned_cart_sessions],
          ]
            .map(
              ([name, n]) => `<div class="db-metric"><span>${esc(name)}</span><strong>${Number(n || 0)}</strong></div>`,
            )
            .join(
              '',
            )}</div><section class="admin-section"><h2>依次转化的会话</h2><div class="db-tasks">${(d.ordered_funnel || []).map((r) => `<div><span>${stage[r.stage]}</span><strong>${Number(r.sessions)} 个会话</strong></div>`).join('')}</div><p class="admin-muted">同一会话、同一身份依次发生各阶段。游客 ${Number(d.anonymous_sessions)} 个、登录 ${Number(d.logged_sessions)} 个会话；行为起始于 ${esc(d.coverage_since ? U.date(d.coverage_since) : '尚未采集')}。另有 ${Number(d.immature_cart_sessions)} 个加购会话未满24小时。</p><h3>优惠金额（已记录 ${Number(d.cost_known_orders)} / ${Number(d.paid_orders)} 笔付款单）</h3><div class="db-metrics">${[
            ['活动优惠', d.campaign_cost],
            ['优惠券', d.coupon_cost],
            ['积分抵扣', d.points_cost],
          ]
            .map(([n, c]) => `<div class="db-metric"><span>${n}</span><strong>${money(c)}</strong></div>`)
            .join(
              '',
            )}</div></section><section class="admin-section"><h2>${labels[s.dimension]}明细</h2><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>${labels[s.dimension]}</th><th>浏览 / 加购</th><th>搜索 / 无结果</th><th>付款订单 / 实付</th><th>退款</th><th>活动 / 券 / 积分优惠</th></tr></thead><tbody>${(d.items || []).map((r) => `<tr><td><strong>${esc(r.name && r.name !== r.key ? r.name : names[r.key] || r.key)}</strong>${r.name !== r.key ? `<small>${esc(r.key)}</small>` : ''}${s.dimension === 'product' && r.key !== 'unknown' ? `<button class="outline" data-diag-sku="${esc(r.key)}">查看商品</button>` : ''}${s.dimension === 'query' && r.key !== 'unknown' ? `<a class="admin-text-link" href="index.html#/search?q=${encodeURIComponent(r.key)}" target="_blank" rel="noopener">复现搜索 ↗</a>` : ''}</td><td>${Number(r.views)} / ${Number(r.carts)}</td><td>${Number(r.searches)} / ${Number(r.zero_results)}</td><td>${Number(r.paid_orders)} 笔<small>${money(r.paid_cents)}</small></td><td>${money(r.refund_cents)}</td><td>${money(r.campaign_cost)} / ${money(r.coupon_cost)} / ${money(r.points_cost)}<small>已记录 ${Number(r.cost_known_orders)} 笔</small></td></tr>`).join('') || '<tr><td colspan="6">暂无符合条件的明细</td></tr>'}</tbody></table></div>${U.pager(d.total, s.page, 20, 'diag', s.busy)}<p class="admin-muted">${esc(d.scope)}</p></section>`)
    );
  }
  document.addEventListener('submit', async (e) => {
    if (e.target.id !== 'diagnostics-filter' || !m.active() || m.state.busy) return;
    e.preventDefault();
    const f = new FormData(e.target);
    Object.assign(m.state, {
      days: Number(f.get('days')),
      dimension: String(f.get('dimension')),
      keyword: String(f.get('keyword') || ''),
      page: 1,
    });
    await m.refresh();
  });
  document.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-diag],[data-diag-page],[data-diag-sku]');
    if (!b || !m.active() || m.state.busy) return;
    if (b.dataset.diagSku) {
      m.adapter.navigate?.('inventory', { skuId: b.dataset.diagSku });
      return;
    }
    if (b.dataset.diagPage) m.state.page = Number(b.dataset.diagPage);
    if (b.dataset.diag === 'reset') Object.assign(m.state, { keyword: '', page: 1 });
    await m.refresh();
  });
  m.open = async (params) => {
    Object.assign(m.state, {
      days: params?.days || 7,
      dimension: params?.dimension || 'product',
      keyword: params?.keyword || '',
      page: 1,
    });
    await m.refresh();
  };
  window.LumoDiagnosticsAdmin = m;
})();
