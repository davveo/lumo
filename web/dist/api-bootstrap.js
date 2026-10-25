(function () {
  'use strict';
  const API = window.LumoAPI;
  let running = null,
    queued = false,
    ready = false,
    lastError = null,
    statusEl = null,
    routeTimer,
    routeEpoch = 0,
    loadingTimer = null,
    coreLoading = false,
    routeDirty = false,
    loadedRoute = '';
  const escaped = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
    );
  function status(message, error = false) {
    if (!statusEl) {
      statusEl = document.createElement('div');
      document.body.prepend(statusEl);
    }
    statusEl.className = 'api-status' + (error ? ' is-error' : '');
    statusEl.setAttribute('role', error ? 'alert' : 'status');
    statusEl.hidden = !message;
    statusEl.innerHTML = message
      ? `${escaped(message)}${error ? '<button data-api-action="retry">重新连接</button><button data-api-action="settings">接口设置</button>' : ''}`
      : '';
  }
  function stopLoading() {
    clearTimeout(loadingTimer);
    loadingTimer = null;
    coreLoading = false;
  }
  function beginLoading() {
    stopLoading();
    status('');
    if (!ready) {
      coreLoading = true;
      loadingTimer = setTimeout(() => {
        if (coreLoading) status('正在加载商城数据…');
      }, 500);
    }
  }
  function draw(errors) {
    try {
      if (typeof render === 'function') render();
    } catch (error) {
      errors.push(error);
    }
  }
  function report(errors) {
    lastError = errors[0] || null;
    if (errors.length) status(errors[0]?.message || '部分功能暂时未能加载，请重试', true);
    else status('');
  }
  function showConfig() {
    modal(
      '后端连接',
      `<p class="api-config-help">前端服务通过同源代理连接API，本地商城地址为 http://localhost:8080，API联调地址为 http://localhost:18080。使用默认部署无需填写地址；跨域部署时填写允许当前前端来源访问的 HTTPS 网关地址。</p><form id="api-config-form"><label class="api-config-field">网关地址<input name="base" type="url" placeholder="留空使用当前站点" value="${escaped(API.getBase())}"></label><p class="api-config-help">更换地址后需重新登录。</p><button class="primary">保存并连接</button><div id="api-config-error" class="api-config-error"></div></form><div class="api-trace-details">${API.requests()
        .slice(0, 4)
        .map((r) => `${escaped(r.service + '/' + r.action)} · ${r.status} · ${escaped(r.traceId || '无链路编号')}`)
        .join('<br>')}</div>`,
    );
  }
  async function invoke(object, method, ...args) {
    if (typeof object?.[method] === 'function') return object[method](...args);
  }
  function marketing(method, ...args) {
    try {
      return window.LumoMarketing?.[method]?.(...args);
    } catch (_) {
      return null;
    }
  }
  async function stage(tasks, errors) {
    const results = await Promise.allSettled(tasks.map((fn) => Promise.resolve().then(fn)));
    for (const result of results) if (result.status === 'rejected') errors.push(result.reason);
  }
  const currentRoute = () => (location.hash || '#/').slice(2).split('/');
  const loggedIn = () => window.LumoAuth?.isLoggedIn?.() === true;
  async function offers(errors, force = false) {
    if (!force && window.LumoPromotions?.status?.().loaded && window.LumoCouponCenter?.model?.().loaded) return;
    await invoke(API, 'prefetchSection', window.LumoConstants.Action.Activities);
    await stage(
      [() => invoke(window.LumoPromotions, 'refresh'), () => invoke(window.LumoCouponCenter, 'refresh')],
      errors,
    );
  }
  async function account(force = false) {
    const summary = await invoke(window.LumoAccountSummary, 'refresh', force);
    if (summary?.loaded && Array.isArray(summary.favorite_ids) && typeof state !== 'undefined') {
      // IDs support card hearts, but do not claim a full favorite collection was loaded.
      state.favoriteSKUs = [...summary.favorite_ids];
      state.favorites = summary.favorite_ids.map((id) =>
        typeof actualProduct === 'function' ? actualProduct(id)?.id || id : id,
      );
    }
  }
  async function routeData(errors, force = false) {
    const key = location.hash || '#/',
      route = currentRoute(),
      tasks = [];
    const memberTab = route[0] === 'member' ? route[1] || 'overview' : '';
    if (['', 'cart'].includes(route[0])) tasks.push(() => invoke(window.LumoRecommendations, 'refreshHome'));
    if (['', 'cart', 'checkout', 'promotions'].includes(route[0])) tasks.push(() => offers(errors, force));
    if (route[0] === 'coupons')
      tasks.push(() => {
        const center = window.LumoCouponCenter;
        return invoke(center, typeof center?.refreshForView === 'function' ? 'refreshForView' : 'refresh', force);
      });
    if (route[0] === 'product') tasks.push(() => invoke(window.LumoProductExtras, 'refresh', route[1]));
    if (route[0] === 'compare') tasks.push(() => invoke(window.LumoDiscovery, 'refresh'));
    if (route[0] === 'editor') tasks.push(() => invoke(window.LumoStudio, 'refresh'));
    if (!['login', 'editor', 'payment', 'checkout'].includes(route[0]))
      if (force || !window.LumoSidebars?.model?.().loaded || window.LumoSidebars?.model?.().error)
        tasks.push(() => invoke(window.LumoSidebars, 'refresh'));
    if (loggedIn()) {
      if (!['login', 'editor', 'payment', 'checkout'].includes(route[0])) tasks.push(() => account(force));
      if (['cart', 'checkout'].includes(route[0])) tasks.push(() => invoke(window.LumoCommerce, 'refreshCart'));
      if (
        ['orders', 'payment', 'success'].includes(route[0]) ||
        ['overview', 'orders', 'aftersales', 'invoices', 'support'].includes(memberTab) ||
        (route[0] === 'promotions' && route[1] === 'my-groups')
      )
        tasks.push(() => invoke(window.LumoCommerce, 'refreshOrders'));
      if (['member', 'cart', 'checkout'].includes(route[0]))
        tasks.push(() => invoke(window.LumoMember, 'refreshForView'));
      if (['messages', 'invoices', 'support'].includes(memberTab))
        tasks.push(() => invoke(window.LumoServices, 'refreshForView'));
      if (route[0] === 'success')
        tasks.push(async () => {
          await invoke(window.LumoCommerce, 'refreshOrders');
          await invoke(window.LumoRecommendations, 'refresh');
        });
    }
    await stage(
      tasks.map((task) => async () => {
        await task();
        draw(errors);
      }),
      errors,
    );
    if (key === (location.hash || '#/')) loadedRoute = key;
    if (route[0] === 'checkout' && key === (location.hash || '#/'))
      await invoke(window.LumoCommerce, 'invalidateQuote');
  }
  async function refreshRoute() {
    if (!ready || running) {
      routeDirty = true;
      return;
    }
    const revision = ++routeEpoch,
      errors = [];
    await routeData(errors);
    if (revision !== routeEpoch) return;
    if (typeof renderKeepScroll === 'function') renderKeepScroll();
    if (errors.length && typeof toast === 'function') toast(errors[0].message || '数据刷新失败');
  }
  async function refresh(force = false) {
    if (running) {
      queued = true;
      return running;
    }
    beginLoading();
    running = (async () => {
      const errors = [];
      let marketingContext = null;
      try {
        // Identity restoration runs beside public reads, never ahead of them.
        const identity = Promise.resolve()
          .then(() => invoke(window.LumoAuth, 'refresh'))
          .catch((error) => ({ identityError: error }));
        await invoke(API, 'beginSections');
        const shell = async () => {
          await invoke(API, 'prefetchSection', window.LumoConstants.Action.Shell);
          await stage(
            [() => invoke(window.LumoCategories, 'refresh'), () => invoke(window.LumoStudio, 'refresh')],
            errors,
          );
          draw(errors);
        };
        const catalog = async () => {
          let catalogOK = true;
          try {
            await invoke(API, 'prefetchSection', window.LumoConstants.Action.Products);
            await invoke(window.LumoCommerce, 'refreshCatalog');
            await stage(
              [() => invoke(window.LumoInventory, 'refresh'), () => invoke(window.LumoDiscovery, 'refreshSearch')],
              errors,
            );
          } catch (error) {
            catalogOK = false;
            errors.push(error);
          }
          ready = catalogOK;
          stopLoading();
          draw(errors);
          report(errors);
        };
        await stage([shell, catalog], errors);
        const identityError = await identity;
        if (identityError?.identityError) errors.push(identityError.identityError);
        marketingContext = marketing('beginBootstrap') || null;
        await routeData(errors, force);
      } catch (error) {
        errors.push(error);
      } finally {
        await invoke(API, 'endBootstrap');
        stopLoading();
      }
      draw(errors);
      report(errors);
      // Optional targeting is its own lifecycle, not part of shell or product data.
      if (ready && marketingContext && marketingContext.placement !== window.LumoConstants.PopupPlacement.None) {
        try {
          const decision = await invoke(
            API,
            'call',
            window.LumoConstants.Service.Storefront,
            window.LumoConstants.Action.Popup,
            { data: { placement: marketingContext.placement } },
          );
          marketing('hydrate', decision?.popup || null, marketingContext);
        } catch (_) {
          marketing('hydrate', null, marketingContext);
        }
      }
      window.dispatchEvent(
        new CustomEvent('lumo:ready', {
          detail: { ready, errors: errors.map((error) => error?.message || '加载失败') },
        }),
      );
      return { ready, errors };
    })().finally(async () => {
      running = null;
      if (queued) {
        queued = false;
        refresh();
      } else if (routeDirty) {
        routeDirty = false;
        if (loadedRoute !== (location.hash || '#/')) await refreshRoute();
      }
    });
    return running;
  }
  document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-api-action]');
    if (!button) return;
    if (button.dataset.apiAction === 'settings') showConfig();
    if (button.dataset.apiAction === 'retry') refresh(true);
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'api-config-form') return;
    event.preventDefault();
    const button = event.target.querySelector('button');
    button.disabled = true;
    try {
      const base = String(new FormData(event.target).get('base') || '').trim();
      API.setBase(base);
      try {
        localStorage.setItem('lumo-api-base-v1', API.getBase());
      } catch (_) {}
      closeModal();
      await refresh();
    } catch (error) {
      const el = document.getElementById('api-config-error');
      if (el) el.textContent = error.message;
    } finally {
      button.disabled = false;
    }
  });
  window.addEventListener('lumo:login', () => refresh());
  window.addEventListener('lumo:logout', () => refresh());
  window.addEventListener('lumo:catalog-changed', () => refresh());
  window.addEventListener('hashchange', () => {
    clearTimeout(routeTimer);
    if (running) {
      routeDirty = true;
      return;
    }
    routeTimer = setTimeout(() => {
      void refreshRoute();
    }, 60);
  });
  // Personal search history and comparisons are useful when these tools open,
  // not prerequisites for painting the public home catalog.
  document.addEventListener('focusin', (event) => {
    if (event.target?.id !== 'search-input' || !ready || running) return;
    void invoke(window.LumoDiscovery, 'refresh').catch(() => {});
  });
  window.LumoConnection = {
    refresh,
    showConfig,
    get ready() {
      return ready;
    },
    get refreshing() {
      return !!running;
    },
    get error() {
      return lastError;
    },
  };
  window.LumoAppReady = refresh();
})();
