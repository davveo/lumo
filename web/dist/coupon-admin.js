(function () {
  'use strict';
  const C = window.LumoConstants;
  const B = window.LumoCouponBenefits;
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const todayDeadline = () => new Date(Date.now() + 30 * 86400000).toISOString();
  const dateInput = (value) => {
    if (!value) return '';
    const date = new Date(value);
    return Number.isNaN(date.getTime())
      ? ''
      : new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  };
  const money = (cents) =>
    (Number(cents || 0) / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const amountInput = (cents) => (Number(cents || 0) / 100).toFixed(2);
  const dateText = (value) => (value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '立即开放');
  let adapter = {},
    scope = '',
    revision = 0,
    items = [],
    total = 0,
    page = 1,
    keyword = '',
    active = '',
    draft = null,
    error = '',
    loaded = false,
    busy = false,
    pending = null,
    writing = false,
    mutation = 0;
  const currentScope = () => adapter.scope?.() || '';
  const authorized = () => Boolean(adapter.isAdmin?.());
  const draw = () => adapter.draw?.();
  const visible = () => {
    const root = document.querySelector('#coupon-admin-root');
    return Boolean(root && !root.hidden);
  };
  function reset() {
    revision++;
    scope = currentScope();
    items = [];
    total = 0;
    page = 1;
    keyword = '';
    active = '';
    draft = null;
    error = '';
    loaded = false;
    busy = false;
    pending = null;
    writing = false;
    mutation++;
  }
  function checkScope() {
    if (scope !== currentScope()) reset();
  }
  async function refresh() {
    checkScope();
    if (!authorized()) return false;
    if (pending) return pending;
    const ticket = ++revision,
      origin = scope;
    busy = true;
    error = '';
    const task = (async () => {
      try {
        let response = await adapter.call(C.Service.Coupon, C.Action.TemplatesAdmin, {
          data: { page, page_size: 10, keyword, active },
        });
        if (ticket !== revision || origin !== currentScope() || !authorized()) return false;
        const pages = Math.max(1, Math.ceil(Number(response.total || 0) / 10));
        if (page > pages) {
          page = pages;
          response = await adapter.call(C.Service.Coupon, C.Action.TemplatesAdmin, {
            data: { page, page_size: 10, keyword, active },
          });
          if (ticket !== revision || origin !== currentScope() || !authorized()) return false;
        }
        items = clone(response.items || []);
        total = Number(response.total || 0);
        loaded = true;
        return true;
      } catch (cause) {
        if (ticket === revision && origin === currentScope()) error = cause.message || '优惠券读取失败';
        return false;
      } finally {
        if (ticket === revision && origin === currentScope()) {
          busy = writing;
          pending = null;
        }
      }
    })();
    pending = task;
    return task;
  }
  const btn = (action, text, extra = '', primary = false) =>
    `<button type="button" class="${primary ? 'primary' : 'outline'}" data-ca-action="${action}" ${extra}${busy ? ' disabled' : ''}>${text}</button>`;
  const field = (label, key, value, options = {}) =>
    `<label class="field ${options.wide ? 'wide' : ''}">${label}<input data-ca-field="${key}" value="${esc(value)}" type="${options.type || 'text'}" ${options.readonly ? 'readonly' : ''} ${options.disabled || busy ? 'disabled' : ''} ${options.attrs || ''}>${options.help ? `<small>${options.help}</small>` : ''}</label>`;
  function state(row) {
    if (!row.active) return ['已停用', 'neutral'];
    if (new Date(row.expires_at).getTime() <= Date.now()) return ['领取已结束', 'neutral'];
    if (row.starts_at && new Date(row.starts_at).getTime() > Date.now()) return ['尚未开始', 'pending'];
    if (row.remaining <= 0) return ['已领完', 'neutral'];
    return ['领取中', 'success'];
  }
  function errorHTML() {
    return error ? `<div class="admin-error" role="alert">${esc(error)}</div>` : '';
  }
  function listView() {
    const pages = Math.max(1, Math.ceil(total / 10));
    return `<div class="admin-title"><div><h1>优惠券管理</h1><p>创建领券权益，配置领取时间、配额和会员条件。</p></div><div class="admin-title-actions">${btn('reload', '刷新')}${btn('new', '＋ 创建优惠券', '', true)}</div></div>${errorHTML()}<section class="admin-list-panel"><form id="coupon-admin-search" class="admin-list-toolbar"><label class="admin-search"><input type="search" name="keyword" value="${esc(keyword)}" placeholder="搜索券名称或模板编号" aria-label="搜索优惠券" maxlength="120" ${busy ? 'disabled' : ''}></label><label class="admin-status-filter">展示状态<select data-ca-filter="active" ${busy ? 'disabled' : ''}><option value="" ${active === '' ? 'selected' : ''}>全部优惠券</option><option value="true" ${active === 'true' ? 'selected' : ''}>已启用</option><option value="false" ${active === 'false' ? 'selected' : ''}>已停用</option></select></label><button class="outline" type="submit" ${busy ? 'disabled' : ''}>搜索</button></form><div class="admin-list-summary"><span>共 <strong>${total}</strong> 种优惠券</span><span>每人每种券限领 1 张</span></div>${
      !loaded && busy
        ? window.LumoSkeleton?.render('table', { count: 6, label: '正在读取优惠券配置' }) || ''
        : items.length
          ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table ca-table"><thead><tr><th scope="col">优惠券</th><th scope="col">权益 / 条件</th><th scope="col">领取时间</th><th scope="col">可领 / 已领取</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead><tbody>${items
              .map((row) => {
                const [label, tone] = state(row);
                return `<tr><td><strong>${esc(row.name)}</strong><small>${esc(row.id)}</small></td><td><strong class="ca-money">${esc(B?.face(row) || '¥' + money(row.amount_cents))}</strong><small>${esc(B?.condition(row) || '满 ¥' + money(row.min_cents) + ' 可用')} · 领取后 ${esc(row.valid_days)} 天</small><small>${row.min_points ? '可用积分 ≥ ' + esc(row.min_points) : ''}${row.min_points && row.min_growth ? ' · ' : ''}${row.min_growth ? '成长值 ≥ ' + esc(row.min_growth) : ''}${!row.min_points && !row.min_growth ? '所有登录用户可领' : ''}</small></td><td><small>${esc(dateText(row.starts_at))}</small><small>至 ${esc(dateText(row.expires_at))}</small></td><td><strong>${esc(row.remaining)} / ${esc(row.claimed_count || 0)}</strong><small>编辑时按增减量调整配额</small></td><td><span class="admin-state ${tone}">${label}</span></td><td><div class="admin-actions"><button data-ca-action="edit" data-ca-id="${esc(row.id)}" ${busy ? 'disabled' : ''}>编辑</button><button data-ca-action="copy" data-ca-id="${esc(row.id)}" ${busy ? 'disabled' : ''}>复制</button><button data-ca-action="toggle" data-ca-id="${esc(row.id)}" ${busy ? 'disabled' : ''}>${row.active ? '停用' : '启用'}</button></div></td></tr>`;
              })
              .join('')}</tbody></table></div>`
          : loaded
            ? '<div class="admin-empty"><strong>暂无匹配的优惠券</strong><span>调整筛选条件，或创建一张新的优惠券。</span></div>'
            : '<div class="admin-empty">读取失败，请刷新重试。</div>'
    }<div class="admin-pagination"><span>第 ${page} / ${pages} 页</span><div>${btn('prev', '上一页', page <= 1 ? 'disabled ' : '')}${btn('next', '下一页', page >= pages ? 'disabled ' : '')}</div></div></section><p class="admin-note ca-list-note">停用仅停止新的领取，顾客已领取的优惠券仍按原有金额、门槛和到期时间使用。积分与成长值是领取资格，不会在领券时扣除。</p>`;
  }
  function previewBody() {
    const rule = {
      coupon_type: draft.coupon_type,
      discount_bps: Math.round(Number(draft.discount_rate || 0) * 1000),
      max_discount_cents: Math.round(Number(draft.max_discount || 0) * 100),
      amount_cents: Math.round(Number(draft.amount || 0) * 100),
      min_cents: Math.round(Number(draft.minimum || 0) * 100),
      scope_type: draft.scope_type,
      scope_values: String(draft.scope_values_text || '')
        .split(/[\n,，]/)
        .map((value) => value.trim())
        .filter(Boolean),
    };

    return `<span>优惠券预览</span><div><small>领券权益</small><strong>${esc([C.CouponType.Discount, C.CouponType.ThresholdDiscount].includes(draft.coupon_type) ? draft.discount_rate + '折' : '¥' + (draft.amount || '0'))}</strong><b>${esc(B?.condition(rule) || '满 ¥' + (draft.minimum || '0') + ' 可用')}</b><p>${esc(B?.scope(rule) || '普通商品通用')}</p><h2>${esc(draft.name || '优惠券名称')}</h2><p>领取后 ${esc(draft.valid_days || '—')} 天有效</p><i>${draft.active ? '启用后可在领券中心领取' : '保存为停用，不对顾客展示'}</i></div><p>实际领取资格、剩余配额和使用规则由服务端校验。</p>`;
  }
  function editView() {
    const existing = draft.version > 0,
      locked = existing && draft.claimed_count > 0;
    return `<div class="admin-title"><div><h1>${existing ? '编辑优惠券' : '创建优惠券'}</h1><p>填写优惠权益与领取规则，保存后在领券中心展示。</p></div><div class="admin-title-actions">${btn('back', '返回列表')}</div></div>${errorHTML()}<form id="coupon-admin-save" class="ca-editor"><div class="ca-editor-sections"><section class="admin-section"><div class="admin-section-heading"><div><h2>基本信息与权益</h2><p>已被领取的券会保留原始权益；调整权益请复制为新券。</p></div></div><div class="admin-form">${field('券名称', 'name', draft.name, { wide: true, attrs: 'required maxlength="120"' })}${field('模板编号', 'id', draft.id, { wide: true, readonly: existing, help: '用于营销弹窗关联优惠券，创建后不能修改。', attrs: 'required maxlength="120"' })}<label class="field wide">优惠券类型<select data-ca-field="coupon_type" ${locked || busy ? 'disabled' : ''}>${[
      [C.CouponType.Reduction, '满减券'],
      [C.CouponType.Cash, '无门槛券'],
      [C.CouponType.Discount, '折扣券'],
      [C.CouponType.ThresholdDiscount, '满额折扣券'],
    ]
      .map(
        ([value, name]) => `<option value="${value}" ${draft.coupon_type === value ? 'selected' : ''}>${name}</option>`,
      )
      .join(
        '',
      )}</select><small>折扣按适用商品金额计算；券规则领取后冻结。</small></label>${[C.CouponType.Discount, C.CouponType.ThresholdDiscount].includes(draft.coupon_type) ? field('折扣（折）', 'discount_rate', draft.discount_rate, { type: 'number', disabled: locked, attrs: 'required min="0.001" max="9.999" step="0.001"', help: '如 8.5 表示支付85%，优惠15%。' }) + field('最大优惠（元）', 'max_discount', draft.max_discount, { type: 'number', disabled: locked, attrs: 'required min="0" step="0.01"', help: '0 表示不单独设置封顶。' }) : field('优惠金额（元）', 'amount', draft.amount, { type: 'number', disabled: locked, attrs: 'required min="0.01" step="0.01"' })}<label class="field">使用范围<select data-ca-field="scope_type" ${locked || busy ? 'disabled' : ''}>${[
      [C.CouponScope.All, '普通商品通用'],
      [C.CouponScope.Category, '指定品类'],
      [C.CouponScope.Product, '指定商品'],
    ]
      .map(
        ([value, name]) => `<option value="${value}" ${draft.scope_type === value ? 'selected' : ''}>${name}</option>`,
      )
      .join(
        '',
      )}</select></label>${draft.scope_type !== C.CouponScope.All ? `<label class="field wide">${draft.scope_type === C.CouponScope.Category ? '适用品类名称' : '适用商品编号'}<textarea data-ca-field="scope_values_text" rows="3" maxlength="7600" ${locked || busy ? 'disabled' : ''}>${esc(draft.scope_values_text)}</textarea><small>每行一项，最多50项；品类名称须与商品分类一致，商品编号使用商品管理里的编号。</small></label>` : ''}${field('使用门槛（元）', 'minimum', draft.minimum, { type: 'number', disabled: locked || [C.CouponType.Cash, C.CouponType.Discount].includes(draft.coupon_type), attrs: 'required min="0" step="0.01"', help: '无门槛券和折扣券无需填写；满减或满额折扣按适用范围内金额判断。' })}${field('领取后有效天数', 'valid_days', draft.valid_days, { type: 'number', disabled: locked, attrs: 'required min="1" max="3660" step="1"' })}<div class="field"><span>每人限领</span><div class="ca-static">每种优惠券 1 张</div><small>由后端按用户与模板唯一校验。</small></div></div>${locked ? '<p class="ca-lock-note">此券已有领取记录，优惠金额、使用门槛和有效天数已锁定。复制优惠券可创建新的权益。</p>' : ''}</section><section class="admin-section"><div class="admin-section-heading"><div><h2>领取时间与配额</h2><p>领取结束时间与顾客领券后的使用到期时间分别计算。</p></div></div><div class="admin-form">${field('开始领取时间', 'starts_at', draft.starts_at, { type: 'datetime-local', help: '留空表示立即开放。' })}${field('领取结束时间', 'expires_at', draft.expires_at, { type: 'datetime-local', attrs: 'required' })}${existing ? `<div class="field"><span>当前可领数量</span><div class="ca-static">${esc(draft.remaining)} 张 · 已领取 ${esc(draft.claimed_count)} 张</div><small>以服务端实际剩余配额为准。</small></div>${field('本次配额增减', 'stock_delta', draft.stock_delta, { type: 'number', attrs: 'required step="1"', help: '增加填正数，减少填负数；不覆盖期间顾客已领取的数量。' })}` : field('初始可领数量', 'remaining', draft.remaining, { type: 'number', attrs: 'required min="0" step="1"' })}</div></section><section class="admin-section"><div class="admin-section-heading"><div><h2>领取条件与展示</h2><p>积分与成长值两项同时设置时，顾客需同时满足。</p></div></div><div class="admin-form">${field('最低可用积分', 'min_points', draft.min_points, { type: 'number', attrs: 'required min="0" step="1"', help: '填写 0 不限制，领取不扣积分。' })}${field('最低会员成长值', 'min_growth', draft.min_growth, { type: 'number', attrs: 'required min="0" step="1"', help: '填写 0 不限制。' })}<label class="ca-check wide"><input type="checkbox" data-ca-field="active" ${draft.active ? 'checked' : ''} ${busy ? 'disabled' : ''}><span>启用并展示在领券中心<small>停用不会作废顾客已领取的券。</small></span></label></div></section><div class="ca-savebar"><span>修改只影响后续领取</span><div>${btn('back', '取消')}<button type="submit" class="primary" ${busy ? 'disabled' : ''}>${busy ? '正在保存…' : '保存优惠券'}</button></div></div></div><aside class="admin-section ca-preview">${previewBody()}</aside></form>`;
  }
  function render() {
    checkScope();
    if (!authorized()) return '<div class="admin-error">请使用管理员账号登录。</div>';
    return `<div id="coupon-admin-root" class="ca-admin">${draft ? editView() : listView()}</div>`;
  }
  function edit(row, duplicate = false) {
    draft = {
      id: 'coupon-' + crypto.randomUUID(),
      name: '',
      coupon_type: C.CouponType.Reduction,
      discount_rate: '8.5',
      max_discount: '0',
      scope_type: C.CouponScope.All,
      scope_values_text: '',
      amount: '10.00',
      minimum: '100.00',
      valid_days: '7',
      remaining: '100',
      stock_delta: '0',
      starts_at: '',
      expires_at: dateInput(todayDeadline()),
      min_points: '0',
      min_growth: '0',
      active: true,
      version: 0,
      claimed_count: 0,
    };
    if (row)
      Object.assign(draft, {
        ...clone(row),
        coupon_type: B?.type(row) || C.CouponType.Reduction,
        discount_rate: String(Number(row.discount_bps || 8500) / 1000),
        max_discount: amountInput(row.max_discount_cents || 0),
        scope_type: row.scope_type || C.CouponScope.All,
        scope_values_text: (row.scope_values || []).join('\n'),
        amount: amountInput(row.amount_cents),
        minimum: amountInput(row.min_cents),
        valid_days: String(row.valid_days),
        remaining: String(row.remaining),
        starts_at: dateInput(row.starts_at),
        expires_at: dateInput(row.expires_at),
        min_points: String(row.min_points || 0),
        min_growth: String(row.min_growth || 0),
        version: Number(row.version || 0),
        claimed_count: Number(row.claimed_count || 0),
      });
    if (duplicate) {
      draft.id = 'coupon-' + crypto.randomUUID();
      draft.name += '（副本）';
      draft.version = 0;
      draft.claimed_count = 0;
      draft.remaining = '100';
      draft.active = false;
      if (new Date(draft.expires_at).getTime() <= Date.now()) {
        draft.starts_at = '';
        draft.expires_at = dateInput(todayDeadline());
      }
    }
    error = '';
    draw();
    window.scrollTo?.(0, 0);
  }
  function parseMoney(value, label, positive) {
    const text = String(value).trim();
    if (!/^\d+(?:\.\d{1,2})?$/.test(text)) throw Error(label + '须为最多两位小数的非负金额');
    const parts = text.split('.');
    const amount = Number(parts[0]) * 100 + Number((parts[1] || '').padEnd(2, '0'));
    if (!Number.isSafeInteger(amount) || (positive && amount <= 0)) throw Error(label + '超出范围或不大于 0');
    return amount;
  }
  function integer(value, label, minimum = 0, maximum = Number.MAX_SAFE_INTEGER) {
    if (!/^-?\d+$/.test(String(value))) throw Error(label + '须为整数');
    const amount = Number(value);
    if (!Number.isSafeInteger(amount) || amount < minimum || amount > maximum) throw Error(label + '超出允许范围');
    return amount;
  }
  function payload(value) {
    const existing = value.version > 0;
    if (
      !value.id ||
      value.id.trim() !== value.id ||
      /\s/.test(value.id) ||
      new TextEncoder().encode(value.id).length > 120
    )
      throw Error('模板编号须为 1–120 字节，不能包含空白');
    if (!value.name.trim() || [...value.name.trim()].length > 120) throw Error('请填写 1–120 字的券名称');
    const starts = value.starts_at ? new Date(value.starts_at) : null,
      ends = new Date(value.expires_at);
    if (Number.isNaN(ends.getTime()) || (starts && (Number.isNaN(starts.getTime()) || starts >= ends)))
      throw Error('请填写正确的领取时间，开始时间须早于结束时间');
    if (!existing && ends.getTime() <= Date.now()) throw Error('新券的领取结束时间须晚于当前时间');
    const rateType = [C.CouponType.Discount, C.CouponType.ThresholdDiscount].includes(value.coupon_type);
    const data = {
      coupon_type: value.coupon_type || C.CouponType.Reduction,
      discount_bps: rateType ? Math.round(Number(value.discount_rate) * 1000) : 0,
      max_discount_cents: rateType ? parseMoney(value.max_discount, '最大优惠', false) : 0,
      scope_type: value.scope_type || C.CouponScope.All,
      scope_values:
        value.scope_type && value.scope_type !== C.CouponScope.All
          ? [
              ...new Set(
                String(value.scope_values_text || '')
                  .split(/[\n,，]/)
                  .map((v) => v.trim())
                  .filter(Boolean),
              ),
            ]
          : [],
      name: value.name.trim(),
      amount_cents: rateType ? 0 : parseMoney(value.amount, '优惠金额', true),
      min_cents: parseMoney(value.minimum, '使用门槛', false),
      valid_days: integer(value.valid_days, '有效天数', 1, 3660),
      active: Boolean(value.active),
      starts_at: starts ? starts.toISOString() : null,
      expires_at: ends.toISOString(),
      min_points: integer(value.min_points, '积分门槛'),
      min_growth: integer(value.min_growth, '成长值门槛'),
      version: value.version,
    };
    if (rateType && (!Number.isInteger(data.discount_bps) || data.discount_bps < 1 || data.discount_bps >= 10000))
      throw Error('折扣请填写大于0且小于10的折数，例如8.5折');
    if (data.scope_type !== C.CouponScope.All && !data.scope_values.length) throw Error('请填写适用品类或商品编号');
    if (data.coupon_type === C.CouponType.Reduction && data.min_cents < 1)
      throw Error('满减券请填写消费门槛；不限制金额请选择无门槛券');
    if (existing) data.stock_delta = integer(value.stock_delta, '配额增减', -Number.MAX_SAFE_INTEGER);
    else data.remaining = integer(value.remaining, '初始可领数量');
    return { id: value.id, data };
  }
  async function save(value) {
    let command;
    try {
      command = payload(value);
    } catch (cause) {
      error = cause.message;
      draw();
      return false;
    }
    const origin = scope,
      ticket = ++mutation;
    writing = true;
    busy = true;
    error = '';
    draw();
    try {
      await adapter.call(C.Service.Coupon, C.Action.TemplateUpsert, command);
      if (ticket !== mutation || origin !== currentScope() || !authorized()) return false;
      draft = null;
      if (pending) await pending;
      const refreshed = await refresh();
      if (origin !== currentScope() || !authorized()) return false;
      adapter.notify?.(refreshed ? '优惠券已保存' : '优惠券已保存，列表读取失败，请刷新核对');
      return true;
    } catch (cause) {
      if (ticket === mutation && origin === currentScope()) error = cause.message || '优惠券保存失败';
      return false;
    } finally {
      if (origin === currentScope() && authorized()) {
        writing = false;
        busy = Boolean(pending);
        draw();
      }
    }
  }
  document.addEventListener('input', (event) => {
    checkScope();
    if (!authorized() || busy || !visible() || !draft) return;
    const key = event.target.dataset?.caField;
    if (
      ![
        'id',
        'name',
        'coupon_type',
        'discount_rate',
        'max_discount',
        'scope_type',
        'scope_values_text',
        'amount',
        'minimum',
        'valid_days',
        'remaining',
        'stock_delta',
        'starts_at',
        'expires_at',
        'min_points',
        'min_growth',
        'active',
      ].includes(key)
    )
      return;
    if (draft.version > 0 && key === 'id') return;
    if (
      draft.claimed_count > 0 &&
      [
        'amount',
        'minimum',
        'valid_days',
        'coupon_type',
        'discount_rate',
        'max_discount',
        'scope_type',
        'scope_values_text',
      ].includes(key)
    )
      return;
    draft[key] = event.target.type === 'checkbox' ? event.target.checked : event.target.value;
    if (['coupon_type', 'scope_type'].includes(key)) {
      if ([C.CouponType.Cash, C.CouponType.Discount].includes(draft.coupon_type)) draft.minimum = '0';
      draw();
      document.querySelector(`[data-ca-field="${key}"]`)?.focus({ preventScroll: true });
      return;
    }
    const preview = document.querySelector('#coupon-admin-root .ca-preview');
    if (preview) preview.innerHTML = previewBody();
  });
  document.addEventListener('change', async (event) => {
    checkScope();
    if (!authorized() || busy || !visible()) return;
    if (event.target.dataset?.caFilter === 'active') {
      active = event.target.value;
      page = 1;
      const loading = refresh();
      draw();
      await loading;
      draw();
    } else if (event.target.dataset?.caField === 'active' && draft) draft.active = event.target.checked;
  });
  document.addEventListener('click', async (event) => {
    const target = event.target.closest?.('[data-ca-action]');
    if (!target) return;
    event.preventDefault();
    checkScope();
    if (!authorized() || busy || !visible() || target.disabled) return;
    const action = target.dataset.caAction;
    if (action === 'new') {
      edit();
      return;
    }
    if (action === 'back') {
      draft = null;
      error = '';
      draw();
      window.scrollTo?.(0, 0);
      return;
    }
    if (['edit', 'copy', 'toggle'].includes(action)) {
      const row = items.find((row) => row.id === target.dataset.caId);
      if (!row) return;
      if (action === 'edit' || action === 'copy') edit(row, action === 'copy');
      else {
        edit(row);
        const value = clone(draft);
        value.active = !row.active;
        await save(value);
      }
      return;
    }
    if (action === 'prev' && page > 1) page--;
    else if (action === 'next' && page < Math.ceil(total / 10)) page++;
    else if (action !== 'reload') return;
    const loading = refresh();
    draw();
    await loading;
    draw();
  });
  document.addEventListener('submit', async (event) => {
    if (!['coupon-admin-save', 'coupon-admin-search'].includes(event.target.id)) return;
    event.preventDefault();
    checkScope();
    if (!authorized() || busy || !visible()) return;
    if (event.target.id === 'coupon-admin-save') {
      if (draft) await save(clone(draft));
      return;
    }
    keyword = String(new FormData(event.target).get('keyword') || '').trim();
    page = 1;
    const loading = refresh();
    draw();
    await loading;
    draw();
  });
  window.addEventListener('lumo:logout', reset);
  window.LumoCouponAdmin = {
    configure(options) {
      adapter = options || {};
      reset();
    },
    refresh,
    render,
    reset,
    readDraft() {
      return draft ? clone(draft) : null;
    },
  };
})();
