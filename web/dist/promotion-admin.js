(function () {
  'use strict';
  const C = window.LumoConstants;
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const copy = (value) => JSON.parse(JSON.stringify(value));
  const money = (value) =>
    (Number(value || 0) / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const editableKinds = [
    [C.CampaignKind.Reduction, '满额立减'],
    [C.CampaignKind.Discount, '折扣特惠'],
    [C.CampaignKind.QuantityDiscount, '多件优惠'],
    [C.CampaignKind.Direct, '限量直降'],
  ];
  const allKinds = [
    ...editableKinds,
    [C.CampaignKind.Addon, '换购规则'],
    [C.CampaignKind.Flash, '秒杀配额'],
    [C.CampaignKind.Group, '拼团配额'],
  ];
  const modes = [
    ['campaigns', '优惠规则'],
    ['flash', '秒杀场次'],
    ['groups', '拼团记录'],
  ];
  const kinds = Object.fromEntries(allKinds);
  const isEditable = (row) => editableKinds.some(([kind]) => kind === row.kind);
  const percentage = (row) => [C.CampaignKind.Discount, C.CampaignKind.QuantityDiscount].includes(row.kind);
  const newState = () => ({
    items: [],
    total: 0,
    page: 1,
    loaded: false,
    keyword: '',
    status: '',
    kind: '',
    error: '',
  });
  let adapter = {},
    scope = '',
    revision = 0,
    mutation = 0,
    mode = 'campaigns',
    busy = false,
    mutating = false,
    draft = null,
    dirty = false;
  let collections = { campaigns: newState(), flash: newState(), groups: newState() };
  const currentScope = () => adapter.scope?.() || '';
  const authorized = () => Boolean(adapter.isAdmin?.());
  const draw = () => adapter.draw?.();
  const state = () => collections[mode];
  const blocked = () => busy || mutating;
  const call = (...args) => adapter.call(...args);
  function reset() {
    revision++;
    mutation++;
    scope = currentScope();
    mode = 'campaigns';
    busy = false;
    mutating = false;
    draft = null;
    dirty = false;
    collections = { campaigns: newState(), flash: newState(), groups: newState() };
  }
  function checkScope() {
    if (scope !== currentScope()) reset();
  }
  async function refresh() {
    checkScope();
    if (!authorized()) return false;
    const selected = mode,
      target = state(),
      ticket = ++revision,
      origin = scope;
    busy = true;
    target.error = '';
    try {
      let result;
      if (selected === 'campaigns') {
        const data = { page: target.page, page_size: 10, keyword: target.keyword, kind: target.kind };
        if (target.status) data.active = target.status === 'active';
        result = await call(C.Service.Campaign, C.Action.AdminList, { data });
      } else result = await call(selected === 'flash' ? C.Service.Flash : C.Service.Group, C.Action.List);
      if (ticket !== revision || origin !== currentScope() || !authorized()) return false;
      target.items = copy(result.items || []);
      target.total = selected === 'campaigns' ? Number(result.total ?? target.items.length) : target.items.length;
      target.loaded = true;
      if (selected === 'campaigns' && !target.items.length && target.total > 0 && target.page > 1) {
        target.page = Math.ceil(target.total / 10);
        busy = false;
        return await refresh();
      }
      return true;
    } catch (cause) {
      if (ticket === revision && origin === currentScope()) target.error = cause.message || '活动读取失败';
      return false;
    } finally {
      if (ticket === revision && origin === currentScope()) busy = false;
    }
  }
  function button(action, label, extra = '', primary = false) {
    return `<button type="button" class="${primary ? 'primary' : 'outline'}" data-pm-action="${action}" ${extra}${blocked() ? ' disabled' : ''}>${label}</button>`;
  }
  function heading() {
    return `<div class="admin-title"><div><h1>${draft ? (draft.originalID ? '编辑优惠规则' : '新建优惠规则') : '活动管理'}</h1><p>${draft ? '配置商品优惠与活动额度，保存后在商城活动中心展示。' : '集中维护优惠规则，查看秒杀场次与真实拼团进度。'}</p></div><div class="admin-title-actions">${draft ? button('back', '返回列表（放弃未保存修改）') : button('reload', '刷新') + (mode === 'campaigns' ? button('new', '新建优惠规则', '', true) : '')}</div></div>`;
  }
  function ruleText(row) {
    const parts = [];
    if (Number(row.threshold_cents) > 0) parts.push(`满 ¥${money(row.threshold_cents)}`);
    if (Number(row.min_quantity) > 1) parts.push(`满 ${row.min_quantity} 件`);
    if (percentage(row)) parts.push(`${Number(row.pay_rate_bps) / 1000} 折`);
    else if (Number(row.discount_cents) > 0) parts.push(`减 ¥${money(row.discount_cents)}`);
    return parts.join(' · ') || '由交易流程控制';
  }
  function toolbar() {
    const s = state();
    return `<form id="promotion-admin-search" class="admin-list-toolbar"><label class="admin-search"><input type="search" name="keyword" value="${esc(s.keyword)}" placeholder="搜索活动名称或编号" aria-label="搜索活动"></label>${mode === 'campaigns' ? `<label class="admin-status-filter">类型<select name="kind"><option value="">全部类型</option>${allKinds.map(([value, text]) => `<option value="${value}"${s.kind === value ? ' selected' : ''}>${text}</option>`).join('')}</select></label><label class="admin-status-filter">状态<select name="status"><option value="">全部状态</option><option value="active"${s.status === 'active' ? ' selected' : ''}>已启用</option><option value="paused"${s.status === 'paused' ? ' selected' : ''}>已停用</option></select></label>` : ''}<button class="outline" type="submit"${blocked() ? ' disabled' : ''}>查询</button></form>`;
  }
  function visibleRows() {
    const s = state();
    if (mode === 'campaigns') return s.items;
    const term = s.keyword.toLowerCase();
    return s.items.filter((row) =>
      [row.id, row.name, row.sku_id, row.activity_id, row.status].some((value) =>
        String(value ?? '')
          .toLowerCase()
          .includes(term),
      ),
    );
  }
  function status(row) {
    if (mode !== 'groups')
      return `<span class="admin-state ${row.active ? 'success' : 'neutral'}">${row.active ? '已启用' : '已停用'}</span>`;
    const label =
      { [C.GroupStatus.Open]: '进行中', [C.GroupStatus.Formed]: '已成团', [C.GroupStatus.Failed]: '未成团' }[
        row.status
      ] || row.status;
    return `<span class="admin-state ${row.status === C.GroupStatus.Formed ? 'success' : row.status === C.GroupStatus.Open ? 'pending' : 'neutral'}">${esc(label)}</span>`;
  }
  function table() {
    const s = state();
    const filtered = visibleRows();
    const total = mode === 'campaigns' ? s.total : filtered.length;
    const pages = Math.max(1, Math.ceil(total / 10));
    s.page = Math.min(s.page, pages);
    const items = mode === 'campaigns' ? filtered : filtered.slice((s.page - 1) * 10, s.page * 10);
    if (!s.loaded && !s.error)
      return (
        window.LumoSkeleton?.render('table', { count: 5, label: '正在读取活动' }) ||
        '<div class="admin-empty">正在读取活动</div>'
      );
    if (!s.loaded && s.error) return '';
    const headers =
      mode === 'campaigns'
        ? ['活动', '优惠规则', '活动额度', '状态', '操作']
        : mode === 'flash'
          ? ['秒杀场次', '商品 SKU', '关联配额', '状态']
          : ['拼团编号', '商品 SKU', '成团进度', '截止时间', '状态'];
    const body = items
      .map((row) => {
        if (mode === 'campaigns')
          return `<tr><td><strong>${esc(row.name || kinds[row.kind] || row.id)}</strong><small>${esc(row.id)}</small><small>${esc(kinds[row.kind] || row.kind)}</small></td><td><strong>${esc(ruleText(row))}</strong><small>${esc(row.description)}</small></td><td><strong>剩余 ${Math.max(0, Number(row.quota) - Number(row.held) - Number(row.used))}</strong><small>总 ${esc(row.quota)} · 预占 ${esc(row.held)} · 已用 ${esc(row.used)}</small></td><td>${status(row)}</td><td><div class="admin-actions">${isEditable(row) ? button('edit', '编辑', `data-pm-id="${esc(row.id)}"`) + button('duplicate', '复制', `data-pm-id="${esc(row.id)}"`) : '<span class="admin-muted">系统规则</span>'}</div></td></tr>`;
        if (mode === 'flash')
          return `<tr><td><strong>${esc(row.name || '秒杀场次')}</strong><small>${esc(row.id)}</small></td><td>${esc(row.sku_id)}</td><td>${esc(row.activity_id)}</td><td>${status(row)}</td></tr>`;
        return `<tr><td><strong>${esc(row.id)}</strong></td><td>${esc(row.sku_id)}</td><td><strong>已付 ${esc(row.paid)} / ${esc(row.required)} 人</strong><small>已占 ${esc(row.occupied)} 个席位</small></td><td>${esc(row.expires_at ? new Date(row.expires_at).toLocaleString('zh-CN') : '—')}</td><td>${status(row)}</td></tr>`;
      })
      .join('');
    return `<div class="admin-list-summary"><span>共 ${total} 条${mode === 'groups' ? '已加载记录' : '记录'}</span><span>${mode === 'groups' ? '服务端最近 100 条' : '服务端真实数据'}</span></div>${items.length ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table"><thead><tr>${headers.map((label) => `<th scope="col">${label}</th>`).join('')}</tr></thead><tbody>${body}</tbody></table></div>` : '<div class="admin-empty"><strong>暂无匹配的活动记录</strong><span>调整查询条件，或创建一条新的优惠规则。</span></div>'}<div class="admin-pagination"><span>${total ? `第 ${(s.page - 1) * 10 + 1}–${Math.min(s.page * 10, total)} 条` : '共 0 条记录'}</span><div>${button('prev', '上一页', s.page <= 1 ? 'disabled' : '')}<span>${s.page} / ${pages}</span>${button('next', '下一页', s.page >= pages ? 'disabled' : '')}</div></div>`;
  }
  function input(label, key, value, type = 'text', attributes = '', help = '') {
    return `<label class="field">${label}<input type="${type}" data-pm-field="${key}" value="${esc(value)}" ${attributes}${blocked() ? ' disabled' : ''}>${help ? `<small>${help}</small>` : ''}</label>`;
  }
  function editor() {
    const locked = Number(draft.held) > 0 || Number(draft.used) > 0;
    const configDisabled = locked ? 'disabled' : '';
    return `<form id="promotion-admin-form"><section class="admin-section"><div class="admin-section-heading"><div><h2>基本信息</h2><p>每位顾客在同一活动中限享一次；取消未付款订单后可重新参与。</p></div></div><div class="admin-form">${input('活动编号', 'id', draft.id, 'text', `${draft.originalID ? 'disabled' : ''} required maxlength="120"`, '唯一编号，保存后不可修改。')}${input('活动名称', 'name', draft.name, 'text', 'required maxlength="120"')}<label class="field wide">活动说明<textarea data-pm-field="description" maxlength="255"${blocked() ? ' disabled' : ''}>${esc(draft.description)}</textarea></label></div></section><section class="admin-section"><div class="admin-section-heading"><div><h2>优惠计算</h2><p>${locked ? '已有参与记录，优惠条件已锁定。需要改变规则时请复制为新活动。' : '金额以元填写；折扣填写支付折数，例如 9 表示9折、8.5表示8.5折。'}</p></div></div><div class="admin-form"><label class="field">活动类型<select data-pm-field="kind" ${configDisabled}${blocked() ? ' disabled' : ''}>${editableKinds.map(([kind, text]) => `<option value="${kind}"${draft.kind === kind ? ' selected' : ''}>${text}</option>`).join('')}</select></label>${input('商品金额门槛（元）', 'threshold', draft.threshold, 'number', `min="0" max="10000000" step="0.01" required ${configDisabled}`)}${input('最低购买件数', 'minQuantity', draft.minQuantity, 'number', `min="${draft.kind === C.CampaignKind.QuantityDiscount ? 2 : 1}" max="10" step="1" required ${configDisabled}`)}${percentage(draft) ? input('支付折数', 'payRate', draft.payRate, 'number', `min="0.001" max="9.999" step="0.001" required ${configDisabled}`) : input('优惠金额（元）', 'discount', draft.discount, 'number', `min="0.01" max="10000000" step="0.01" required ${configDisabled}`)}</div></section><section class="admin-section"><div class="admin-section-heading"><div><h2>活动额度与展示</h2><p>活动额度独立于商品库存，不会修改实际 SKU 库存。</p></div></div><div class="admin-form">${input('总活动额度（件）', 'quota', draft.quota, 'number', 'min="1" max="1000000000" step="1" required', `已预占 ${Number(draft.held) || 0} 件，已使用 ${Number(draft.used) || 0} 件；总额度不得低于两者之和。`)}<label class="field">展示状态<select data-pm-field="active"${blocked() ? ' disabled' : ''}><option value="true"${draft.active ? ' selected' : ''}>启用</option><option value="false"${!draft.active ? ' selected' : ''}>停用</option></select><small>停用后不再接受新参与，已产生的业务记录继续按流程处理。</small></label></div><div class="admin-form-footer"><span id="pm-save-status">${dirty ? '有未保存的修改' : '已与服务端配置同步'}</span>${button('save', '保存优惠规则', '', true)}</div></section></form>`;
  }
  function render() {
    checkScope();
    if (!authorized()) return '<div class="admin-error">请使用管理员账号登录后管理活动。</div>';
    const s = state();
    return `<div id="promotion-admin-root" class="pm-admin">${heading()}${s.error ? `<div class="admin-error" role="alert">${esc(s.error)}</div>` : ''}${draft ? editor() : `<div class="pm-tabs" role="group" aria-label="活动类型">${modes.map(([key, title]) => `<button type="button" data-pm-mode="${key}" class="${mode === key ? 'active' : ''}" aria-pressed="${mode === key}"${blocked() ? ' disabled' : ''}>${title}</button>`).join('')}</div>${mode === 'campaigns' ? '<p class="pm-note">可维护满减、折扣、多件优惠与直降活动。换购、秒杀及拼团配额为系统规则，在此只读展示。</p>' : `<p class="pm-note">${mode === 'flash' ? '查看后端已配置的秒杀场次。场次、商品与交易准入相关，本页尚未开放编辑。' : '查看顾客实际创建的拼团与付费进度。这里展示真实团记录，不作为活动模板编辑。'}</p>`}<section class="admin-list-panel">${toolbar()}${table()}</section>`}</div>`;
  }
  function edit(row, duplicate = false) {
    const initial = row || {
      id: '',
      name: '',
      description: '',
      kind: C.CampaignKind.Reduction,
      threshold_cents: 0,
      discount_cents: 100,
      pay_rate_bps: 9000,
      min_quantity: 1,
      quota: 1000,
      held: 0,
      used: 0,
      active: false,
      version: 0,
    };
    if (row && !isEditable(row)) return;
    draft = {
      ...copy(initial),
      originalID: duplicate ? '' : initial.id,
      id: duplicate ? '' : initial.id,
      name: duplicate ? initial.name + '（副本）' : initial.name,
      threshold: Number(initial.threshold_cents) / 100,
      discount: Number(initial.discount_cents) / 100,
      payRate: Number(initial.pay_rate_bps) / 1000,
      minQuantity: Number(initial.min_quantity) || 1,
    };
    if (duplicate) {
      draft.version = 0;
      draft.held = 0;
      draft.used = 0;
      draft.active = false;
    }
    dirty = !draft.originalID;
    state().error = '';
    draw();
    window.scrollTo?.(0, 0);
  }
  function markDirty() {
    dirty = true;
    const notice = document.querySelector('#pm-save-status');
    if (notice) notice.textContent = '有未保存的修改';
  }
  const cents = (value) => {
    const text = String(value).trim();
    if (!/^\d+(\.\d{1,2})?$/.test(text)) throw Error('金额最多填写两位小数。');
    const [whole, decimal = ''] = text.split('.');
    const result = Number(whole) * 100 + Number(decimal.padEnd(2, '0'));
    if (!Number.isSafeInteger(result) || result > 1000000000) throw Error('金额超出允许范围。');
    return result;
  };
  function payload() {
    if (!draft.id?.trim() || draft.id !== draft.id.trim() || draft.id.length > 120 || !draft.name?.trim())
      throw Error('请填写有效的活动编号和名称。');
    if (!Number.isSafeInteger(Number(draft.quota)) || Number(draft.quota) < 1 || Number(draft.quota) > 1000000000)
      throw Error('活动额度必须是1至10亿的整数。');
    if (Number(draft.quota) < Number(draft.held) + Number(draft.used))
      throw Error('总额度不得低于已预占和已使用数量。');
    const minQuantity = Number(draft.minQuantity);
    if (
      !Number.isInteger(minQuantity) ||
      minQuantity < (draft.kind === C.CampaignKind.QuantityDiscount ? 2 : 1) ||
      minQuantity > 10
    )
      throw Error('请填写有效的最低购买件数（1–10件，多件优惠至少2件）。');
    let rate = 0,
      discount = 0;
    if (percentage(draft)) {
      if (!/^\d+(\.\d{1,3})?$/.test(String(draft.payRate))) throw Error('支付折数最多填写三位小数。');
      rate = Math.round(Number(draft.payRate) * 1000);
      if (!Number.isSafeInteger(rate) || rate < 1 || rate >= 10000) throw Error('支付折数须大于0且小于10。');
    } else {
      discount = cents(draft.discount);
      if (discount < 1) throw Error('优惠金额须大于0。');
    }
    return {
      id: draft.id,
      data: {
        version: Number(draft.version) || 0,
        kind: draft.kind,
        name: draft.name.trim(),
        description: draft.description.trim(),
        threshold_cents: cents(draft.threshold),
        discount_cents: discount,
        pay_rate_bps: rate,
        min_quantity: minQuantity,
        quota: Number(draft.quota),
        active: Boolean(draft.active),
      },
    };
  }
  async function save() {
    let command;
    try {
      command = payload();
    } catch (cause) {
      state().error = cause.message;
      draw();
      return;
    }
    const ticket = ++mutation,
      origin = scope;
    // A read refresh may be re-entered by the page shell while this write is
    // pending. It must not cancel an accepted mutation or unlock its controls.
    mutating = true;
    revision++;
    busy = false;
    state().error = '';
    draw();
    const current = () => ticket === mutation && origin === currentScope() && authorized();
    try {
      const saved = await call(C.Service.Campaign, C.Action.Upsert, command);
      if (!current()) return;
      // Fence all reads started before the write committed. The following read
      // is authoritative for usage counters and list totals, while the write
      // response supplies the canonical draft and its new optimistic version.
      revision++;
      busy = false;
      edit(saved);
      dirty = false;
      adapter.notify?.('优惠规则已保存');
      await refresh();
    } catch (cause) {
      if (current()) state().error = cause.message || '活动保存失败';
    } finally {
      if (current()) {
        mutating = false;
        draw();
      }
    }
  }
  document.addEventListener('input', (event) => {
    checkScope();
    if (!authorized() || blocked() || !draft) return;
    const key = event.target.dataset?.pmField;
    if (!['id', 'name', 'description', 'threshold', 'discount', 'payRate', 'minQuantity', 'quota'].includes(key))
      return;
    if (draft.originalID && key === 'id') return;
    if (
      (Number(draft.held) > 0 || Number(draft.used) > 0) &&
      ['threshold', 'discount', 'payRate', 'minQuantity'].includes(key)
    )
      return;
    draft[key] = event.target.value;
    markDirty();
  });
  document.addEventListener('change', (event) => {
    checkScope();
    if (!authorized() || blocked() || !draft) return;
    const key = event.target.dataset?.pmField;
    if (
      key === 'kind' &&
      editableKinds.some(([value]) => value === event.target.value) &&
      !Number(draft.held) &&
      !Number(draft.used)
    ) {
      draft.kind = event.target.value;
      if (draft.kind === C.CampaignKind.QuantityDiscount && Number(draft.minQuantity) < 2) draft.minQuantity = 2;
      if (percentage(draft) && !Number(draft.payRate)) draft.payRate = 9;
      dirty = true;
      draw();
    }
    if (key === 'active') {
      draft.active = event.target.value === 'true';
      markDirty();
    }
  });
  document.addEventListener('click', async (event) => {
    const target = event.target.closest?.('[data-pm-action],[data-pm-mode]');
    if (!target) return;
    checkScope();
    if (!authorized() || blocked() || target.disabled) return;
    event.preventDefault();
    if (target.dataset.pmMode && modes.some(([key]) => key === target.dataset.pmMode)) {
      mode = target.dataset.pmMode;
      draft = null;
      await refresh();
      draw();
      return;
    }
    const action = target.dataset.pmAction;
    if (action === 'reload') {
      await refresh();
      draw();
      return;
    }
    if (action === 'new' && mode === 'campaigns') {
      edit();
      return;
    }
    if (mode === 'campaigns' && (action === 'edit' || action === 'duplicate')) {
      const row = state().items.find((item) => item.id === target.dataset.pmId);
      if (row) edit(row, action === 'duplicate');
      return;
    }
    if (action === 'back') {
      draft = null;
      dirty = false;
      state().error = '';
      draw();
      window.scrollTo?.(0, 0);
      return;
    }
    if (action === 'save' && mode === 'campaigns' && draft) {
      await save();
      return;
    }
    if (action === 'prev' || action === 'next') {
      const total = mode === 'campaigns' ? state().total : visibleRows().length;
      const page = state().page + (action === 'prev' ? -1 : 1);
      if (page < 1 || page > Math.max(1, Math.ceil(total / 10))) return;
      state().page = page;
      if (mode === 'campaigns') await refresh();
      draw();
    }
  });
  document.addEventListener('submit', async (event) => {
    if (!['promotion-admin-form', 'promotion-admin-search'].includes(event.target.id)) return;
    event.preventDefault();
    checkScope();
    if (!authorized() || blocked()) return;
    if (event.target.id === 'promotion-admin-form') {
      if (mode === 'campaigns' && draft) await save();
      return;
    }
    const data = Object.fromEntries(new FormData(event.target));
    state().keyword = String(data.keyword || '').trim();
    state().kind = String(data.kind || '');
    state().status = String(data.status || '');
    state().page = 1;
    if (mode === 'campaigns') await refresh();
    draw();
  });
  window.LumoPromotionAdmin = {
    configure(options) {
      adapter = options || {};
      checkScope();
    },
    refresh,
    render,
    reset,
    readDraft: () => (draft ? copy(draft) : null),
    isDirty: () => dirty,
  };
})();
