(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc, money } = U;
  const modules = [
    ['order', '订单', 'orders'],
    ['payment', '支付单', 'payments'],
    ['fulfillment', '仓储履约', 'fulfillment'],
    ['aftersale', '售后申请', 'aftersales'],
    ['refund', '退款单', 'refunds'],
    ['invoice', '发票申请', 'invoices'],
    ['users', '商城账号', 'users'],
  ];
  const m = U.controller(
    'dashboard-admin-root',
    () => ({
      days: 7,
      data: null,
      busy: false,
      error: '',
      historyPhase: 'payment',
      historyCursor: '',
      historyCount: 0,
    }),
    async (module) => ({
      data: await module.adapter.call(C.Service.Management, C.Action.Dashboard, { data: { days: module.state.days } }),
    }),
    view,
  );
  const bucket = (data, key, status) => data?.[key]?.states?.find((row) => row.status === status);
  const count = (data, key, status) => (data?.[key] ? Number(bucket(data, key, status)?.count || 0) : null);
  const metric = (data, key, status) => (data?.[key] ? Number(bucket(data, key, status)?.amount_cents || 0) : null);
  function view(module) {
    const s = module.state,
      data = s.data;
    const head = U.heading(
      '经营工作台',
      '掌握收款、履约与售后进度，及时处理待办事项。',
      `<div class="db-range" role="group" aria-label="看板统计范围">${[7, 30, 90].map((days) => `<button data-db-days="${days}" class="${s.days === days ? 'primary' : 'outline'}" ${s.busy ? 'disabled' : ''}>近 ${days} 天</button>`).join('')}</div><button class="outline" data-db="refresh" ${s.busy ? 'disabled' : ''}>刷新看板</button>`,
    );
    if (!data && s.error && !s.busy) return head + U.empty('看板读取失败', '请点击重试，稍后重新读取统计。');
    if (!data)
      return (
        head + window.LumoSkeleton.render('summary', { count: 6 }) + window.LumoSkeleton.render('rows', { count: 5 })
      );
    const cards = [
      ['累计订单', data.order ? Number(data.order.total || 0) : null, 'orders', 'number', '所有状态的订单数量'],
      ['待支付订单', count(data, 'order', C.OrderStatus.PendingPayment), 'orders', 'number', '等待顾客支付的订单'],
      ['累计成功收款', metric(data, 'payment', C.PaymentStatus.Succeeded), 'payments', 'money', '支付成功单据金额'],
      ['待确认支付', count(data, 'payment', C.PaymentStatus.Pending), 'payments', 'number', '等待渠道确认的支付单'],
      [
        '待出库',
        count(data, 'fulfillment', C.FulfillmentStatus.Ready),
        'fulfillment',
        'number',
        '等待仓库处理的履约单',
      ],
      [
        '待审核售后',
        count(data, 'aftersale', C.AftersaleStatus.Requested),
        'aftersales',
        'number',
        '等待审核的顾客申请',
      ],
      ['退款处理中', count(data, 'refund', C.RefundStatus.Processing), 'refunds', 'number', '等待渠道回执的退款单'],
      ['商城会员', count(data, 'users', C.Role.User), 'users', 'number', '普通用户账号总数'],
    ];
    return (
      head +
      (data.degraded
        ? `<div class="admin-error" role="status">部分看板暂不可用：${modules
            .filter(([key]) => data.errors?.[key])
            .map(([, name]) => esc(name))
            .join('、')}。可点击刷新重试。</div>`
        : '') +
      `<div class="db-metrics">${cards.map(([name, value, tab, type, note]) => `<button class="db-metric" data-db-tab="${tab}"><span>${name}</span><strong>${value == null ? '—' : type === 'money' ? money(value) : value.toLocaleString('zh-CN')}</strong><small>${note}</small><i aria-hidden="true">↗</i></button>`).join('')}</div>${insights(data.insights)}<div class="db-panels"><section class="admin-section db-trend"><div class="admin-panel-heading"><h2>收款与退款趋势</h2><span>近 ${s.days} 天 · 按单据创建日期统计成功金额</span></div>${trend(data, s.days)}<div class="db-legend"><span><i></i>成功支付</span><span><i></i>成功退款</span></div></section><section class="admin-section"><div class="admin-panel-heading"><h2>待办事项</h2><span>实时状态汇总</span></div><div class="db-tasks">${[
        ['payments', '支付回执待确认', count(data, 'payment', C.PaymentStatus.Pending)],
        ['fulfillment', '仓库待出库', count(data, 'fulfillment', C.FulfillmentStatus.Ready)],
        ['aftersales', '售后申请待审核', count(data, 'aftersale', C.AftersaleStatus.Requested)],
        ['invoices', '待开具发票', count(data, 'invoice', C.InvoiceStatus.Requested)],
      ]
        .map(
          ([tab, name, value]) =>
            `<button data-db-tab="${tab}"><span>${name}</span><strong>${value ?? '—'}</strong><i>→</i></button>`,
        )
        .join(
          '',
        )}</div></section><section class="admin-section"><div class="admin-panel-heading"><h2>业务分布</h2><span>全量记录，非最近一页</span></div><div class="db-volume">${modules
        .map(([key, name, tab]) => {
          const value = data[key]?.total;
          return `<button data-db-tab="${tab}"><span>${name}</span><strong>${value == null ? '—' : Number(value).toLocaleString('zh-CN')}</strong><i style="--volume:${value ? Math.max(2, (Number(value) / Math.max(1, ...modules.map(([k]) => Number(data[k]?.total || 0)))) * 100) : 0}%"></i></button>`;
        })
        .join(
          '',
        )}</div></section><section class="admin-section"><div class="admin-panel-heading"><h2>运营快捷入口</h2><span>常用配置</span></div><div class="db-shortcuts">${[
        ['products', '商品管理', '资料与上下架'],
        ['inventory', '库存管理', '按商品与规格调整'],
        ['coupons', '优惠券管理', '规则与发放条件'],
        ['popups', '营销弹窗', '内容、人群与频次'],
        ['settings', '商城装修', '品牌、页面与模块'],
        ['users', '用户管理', '账号与会员资料'],
      ]
        .map(
          ([tab, name, note]) =>
            `<button data-db-tab="${tab}"><strong>${name}</strong><span>${note}</span><i>↗</i></button>`,
        )
        .join(
          '',
        )}</div></section></div><p class="db-footnote">金额单位：人民币 · 统计时区：UTC+8 · 退款金额独立展示，不与收款金额重复相加。</p>`
    );
  }
  function insights(data) {
    if (!data) return '<p class="admin-error">经营分析暂不可用，请刷新重试。</p>';
    const labels = { view: '浏览商品', add_cart: '加入购物车', checkout: '进入结算', paid: '完成支付' };
    const cards = [
      ['期间实付', money(data.paid_cents)],
      ['期间退款', money(data.refund_cents)],
      ['净收款', money(data.net_cents)],
      ['客单价', money(data.average_order_cents)],
      ['付费顾客', data.buyers],
      ['期间复购率', (data.repeat_purchase_bps / 100).toFixed(1) + '%'],
    ];
    return `<section class="admin-section"><h2>经营分析</h2><button class="outline" data-db="history">同步或继续同步历史交易</button><div class="db-metrics">${cards.map(([label, value]) => `<div class="db-metric"><span>${esc(label)}</span><strong>${esc(value)}</strong></div>`).join('')}</div><h3>登录顾客转化漏斗</h3><div class="db-tasks">${(data.funnel || []).map((row) => `<div><strong>${labels[row.stage]}</strong> ${row.users} 人 · 同时到达此前阶段 ${row.cohort_users} 人</div>`).join('')}</div><p>${esc(data.funnel_scope)}</p><h3>销售数量排行</h3><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>商品</th><th>规格</th><th>已支付件数</th></tr></thead><tbody>${(data.top_products || []).map((row) => `<tr><td>${esc(row.name)}</td><td>${esc(row.sku_id)}</td><td>${esc(row.quantity)}</td></tr>`).join('')}</tbody></table></div><p>按实际收款和退款事件发生时间统计，UTC+8。复购指期间内至少两笔已支付订单。${data.historical_backfill ? '' : '当前为启用分析后的事件数据，可点击同步历史交易补全。'}</p></section>`;
  }
  function trend(data, days) {
    if (!data.payment || !data.refund) return U.empty('趋势暂不可用', '支付或退款统计服务未返回，请刷新重试。');
    const fmt = new Intl.DateTimeFormat('sv-SE', {
      timeZone: 'Asia/Shanghai',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    });
    const rows = Array.from({ length: days }, (_, index) => {
      const day = fmt.format(new Date(Date.now() - (days - index - 1) * 86400000));
      return {
        day,
        pay: (data.payment.daily || [])
          .filter((row) => row.day === day && row.status === C.PaymentStatus.Succeeded)
          .reduce((sum, row) => sum + Number(row.amount_cents || 0), 0),
        refund: (data.refund.daily || [])
          .filter((row) => row.day === day && row.status === C.RefundStatus.Succeeded)
          .reduce((sum, row) => sum + Number(row.amount_cents || 0), 0),
      };
    });
    if (rows.every((row) => row.pay === 0 && row.refund === 0))
      return U.empty('该时段暂无成功收款或退款', '完成支付或退款后会显示相应趋势。');
    const max = Math.max(...rows.flatMap((row) => [row.pay, row.refund]));
    return `<div class="db-chart" role="img" aria-label="近${days}天支付与退款金额趋势"><div class="db-chart-axis"><span>${money(max)}</span><span>${money(max / 2)}</span><span>¥0</span></div><div class="db-bars">${rows.map((row, index) => `<div class="db-bar-group"><div class="db-bar-pair" title="${row.day} 收款 ${money(row.pay)}，退款 ${money(row.refund)}"><i style="height:${(row.pay / max) * 100}%"></i><i style="height:${(row.refund / max) * 100}%"></i></div><span>${days === 7 || index % 5 === 0 || index === days - 1 ? row.day.slice(5) : ''}</span></div>`).join('')}</div></div><details class="db-chart-data"><summary>查看每日金额</summary><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>日期</th><th>成功收款</th><th>成功退款</th></tr></thead><tbody>${rows.map((row) => `<tr><td>${row.day}</td><td>${money(row.pay)}</td><td>${money(row.refund)}</td></tr>`).join('')}</tbody></table></div></details>`;
  }
  document.addEventListener('click', async (event) => {
    const el = event.target.closest('[data-db],[data-db-days],[data-db-tab]');
    if (!el || !m.active() || m.state.busy) return;
    if (el.dataset.dbTab) {
      m.adapter.navigate?.(el.dataset.dbTab);
      return;
    }
    if (el.dataset.db === 'history') {
      await m.run(async () => {
        for (let page = 0; page < 40 && m.active(); page++) {
          const value = await m.adapter.call('analytics', 'sync_history', {
            data: { phase: m.state.historyPhase || 'payment', cursor: m.state.historyCursor || '' },
          });
          m.state.historyCount = (m.state.historyCount || 0) + Number(value.processed || 0);
          m.state.historyPhase = value.phase === 'done' ? 'payment' : value.phase;
          m.state.historyCursor = value.next_cursor || '';
          m.adapter.notify?.('已同步 ' + m.state.historyCount + ' 条交易');
          if (value.done) break;
        }
      });
      await m.refresh();
      return;
    }
    if (el.dataset.dbDays) m.state.days = Number(el.dataset.dbDays);
    await m.refresh();
  });
  window.LumoDashboardAdmin = m;
})();
