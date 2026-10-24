(function () {
  'use strict';
  const C = window.LumoConstants;
  const API = window.LumoAPI;
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (ch) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[ch],
    );
  const text = (value, limit = 300) => (typeof value === 'string' ? [...value].slice(0, limit).join('') : '');
  const number = (value) => Math.max(0, Math.floor(Number(value) || 0));
  const yuan = (value) => (number(value) / 100).toLocaleString('zh-CN', { maximumFractionDigits: 2 });
  const date = (value) => {
    const timestamp = Date.parse(value);
    return Number.isFinite(timestamp)
      ? new Date(timestamp).toLocaleString('zh-CN', {
          timeZone: 'Asia/Shanghai',
          year: 'numeric',
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        })
      : '';
  };
  const loggedIn = () => !!window.LumoAuth?.isLoggedIn();
  const scope = () =>
    JSON.stringify([
      API.getBase?.() || location.origin,
      API.getToken?.() || '',
      window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '',
    ]);
  function placement() {
    const name = (location.hash || '#/').slice(2).split('/')[0];
    if (!name)
      return typeof ui !== 'undefined' && ((ui.category && ui.category !== '全部好物') || ui.query || ui.onlyFavorites)
        ? C.PopupPlacement.Category
        : C.PopupPlacement.Home;
    return [C.PopupPlacement.Category, C.PopupPlacement.Product, C.PopupPlacement.Coupons].includes(name)
      ? name
      : C.PopupPlacement.None;
  }
  const route = () =>
    JSON.stringify([
      location.hash || '#/',
      placement(),
      typeof ui !== 'undefined' && placement() === C.PopupPlacement.Category
        ? [ui.category, ui.query, ui.onlyFavorites]
        : null,
    ]);
  const safeTarget = (value) =>
    typeof value === 'string' &&
    /^#\/(?:coupons|member\/coupons|promotions(?:\/(?:flash|group|exchange|reduction|discount|quantity_discount|direct)(?:\/[A-Za-z0-9_-]+)?)?|product\/(?:[A-Za-z0-9_.:-]|%[A-Fa-f0-9]{2}){1,180})?$/.test(
      value,
    )
      ? value
      : '#/coupons';
  let epoch = 0,
    ready = false,
    candidate = null,
    root = null,
    observer = null,
    waitTimer = null,
    delayTimer = null,
    clockTimer = null,
    routeTimer = null;
  let lastDecision = '',
    requestController = null,
    restoreFocus = null,
    previousOverflow = '',
    visible = false,
    approving = false,
    approved = false,
    busy = false,
    claimed = null,
    failure = '',
    shownAt = 0,
    delayElapsed = false;
  const context = () => ({ epoch, scope: scope(), route: route(), placement: placement() });
  const current = (ticket) => ticket && ticket.epoch === epoch && ticket.scope === scope() && ticket.route === route();
  function clearTimers() {
    clearTimeout(waitTimer);
    clearTimeout(delayTimer);
    clearInterval(clockTimer);
    waitTimer = delayTimer = clockTimer = null;
    observer?.disconnect();
    observer = null;
  }
  function teardown() {
    clearTimers();
    requestController?.abort();
    requestController = null;
    if (root) {
      root.remove();
      root = null;
    }
    if (visible) {
      // Another modal may have opened while this one was visible. Its scroll
      // lock remains owned by that modal, rather than by this marketing view.
      if (!otherModal()) {
        document.body.style.overflow = previousOverflow;
        if (restoreFocus?.isConnected && typeof restoreFocus.focus === 'function') restoreFocus.focus();
      }
    }
    visible = false;
    restoreFocus = null;
  }
  function reset() {
    epoch++;
    teardown();
    candidate = null;
    approving = approved = busy = false;
    claimed = null;
    failure = '';
    ready = false;
    lastDecision = '';
  }
  function beginBootstrap() {
    reset();
    lastDecision = route();
    return context();
  }
  function validPopup(value) {
    if (
      !value ||
      typeof value !== 'object' ||
      !text(value.delivery_id, 180) ||
      !value.content ||
      typeof value.content !== 'object'
    )
      return null;
    const expiry = Date.parse(value.expires_at);
    if (!Number.isFinite(expiry) || expiry <= Date.now()) return null;
    return {
      ...value,
      delivery_id: text(value.delivery_id, 180),
      content: { ...value.content },
      view_id: API.key('popup-view'),
    };
  }
  function hydrate(value, ticket = context()) {
    if (!current(ticket)) return false;
    ready = true;
    lastDecision = ticket.route;
    teardown();
    candidate = validPopup(value);
    approving = approved = busy = false;
    claimed = null;
    failure = '';
    delayElapsed = false;
    if (candidate && ticket.placement !== C.PopupPlacement.None) schedule(ticket);
    return !!candidate;
  }
  function otherModal() {
    const modal = document.getElementById('modal-root');
    if (modal && modal.innerHTML.trim()) return true;
    return [...document.querySelectorAll('[role="dialog"][aria-modal="true"]')].some(
      (element) => !root?.contains(element),
    );
  }
  function expired() {
    return !candidate || Date.parse(candidate.expires_at) <= Date.now();
  }
  function schedule(ticket) {
    if (!current(ticket) || !candidate || visible || approving || expired()) return;
    // Background tabs and hidden previews never consume an impression.
    if (document.visibilityState !== 'visible') return;
    if (otherModal()) {
      if (!observer && window.MutationObserver) {
        observer = new MutationObserver(() => schedule(ticket));
        observer.observe(document.body, { childList: true, subtree: true });
      }
      if (!waitTimer)
        waitTimer = setTimeout(() => {
          waitTimer = null;
          schedule(ticket);
        }, 300);
      return;
    }
    observer?.disconnect();
    observer = null;
    if (!delayElapsed && number(candidate.content.delay_seconds)) {
      if (!delayTimer)
        delayTimer = setTimeout(
          () => {
            delayTimer = null;
            delayElapsed = true;
            schedule(ticket);
          },
          Math.min(30, number(candidate.content.delay_seconds)) * 1000,
        );
      return;
    }
    void show(ticket);
  }
  async function show(ticket) {
    if (
      !current(ticket) ||
      !candidate ||
      visible ||
      approving ||
      document.visibilityState !== 'visible' ||
      otherModal() ||
      expired()
    )
      return;
    if (!approved) {
      approving = true;
      const item = candidate;
      requestController = new AbortController();
      try {
        const result = await API.call(
          C.Service.Storefront,
          C.Action.PopupEvent,
          { data: { delivery_id: item.delivery_id, view_id: item.view_id, event: C.PopupEvent.Impression } },
          { signal: requestController.signal },
        );
        if (!current(ticket) || item !== candidate) return;
        if (result.allowed !== true) {
          candidate = null;
          return;
        }
        approved = true;
      } catch (_) {
        if (current(ticket)) candidate = null;
        return;
      } finally {
        if (current(ticket)) {
          approving = false;
          requestController = null;
        }
      }
    }
    if (!current(ticket) || !candidate || expired()) return;
    if (document.visibilityState !== 'visible' || otherModal()) {
      schedule(ticket);
      return;
    }
    clearTimers();
    restoreFocus = document.activeElement;
    previousOverflow = document.body.style.overflow || '';
    visible = true;
    shownAt = Date.now();
    document.body.style.overflow = 'hidden';
    root = document.createElement('div');
    root.id = 'lumo-marketing-popup';
    root.className = 'mp-overlay';
    document.body.appendChild(root);
    render();
    root.querySelector('[data-popup-action="primary"]')?.focus();
    clockTimer = setInterval(tick, 1000);
  }
  function coupon() {
    return claimed ? { ...candidate?.coupon, ...claimed } : candidate?.coupon || null;
  }
  function hasClaimed() {
    const row = coupon();
    return !!claimed || row?.claimed === true || row?.claim_status === 'claimed';
  }
  function label() {
    if (busy) return '正在领取…';
    const content = candidate.content;
    if (content.action_type === C.PopupAction.ClaimCoupon)
      return hasClaimed() ? '立即使用' : !loggedIn() ? '登录领取' : text(content.button_text, 32) || '立即领取';
    return text(content.button_text, 32) || '去领券中心';
  }
  function render() {
    if (!root || !candidate) return;
    const content = candidate.content,
      row = coupon(),
      theme = [C.PopupTheme.Red, C.PopupTheme.Peach, C.PopupTheme.Plum].includes(content.theme)
        ? content.theme
        : C.PopupTheme.Red;
    const end = claimed?.expires_at || candidate.coupon?.expires_at;
    const validity =
      hasClaimed() && date(end)
        ? `有效期至 ${date(end)}（北京时间）`
        : row
          ? `领取后 ${number(row.valid_days)} 天内有效${date(end) ? ` · ${date(end)} 截止领取` : ''}`
          : '';
    const min = number(row?.min_cents),
      amount = number(row?.amount_cents);
    root.innerHTML = `<div class="mp-shell"><section class="mp-card mp-theme-${theme}" role="dialog" aria-modal="true" aria-labelledby="mp-title" aria-describedby="mp-description"><span class="mp-confetti mp-confetti-one" aria-hidden="true">✦</span><span class="mp-confetti mp-confetti-two" aria-hidden="true">✧</span><div class="mp-heading"><span class="mp-eyebrow">LUMO · 为你准备的好礼</span><h2 id="mp-title">${esc(text(content.title, 80) || '送你一份专属好礼')}</h2>${content.subtitle ? `<p>${esc(text(content.subtitle, 160))}</p>` : ''}</div>${row ? `<div class="mp-ticket"><div class="mp-value"><small>¥</small><strong>${yuan(amount)}</strong></div><div class="mp-ticket-info"><strong>${min ? `满 ¥${yuan(min)} 可用` : '无使用门槛'}</strong><span>${esc(text(row.name, 80) || '品质生活优惠券')}</span><small>普通商品可用 · 每人限领一次</small></div><span class="mp-ticket-seal">${hasClaimed() ? '已领取' : '专享礼券'}</span></div>` : '<div class="mp-gift" aria-hidden="true"><span>礼</span><i></i></div>'}<div class="mp-details"><p id="mp-description">${esc(text(content.body, 400) || (hasClaimed() ? '优惠券已放入你的账户，去挑选心动好物吧。' : '好券搭配好物，让这一次心动更划算。'))}</p>${failure ? `<p class="mp-error" role="alert">${esc(failure)}</p>` : hasClaimed() ? '<p class="mp-success" role="status">领取成功，已放入我的优惠券</p>' : ''}<button type="button" class="mp-primary" data-popup-action="primary" ${busy ? 'disabled aria-busy="true"' : ''}>${esc(label())}<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7"/></svg></button>${validity ? `<p class="mp-validity">${esc(validity)}</p>` : ''}<span class="mp-rule">领取与使用条件以优惠券规则为准</span></div></section><button type="button" class="mp-close" data-popup-action="close" aria-label="关闭优惠券弹窗"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18"/></svg></button><p class="mp-countdown" data-popup-countdown aria-live="off"></p></div>`;
    updateClock();
  }
  function updateClock() {
    const element = root?.querySelector('[data-popup-countdown]');
    const duration = Math.min(60, number(candidate?.content.auto_close_seconds));
    if (element)
      element.textContent = duration
        ? `${Math.max(0, duration - Math.floor((Date.now() - shownAt) / 1000))} 秒后自动关闭`
        : '';
  }
  function tick() {
    if (!candidate || !visible) return;
    const duration = Math.min(60, number(candidate.content.auto_close_seconds));
    if (expired() || (duration && Date.now() - shownAt >= duration * 1000 && !busy)) {
      close(C.PopupEvent.AutoClose);
      return;
    }
    updateClock();
  }
  function event(name, item = candidate) {
    if (!item || !approved) return Promise.resolve();
    return API.call(
      C.Service.Storefront,
      C.Action.PopupEvent,
      { data: { delivery_id: item.delivery_id, view_id: item.view_id, event: name } },
      { timeout: 10000 },
    ).catch(() => {});
  }
  function close(reason = C.PopupEvent.Dismiss) {
    const item = candidate;
    if (visible) void event(reason, item);
    epoch++;
    teardown();
    candidate = null;
    approving = approved = busy = false;
    claimed = null;
    failure = '';
  }
  async function primary() {
    if (!visible || !candidate || busy) return;
    const item = candidate,
      ticket = context(),
      content = item.content;
    void event(C.PopupEvent.Click, item);
    if (content.action_type !== C.PopupAction.ClaimCoupon || hasClaimed()) {
      const destination = content.action_type === C.PopupAction.Link ? safeTarget(content.action_target) : '#/coupons';
      close(C.PopupEvent.Dismiss);
      location.hash = destination;
      return;
    }
    if (!loggedIn()) {
      close(C.PopupEvent.Dismiss);
      window.LumoAuth?.requireLogin('#/coupons');
      return;
    }
    busy = true;
    failure = '';
    render();
    requestController = new AbortController();
    try {
      const result = await API.call(
        C.Service.Storefront,
        C.Action.PopupClaim,
        { data: { delivery_id: item.delivery_id, view_id: item.view_id } },
        { signal: requestController.signal },
      );
      if (!current(ticket) || item !== candidate) return;
      if (!result.coupon || typeof result.coupon !== 'object') throw new Error('领取结果暂未确认，请使用原弹窗重试');
      claimed = result.coupon;
      failure = '';
      busy = false;
      render();
      root?.querySelector('[data-popup-action="primary"]')?.focus();
      const refreshed = await Promise.allSettled([
        window.LumoMember?.refresh?.(true),
        window.LumoCouponCenter?.refresh?.(true),
      ]);
      if (!current(ticket) || item !== candidate) return;
      if (refreshed.some((row) => row.status === 'rejected')) {
        failure = '券已到账，账户信息可在我的优惠券中刷新查看。';
        render();
      }
    } catch (error) {
      if (!current(ticket) || item !== candidate) return;
      failure = text(error?.message, 250) || '领取失败，请稍后重试';
      busy = false;
      render();
      root?.querySelector('[data-popup-action="primary"]')?.focus();
    } finally {
      if (current(ticket)) {
        busy = false;
        requestController = null;
      }
    }
  }
  async function requestRoute() {
    if (!ready || window.LumoConnection?.refreshing) return;
    const next = route();
    if (lastDecision === next) return;
    epoch++;
    teardown();
    candidate = null;
    approving = approved = busy = false;
    claimed = null;
    failure = '';
    lastDecision = next;
    const ticket = context();
    if (ticket.placement === C.PopupPlacement.None) return;
    requestController = new AbortController();
    try {
      const result = await API.call(
        C.Service.Storefront,
        C.Action.Popup,
        { data: { placement: ticket.placement } },
        { signal: requestController.signal },
      );
      if (current(ticket)) hydrate(result.popup, ticket);
    } catch (_) {
      /* Optional marketing never interrupts shopping. */
    } finally {
      if (current(ticket)) requestController = null;
    }
  }
  function routeChanged() {
    epoch++;
    teardown();
    candidate = null;
    approving = approved = busy = false;
    claimed = null;
    failure = '';
    clearTimeout(routeTimer);
    routeTimer = setTimeout(() => {
      routeTimer = null;
      void requestRoute();
    }, 0);
  }
  document.addEventListener(
    C.PopupEvent.Click,
    (event) => {
      const button = event.target.closest?.('[data-popup-action]');
      if (button && root?.contains(button)) {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (button.dataset.popupAction === 'close') close();
        if (button.dataset.popupAction === 'primary') void primary();
        return;
      }
      const category = event.target.closest?.(
        '[data-category-select],[data-action="category"],[data-action="reset-filters"]',
      );
      if (category)
        Promise.resolve().then(() => {
          if (lastDecision !== route()) routeChanged();
        });
    },
    true,
  );
  document.addEventListener(
    'keydown',
    (event) => {
      if (!visible || !root) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopImmediatePropagation();
        close();
        return;
      }
      if (event.key === 'Tab') {
        const buttons = [...root.querySelectorAll('button:not(:disabled),a[href],[tabindex="0"]')];
        const first = buttons[0],
          last = buttons[buttons.length - 1];
        if (!first) {
          event.preventDefault();
          return;
        }
        if (event.shiftKey && (document.activeElement === first || !root.contains(document.activeElement))) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && (document.activeElement === last || !root.contains(document.activeElement))) {
          event.preventDefault();
          first.focus();
        }
      }
    },
    true,
  );
  document.addEventListener('visibilitychange', () => {
    if (candidate && !visible) schedule(context());
  });
  window.addEventListener('hashchange', routeChanged);
  for (const name of ['lumo:login', 'lumo:logout', 'lumo:session-expired', 'lumo:api-scope-changed'])
    window.addEventListener(name, reset);
  window.addEventListener('lumo:ready', (event) => {
    ready = event.detail?.ready !== false;
    // Bootstrap dispatches ready before releasing its running promise. Route
    // entry work begins in the following task, when that snapshot is released.
    clearTimeout(routeTimer);
    routeTimer = setTimeout(() => {
      routeTimer = null;
      if (lastDecision !== route()) void requestRoute();
      else if (candidate) schedule(context());
    }, 0);
  });
  window.LumoMarketing = {
    hydrate,
    beginBootstrap,
    reset,
    placement,
    model: () => ({
      visible,
      pending: !!candidate && !visible,
      busy,
      claimed: hasClaimed(),
      error: failure,
      campaign_id: candidate?.campaign_id || '',
    }),
  };
})();
