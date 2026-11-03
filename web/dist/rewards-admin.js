(function () {
  'use strict';
  const C = LumoConstants,
    U = LumoAdminUI,
    { esc, date } = U;
  const status = {
    PENDING: '等待处理',
    PROCESSING: '处理中',
    ISSUED: '已兑换',
    SHIPPED: '已发货',
    DELIVERED: '已签收',
    REFUNDED: '积分已返还',
    ATTENTION: '需跟进',
  };
  const initial = () => ({
    offers: [],
    records: [],
    total: 0,
    page: 1,
    editor: null,
    retry: null,
    busy: false,
    error: '',
  });
  const m = U.controller(
    'rewards-admin-root',
    initial,
    async (module) => {
      const [o, r] = await Promise.all([
        module.adapter.call(C.Service.Points, C.Action.OffersAdmin),
        module.adapter.call(C.Service.Points, C.Action.RedemptionsAdmin, {
          data: { page: module.state.page, page_size: 20 },
        }),
      ]);
      return { offers: o.items || [], records: r.items || [], total: Number(r.total || 0) };
    },
    (module) => view(module.state),
  );
  const localDate = (v) => {
    if (!v) return '';
    const d = new Date(v);
    return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  };
  function view(s) {
    const r = s.editor;
    return (
      U.heading(
        '积分兑换',
        '管理奖品与兑换处理',
        '<button class="primary" data-rwa="create">新增兑换项目</button><button class="outline" data-rwa="refresh">刷新</button>',
      ) +
      (r
        ? `<section class="admin-section"><h2>${r.id ? '编辑' : '新增'}兑换项目</h2><form id="reward-offer-editor"><div class="admin-form"><label class="field">名称<input name="name" required maxlength="120" value="${esc(r.name)}"></label><label class="field">奖品类型<select name="kind"><option value="${C.RewardKind.Coupon}" ${r.kind === C.RewardKind.Coupon ? 'selected' : ''}>优惠券</option><option value="${C.RewardKind.Gift}" ${r.kind === C.RewardKind.Gift ? 'selected' : ''}>实物商品</option></select></label><label class="field">券模板编号 / 商品 SKU<input name="reference_id" required maxlength="120" value="${esc(r.reference_id)}"></label><label class="field">消耗积分<input type="number" min="1" max="100000000" required name="cost" value="${Number(r.cost || 100)}"></label><label class="field">总名额<input type="number" min="${Math.max(1, Number(r.used || 0))}" max="1000000" required name="total_quota" value="${Number(r.total_quota || 100)}"></label><label class="field">每人上限（券为 1）<input type="number" min="1" max="100" required name="max_per_user" value="${Number(r.max_per_user || 1)}"></label><label class="field">开始时间<input type="datetime-local" name="starts_at" required value="${esc(localDate(r.starts_at))}"></label><label class="field">结束时间<input type="datetime-local" name="ends_at" required value="${esc(localDate(r.ends_at))}"></label><label class="field wide">说明<textarea name="description" maxlength="500">${esc(r.description)}</textarea></label><label class="check"><input type="checkbox" name="active" ${r.active ? 'checked' : ''}>开放兑换</label><div class="admin-form-footer"><button class="primary" ${s.busy ? 'disabled' : ''}>保存项目</button><button type="button" class="outline" data-rwa="cancel">取消</button></div></div><p class="admin-muted">优惠券需使用有效且无成长值、积分门槛的模板，每人限领一次。实物使用真实库存，兑换成功后进入仓库待办。</p></form></section>`
        : '') +
      `<section class="admin-section"><h2>兑换项目（最多显示 200 项）</h2><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>项目</th><th>奖品</th><th>积分 / 名额</th><th>开放时间</th><th>操作</th></tr></thead><tbody>${s.offers.map((o) => `<tr><td><strong>${esc(o.name)}</strong><small>${esc(o.id)}</small></td><td>${o.kind === C.RewardKind.Gift ? '实物礼品' : '优惠券'}<small>${esc(o.reference_id)}</small></td><td>${Number(o.cost)} 分<small>已兑换 ${Number(o.used)} / ${Number(o.total_quota)}</small></td><td>${o.active ? '开放' : '关闭'}<small>${date(o.starts_at)} 至 ${date(o.ends_at)}</small></td><td><button class="outline" data-rwa="edit" data-id="${esc(o.id)}">编辑</button></td></tr>`).join('') || '<tr><td colspan="5">暂无兑换项目</td></tr>'}</tbody></table></div></section><section class="admin-section"><h2>兑换记录</h2>${s.retry ? `<form id="reward-retry-form" class="op-confirm"><h3>重试兑换 ${esc(s.retry.id)}</h3><p>使用原兑换单继续处理，不再扣除积分。</p><label class="field">跟进原因<input name="reason" required maxlength="240"></label><div class="admin-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>确认重试</button><button type="button" class="outline" data-rwa="cancel-retry">取消</button></div></form>` : ''}<div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>兑换单 / 用户</th><th>奖品 / 积分</th><th>状态</th><th>处理信息</th><th>操作</th></tr></thead><tbody>${s.records.map((r) => `<tr><td><strong>${esc(r.id)}</strong><small>${esc(r.user_id)}</small></td><td>${esc(r.name)}<small>${Number(r.cost)} 积分</small></td><td>${esc(status[r.state] || r.state)}<small>${date(r.created_at)}</small></td><td>${esc(r.coupon_id || r.tracking_id || r.fulfillment_id || '—')}<small>${esc(r.last_error || '')}</small></td><td>${r.state === C.RedemptionState.Attention ? `<button class="outline" data-rwa="retry" data-id="${esc(r.id)}">跟进重试</button>` : '—'}</td></tr>`).join('') || '<tr><td colspan="5">暂无记录</td></tr>'}</tbody></table></div>${U.pager(s.total, s.page, 20, 'rwa', s.busy)}</section>`
    );
  }
  document.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-rwa],[data-rwa-page]');
    if (!b || !m.active() || m.state.busy) return;
    const s = m.state,
      a = b.dataset.rwa;
    if (b.dataset.rwaPage) {
      s.page = Number(b.dataset.rwaPage);
      await m.refresh();
      return;
    }
    if (a === 'refresh') await m.refresh();
    if (a === 'create') {
      s.editor = {
        name: '',
        kind: C.RewardKind.Coupon,
        active: false,
        starts_at: new Date().toISOString(),
        ends_at: new Date(Date.now() + 30 * 86400000).toISOString(),
      };
      m.draw();
    }
    if (a === 'edit') {
      s.editor = { ...s.offers.find((r) => r.id === b.dataset.id) };
      m.draw();
    }
    if (a === 'cancel') {
      s.editor = null;
      m.draw();
    }
    if (a === 'retry') {
      s.retry = s.records.find((r) => r.id === b.dataset.id);
      m.draw();
    }
    if (a === 'cancel-retry') {
      s.retry = null;
      m.draw();
    }
  });
  document.addEventListener('submit', async (e) => {
    if (!['reward-offer-editor', 'reward-retry-form'].includes(e.target.id) || !m.active()) return;
    e.preventDefault();
    const f = new FormData(e.target),
      s = m.state;
    if (e.target.id === 'reward-retry-form') {
      const ok = await m.run(
        () =>
          m.adapter.call(C.Service.Points, C.Action.RedemptionRetry, {
            id: s.retry.id,
            data: { version: s.retry.version, reason: String(f.get('reason')) },
          }),
        () => {
          s.retry = null;
        },
      );
      if (ok) await m.refresh();
      return;
    }
    const data = { ...Object.fromEntries(f.entries()), version: s.editor.version || 0, active: f.has('active') };
    for (const key of ['cost', 'total_quota', 'max_per_user']) data[key] = Number(data[key]);
    for (const key of ['starts_at', 'ends_at']) data[key] = new Date(data[key]).toISOString();
    const ok = await m.run(
      () => m.adapter.call(C.Service.Points, C.Action.OfferUpsert, { id: s.editor.id || '', data }),
      () => {
        s.editor = null;
      },
    );
    if (ok) await m.refresh();
  });
  window.LumoRewardsAdmin = m;
})();
