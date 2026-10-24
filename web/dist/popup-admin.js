(function () {
  'use strict';
  const C = window.LumoConstants;
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const copy = (value) => JSON.parse(JSON.stringify(value));
  const segments = [
    [C.PopupSegment.NewGuest, '首次访客'],
    [C.PopupSegment.ReturningGuest, '再次来访的游客'],
    [C.PopupSegment.NewMember, '新会员'],
    [C.PopupSegment.ReturningMember, '老会员'],
    [C.PopupSegment.GoldMember, '高成长值会员'],
    [C.PopupSegment.Guest, '所有未登录访客'],
    [C.PopupSegment.Member, '所有已登录会员'],
    [C.PopupSegment.All, '所有顾客'],
  ];
  const placements = [
    [C.PopupPlacement.Home, '商城首页'],
    [C.PopupPlacement.Category, '分类与搜索'],
    [C.PopupPlacement.Product, '商品详情'],
    [C.PopupPlacement.Coupons, '领券中心'],
  ];
  const statuses = {
    [C.PopupStatus.Draft]: '草稿',
    [C.PopupStatus.Published]: '已启用',
    [C.PopupStatus.Paused]: '已暂停',
  };
  const defaults = () => ({
    id: '',
    name: '',
    status: C.PopupStatus.Draft,
    priority: 100,
    starts_at: '',
    ends_at: '',
    placements: [C.PopupPlacement.Home],
    audience: {
      segments: [C.PopupSegment.NewMember],
      new_member_days: 7,
      min_growth: 0,
      max_growth: 0,
      min_points: 0,
      max_points: 0,
      include_user_ids: [],
      exclude_user_ids: [],
      interest_categories: [],
    },
    frequency: {
      max_per_user: 1,
      max_per_day: 1,
      cooldown_hours: 24,
      once_per_session: true,
      close_cooldown_hours: 24,
      stop_after_claim: true,
    },
    content: {
      title: '送你一份专属好礼',
      subtitle: '精选优惠，为你而来',
      body: '领取后可在领券中心查看使用范围。',
      theme: C.PopupTheme.Red,
      coupon_template_id: '',
      button_text: '立即领取',
      action_type: C.PopupAction.ClaimCoupon,
      action_target: '#/coupons',
      auto_close_seconds: 0,
      delay_seconds: 1,
    },
  });
  let adapter = {},
    scope = '',
    revision = 0,
    rows = [],
    coupons = [],
    settings = null,
    draft = null,
    dirty = false,
    busy = false,
    error = '',
    filter = '',
    search = '',
    page = 1;
  const currentScope = () => adapter.scope?.() || '';
  const authorized = () => Boolean(adapter.isAdmin?.());
  const call = (...args) => adapter.call(...args);
  const draw = () => adapter.draw?.();
  function reset() {
    revision++;
    scope = currentScope();
    rows = [];
    coupons = [];
    settings = null;
    draft = null;
    dirty = false;
    busy = false;
    error = '';
    filter = '';
    search = '';
    page = 1;
  }
  function checkScope() {
    if (scope !== currentScope()) reset();
  }
  async function refresh() {
    checkScope();
    if (!authorized()) return false;
    const ticket = ++revision,
      origin = scope;
    busy = true;
    error = '';
    try {
      const result = await Promise.allSettled([
        call(C.Service.Content, C.Action.PopupList),
        call(C.Service.Coupon, C.Action.TemplatesSnapshot, { data: { page: 1, page_size: 5000 } }),
      ]);
      if (ticket !== revision || origin !== currentScope() || !authorized()) return false;
      if (result[0].status === 'rejected') throw result[0].reason;
      rows = copy(result[0].value.items || []);
      settings = copy(result[0].value.settings || { enabled: true, max_per_session: 1, min_gap_seconds: 60 });
      coupons = result[1].status === 'fulfilled' ? copy(result[1].value.items || []) : [];
      if (result[1].status === 'rejected')
        error = '优惠券列表读取失败，仍可填写已知的券模板编号。' + (result[1].reason.message || '');
      return true;
    } catch (cause) {
      if (ticket === revision && origin === currentScope()) error = cause.message || '弹窗配置读取失败';
      return false;
    } finally {
      if (ticket === revision && origin === currentScope()) busy = false;
    }
  }
  function button(action, label, extra = '', primary = false) {
    return `<button type="button" class="pa-button${primary ? ' pa-primary' : ''}" data-pa-action="${action}" ${extra}${busy ? ' disabled' : ''}>${label}</button>`;
  }
  function field(label, path, value, type = 'text', help = '', attributes = '') {
    return `<label class="pa-field">${label}<input type="${type}" data-pa-field="${path}" value="${esc(value)}" ${attributes}${busy ? ' disabled' : ''}>${help ? `<small>${help}</small>` : ''}</label>`;
  }
  function textarea(label, path, value, help = '', list = false) {
    return `<label class="pa-field">${label}<textarea ${list ? 'data-pa-list' : 'data-pa-field'}="${path}" rows="2" maxlength="12000"${busy ? ' disabled' : ''}>${esc(Array.isArray(value) ? value.join('\n') : value)}</textarea>${help ? `<small>${help}</small>` : ''}</label>`;
  }
  function select(label, path, value, options) {
    return `<label class="pa-field">${label}<select data-pa-field="${path}"${busy ? ' disabled' : ''}>${options.map(([key, text]) => `<option value="${key}"${key === value ? ' selected' : ''}>${text}</option>`).join('')}</select></label>`;
  }
  function toggle(label, path, value, help = '') {
    return `<label class="pa-check"><input type="checkbox" data-pa-field="${path}"${value ? ' checked' : ''}${busy ? ' disabled' : ''}><span>${label}${help ? `<small>${help}</small>` : ''}</span></label>`;
  }
  const money = (value) => (Number(value || 0) / 100).toLocaleString('zh-CN', { maximumFractionDigits: 2 });
  const dateInput = (value) => {
    if (!value) return '';
    const date = new Date(value);
    return Number.isNaN(date.getTime())
      ? ''
      : new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  };
  function audienceText(campaign) {
    const selected = campaign.audience?.segments || [];
    return (
      segments
        .filter(([key]) => selected.includes(key))
        .map(([, text]) => text)
        .join(' / ') || '尚未选择人群'
    );
  }
  function couponFor(campaign) {
    return coupons.find((coupon) => coupon.id === campaign.content?.coupon_template_id);
  }
  function preview() {
    if (!draft) return '';
    const content = draft.content,
      coupon = couponFor(draft),
      isCoupon = content.action_type === C.PopupAction.ClaimCoupon;
    const theme = [C.PopupTheme.Red, C.PopupTheme.Peach, C.PopupTheme.Plum].includes(content.theme)
      ? content.theme
      : C.PopupTheme.Red;
    return `<div class="pa-preview-stage"><div class="pa-preview-card pa-theme-${theme}"><span class="pa-preview-spark">✦</span><p>${esc(content.subtitle || '专属优惠')}</p><h2>${esc(content.title || '填写弹窗标题')}</h2>${isCoupon ? `<div class="pa-preview-ticket"><div><span>¥</span><strong>${coupon ? money(coupon.amount_cents ?? coupon.amount) : '—'}</strong></div><section><b>${coupon ? `满 ${money(coupon.min_cents ?? coupon.minimum_cents ?? coupon.minimum)} 元可用` : '请选择优惠券'}</b><span>${esc(coupon?.name || content.coupon_template_id || '优惠券名称')}</span></section></div>` : `<div class="pa-preview-message">${esc(content.body || '填写活动说明')}</div>`}<p class="pa-preview-description">${esc(isCoupon ? content.body : '按配置条件展示专属内容')}</p><div class="pa-preview-cta">${esc(content.button_text || (isCoupon ? '立即领取' : '前往查看'))}</div><small>${coupon?.expires_at ? `活动截止 ${esc(new Date(coupon.expires_at).toLocaleDateString('zh-CN'))}` : '权益详情以实际页面为准'}</small></div><span class="pa-preview-close" aria-hidden="true">×</span></div><p class="pa-preview-note">样式预览 · 不触发展示、不发放优惠券</p><div class="pa-preview-rules"><strong>谁会看到</strong><p>${esc(audienceText(draft))}</p><span>同时满足所配条件；同一时刻优先展示优先级更高的弹窗。</span><strong>展示频次</strong><p>每人最多 ${esc(draft.frequency.max_per_user)} 次 · 每天最多 ${esc(draft.frequency.max_per_day)} 次</p><span>全局频次与单个弹窗频次同时生效。</span></div>`;
  }
  function listView() {
    const visible = rows.filter(
        (row) =>
          (!filter || row.status === filter) &&
          (!search || `${row.name} ${row.id}`.toLowerCase().includes(search.toLowerCase())),
      ),
      pages = Math.max(1, Math.ceil(visible.length / 8));
    page = Math.max(1, Math.min(page, pages));
    const totals = rows.reduce(
      (value, row) => {
        for (const key of ['impressions', 'clicks', 'claims']) value[key] += Number(row.stats?.[key] || 0);
        return value;
      },
      { impressions: 0, clicks: 0, claims: 0 },
    );
    return `<div class="pa-metrics">${[
      ['已启用弹窗', rows.filter((row) => row.status === C.PopupStatus.Published).length],
      ['实际展示', totals.impressions],
      ['点击次数', totals.clicks],
      ['领取成功', totals.claims],
    ]
      .map(([label, value]) => `<div><span>${label}</span><strong>${value.toLocaleString('zh-CN')}</strong></div>`)
      .join('')}</div>
      ${settings ? `<section class="pa-card pa-global"><div><h2>全局展示控制</h2><p>进入页面只匹配一条弹窗，关闭后不连续弹出下一条。</p></div><form id="popup-settings-form">${toggle('开启营销弹窗', 'settings.enabled', settings.enabled)}${field('每次会话最多展示', 'settings.max_per_session', settings.max_per_session, 'number', '', 'min="1" max="10" step="1"')}${field('两次展示最小间隔（秒）', 'settings.min_gap_seconds', settings.min_gap_seconds, 'number', '', 'min="0" max="86400" step="1"')}${button('settings', '保存全局设置')}</form></section>` : ''}
      <section class="pa-card"><div class="pa-list-toolbar"><h2>全部弹窗 <small>${rows.length}</small></h2><input type="search" data-pa-search value="${esc(search)}" placeholder="搜索名称或编号" aria-label="搜索弹窗"><select data-pa-filter aria-label="按状态筛选"><option value="">全部状态</option>${Object.entries(
        statuses,
      )
        .map(([key, text]) => `<option value="${key}"${filter === key ? ' selected' : ''}>${text}</option>`)
        .join('')}</select></div>
      <div class="pa-table-scroll"><table class="pa-table"><thead><tr><th>弹窗 / 优先级</th><th>目标人群</th><th>生效时间</th><th>展示 / 领取</th><th>状态</th><th>操作</th></tr></thead><tbody>${
        visible
          .slice((page - 1) * 8, page * 8)
          .map(
            (row) =>
              `<tr><td><strong>${esc(row.name)}</strong><small>${esc(row.id)} · 优先级 ${esc(row.priority)}</small></td><td>${esc(audienceText(row))}<small>${(
                row.placements || []
              )
                .map((key) => placements.find((value) => value[0] === key)?.[1] || key)
                .map(esc)
                .join(
                  '、',
                )}</small></td><td>${esc(row.starts_at ? new Date(row.starts_at).toLocaleString('zh-CN') : '即刻生效')}<small>${esc(row.ends_at ? new Date(row.ends_at).toLocaleString('zh-CN') : '无截止时间')}</small></td><td>${Number(row.stats?.impressions || 0)} / ${Number(row.stats?.claims || 0)}</td><td><span class="pa-status pa-status-${esc(row.status)}">${statuses[row.status] || esc(row.status)}</span></td><td>${button('edit', '编辑', `data-pa-id="${esc(row.id)}"`)}${button('duplicate', '复制', `data-pa-id="${esc(row.id)}"`)}</td></tr>`,
          )
          .join('') || '<tr><td colspan="6" class="pa-empty">暂无匹配的弹窗，点击“新建弹窗”开始配置。</td></tr>'
      }</tbody></table></div><div class="pa-pagination"><span>共 ${visible.length} 条 · 第 ${page} / ${pages} 页</span>${button('prev', '上一页', page <= 1 ? 'disabled' : '')}${button('next', '下一页', page >= pages ? 'disabled' : '')}</div></section>`;
  }
  function editView() {
    const a = draft.audience,
      f = draft.frequency,
      c = draft.content;
    return `<div class="pa-editor-layout"><form id="popup-campaign-form" class="pa-editor-form">
      <section class="pa-card"><div class="pa-section-title"><b>01</b><div><h2>基本信息</h2><p>先确定活动名称、投放位置和生效时间。</p></div></div><div class="pa-fields">${field('弹窗名称', 'name', draft.name, 'text', '仅后台可见', 'required maxlength="80"')}${field('优先级', 'priority', draft.priority, 'number', '数值越大，越优先展示', 'min="0" max="1000" step="1" required')}${field('开始时间', 'starts_at', dateInput(draft.starts_at), 'datetime-local', '留空即刻生效；按当前设备时区显示')}${field('结束时间', 'ends_at', dateInput(draft.ends_at), 'datetime-local', '留空为长期有效')}</div><fieldset class="pa-choice-group"><legend>展示页面（至少选择一个）</legend>${placements.map(([key, text]) => `<label class="pa-choice"><input type="checkbox" data-pa-placement="${key}"${draft.placements.includes(key) ? ' checked' : ''}>${text}</label>`).join('')}</fieldset></section>
      <section class="pa-card"><div class="pa-section-title"><b>02</b><div><h2>目标人群与条件</h2><p>选中的人群满足任意一项即可；下方附加条件需要同时满足。</p></div></div><fieldset class="pa-choice-group"><legend>目标人群</legend>${segments.map(([key, text]) => `<label class="pa-choice"><input type="checkbox" data-pa-segment="${key}"${a.segments.includes(key) ? ' checked' : ''}>${text}</label>`).join('')}</fieldset><div class="pa-fields">${field('新会员认定天数', 'audience.new_member_days', a.new_member_days, 'number', '按真实账号注册时间判断；超过此天数为老会员', 'min="1" max="365" step="1" required')}${textarea('兴趣品类', 'audience.interest_categories', a.interest_categories, '每行一个商品品类；匹配任意一个即可，留空不限制。', true)}${field('最低成长值', 'audience.min_growth', a.min_growth, 'number', '', 'min="0" step="1" required')}${field('最高成长值', 'audience.max_growth', a.max_growth, 'number', '0 表示不限制', 'min="0" step="1" required')}${field('最低可用积分', 'audience.min_points', a.min_points, 'number', '', 'min="0" step="1" required')}${field('最高可用积分', 'audience.max_points', a.max_points, 'number', '0 表示不限制', 'min="0" step="1" required')}${textarea('指定用户编号', 'audience.include_user_ids', a.include_user_ids, '每行一个编号；留空时不限制具体用户。', true)}${textarea('排除用户编号', 'audience.exclude_user_ids', a.exclude_user_ids, '排除优先；即使满足其他条件也不会展示。', true)}</div><p class="pa-hint">“高成长值会员”按会员服务的等级判断。游客无法满足积分、成长值或指定用户条件。</p></section>
      <section class="pa-card"><div class="pa-section-title"><b>03</b><div><h2>内容与优惠券</h2><p>配置文案与主题；优惠券的库存、门槛和有效期由优惠券服务校验。</p></div></div><div class="pa-fields">${field('弹窗标题', 'content.title', c.title, 'text', '', 'required maxlength="60"')}${field('副标题', 'content.subtitle', c.subtitle, 'text', '', 'maxlength="100"')}${select(
        '视觉主题',
        'content.theme',
        c.theme,
        [
          [C.PopupTheme.Red, '欢庆红 · 新客礼遇'],
          [C.PopupTheme.Peach, '暖杏色 · 日常回馈'],
          [C.PopupTheme.Plum, '莓紫色 · 会员专享'],
        ],
      )}${select('按钮行为', 'content.action_type', c.action_type, [
        [C.PopupAction.ClaimCoupon, '领取优惠券'],
        [C.PopupAction.CouponCenter, '进入领券中心'],
        [C.PopupAction.Link, '打开商城页面'],
      ])}${field('优惠券模板编号', 'content.coupon_template_id', c.coupon_template_id, 'text', '领取优惠券时必填；可输入编号或从下拉建议中选择。', 'list="pa-coupon-options" maxlength="120"')}<datalist id="pa-coupon-options">${coupons.map((coupon) => `<option value="${esc(coupon.id)}">${esc(coupon.name)} · ¥${money(coupon.amount_cents ?? coupon.amount)} · ${esc(coupon.state || '')}</option>`).join('')}</datalist>${field('按钮文字', 'content.button_text', c.button_text, 'text', '', 'required maxlength="24"')}${field('商城内跳转地址', 'content.action_target', c.action_target, 'text', '仅允许商城内页面，如 #/coupons；不支持外部网址。', 'maxlength="200"')}${textarea('活动说明', 'content.body', c.body, '向用户说明优惠用途或使用范围。')}</div></section>
      <section class="pa-card"><div class="pa-section-title"><b>04</b><div><h2>展示频次</h2><p>展示次数以实际弹出为准；刷新页面、重复请求不会重复计算。</p></div></div><div class="pa-fields">${field('每人累计最多展示', 'frequency.max_per_user', f.max_per_user, 'number', '', 'min="1" max="100" step="1" required')}${field('每人每天最多展示', 'frequency.max_per_day', f.max_per_day, 'number', '', 'min="1" max="10" step="1" required')}${field('两次展示间隔（小时）', 'frequency.cooldown_hours', f.cooldown_hours, 'number', '', 'min="0" max="8760" step="1" required')}${field('关闭后不再展示（小时）', 'frequency.close_cooldown_hours', f.close_cooldown_hours, 'number', '', 'min="0" max="8760" step="1" required')}${field('进入页面后延迟（秒）', 'content.delay_seconds', c.delay_seconds, 'number', '', 'min="0" max="30" step="1" required')}${field('自动关闭（秒）', 'content.auto_close_seconds', c.auto_close_seconds, 'number', '0 为手动关闭；启用时填写 5–60 秒', 'min="0" max="60" step="1" required')}</div><div class="pa-toggles">${toggle('同一会话最多展示一次', 'frequency.once_per_session', f.once_per_session)}${toggle('领取成功后不再展示', 'frequency.stop_after_claim', f.stop_after_claim)}</div></section>
      <footer class="pa-save-bar"><span data-pa-save-state>${dirty ? '有未保存的修改' : draft.id ? '已保存 · ' + (statuses[draft.status] || '草稿') : '新建弹窗尚未保存'}</span><div>${draft.status === C.PopupStatus.Published ? button('pause', '保存并暂停') : ''}${button('save-draft', '保存草稿')}${button('publish', busy ? '正在保存…' : '保存并启用', '', true)}</div></footer>
      </form><aside class="pa-preview"><div class="pa-preview-title"><span>实时预览</span><small>内容随编辑更新</small></div><div data-pa-preview>${preview()}</div></aside></div>`;
  }
  function render() {
    checkScope();
    if (!authorized()) return '<section class="pa-card"><h1>营销弹窗</h1><p>请使用管理员账号登录后配置。</p></section>';
    return `<section class="pa-admin"><header class="pa-heading"><div><span>MARKETING POPUPS</span><h1>${draft ? (draft.id ? '编辑营销弹窗' : '新建营销弹窗') : '营销弹窗'}</h1><p>为不同顾客展示不同的优惠与活动，每一次弹出都有明确规则。</p></div><div>${draft ? button('back', '返回列表（放弃未保存修改）') : button('reload', '刷新')}${!draft ? button('new', '＋ 新建弹窗', '', true) : ''}</div></header>${error ? `<div class="pa-error" role="alert">${esc(error)}</div>` : ''}${draft ? editView() : settings ? listView() : error ? '' : (window.LumoSkeleton?.render('summary', { count: 4, label: '正在加载营销弹窗统计' }) || '') + (window.LumoSkeleton?.render('table', { count: 4, label: '正在加载营销弹窗配置' }) || '')}</section>`;
  }
  function write(path, value) {
    const permitted = [
      'name',
      'priority',
      'starts_at',
      'ends_at',
      'audience.new_member_days',
      'audience.min_growth',
      'audience.max_growth',
      'audience.min_points',
      'audience.max_points',
      'content.title',
      'content.subtitle',
      'content.body',
      'content.theme',
      'content.action_type',
      'content.coupon_template_id',
      'content.button_text',
      'content.action_target',
      'content.auto_close_seconds',
      'content.delay_seconds',
      'frequency.max_per_user',
      'frequency.max_per_day',
      'frequency.cooldown_hours',
      'frequency.close_cooldown_hours',
      'frequency.once_per_session',
      'frequency.stop_after_claim',
    ];
    if (!draft || !permitted.includes(path)) return;
    const keys = path.split('.');
    let target = draft;
    for (const key of keys.slice(0, -1)) target = target[key];
    target[keys.at(-1)] = value;
    dirty = true;
    error = '';
  }
  function updatePreview() {
    const target = document.querySelector('[data-pa-preview]');
    if (target) target.innerHTML = preview();
    const state = document.querySelector('[data-pa-save-state]');
    if (state) state.textContent = dirty ? '有未保存的修改' : '';
  }
  function validate() {
    if (!draft.name.trim() || !draft.content.title.trim() || !draft.content.button_text.trim())
      throw Error('请填写弹窗名称、标题和按钮文字');
    if (!draft.placements.length || !draft.audience.segments.length)
      throw Error('请至少选择一个展示页面和一个目标人群');
    if (draft.content.action_type === C.PopupAction.ClaimCoupon && !draft.content.coupon_template_id.trim())
      throw Error('请选择要领取的优惠券模板');
    if (draft.content.auto_close_seconds !== 0 && draft.content.auto_close_seconds < 5)
      throw Error('自动关闭请填写 0，或 5–60 秒');
    if (draft.starts_at && draft.ends_at && new Date(draft.ends_at) <= new Date(draft.starts_at))
      throw Error('结束时间应晚于开始时间');
    for (const [low, high, label] of [
      ['min_growth', 'max_growth', '成长值'],
      ['min_points', 'max_points', '积分'],
    ])
      if (draft.audience[high] > 0 && draft.audience[high] < draft.audience[low])
        throw Error(`${label}上限不能低于下限`);
    const form = document.querySelector('#popup-campaign-form');
    if (form?.reportValidity && !form.reportValidity()) throw Error('请检查表单中标记的字段');
  }
  async function save(status) {
    try {
      validate();
    } catch (cause) {
      error = cause.message;
      draw();
      return;
    }
    const ticket = ++revision,
      origin = scope,
      outgoing = copy(draft);
    outgoing.status = status;
    busy = true;
    error = '';
    draw();
    try {
      const saved = await call(C.Service.Content, C.Action.PopupUpsert, {
        ...(outgoing.id ? { id: outgoing.id } : {}),
        data: outgoing,
      });
      if (ticket !== revision || origin !== currentScope() || !authorized()) return;
      const index = rows.findIndex((row) => row.id === saved.id);
      if (index >= 0) rows[index] = copy(saved);
      else rows.push(copy(saved));
      draft = copy(saved);
      dirty = false;
      adapter.notify?.(
        status === C.PopupStatus.Published
          ? '弹窗已保存并启用'
          : status === C.PopupStatus.Paused
            ? '弹窗已保存并暂停'
            : '弹窗草稿已保存',
      );
    } catch (cause) {
      if (ticket === revision && origin === currentScope()) error = cause.message || '保存失败，草稿已保留';
    } finally {
      if (ticket === revision && origin === currentScope()) {
        busy = false;
        draw();
      }
    }
  }
  async function saveSettings() {
    const outgoing = copy(settings),
      ticket = ++revision,
      origin = scope;
    busy = true;
    error = '';
    draw();
    try {
      const saved = await call(C.Service.Content, C.Action.PopupConfigure, { data: outgoing });
      if (ticket !== revision || origin !== currentScope() || !authorized()) return;
      settings = copy(saved);
      adapter.notify?.('全局弹窗设置已保存');
    } catch (cause) {
      if (ticket === revision && origin === currentScope()) error = cause.message || '全局设置保存失败';
    } finally {
      if (ticket === revision && origin === currentScope()) {
        busy = false;
        draw();
      }
    }
  }
  function edit(row, duplicate = false) {
    draft = copy(row || defaults());
    if (duplicate) {
      draft.id = '';
      draft.name += '（副本）';
      draft.status = C.PopupStatus.Draft;
      delete draft.version;
      delete draft.stats;
      delete draft.created_at;
      delete draft.updated_at;
    }
    dirty = !draft.id;
    error = '';
    draw();
  }
  document.addEventListener('input', (event) => {
    checkScope();
    if (!authorized() || busy) return;
    const target = event.target,
      path = target.dataset?.paField,
      list = target.dataset?.paList;
    if (path?.startsWith('settings.') && settings) {
      const key = path.slice(9);
      if (['enabled', 'max_per_session', 'min_gap_seconds'].includes(key))
        settings[key] = target.type === 'checkbox' ? target.checked : Number(target.value);
      return;
    }
    if (
      list &&
      draft &&
      ['audience.include_user_ids', 'audience.exclude_user_ids', 'audience.interest_categories'].includes(list)
    ) {
      draft.audience[list.slice(9)] = [
        ...new Set(
          String(target.value)
            .split(/[\n,，]/)
            .map((value) => value.trim())
            .filter(Boolean),
        ),
      ];
      dirty = true;
      updatePreview();
      return;
    }
    if (path && draft) {
      let value =
        target.type === 'checkbox' ? target.checked : target.type === 'number' ? Number(target.value) : target.value;
      if (['starts_at', 'ends_at'].includes(path) && value) {
        const date = new Date(value);
        if (!Number.isNaN(date.getTime())) value = date.toISOString();
      }
      write(path, value);
      updatePreview();
    }
  });
  document.addEventListener('change', (event) => {
    checkScope();
    if (!authorized() || busy) return;
    const target = event.target;
    if (target.dataset?.paSegment && draft) {
      const key = target.dataset.paSegment;
      if (!segments.some((value) => value[0] === key)) return;
      let selected = draft.audience.segments.filter((value) => value !== key && value !== C.PopupSegment.All);
      if (target.checked) selected = key === C.PopupSegment.All ? [C.PopupSegment.All] : [...selected, key];
      draft.audience.segments = selected;
      dirty = true;
      document.querySelectorAll('[data-pa-segment]').forEach((input) => {
        input.checked = selected.includes(input.dataset.paSegment);
      });
      updatePreview();
      return;
    }
    if (target.dataset?.paPlacement && draft) {
      const key = target.dataset.paPlacement;
      if (!placements.some((value) => value[0] === key)) return;
      draft.placements = draft.placements.filter((value) => value !== key);
      if (target.checked) draft.placements.push(key);
      dirty = true;
      updatePreview();
      return;
    }
    if (target.dataset && Object.hasOwn(target.dataset, 'paFilter')) {
      filter = target.value;
      page = 1;
      draw();
      return;
    }
    if (target.dataset && Object.hasOwn(target.dataset, 'paSearch')) {
      search = target.value.trim();
      page = 1;
      draw();
      return;
    }
    if (target.dataset?.paField) {
      target.dispatchEvent?.(new Event('input', { bubbles: true }));
    }
  });
  document.addEventListener('click', async (event) => {
    const target = event.target.closest?.('[data-pa-action]');
    if (!target) return;
    event.preventDefault();
    checkScope();
    if (!authorized() || busy || target.disabled) return;
    const action = target.dataset.paAction;
    if (action === 'reload') {
      await refresh();
      draw();
      return;
    }
    if (action === 'new') {
      edit();
      return;
    }
    if (action === 'edit' || action === 'duplicate') {
      const row = rows.find((value) => value.id === target.dataset.paId);
      if (row) edit(row, action === 'duplicate');
      return;
    }
    if (action === 'back') {
      draft = null;
      dirty = false;
      error = '';
      draw();
      return;
    }
    if (action === 'prev' || action === 'next') {
      page += action === 'prev' ? -1 : 1;
      draw();
      return;
    }
    if (action === 'settings' && settings) {
      await saveSettings();
      return;
    }
    if (!draft) return;
    if (action === 'save-draft') await save(C.PopupStatus.Draft);
    if (action === 'publish') await save(C.PopupStatus.Published);
    if (action === 'pause') await save(C.PopupStatus.Paused);
  });
  document.addEventListener('submit', (event) => {
    if (['popup-campaign-form', 'popup-settings-form'].includes(event.target.id)) event.preventDefault();
  });
  for (const event of ['lumo:logout', 'lumo:session-expired', 'lumo:login']) window.addEventListener(event, reset);
  window.LumoPopupAdmin = {
    configure(options) {
      adapter = options || {};
      reset();
    },
    refresh,
    render,
    reset,
    get draft() {
      checkScope();
      return draft ? copy(draft) : null;
    },
    get dirty() {
      return dirty;
    },
  };
})();
