(function () {
  'use strict';
  const U = window.LumoAdminUI,
    C = window.LumoConstants,
    { esc, date } = U;
  const kinds = {
    [C.JourneyKind.Cart]: '购物车召回',
    [C.JourneyKind.Unpaid]: '待支付订单提醒',
    [C.JourneyKind.Repurchase]: '商品复购提醒',
  };
  const states = {
    [C.JourneyStatus.Watching]: '等待检查',
    [C.JourneyStatus.Queued]: '待投递',
    [C.JourneyStatus.Delivered]: '已投递站内信',
    [C.JourneyStatus.Suppressed]: '未发送',
    [C.JourneyStatus.Exited]: '已退出',
    [C.JourneyStatus.Recovered]: '点击后匹配付款',
  };
  const fresh = () => ({
    name: '',
    kind: C.JourneyKind.Cart,
    enabled: false,
    delay_minutes: 1440,
    cycle_days: 30,
    gap_hours: 24,
    sku_id: '',
    segment_id: '',
    version: 0,
  });
  const m = U.controller(
    'journeys-admin-root',
    () => ({
      rules: [],
      ruleTotal: 0,
      rulePage: 1,
      items: [],
      total: 0,
      page: 1,
      counts: {},
      segments: [],
      draft: fresh(),
      ruleKey: crypto.randomUUID(),
      customerQuery: '',
      customers: [],
      manual: { rule_id: '', customer_id: '', order_id: '' },
      keyword: '',
      status: '',
      scope: '',
      busy: false,
      error: '',
    }),
    async (module) => {
      const s = module.state;
      const [rules, records, segments] = await Promise.all([
        module.adapter.call('engagement', 'journey_rules', { data: { page: s.rulePage, page_size: 20 } }),
        module.adapter.call('engagement', 'journey_list', {
          data: { page: s.page, page_size: 10, keyword: s.keyword, status: s.status },
        }),
        module.adapter.call('member', 'segment_list', { data: { page: 1, page_size: 100 } }),
      ]);
      return {
        rules: rules.items || [],
        ruleTotal: Number(rules.total) || 0,
        items: records.items || [],
        total: Number(records.total) || 0,
        counts: records.counts || {},
        scope: records.scope,
        segments: segments.items || [],
      };
    },
    (module) => {
      const s = module.state,
        d = s.draft;
      const field = (key, label, type = 'text', extra = '') =>
        `<label class="field">${label}<input name="${key}" data-journey-field="${key}" type="${type}" value="${esc(d[key])}" ${extra}></label>`;
      return (
        U.heading(
          '顾客触达',
          '',
          `<button class="outline" data-journey="refresh" ${s.busy ? 'disabled' : ''}>刷新</button>`,
        ) +
        module.tabs(
          [
            ['overview', '触达概览'],
            ['rules', '触达规则'],
            ['editor', '规则设置'],
            ['manual', '手动触达'],
            ['records', '触达记录'],
          ],
          '顾客触达功能',
        ) +
        module.panel(
          'overview',
          `<section class="admin-section"><div class="admin-panel-heading"><div><h2>购物提醒，尊重顾客的选择</h2><p class="admin-muted">仅发送站内消息。默认关闭新规则；只有明确开启“优惠活动通知”的普通会员才会收到。匿名行为不会用于猜测身份。</p></div></div><div class="customer-stats">${[
            ['queued', '已排队'],
            ['delivered', '已写入消息中心'],
            ['clicked', '恢复入口点击'],
            ['recovered', '点击后匹配付款'],
          ]
            .map(([k, label]) => `<div><span>${label}</span><strong>${Number(s.counts[k]) || 0}</strong></div>`)
            .join(
              '',
            )}</div><p class="admin-muted">${esc(s.scope || '付款匹配不代表触达产生的因果提升。')}</p></section>`,
        ) +
        module.panel(
          'editor',
          `<section class="admin-section"><div class="admin-panel-heading"><h2>${d.id ? '编辑触达规则' : '新增触达规则'}</h2><button class="outline" data-journey="new-rule" ${s.busy ? 'disabled' : ''}>新建规则</button></div><form id="journey-rule-form" class="admin-form-stack"><div class="admin-form">${field('name', '规则名称', 'text', 'required maxlength="80"')}<label class="field">提醒类型<select name="kind" data-journey-field="kind">${Object.entries(
            kinds,
          )
            .map(([v, t]) => `<option value="${v}" ${d.kind === v ? 'selected' : ''}>${t}</option>`)
            .join(
              '',
            )}</select></label>${field('delay_minutes', '活动后等待（分钟）', 'number', 'min="1" max="10080" required')}${field('cycle_days', '复购周期（天）', 'number', 'min="1" max="365" required')}${field('gap_hours', '跨规则发送间隔（小时）', 'number', 'min="24" max="720" required')}${d.kind === C.JourneyKind.Repurchase ? field('sku_id', '复购商品完整SKU', 'text', 'required maxlength="128"') : ''}<label class="field">客户分群（可选）<select name="segment_id" data-journey-field="segment_id"><option value="">所有满足通知偏好的顾客</option>${s.segments.map((row) => `<option value="${esc(row.id)}" ${d.segment_id === row.id ? 'selected' : ''}>${esc(row.name)}</option>`).join('')}${d.segment_id && !s.segments.some((v) => v.id === d.segment_id) ? `<option value="${esc(d.segment_id)}" selected>${esc(d.segment_id)}</option>` : ''}</select></label><label class="field">启用规则<select name="enabled" data-journey-field="enabled"><option value="false" ${!d.enabled ? 'selected' : ''}>暂不启用</option><option value="true" ${d.enabled ? 'selected' : ''}>启用后处理新的业务行为</option></select></label></div><p class="admin-muted">购物车与待支付按等待分钟检查；复购按已完成订单的商品周期检查。同一顾客跨规则至少间隔24小时。已付款、订单关闭、购物车清空或不符合分群时退出。</p><div class="admin-form-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>保存规则</button><span>版本 ${d.version}</span></div></form></section>`,
        ) +
        module.panel(
          'rules',
          `<section class="admin-section"><div class="admin-panel-heading"><h2>触达规则</h2><button class="primary" data-journey="new-rule" ${s.busy ? 'disabled' : ''}>新建规则</button></div><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>规则</th><th>类型 / 时间</th><th>状态</th><th>操作</th></tr></thead><tbody>${s.rules.map((row) => `<tr><td>${esc(row.name)}<small>${esc(row.id)}</small></td><td>${esc(kinds[row.kind])}<small>${row.kind === C.JourneyKind.Repurchase ? `${row.cycle_days} 天 · ${esc(row.sku_id)}` : `等待 ${row.delay_minutes} 分钟`} · 间隔 ${row.gap_hours} 小时</small></td><td>${row.enabled ? '已启用' : '已停用'}</td><td><button class="outline" data-journey="edit-rule" data-id="${esc(row.id)}">编辑</button></td></tr>`).join('') || '<tr><td colspan="4">还没有触达规则</td></tr>'}</tbody></table></div>${U.pager(s.ruleTotal, s.rulePage, 20, 'journey-rule', s.busy)}</section>`,
        ) +
        module.panel(
          'manual',
          `<section class="admin-section"><h2>手动创建触达检查</h2><form id="journey-customer-search" class="admin-filter-form"><label class="field">查找顾客<input name="keyword" value="${esc(s.customerQuery)}" placeholder="顾客账号或编号" maxlength="100"></label><button class="outline" ${s.busy ? 'disabled' : ''}>查找顾客</button></form><div class="customer-tags">${s.customers.map((row) => `<button class="outline" data-journey="customer" data-id="${esc(row.id)}">${esc(row.username)} · ${esc(row.id)}</button>`).join('')}</div><form id="journey-run-form" class="admin-form-stack"><div class="admin-form"><label class="field">启用的规则<select name="rule_id" data-journey-manual="rule_id" required><option value="">选择规则</option>${s.rules
            .filter((r) => r.enabled)
            .map(
              (r) =>
                `<option value="${esc(r.id)}" ${s.manual.rule_id === r.id ? 'selected' : ''}>${esc(r.name)}</option>`,
            )
            .join(
              '',
            )}</select></label><label class="field">顾客编号<input name="customer_id" data-journey-manual="customer_id" required value="${esc(s.manual.customer_id)}" maxlength="128"></label><label class="field">原订单编号（订单 / 复购必填）<input name="order_id" data-journey-manual="order_id" value="${esc(s.manual.order_id)}" maxlength="128"></label></div><div class="admin-form-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>创建检查任务</button><small>手动任务同样遵守偏好、等待时间、业务状态和频控。</small></div></form></section>`,
        ) +
        module.panel(
          'records',
          `<section class="admin-section"><h2>触达记录</h2><form id="journey-filter" class="admin-filter-form"><label class="field">搜索记录<input name="keyword" value="${esc(s.keyword)}" placeholder="顾客、订单或SKU编号"></label><label class="field">状态<select name="status"><option value="">全部状态</option>${Object.entries(
            states,
          )
            .map(([v, t]) => `<option value="${v}" ${s.status === v ? 'selected' : ''}>${t}</option>`)
            .join(
              '',
            )}</select></label><button class="outline">筛选</button></form><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>顾客 / 来源</th><th>状态</th><th>执行与结果</th><th>操作</th></tr></thead><tbody>${s.items.map((j) => `<tr><td>${esc(j.user_id)}<small>${esc(kinds[j.kind])} · ${esc(j.source_id)}</small></td><td><span class="admin-state">${esc(states[j.status] || j.status)}</span><small>${esc(j.reason)}</small></td><td>检查 ${date(j.due_at)}<small>${j.delivered_at ? '投递 ' + date(j.delivered_at) : ''} ${j.clicked_at ? '点击 ' + date(j.clicked_at) : ''}</small>${j.recovered_order_id ? `<small>匹配付款 ${esc(j.recovered_order_id)}</small>` : ''}</td><td>${[C.JourneyStatus.Watching, C.JourneyStatus.Queued].includes(j.status) ? `<button class="outline" data-journey="cancel" data-id="${esc(j.id)}">取消待发</button>` : '—'}</td></tr>`).join('') || '<tr><td colspan="4">暂无触达检查记录</td></tr>'}</tbody></table></div>${U.pager(s.total, s.page, 10, 'journey', s.busy)}</section>`,
        )
      );
    },
  );
  function remember(e) {
    if (!m.active()) return;
    const n = e.target;
    if (n.dataset.journeyField) {
      m.state.draft[n.dataset.journeyField] = n.dataset.journeyField === 'enabled' ? n.value === 'true' : n.value;
      if (n.dataset.journeyField === 'kind' && e.type === 'change') m.draw();
    }
    if (n.dataset.journeyManual) m.state.manual[n.dataset.journeyManual] = n.value;
  }
  document.addEventListener('input', remember);
  document.addEventListener('change', remember);
  document.addEventListener('submit', async (e) => {
    if (!m.active() || m.state.busy) return;
    const id = e.target.id;
    if (!['journey-rule-form', 'journey-run-form', 'journey-filter', 'journey-customer-search'].includes(id)) return;
    e.preventDefault();
    const s = m.state,
      f = new FormData(e.target);
    if (id === 'journey-filter') {
      s.keyword = String(f.get('keyword') || '');
      s.status = String(f.get('status') || '');
      s.page = 1;
      await m.refresh();
      return;
    }
    if (id === 'journey-customer-search') {
      s.customerQuery = String(f.get('keyword') || '');
      await m.run(async () => ({
        customers:
          (
            await m.adapter.call('iam', 'admin_list', {
              data: { keyword: s.customerQuery, status: C.Role.User, page: 1, page_size: 10 },
            })
          ).items || [],
      }));
      return;
    }
    const ok = await m.run(async () => {
      if (id === 'journey-rule-form') {
        const data = {
          ...s.draft,
          delay_minutes: Number(s.draft.delay_minutes),
          cycle_days: Number(s.draft.cycle_days),
          gap_hours: Number(s.draft.gap_hours),
        };
        const result = await m.adapter.call('engagement', 'journey_rule_save', {
          id: data.id || '',
          idempotency_key: s.ruleKey,
          data,
        });
        return { draft: result };
      }
      await m.adapter.call('engagement', 'journey_run', {
        id: s.manual.rule_id,
        data: { customer_id: s.manual.customer_id, order_id: s.manual.order_id },
      });
      return {};
    });
    if (ok) {
      s.sectionTab = id === 'journey-rule-form' ? 'rules' : 'records';
      m.adapter.notify?.(id === 'journey-rule-form' ? '触达规则已保存' : '已创建触达检查');
      await m.refresh();
    }
  });
  document.addEventListener('click', async (e) => {
    if (!m.active() || m.state.busy) return;
    const n = e.target.closest('[data-journey],[data-journey-page],[data-journey-rule-page]');
    if (!n) return;
    const s = m.state;
    if (n.dataset.journeyPage) {
      s.page = Math.max(1, Number(n.dataset.journeyPage));
      await m.refresh();
      return;
    }
    if (n.dataset.journeyRulePage) {
      s.rulePage = Math.max(1, Number(n.dataset.journeyRulePage));
      await m.refresh();
      return;
    }
    if (n.dataset.journey === 'new-rule') {
      s.draft = fresh();
      s.ruleKey = crypto.randomUUID();
      s.sectionTab = 'editor';
      m.draw();
    }
    if (n.dataset.journey === 'edit-rule') {
      s.draft = { ...s.rules.find((r) => r.id === n.dataset.id) };
      s.sectionTab = 'editor';
      m.draw();
    }
    if (n.dataset.journey === 'customer') {
      s.manual.customer_id = n.dataset.id;
      m.draw();
    }
    if (n.dataset.journey === 'refresh') await m.refresh();
    if (n.dataset.journey === 'cancel') {
      if (await m.run(() => m.adapter.call('engagement', 'journey_cancel', { id: n.dataset.id }))) await m.refresh();
    }
  });
  window.LumoJourneysAdmin = m;
})();
