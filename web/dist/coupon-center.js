(function () {
  'use strict';
  const C = window.LumoConstants;
  const filters = [
    [C.CouponFilter.All, '全部优惠券'],
    [C.CouponClaimStatus.Available, '可领取'],
    [C.CouponFilter.Timed, '限时券'],
    [C.CouponFilter.Conditional, '条件券'],
    [C.CouponClaimStatus.Claimed, '已领取'],
  ];
  const pageSize = 6;
  const loggedIn = () => !!window.LumoAuth?.isLoggedIn();
  const scope = () =>
    JSON.stringify([
      window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '',
      window.LumoAPI.getBase?.() || location.origin,
      window.LumoAPI.getToken?.() || '',
    ]);
  const redraw = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  const notify = (message) => typeof toast === 'function' && toast(message);
  const number = (value) => Math.max(0, Math.floor(Number(value) || 0));
  const time = (value) => (value ? Date.parse(value) || 0 : 0);
  const yuan = (value) => money(number(value) / 100);
  const date = (value) =>
    new Date(value).toLocaleString('zh-CN', {
      timeZone: 'Asia/Shanghai',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  const refreshIcon = () =>
    '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 7v5h-5M4 17v-5h5M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1"/></svg>';
  let owner = scope(),
    revision = 0,
    generation = 0,
    rows = [],
    loaded = false,
    complete = false,
    total = 0,
    loading = false,
    error = '',
    flight = null,
    filter = C.CouponFilter.All,
    displayPage = 1,
    offset = 0,
    timer = null,
    boundary = '',
    claiming = new Set();
  let centerView = null,
    entryPending = false;
  function reset() {
    revision++;
    generation++;
    owner = scope();
    rows = [];
    loaded = false;
    complete = false;
    total = 0;
    loading = false;
    error = '';
    flight = null;
    filter = C.CouponFilter.All;
    displayPage = 1;
    offset = 0;
    boundary = '';
    claiming = new Set();
    clearInterval(timer);
    timer = null;
    centerView = null;
    entryPending = false;
  }
  function ensureScope() {
    if (owner !== scope()) reset();
  }
  function observeRoute() {
    const active = /^#\/coupons(?:\/|$)/.test(location.hash);
    if (active !== centerView) {
      centerView = active;
      entryPending = active;
    }
    return active;
  }
  const serverNow = () => Date.now() + offset;
  function status(row) {
    if (row.claimed || row.claim_status === C.CouponClaimStatus.Claimed) return C.CouponClaimStatus.Claimed;
    const start = time(row.starts_at),
      end = time(row.expires_at),
      now = serverNow();
    if (start > now) return C.CouponClaimStatus.Upcoming;
    if (end && end <= now) return C.CouponClaimStatus.Ended;
    return row.claim_status || C.CouponClaimStatus.Unavailable;
  }
  const conditional = (row) => number(row.min_points) > 0 || number(row.min_growth) > 0;
  const timed = (row) => time(row.starts_at) > 0;
  function ordered(items) {
    const rank = {
      [C.CouponClaimStatus.Available]: 0,
      [C.CouponClaimStatus.LoginRequired]: 1,
      [C.CouponClaimStatus.Upcoming]: 2,
      [C.CouponClaimStatus.ConditionsUnmet]: 3,
      [C.CouponClaimStatus.Claimed]: 4,
      [C.CouponClaimStatus.Exhausted]: 5,
      [C.CouponClaimStatus.Ended]: 6,
    };
    return items
      .slice()
      .sort(
        (a, b) =>
          (rank[status(a)] ?? 7) - (rank[status(b)] ?? 7) ||
          time(b.created_at) - time(a.created_at) ||
          String(a.id).localeCompare(String(b.id)),
      );
  }
  function filteredRows() {
    return rows.filter(
      (row) =>
        filter === C.CouponFilter.All ||
        (filter === C.CouponClaimStatus.Available &&
          [C.CouponClaimStatus.Available, C.CouponClaimStatus.LoginRequired].includes(status(row))) ||
        (filter === C.CouponFilter.Timed && timed(row)) ||
        (filter === C.CouponFilter.Conditional && conditional(row)) ||
        (filter === C.CouponClaimStatus.Claimed && status(row) === C.CouponClaimStatus.Claimed),
    );
  }
  function pageCount(total) {
    return Math.max(1, Math.ceil(total / pageSize));
  }
  function clampPage(total) {
    displayPage = Math.max(1, Math.min(displayPage, pageCount(total)));
  }
  function pagination(total) {
    if (total <= pageSize) return '';
    return `<nav class="cc-pagination is-bottom" aria-label="优惠券分页（底部）"><button type="button" data-cc-action="page" data-value="${displayPage - 1}" aria-label="上一页优惠券" ${displayPage === 1 ? 'disabled' : ''}>${icon('back')}上一页</button><span aria-live="polite">第 <strong>${displayPage}</strong> / ${pageCount(total)} 页</span><button type="button" data-cc-action="page" data-value="${displayPage + 1}" aria-label="下一页优惠券" ${displayPage === pageCount(total) ? 'disabled' : ''}>下一页${icon('chevron')}</button></nav>`;
  }
  function conditions(row) {
    const requirements = [];
    if (number(row.min_points)) requirements.push(`可用积分满 ${number(row.min_points).toLocaleString('zh-CN')}`);
    if (number(row.min_growth)) requirements.push(`成长值满 ${number(row.min_growth).toLocaleString('zh-CN')}`);
    return requirements.length ? requirements.join('，且') : '所有用户可领';
  }
  function canClaim(row) {
    return loggedIn() && status(row) === C.CouponClaimStatus.Available && row.claimable === true;
  }
  function label(row) {
    if (claiming.has(row.id)) return '正在领取…';
    return (
      {
        [C.CouponClaimStatus.Available]: '立即领取',
        [C.CouponClaimStatus.LoginRequired]: '登录领取',
        [C.CouponClaimStatus.Upcoming]: '即将开放',
        [C.CouponClaimStatus.Ended]: '领取已结束',
        [C.CouponClaimStatus.Exhausted]: '已领完',
        [C.CouponClaimStatus.Claimed]: '已领取 · 查看',
        [C.CouponClaimStatus.ConditionsUnmet]: '暂不满足条件',
        [C.CouponClaimStatus.Unavailable]: '暂不可领取',
      }[status(row)] || '暂不可领取'
    );
  }
  function reason(row) {
    const state = status(row);
    if (state === C.CouponClaimStatus.Claimed) return '已放入我的优惠券，每人限领一次';
    if (state === C.CouponClaimStatus.Upcoming) return `${date(row.starts_at)} 开始领取`;
    if (state === C.CouponClaimStatus.Ended) return '本次领取已结束，看看其他好券吧';
    if (state === C.CouponClaimStatus.Exhausted) return '本次优惠券已领完';
    if (state === C.CouponClaimStatus.LoginRequired) return conditional(row) ? '登录后核验领取条件' : '登录后即可领取';
    return row.claim_reason || (state === C.CouponClaimStatus.Available ? '已满足领取条件' : '暂不可领取，请稍后再试');
  }
  async function refresh(force = false) {
    ensureScope();
    const full = observeRoute();
    if (full) entryPending = false;
    if (flight && !force && flight.full === full) return flight.promise;
    const current = { owner, revision: ++revision, full };
    flight = current;
    loading = true;
    error = '';
    current.promise = (async () => {
      try {
        const next = [],
          ids = new Set();
        let page = 1,
          serverTime = 0;
        while (true) {
          const result = await window.LumoAPI.call(
            C.Service.Coupon,
            full ? C.Action.TemplatesSnapshot : C.Action.Templates,
            { data: { page, page_size: full ? 5000 : 100 } },
            loggedIn() ? {} : { auth: false },
          );
          if (current.owner !== scope() || current.revision !== revision) return model();
          const previousSize = ids.size;
          for (const row of result.items || [])
            if (row && typeof row.id === 'string' && !ids.has(row.id)) {
              ids.add(row.id);
              next.push(row);
            }
          serverTime = time(result.server_time) || serverTime;
          total = number(result.total) || next.length;
          if (!full || !result.has_more) break;
          // A broken pagination response must not cause an endless request loop.
          if (ids.size === previousSize || page >= 10000) throw new Error('优惠券列表加载不完整，请刷新重试');
          page++;
        }
        rows = next;
        loaded = true;
        complete = full;
        offset = serverTime ? serverTime - Date.now() : 0;
        return model();
      } catch (failure) {
        if (current.owner !== scope() || current.revision !== revision) return model();
        error = failure?.message || '优惠券加载失败，请稍后重试';
        throw failure;
      } finally {
        if (flight === current) {
          loading = false;
          flight = null;
        }
      }
    })();
    return current.promise;
  }
  function model() {
    ensureScope();
    clampPage(filteredRows().length);
    return {
      items: rows.map((row) => ({ ...row, claim_status: status(row) })),
      loaded,
      complete,
      total,
      loading,
      error,
      filter,
      display_page: displayPage,
      display_page_size: pageSize,
    };
  }
  function clock(value) {
    const seconds = Math.max(0, Math.ceil((time(value) - serverNow()) / 1000));
    if (seconds >= 86400)
      return `${Math.floor(seconds / 86400)} 天 ${String(Math.floor(seconds / 3600) % 24).padStart(2, '0')} 小时`;
    return [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60]
      .map((n) => String(n).padStart(2, '0'))
      .join(':');
  }
  function claimWindow(row) {
    const state = status(row),
      target = state === C.CouponClaimStatus.Upcoming ? row.starts_at : row.expires_at;
    const countdown =
      [
        C.CouponClaimStatus.Available,
        C.CouponClaimStatus.LoginRequired,
        C.CouponClaimStatus.ConditionsUnmet,
        C.CouponClaimStatus.Upcoming,
      ].includes(state) && time(target) > serverNow();
    return `<div class="cc-window">${icon('clock')}<span>${state === C.CouponClaimStatus.Upcoming ? '距开放' : countdown ? '距领取截止' : '领取截止'} ${countdown ? `<time data-cc-countdown="${esc(target)}">${clock(target)}</time>` : time(target) ? esc(date(target)) : '以活动规则为准'}</span></div>`;
  }
  function card(row) {
    const state = status(row),
      pending = claiming.has(row.id),
      enabled = state === C.CouponClaimStatus.Claimed || state === C.CouponClaimStatus.LoginRequired || canClaim(row);
    return `<article class="cc-card is-${esc(state)}" aria-label="${esc(row.name || '品质生活优惠券')}"><div class="cc-card-top"><span class="cc-card-badge">${conditional(row) ? '会员条件券' : timed(row) ? '限时专享券' : '日常好券'}</span><span>剩余 ${number(row.remaining).toLocaleString('zh-CN')} 张</span></div><div class="cc-card-value"><span><small>¥</small>${yuan(row.amount_cents)}</span><strong>${number(row.min_cents) ? `满 ¥${yuan(row.min_cents)} 可用` : '无使用门槛'}</strong></div><h2>${esc(row.name || '品质生活优惠券')}</h2><p class="cc-card-condition">${icon(conditional(row) ? 'user' : 'ticket')}${esc(conditions(row))}${number(row.min_points) ? '<small>仅核验，不扣积分</small>' : ''}</p><p class="cc-card-validity">领取后 ${number(row.valid_days)} 天有效 · 普通商品可用</p>${claimWindow(row)}<div class="cc-card-bottom"><p class="cc-card-reason" data-cc-reason="${esc(row.id)}">${esc(reason(row))}</p><button type="button" class="cc-claim ${state === C.CouponClaimStatus.Claimed ? 'is-claimed' : ''}" data-cc-action="claim" data-id="${esc(row.id)}" ${pending || !enabled ? 'disabled' : ''}>${esc(label(row))}${state === C.CouponClaimStatus.Claimed ? icon('chevron') : canClaim(row) || state === C.CouponClaimStatus.LoginRequired ? icon('plus') : ''}</button></div></article>`;
  }
  function home(options = {}) {
    ensureScope();
    const limit = options.limit === 2 ? 2 : 3;
    const picked = ordered(rows).slice(0, limit);
    if (!loaded && !error && !picked.length)
      return `<section class="cc-home" aria-label="领券中心"><div class="cc-home-intro"><span class="cc-kicker">A LITTLE MORE FOR YOU</span><h2>${icon('ticket')}领券中心</h2><p>先领好券，再选心动好物</p><a href="#/coupons">查看全部优惠券 ${icon('arrow')}</a></div><div class="cc-home-coupons">${window.LumoSkeleton?.render('coupons', { count: limit, label: '正在加载好券', className: 'sk-coupons-compact' }) || ''}</div></section>`;
    return `<section class="cc-home" aria-label="领券中心"><div class="cc-home-intro"><span class="cc-kicker">A LITTLE MORE FOR YOU</span><h2>${icon('ticket')}领券中心</h2><p>先领好券，再选心动好物</p><a href="#/coupons">查看全部优惠券 ${icon('arrow')}</a></div><div class="cc-home-coupons">${picked.length ? picked.map((row) => `<a class="cc-home-ticket" href="#/coupons"><span class="cc-home-amount"><small>¥</small>${yuan(row.amount_cents)}</span><div><strong>${esc(row.name || '品质生活优惠券')}</strong><span>${number(row.min_cents) ? `满 ¥${yuan(row.min_cents)} 可用` : '无使用门槛'} · ${number(row.valid_days)} 天有效</span><small>${esc({ [C.CouponClaimStatus.Claimed]: '已领取', [C.CouponClaimStatus.Available]: '立即领取', [C.CouponClaimStatus.LoginRequired]: '登录领取', [C.CouponClaimStatus.Upcoming]: '即将开放', [C.CouponClaimStatus.ConditionsUnmet]: '满足条件可领', [C.CouponClaimStatus.Ended]: '领取已结束', [C.CouponClaimStatus.Exhausted]: '已领完' }[status(row)] || '查看详情')}</small></div>${icon('chevron')}</a>`).join('') : `<a class="cc-home-empty" href="#/coupons">${icon('ticket')}<span>${error ? '优惠券暂未加载，进入中心重试' : loaded ? '新优惠正在准备中，进来看看吧' : '正在为你准备好券…'}</span>${icon('arrow')}</a>`}</div></section>`;
  }
  function page() {
    ensureScope();
    const pendingFull = !complete;
    const visible = pendingFull ? [] : ordered(filteredRows());
    clampPage(visible.length);
    const first = (displayPage - 1) * pageSize,
      displayed = visible.slice(first, first + pageSize);
    return `${header(true)}<main class="container cc-page" id="main"><nav class="cc-breadcrumb" aria-label="面包屑"><a href="#/">首页</a>${icon('chevron')}<span>领券中心</span></nav><section class="cc-hero"><div><span class="cc-kicker">LUMO COUPON COLLECTION</span><h1>领券中心</h1><p>把喜欢的好券收入囊中，让每一次下单更划算。</p><div class="cc-hero-tags"><span>${icon('ticket')}每人每券限领 1 次</span><span>${icon('clock')}限时开放 · 满足条件可领</span></div></div><a class="cc-wallet" href="#/member/coupons">${icon('ticket')}我的优惠券 ${icon('arrow')}</a></section><div class="cc-toolbar"><nav class="cc-filters" aria-label="优惠券分类">${filters.map(([value, title]) => `<button type="button" data-cc-action="filter" data-value="${value}" class="${filter === value ? 'active' : ''}" aria-pressed="${filter === value}">${title}</button>`).join('')}</nav><button type="button" class="cc-refresh" data-cc-action="refresh" ${loading ? 'disabled' : ''}>${refreshIcon()}${loading ? '正在更新' : '刷新'}</button></div>${error ? `<div class="cc-error" role="alert"><span>${esc(error)}</span><button data-cc-action="refresh">重新加载</button></div>` : ''}${!loggedIn() ? '<p class="cc-login-note">可以先看看所有好券，登录后即可核验领取资格。<button data-cc-action="login">登录领券</button></p>' : ''}<div class="cc-results-head"><p>${!loaded ? (error ? '优惠券信息暂不可用' : '正在加载优惠券') : `共 ${complete ? rows.length : total} 种优惠券`}${filter !== C.CouponFilter.All ? ` · 当前分类 ${visible.length} 种` : ''}${visible.length ? ` · 当前显示 ${first + 1}–${first + displayed.length} 种` : ''}</p><span>领取条件与使用门槛分别计算</span></div>${pendingFull && !error ? window.LumoSkeleton?.render('coupons', { count: 6, label: '正在加载全部优惠券' }) || '' : visible.length ? `<div class="cc-grid">${displayed.map(card).join('')}</div>${pagination(visible.length)}` : `<div class="cc-empty">${icon('ticket')}<h2>${error ? '优惠券暂未加载成功' : loaded ? '这里暂时没有优惠券' : '优惠券正在准备中'}</h2><p>${filter === C.CouponClaimStatus.Claimed ? '领取成功的券会显示在这里，也可以在「我的优惠券」查看。' : '试试其他分类，或稍后再来看看。'}</p>${filter !== C.CouponFilter.All ? '<button class="cc-claim" data-cc-action="filter" data-value="all">查看全部优惠券</button>' : ''}</div>`}<section class="cc-rules" aria-label="领券说明"><h2>${icon('info')}领券小提示</h2><p>限时券需在开放时间内领取；条件券会核验当前账号的可用积分或成长值，积分门槛仅用于核验，不会扣除积分。</p><p>领取后有效期从领取时计算，满减门槛按普通商品优惠前金额判断。每单限用 1 张，不适用于拼团和秒杀；换购商品不计入用券门槛。</p></section></main>${footer()}`;
  }
  async function claim(id) {
    ensureScope();
    const row = rows.find((item) => item.id === id);
    if (!row || claiming.has(id)) return;
    if (status(row) === C.CouponClaimStatus.Claimed) {
      location.hash = '#/member/coupons';
      return;
    }
    if (!window.LumoAuth?.requireLogin('#/coupons')) return;
    if (!canClaim(row)) {
      notify(reason(row));
      return;
    }
    const currentOwner = owner,
      currentGeneration = generation;
    const activeClaims = claiming;
    activeClaims.add(id);
    redraw();
    try {
      const result = await window.LumoAPI.call(C.Service.Coupon, C.Action.Claim, { data: { template_id: id } });
      if (currentOwner !== scope() || currentGeneration !== generation) return;
      // Persist the successful result in the view even when a subsequent wallet
      // refresh fails. The server remains authoritative for using the coupon.
      const latest = rows.find((item) => item.id === id);
      if (latest) {
        latest.claimed = true;
        latest.claim_status = C.CouponClaimStatus.Claimed;
        latest.claimable = false;
        latest.coupon_id = result.id || result.coupon_id || latest.coupon_id;
      }
      notify(result.already_claimed ? '这张券已领取，可在我的优惠券中查看' : '领取成功，已放入我的优惠券');
      redraw();
      const results = await Promise.allSettled([refresh(true), window.LumoMember?.refresh(true)]);
      if (currentOwner === scope() && results.some((result) => result.status === 'rejected'))
        notify('优惠券已领取，部分信息更新失败，可在我的优惠券中刷新查看');
    } catch (failure) {
      if (currentOwner !== scope() || currentGeneration !== generation) return;
      notify(failure?.message || '领取失败，请刷新后重试');
      await refresh(true).catch(() => {});
    } finally {
      activeClaims.delete(id);
      if (currentOwner === scope()) redraw();
    }
  }
  function afterRender(fromReady = false) {
    ensureScope();
    clearInterval(timer);
    timer = null;
    if (!observeRoute()) return;
    // Initial hydration owns its explicit refresh stage. A route entered while
    // it is running is picked up by lumo:ready, after the snapshot is released.
    if (
      window.LumoAPI.isBootstrapActive?.() ||
      (window.LumoConnection && (!window.LumoConnection.ready || (!fromReady && window.LumoConnection.refreshing)))
    )
      return;
    if (entryPending) refresh().then(redraw).catch(redraw);
    const tick = () => {
      document.querySelectorAll('[data-cc-countdown]').forEach((element) => {
        element.textContent = clock(element.dataset.ccCountdown);
      });
      const passed = rows
        .filter(
          (row) =>
            !row.claimed &&
            ((row.claim_status === C.CouponClaimStatus.Upcoming && time(row.starts_at) <= serverNow()) ||
              ([
                C.CouponClaimStatus.Available,
                C.CouponClaimStatus.LoginRequired,
                C.CouponClaimStatus.ConditionsUnmet,
              ].includes(row.claim_status) &&
                time(row.expires_at) > 0 &&
                time(row.expires_at) <= serverNow())),
        )
        .map((row) => row.id + ':' + row.claim_status)
        .join('|');
      if (!passed) boundary = '';
      else if (passed !== boundary && !loading) {
        boundary = passed;
        refresh(true).then(redraw).catch(redraw);
      }
    };
    tick();
    timer = setInterval(tick, 1000);
  }
  document.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-cc-action]');
    if (!button || button.disabled) return;
    event.preventDefault();
    const action = button.dataset.ccAction;
    if (action === 'filter') {
      filter = filters.some(([value]) => value === button.dataset.value) ? button.dataset.value : C.CouponFilter.All;
      displayPage = 1;
      redraw();
    }
    if (action === 'page') {
      displayPage = Math.max(1, number(button.dataset.value));
      clampPage(filteredRows().length);
      redraw();
    }
    if (action === 'login') window.LumoAuth?.requireLogin('#/coupons');
    if (action === 'claim') await claim(button.dataset.id);
    if (action === 'refresh') {
      const pending = refresh(true);
      redraw();
      await pending.catch(() => {});
      redraw();
    }
  });
  for (const event of ['lumo:login', 'lumo:logout', 'lumo:session-expired']) window.addEventListener(event, reset);
  window.addEventListener('lumo:ready', () => afterRender(true));
  window.LumoCouponCenter = { refresh, reset, home, page, afterRender, model, claim };
})();
