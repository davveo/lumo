(function () {
  'use strict';
  const C = LumoConstants,
    U = LumoAdminUI,
    { esc, money } = U,
    labels = { product: '商品', query: '搜索词', source: '来源', customer: '客群' },
    stages = [
      ['view', '浏览商品', '发现感兴趣的商品'],
      ['add_cart', '加入购物车', '产生购买意向'],
      ['checkout', '进入结算', '确认订单信息'],
      ['paid', '完成支付', '完成本次购买'],
    ],
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
  const count = (value) => Math.max(0, Math.floor(Number(value) || 0));
  const number = (value) => count(value).toLocaleString('zh-CN');
  const percent = (value, base) => (base > 0 && value != null ? ((value / base) * 100).toFixed(1) + '%' : '—');
  function icon(name) {
    const paths = {
      sessions: '<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8m-4-4v4"/>',
      search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
      empty: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5M8 10.5h5"/>',
      paid: '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 10h18m-6 5h3"/>',
      attribution: '<circle cx="12" cy="12" r="9"/><path d="M12 8v5m0 3h.01"/>',
      clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
      campaign: '<path d="m3 11 14-6v14L3 13v-2Zm3 3 2 6h3l-2-5m11-6 2-1m-2 7 2 1"/>',
      coupon: '<path d="M3 5h18v5a2 2 0 0 0 0 4v5H3v-5a2 2 0 0 0 0-4V5Zm12 0v3m0 3v2m0 3v3"/>',
      points:
        '<circle cx="12" cy="12" r="9"/><path d="m12 7 1.5 3 3.5.5-2.5 2.5.5 3.5-3-1.5-3 1.5.5-3.5L7 10.5l3.5-.5L12 7Z"/>',
    };
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.sessions}</svg>`;
  }
  function overview(d) {
    return `<div class="diag-overview">${[
      ['访问会话', count(d.anonymous_sessions) + count(d.logged_sessions), 'sessions', '游客与登录会话合计'],
      ['搜索次数', d.searches, 'search', '顾客发起的商品搜索'],
      ['无结果搜索', d.zero_results, 'empty', '可关注搜索词与商品覆盖'],
      ['已付款订单', d.paid_orders, 'paid', '以实际付款订单为准'],
      ['待归因订单', d.missing_attribution_orders, 'attribution', '未关联到访问会话'],
      ['观察期后未支付', d.abandoned_cart_sessions, 'clock', '加购后已观察满 24 小时'],
    ]
      .map(
        ([label, value, symbol, note]) =>
          `<article class="diag-overview-card"><span class="diag-metric-icon">${icon(symbol)}</span><div><span>${label}</span><strong>${number(value)}</strong><small>${note}</small></div></article>`,
      )
      .join('')}</div>`;
  }
  function funnel(d, days) {
    const rows = stages.map(([key, label, note]) => {
      const raw = (d.ordered_funnel || []).find((r) => r.stage === key);
      return { label, note, value: raw?.sessions == null ? null : count(raw.sessions) };
    });
    const first = rows[0].value,
      paid = rows.at(-1).value;
    return `<section class="admin-section diag-conversion"><div class="diag-section-heading"><div><h2>会话转化漏斗</h2><p>看清从浏览到支付，每一步的转化表现</p></div><span class="diag-period">近 ${number(days)} 天</span></div><div class="diag-funnel-layout"><ol class="diag-funnel" aria-label="依次转化的会话">${rows
      .map((row, index) => {
        const previous = rows[index - 1]?.value,
          width = first > 0 && row.value != null ? Math.min(100, (row.value / first) * 100) : 0;
        return `<li class="diag-funnel-step"><span class="diag-step-number" aria-hidden="true">0${index + 1}</span><div class="diag-step-content"><div class="diag-step-heading"><span>${row.label}</span><strong>${row.value == null ? '—' : number(row.value)}<small>个会话</small></strong></div><div class="diag-funnel-track" aria-hidden="true"><i style="width:${width}%"></i></div><div class="diag-step-caption"><span>${row.note}</span>${index ? `<span>上一步转化 <b>${percent(row.value, previous)}</b></span>` : '<span>漏斗起点</span>'}</div></div></li>`;
      })
      .join(
        '',
      )}</ol><aside class="diag-funnel-summary"><span>浏览至支付转化率</span><strong>${percent(paid, first)}</strong><p>${first > 0 ? `每 ${number(first)} 个浏览会话中，${paid == null ? '支付阶段尚未返回' : number(paid) + ' 个完成支付'}` : first === 0 ? '有浏览会话后展示转化表现' : '暂未返回会话转化数据'}</p><div class="diag-session-split"><div><span>游客会话</span><b>${number(d.anonymous_sessions)}</b></div><div><span>登录会话</span><b>${number(d.logged_sessions)}</b></div></div><div class="diag-coverage"><span>行为采集起始时间</span><time>${esc(d.coverage_since ? U.date(d.coverage_since) : '尚未采集')}</time></div></aside></div><div class="diag-funnel-note">${icon('attribution')}<span>仅统计同一会话、同一身份依次完成的行为，登录前后分开统计。${count(d.immature_cart_sessions) ? `另有 <b>${number(d.immature_cart_sessions)}</b> 个加购会话仍在 24 小时观察期内。` : '弃购统计需观察满 24 小时。'}</span></div></section>`;
  }
  function discounts(d) {
    const known = count(d.cost_known_orders),
      paid = count(d.paid_orders),
      missing = Math.max(0, paid - known),
      costs = [
        ['活动优惠', d.campaign_cost, 'campaign'],
        ['优惠券', d.coupon_cost, 'coupon'],
        ['积分抵扣', d.points_cost, 'points'],
      ],
      total = costs.reduce((sum, [, value]) => sum + count(value), 0);
    return `<section class="admin-section diag-discounts"><div class="diag-section-heading"><div><h2>优惠投入</h2><p>已记录优惠合计 <strong>${known ? money(total) : '—'}</strong></p></div><span class="diag-record-badge ${missing ? 'is-incomplete' : ''}">已记录 ${number(known)} / ${number(paid)} 笔付款单</span></div><div class="diag-discount-grid">${costs
      .map(
        ([label, value, symbol]) =>
          `<article class="diag-discount-card"><div><span class="diag-metric-icon is-${symbol}">${icon(symbol)}</span><h3>${label}</h3></div><strong>${known ? money(value) : '—'}</strong><span>${known ? (total > 0 ? `占已记录优惠 ${percent(count(value), total)}` : '已记录订单暂无此项优惠') : paid ? '等待优惠明细' : '暂无付款订单'}</span></article>`,
      )
      .join(
        '',
      )}</div><p class="diag-cost-note">${missing ? `还有 ${number(missing)} 笔付款单未记录优惠，当前金额未覆盖全部订单。` : known ? '优惠金额仅统计已有记录的付款订单，未扣除退款。' : '付款订单的优惠记录会展示在这里。'}${missing ? ' 优惠金额未扣除退款。' : ''}</p></section>`;
  }
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
        : `${overview(d)}${funnel(d, s.days)}${discounts(d)}<section class="admin-section"><div class="diag-section-heading"><div><h2>${labels[s.dimension]}明细</h2><p>按当前维度查看行为与交易记录</p></div></div><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>${labels[s.dimension]}</th><th>浏览 / 加购</th><th>搜索 / 无结果</th><th>付款订单 / 实付</th><th>退款</th><th>活动 / 券 / 积分优惠</th></tr></thead><tbody>${(d.items || []).map((r) => `<tr><td><strong>${esc(r.name && r.name !== r.key ? r.name : names[r.key] || r.key)}</strong>${r.name !== r.key ? `<small>${esc(r.key)}</small>` : ''}${s.dimension === 'product' && r.key !== 'unknown' ? `<button class="outline" data-diag-sku="${esc(r.key)}">查看商品</button>` : ''}${s.dimension === 'query' && r.key !== 'unknown' ? `<a class="admin-text-link" href="index.html#/search?q=${encodeURIComponent(r.key)}" target="_blank" rel="noopener">复现搜索 ↗</a>` : ''}</td><td>${Number(r.views)} / ${Number(r.carts)}</td><td>${Number(r.searches)} / ${Number(r.zero_results)}</td><td>${Number(r.paid_orders)} 笔<small>${money(r.paid_cents)}</small></td><td>${money(r.refund_cents)}</td><td>${count(r.cost_known_orders) ? `${money(r.campaign_cost)} / ${money(r.coupon_cost)} / ${money(r.points_cost)}` : '<span class="admin-muted">暂无优惠记录</span>'}<small>已记录 ${number(r.cost_known_orders)} / ${number(r.paid_orders)} 笔</small></td></tr>`).join('') || '<tr><td colspan="6">暂无符合条件的明细</td></tr>'}</tbody></table></div>${U.pager(d.total, s.page, 20, 'diag', s.busy)}<details class="diag-method"><summary>查看统计口径</summary><p>${esc(d.scope)}</p></details></section>`)
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
