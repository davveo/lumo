(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc } = U;
  const labels = {
    [C.SubscriptionStatus.Active]: '等待触发',
    [C.SubscriptionStatus.Triggered]: '已提醒',
    [C.SubscriptionStatus.Cancelled]: '已取消',
    [C.SubscriptionStatus.Expired]: '已过期',
  };
  const m = U.controller(
    'admin-product-alerts',
    () => ({
      ready: false,
      busy: false,
      error: '',
      config: {},
      version: 0,
      items: [],
      total: 0,
      page: 1,
      kind: '',
      status: '',
      sku: '',
      user: '',
      reason: '',
      cancelID: '',
    }),
    async (module) => {
      const s = module.state;
      const [policy, list] = await Promise.all([
        module.adapter.call(C.Service.Engagement, C.Action.PolicyGet, {}),
        module.adapter.call(C.Service.Engagement, C.Action.AdminList, {
          data: {
            limit: 10,
            offset: (s.page - 1) * 10,
            kind: s.kind,
            status: s.status,
            sku_id: s.sku,
            user_id: s.user,
          },
        }),
      ]);
      return {
        ready: true,
        config: policy.config || {},
        version: policy.version || 0,
        items: list.items || [],
        total: Number(list.total) || 0,
      };
    },
    (module) => {
      const s = module.state;
      return (
        U.heading(
          '购物提醒',
          '管理商品降价与到货订阅。按实际价格和可售库存检查，触发一次后发送站内消息。',
          `<button class="outline" data-alert-admin="refresh" ${s.busy ? 'disabled' : ''}>刷新</button>`,
        ) +
        (!s.ready
          ? window.LumoSkeleton.render('table', { count: 5 })
          : `
      <details class="admin-section"><summary>提醒规则与开关</summary><form data-alert-policy><div class="access-switches"><label class="access-setting"><span><strong>降价提醒</strong><small>关闭后暂停新订阅及触发检查。</small></span><input name="price_enabled" type="checkbox" role="switch" aria-label="降价提醒" ${s.config.price_enabled ? 'checked' : ''}></label><label class="access-setting"><span><strong>到货提醒</strong><small>已有订阅保留至到期，可随时重新开启。</small></span><input name="stock_enabled" type="checkbox" role="switch" aria-label="到货提醒" ${s.config.stock_enabled ? 'checked' : ''}></label></div><div class="admin-form"><label class="field">订阅有效期（天）<input name="ttl_days" type="number" min="1" max="90" required value="${s.config.ttl_days}"></label><label class="field">每位会员最多生效订阅<input name="max_per_user" type="number" min="1" max="100" required value="${s.config.max_per_user}"></label></div><label class="field">修改原因<input name="reason" required maxlength="240" value="${esc(s.reason)}"></label><div class="admin-form-footer"><span>版本 ${s.version} · 规则用于新订阅 · 每 5 分钟检查</span><button class="primary" ${s.busy ? 'disabled' : ''}>保存规则</button></div></form></details>
      <section class="admin-section"><form data-alert-filter class="op-filters"><label class="field">类型<select name="kind"><option value="">全部类型</option>${[
        [C.SubscriptionKind.Price, '降价'],
        [C.SubscriptionKind.Stock, '到货'],
      ]
        .map(([v, l]) => `<option value="${v}" ${s.kind === v ? 'selected' : ''}>${l}</option>`)
        .join(
          '',
        )}</select></label><label class="field">状态<select name="status"><option value="">全部状态</option>${Object.entries(
        labels,
      )
        .map(([v, l]) => `<option value="${v}" ${s.status === v ? 'selected' : ''}>${l}</option>`)
        .join(
          '',
        )}</select></label><label class="field">商品编号<input name="sku" maxlength="128" value="${esc(s.sku)}"></label><label class="field">会员编号<input name="user" maxlength="128" value="${esc(s.user)}"></label><button class="outline" ${s.busy ? 'disabled' : ''}>筛选</button></form><div class="admin-table-wrap"><table class="admin-table alerts-table"><thead><tr><th>提醒编号 / 会员</th><th>商品 / 类型</th><th>目标价格</th><th>状态 / 有效期</th><th>操作</th></tr></thead><tbody>${s.items.length ? s.items.map((r) => `<tr><td><strong>${esc(r.id)}</strong><small>${esc(r.user_id)}</small></td><td>${esc(r.product_name)}<small>${esc(r.sku_id)} · ${r.kind === C.SubscriptionKind.Price ? '降价' : '到货'}</small></td><td>${r.kind === C.SubscriptionKind.Price ? U.money(r.target_cents) : '库存恢复可售'}</td><td>${esc(labels[r.status] || r.status)}<small>${U.date(r.expires_at)}</small></td><td>${r.status === C.SubscriptionStatus.Active ? `<button class="outline" data-alert-admin="cancel" data-id="${esc(r.id)}" ${s.busy ? 'disabled' : ''}>停用</button>` : '—'}</td></tr>`).join('') : '<tr><td colspan="5">暂无符合条件的订阅</td></tr>'}</tbody></table></div>${U.pager(s.total, s.page, 10, 'alert-admin', s.busy)}</section>${s.cancelID ? `<section class="admin-section"><h2>停用订阅 ${esc(s.cancelID)}</h2><form data-alert-cancel><label class="field">停用原因<input name="reason" required maxlength="240"></label><button class="primary" ${s.busy ? 'disabled' : ''}>确认停用</button><button type="button" class="outline" data-alert-admin="dismiss">取消</button></form></section>` : ''}`)
      );
    },
  );
  document.addEventListener('click', async (event) => {
    if (!m.active() || m.state.busy) return;
    const p = event.target.closest('[data-alert-admin-page]');
    if (p) {
      const before = m.state.page;
      m.state.page = Math.max(1, Number(p.dataset.alertAdminPage) || 1);
      if (!(await m.refresh())) m.state.page = before;
      m.draw();
      return;
    }
    const el = event.target.closest('[data-alert-admin]');
    if (!el) return;
    if (el.dataset.alertAdmin === 'refresh') await m.refresh();
    if (el.dataset.alertAdmin === 'cancel') {
      m.state.cancelID = el.dataset.id;
      m.draw();
    }
    if (el.dataset.alertAdmin === 'dismiss') {
      m.state.cancelID = '';
      m.draw();
    }
  });
  document.addEventListener('submit', async (event) => {
    if (!m.active() || m.state.busy) return;
    const form = event.target;
    if (!form.matches('[data-alert-policy],[data-alert-filter],[data-alert-cancel]')) return;
    event.preventDefault();
    const f = new FormData(form),
      s = m.state;
    if (form.matches('[data-alert-filter]')) {
      s.kind = String(f.get('kind') || '');
      s.status = String(f.get('status') || '');
      s.sku = String(f.get('sku') || '').trim();
      s.user = String(f.get('user') || '').trim();
      s.page = 1;
      await m.refresh();
      return;
    }
    const reason = String(f.get('reason') || '').trim();
    if (!reason) {
      s.error = '请填写修改原因';
      m.draw();
      return;
    }
    let action, body;
    if (form.matches('[data-alert-policy]')) {
      s.config = {
        price_enabled: f.get('price_enabled') === 'on',
        stock_enabled: f.get('stock_enabled') === 'on',
        ttl_days: Number(f.get('ttl_days')),
        max_per_user: Number(f.get('max_per_user')),
      };
      s.reason = reason;
      action = C.Action.PolicyUpsert;
      body = { data: { config: s.config, version: s.version, reason } };
    } else {
      action = C.Action.AdminCancel;
      body = { id: s.cancelID, data: { reason } };
    }
    if (
      await m.run(
        () => m.adapter.call(C.Service.Engagement, action, body),
        () => {},
      )
    ) {
      s.cancelID = '';
      s.reason = '';
      m.adapter.notify?.('已保存');
      await m.refresh();
    }
  });
  window.LumoAlertsAdmin = m;
})();
