(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc, date, pair } = U;
  const differenceNames = {
    order_unavailable: '关联订单不可用',
    amount_mismatch: '支付与订单金额不一致',
    payment_not_accepted_or_pending_event: '订单尚未确认该支付单',
  };
  const m = U.controller(
    'reconciliation-admin-root',
    () => ({
      items: [],
      page: 1,
      differencePage: 1,
      ready: false,
      busy: false,
      error: '',
      detail: null,
      confirm: false,
    }),
    async (module) => ({
      items: (await module.adapter.call(C.Service.Reconciliation, C.Action.List)).items || [],
      ready: true,
    }),
    view,
  );
  function view(module) {
    const s = module.state,
      latest = s.detail || s.items[0],
      details = latest?.details || {},
      diffs = Array.isArray(details.differences) ? details.differences : [];
    s.page = Math.max(1, Math.min(s.page, Math.ceil(s.items.length / 10) || 1));
    s.differencePage = Math.max(1, Math.min(s.differencePage, Math.ceil(diffs.length / 8) || 1));
    return (
      U.heading(
        '交易对账',
        '核对内部支付台账与订单，查看差异及待同步事项。',
        `<button class="outline" data-rc="refresh" ${s.busy ? 'disabled' : ''}>刷新</button><button class="primary" data-rc="prepare" ${s.busy ? 'disabled' : ''}>执行内部对账</button>`,
      ) +
      `<div class="admin-inline-note">当前核对最近100条内部支付单，展示最近30次对账记录。尚未接入外部支付渠道账单，本页不代表全账期渠道对账。</div>${s.confirm ? `<form data-rc-run class="op-confirm"><h2>确认执行内部对账</h2><p>读取当前内部支付与订单台账，生成一份新的核对报告，不修改订单及收款金额。</p><div class="admin-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>确认执行</button><button type="button" class="outline" data-rc="cancel">取消</button></div></form>` : ''}${
        !s.ready
          ? s.error
            ? U.empty('对账记录读取失败')
            : window.LumoSkeleton.render('table', { count: 5 })
          : !latest
            ? U.empty('暂无对账报告', '点击“执行内部对账”生成第一份核对结果。')
            : `<section class="admin-section rc-summary"><div class="admin-panel-heading"><h2>${s.detail ? '所选对账报告' : '最近一次对账'}</h2><span>${date(latest.created_at)}</span></div><dl class="op-definition">${pair('报告编号', latest.id)}${pair('核对结果', latest.status === C.ReconciliationStatus.Matched ? '账目一致' : '存在差异')}${pair('读取支付单数', details.checked_payments ?? 0)}${pair('差异条数', diffs.length)}</dl>${
                diffs.length
                  ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table"><thead><tr><th>支付单号</th><th>差异类型</th><th>处理建议</th></tr></thead><tbody>${diffs
                      .slice((s.differencePage - 1) * 8, s.differencePage * 8)
                      .map(
                        (row) =>
                          `<tr><td>${esc(row.payment_id)}</td><td>${esc(differenceNames[row.kind] || row.kind)}</td><td>核对支付回执与关联订单，检查同步状态后重新对账。</td></tr>`,
                      )
                      .join('')}</tbody></table></div>${U.pager(diffs.length, s.differencePage, 8, 'rc-diff', s.busy)}`
                  : '<div class="rc-matched">本次已核验单据未发现差异。</div>'
              }</section><section class="admin-list-panel"><div class="admin-list-summary"><strong>运行记录</strong><span>最近 ${s.items.length} 次 · 按时间倒序</span></div><div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table"><thead><tr><th>报告编号</th><th>创建时间</th><th>读取支付单数</th><th>差异数</th><th>结果</th><th>操作</th></tr></thead><tbody>${s.items
                .slice((s.page - 1) * 10, s.page * 10)
                .map(
                  (row) =>
                    `<tr><td>${esc(row.id)}</td><td>${date(row.created_at)}</td><td>${esc(row.details?.checked_payments ?? 0)}</td><td>${Array.isArray(row.details?.differences) ? row.details.differences.length : 0}</td><td><span class="admin-state ${row.status === C.ReconciliationStatus.Matched ? 'success' : 'danger'}">${row.status === C.ReconciliationStatus.Matched ? '一致' : '有差异'}</span></td><td><button class="outline" data-rc="detail" data-id="${esc(row.id)}">查看报告</button></td></tr>`,
                )
                .join('')}</tbody></table></div>${U.pager(s.items.length, s.page, 10, 'rc', s.busy)}</section>`
      }`
    );
  }
  document.addEventListener('click', async (event) => {
    const el = event.target.closest('[data-rc],[data-rc-page],[data-rc-diff-page]');
    if (!el || el.disabled || !m.active() || m.state.busy) return;
    const s = m.state;
    if (el.dataset.rcPage) {
      s.page = Number(el.dataset.rcPage);
      m.draw();
      return;
    }
    if (el.dataset.rcDiffPage) {
      const page = Number(el.dataset.rcDiffPage),
        diffs = (s.detail || s.items[0])?.details?.differences || [];
      if (!Number.isInteger(page) || page < 1 || page > Math.max(1, Math.ceil(diffs.length / 8))) return;
      s.differencePage = page;
      m.draw();
      return;
    }
    switch (el.dataset.rc) {
      case 'refresh':
        s.detail = null;
        s.differencePage = 1;
        await m.refresh();
        break;
      case 'prepare':
        s.confirm = true;
        m.draw();
        break;
      case 'cancel':
        s.confirm = false;
        m.draw();
        break;
      case 'detail':
        s.detail = s.items.find((row) => row.id === el.dataset.id) || null;
        s.differencePage = 1;
        m.draw();
        break;
    }
  });
  document.addEventListener('submit', async (event) => {
    if (!event.target.matches('[data-rc-run]') || !m.active()) return;
    event.preventDefault();
    const ok = await m.run(async () => {
      await m.adapter.call(C.Service.Reconciliation, C.Action.Run, { idempotency_key: crypto.randomUUID() });
      return { confirm: false, detail: null, differencePage: 1, page: 1 };
    });
    if (ok) {
      m.adapter.notify?.('对账报告已生成');
      await m.refresh();
    }
  });
  window.LumoReconciliationAdmin = m;
})();
