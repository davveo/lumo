(function () {
  'use strict';
  const C = window.LumoConstants,
    esc = (v) =>
      String(v ?? '').replace(
        /[&<>"']/g,
        (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
      );
  const labels = {
    PENDING: '等待处理',
    PROCESSING: '正在处理',
    ISSUED: '已兑换',
    SHIPPED: '礼品已发货',
    DELIVERED: '礼品已签收',
    REFUNDED: '积分已返还',
    ATTENTION: '商家正在跟进',
  };
  const scope = () => JSON.stringify([LumoAPI.getBase(), LumoAPI.getToken?.(), LumoAuth.user()?.id]);
  const fresh = () => ({
    owner: scope(),
    loaded: false,
    loading: false,
    error: '',
    offers: [],
    records: [],
    total: 0,
    page: 1,
    catalogPage: 1,
    selected: null,
    request: '',
    address: '',
    busy: false,
    reminder: null,
    reminderLoading: false,
    reminderError: '',
  });
  let state = fresh(),
    rev = 0;
  const render = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  function current() {
    if (state.owner !== scope()) {
      rev++;
      state = fresh();
    }
    return state;
  }
  async function load(force = false) {
    const s = current();
    if (s.loading || (s.loaded && !force) || !LumoAuth.isLoggedIn()) return;
    const ticket = ++rev,
      owner = scope();
    s.loading = true;
    s.error = '';
    try {
      const [offers, records] = await Promise.all([
        LumoAPI.call(C.Service.Points, C.Action.Offers),
        LumoAPI.call(C.Service.Points, C.Action.Redemptions, { data: { page: s.page, page_size: 10 } }),
      ]);
      if (owner !== scope() || ticket !== rev) return;
      Object.assign(s, {
        offers: offers.items || [],
        records: records.items || [],
        total: Number(records.total || 0),
        loaded: true,
      });
    } catch (e) {
      if (owner === scope() && ticket === rev) s.error = e.message;
    } finally {
      if (owner === scope() && ticket === rev) {
        s.loading = false;
        render();
      }
    }
  }
  async function reminder() {
    const s = current();
    if (s.reminder || s.reminderLoading || s.reminderError || !LumoAuth.isLoggedIn()) return;
    const owner = scope();
    s.reminderLoading = true;
    try {
      const p = await LumoAPI.call(C.Service.Coupon, C.Action.ReminderGet);
      if (owner === scope()) s.reminder = p;
    } catch (e) {
      if (owner === scope()) s.reminderError = e.message;
    } finally {
      if (owner === scope()) {
        s.reminderLoading = false;
        render();
      }
    }
  }
  function reminderView() {
    const s = current();
    return `<section class="mem-panel reward-reminder"><div><h2>优惠券到期提醒</h2><p>站内信提醒未来 72 小时内到期的可用券，每 24 小时最多一次。</p>${s.reminder?.expiring_count ? `<small>当前有 ${Number(s.reminder.expiring_count)} 张即将到期</small>` : ''}${s.reminderError ? `<p role="alert">${esc(s.reminderError)}</p>` : ''}</div><button class="mem-outline" data-reward="reminder" ${s.busy || !s.reminder ? 'disabled' : ''} aria-pressed="${s.reminder?.enabled === true}">${s.reminderLoading ? '读取中' : s.reminder?.enabled ? '已开启 · 关闭提醒' : '开启提醒'}</button>${s.reminderError ? '<button class="mem-outline" data-reward="reminder-retry">重新读取</button>' : ''}</section>`;
  }
  function view(data) {
    const s = current(),
      g = data.membership || {},
      size = 6,
      maxPage = Math.max(1, Math.ceil(s.offers.length / size));
    s.catalogPage = Math.min(s.catalogPage, maxPage);
    const chosen = s.offers.find((r) => r.id === s.selected);
    const confirmation = chosen
      ? `<form id="reward-confirm" class="reward-confirm"><h3>确认兑换：${esc(chosen.name)}</h3><p>本次扣除 <strong>${Number(chosen.cost)} 积分</strong>，确认后创建兑换单。网络重试不会重复扣分。</p>${chosen.kind === C.RewardKind.Gift ? `<label class="field">收货地址<select name="address" required><option value="">请选择收货地址</option>${(data.addresses || []).map((a) => `<option value="${esc(a.id)}" ${s.address === a.id ? 'selected' : ''}>${esc(a.name + ' · ' + a.region + ' ' + a.detail)}</option>`).join('')}</select></label><a href="#/member/addresses">管理收货地址</a>` : '<p>同一优惠券模板每人限领一次；无法发券时会返还积分。</p>'}<div class="svc-modal-actions"><button class="mem-primary" ${s.busy ? 'disabled' : ''}>${s.busy ? '正在提交' : '确认扣除积分并兑换'}</button><button type="button" class="mem-outline" data-reward="cancel" ${s.busy ? 'disabled' : ''}>取消</button></div></form>`
      : '';
    return `<section class="mem-panel reward-growth"><div class="mem-section-head"><h2>会员成长</h2><span>${esc(data.profile.level)}</span></div><p>当前成长值 <strong>${Number(g.growth || 0)}</strong> · ${g.reached ? '已达到当前最高会员等级' : `再获得 ${Number(g.remaining || 0)} 成长值可升级金卡会员`}</p><progress max="100" value="${Number(g.progress || 0)}" aria-label="会员成长进度"></progress><div class="reward-tasks"><a href="#/member/settings">${data.profile.nickname && data.profile.nickname !== 'LUMO 生活家' ? '✓ 已设置昵称' : '完善账户资料'}<small>管理昵称与常用信息，不发放额外积分</small></a><button data-member-action="signin" ${data.signedToday ? 'disabled' : ''}>${data.signedToday ? '✓ 今日已签到' : '每日签到'}<small>按当前规则获得 ${Number(data.pointsPolicy?.checkin_points ?? 20)} 积分</small></button><a href="#/member/orders">完成购物订单<small>订单完成后累计成长，退款按规则回收</small></a></div></section><section class="mem-panel reward-store"><div class="mem-section-head"><h2>积分兑换</h2><button class="mem-outline" data-reward="refresh" ${s.loading || s.busy ? 'disabled' : ''}>刷新</button></div><p>奖品由商家配置；实物按真实库存占用并发货，优惠券以实际发放结果为准。</p>${s.error ? `<p role="alert">${esc(s.error)}</p>` : ''}${
      s.loading && !s.loaded
        ? '<p role="status">正在读取兑换项目…</p>'
        : !s.offers.length
          ? '<p>暂无开放的兑换项目。</p>'
          : `<div class="reward-grid">${s.offers
              .slice((s.catalogPage - 1) * size, s.catalogPage * size)
              .map(
                (r) =>
                  `<article><span class="mem-badge">${r.kind === C.RewardKind.Gift ? '实物礼品' : '优惠券'}</span><h3>${esc(r.name)}</h3><p>${esc(r.description)}</p><strong>${Number(r.cost)} 积分</strong><small>剩余 ${Math.max(0, Number(r.total_quota) - Number(r.used))} 份 · 每人最多 ${Number(r.max_per_user)} 份</small><button class="mem-outline" data-reward="choose" data-id="${esc(r.id)}" ${s.busy || Number(r.used) >= Number(r.total_quota) || data.points < r.cost || data.debtPoints ? 'disabled' : ''}>${data.points < r.cost ? '积分不足' : '兑换'}</button></article>`,
              )
              .join(
                '',
              )}</div>${maxPage > 1 ? `<div class="reward-pager"><button data-reward="catalog-prev" ${s.catalogPage <= 1 ? 'disabled' : ''}>上一页</button><span>${s.catalogPage} / ${maxPage}</span><button data-reward="catalog-next" ${s.catalogPage >= maxPage ? 'disabled' : ''}>下一页</button></div>` : ''}`
    }${confirmation}<h3>我的兑换记录</h3>${s.records.length ? `<ul class="reward-records">${s.records.map((r) => `<li><div><strong>${esc(r.name)}</strong><small>${esc(r.id)} · ${Number(r.cost)} 积分</small></div><span class="mem-badge">${esc(labels[r.state] || r.state)}</span>${r.tracking_id ? `<small>运单 ${esc(r.tracking_id)}</small>` : ''}${r.state === C.RedemptionState.Attention ? '<small>商家会跟进处理结果，可携兑换单号咨询客服。</small>' : ''}${r.state === C.RedemptionState.Refunded ? '<small>积分已返还，存在待回收积分时优先冲抵。</small>' : ''}</li>`).join('')}</ul>` : '<p>暂无兑换记录。</p>'}<div class="reward-pager"><button data-reward="prev" ${s.page <= 1 || s.loading ? 'disabled' : ''}>上一页</button><span>第 ${s.page} / ${Math.max(1, Math.ceil(s.total / 10))} 页 · 共 ${s.total} 条</span><button data-reward="next" ${s.page * 10 >= s.total || s.loading ? 'disabled' : ''}>下一页</button></div></section>`;
  }
  async function afterRender() {
    const tab = location.hash.split('/')[2];
    if (tab === 'points') void load();
    if (tab === 'coupons') void reminder();
  }
  document.addEventListener('change', (e) => {
    if (e.target.matches('#reward-confirm select')) current().address = e.target.value;
  });
  document.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-reward]');
    if (!b) return;
    const s = current(),
      action = b.dataset.reward;
    if (s.busy) return;
    if (action === 'choose') {
      s.selected = b.dataset.id;
      s.request = crypto.randomUUID();
      s.address = '';
      render();
      return;
    }
    if (action === 'cancel') {
      s.selected = null;
      render();
      return;
    }
    if (action === 'catalog-prev' || action === 'catalog-next') {
      s.catalogPage += action === 'catalog-next' ? 1 : -1;
      render();
      return;
    }
    if (action === 'prev' || action === 'next') {
      if (s.loading) return;
      s.page += action === 'next' ? 1 : -1;
      await load(true);
      return;
    }
    if (action === 'refresh') {
      await load(true);
      return;
    }
    if (action === 'reminder-retry') {
      s.reminderError = '';
      await reminder();
      return;
    }
    if (action === 'reminder' && s.reminder) {
      const owner = scope();
      s.busy = true;
      render();
      try {
        const p = await LumoAPI.call(C.Service.Coupon, C.Action.ReminderUpdate, {
          data: { enabled: !s.reminder.enabled },
        });
        if (owner === scope()) s.reminder = p;
      } catch (err) {
        if (owner === scope()) s.reminderError = err.message;
      } finally {
        if (owner === scope()) {
          s.busy = false;
          render();
        }
      }
    }
  });
  document.addEventListener('submit', async (e) => {
    if (e.target.id !== 'reward-confirm') return;
    e.preventDefault();
    const s = current(),
      offer = s.offers.find((r) => r.id === s.selected);
    if (s.busy || !offer) return;
    s.address = String(new FormData(e.target).get('address') || '');
    if (offer.kind === C.RewardKind.Gift && !s.address) return;
    const owner = scope();
    s.busy = true;
    s.error = '';
    render();
    try {
      await LumoAPI.call(C.Service.Points, C.Action.Redeem, {
        id: offer.id,
        idempotency_key: s.request,
        data: { version: offer.version, address_id: s.address },
      });
      if (owner !== scope()) return;
      s.selected = null;
      s.page = 1;
      await window.LumoMember.refresh(true, ['points', 'coupons']);
      await load(true);
    } catch (err) {
      if (owner === scope()) s.error = err.message;
    } finally {
      if (owner === scope()) {
        s.busy = false;
        render();
      }
    }
  });
  for (const name of ['lumo:login', 'lumo:logout', 'lumo:api-base-changed'])
    window.addEventListener(name, () => {
      rev++;
      state = fresh();
    });
  window.LumoRewards = { view, reminderView, afterRender };
})();
