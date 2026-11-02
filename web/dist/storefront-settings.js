(function () {
  'use strict';
  const C = window.LumoConstants;
  const defaults = {
    brand: 'LUMO',
    brandSuffix: 'Mall',
    heroTitle: '智享生活\n从此不同',
    heroSubtitle: '甄选品质好物，享受更美好的生活',
    heroEyebrow: 'SMART LIVING, SIMPLY BETTER',
    heroImage: 'hero.jpg',
    accent: '#ff443d',
    sectionTitle: '今日推荐',
    sectionSubtitle: '精选好物，为你而来',
  };
  const shop = { ...defaults };
  const safeText = (value, max, fallback = '') => (typeof value === 'string' ? value.trim().slice(0, max) : fallback);
  function safeImage(value, fallback = 'hero.jpg') {
    if (typeof value !== 'string' || value.length > 2500000) return fallback;
    if (/^data:image\/(png|jpeg|webp);base64,[A-Za-z0-9+/=]+$/.test(value)) return value;
    if (/^https:\/\//i.test(value)) {
      try {
        const u = new URL(value);
        if (u.protocol === 'https:' && !u.username && !u.password) return u.href;
      } catch (_) {}
    }
    if (/^(?:assets\/)?demo-[a-z0-9-]+\.svg$/.test(value)) return value;
    if (/^(?:assets\/)?[A-Za-z0-9_-]+\.(?:png|jpe?g|webp)$/i.test(value)) return value;
    return fallback;
  }
  function asset(value) {
    const src = safeImage(value);
    return /^https:|^data:|^assets\//i.test(src) ? src : 'assets/' + src;
  }
  function normalizeSettings(raw) {
    const output = { ...defaults, ...(window.LumoStorefrontLayout?.normalize(raw) || {}) };
    if (raw?.heroFileId) output.heroFileId = safeText(raw.heroFileId, 128);
    const lengths = {
      brand: 20,
      brandSuffix: 12,
      heroTitle: 60,
      heroSubtitle: 120,
      heroEyebrow: 80,
      sectionTitle: 30,
      sectionSubtitle: 80,
    };
    for (const key of Object.keys(lengths))
      if (raw && typeof raw[key] === 'string')
        output[key] = safeText(raw[key], lengths[key], defaults[key]) || defaults[key];
    output.heroImage = safeImage(raw?.heroImage, defaults.heroImage);
    output.accent = /^#[0-9a-f]{6}$/i.test(raw?.accent || '') ? raw.accent : defaults.accent;
    return output;
  }
  function applyTheme() {
    document.documentElement.style.setProperty('--red', shop.accent);
    document.documentElement.style.removeProperty?.('--red-dark');
    document.documentElement.style.removeProperty?.('--pale');
  }

  let revision = 0,
    flight = null,
    editorFlight = null,
    loaded = false,
    error = '';
  const scope = () => window.LumoAPI?.getBase?.() || location.origin;
  async function refresh() {
    if (/^#\/editor(?:\/|$)/.test(location.hash) && window.LumoAuth?.user()?.role === C.Role.Admin) {
      if (editorFlight) return editorFlight;
      error = '';
      editorFlight = window.LumoRouteAssets.ensureEditor()
        .then(() => window.LumoStudio.refresh())
        .catch((cause) => {
          error = cause.message;
          throw cause;
        })
        .finally(() => {
          editorFlight = null;
        });
      return editorFlight;
    }
    if (flight?.scope === scope()) return flight.promise;
    const owner = scope(),
      ticket = ++revision;
    const pending = { scope: owner, promise: null };
    pending.promise = (async () => {
      try {
        const row = await window.LumoAPI.call(
          C.Service.Content,
          C.Action.Get,
          { id: 'storefront-main' },
          { auth: false },
        ).catch((cause) => {
          if (cause.status === 404) return null;
          throw cause;
        });
        if (owner !== scope() || ticket !== revision) return;
        Object.assign(shop, row ? normalizeSettings(JSON.parse(row.body)) : defaults);
        loaded = true;
        error = '';
        applyTheme();
      } catch (cause) {
        if (owner === scope() && ticket === revision) error = cause.message;
        throw cause;
      } finally {
        if (flight === pending) flight = null;
      }
    })();
    flight = pending;
    return pending.promise;
  }
  const stub = {
    settings: shop,
    safeImage,
    asset,
    refresh,
    readState: () => ({ settingsLoaded: loaded, catalogLoaded: false, loading: !!flight, error }),
    renderEditor() {
      if (window.LumoAuth?.user()?.role !== C.Role.Admin)
        return `${header()}<main id="main" class="container"><h1>商城编辑工作台</h1><p>需要使用管理员账号登录后，才能管理商品、库存和首页装修。</p><a href="#/">返回商城</a></main>${footer()}`;
      if (!editorFlight && !error)
        refresh()
          .then(() => renderKeepScroll())
          .catch(() => renderKeepScroll());
      return `${header()}<main id="main" class="container"><h1>商城编辑工作台</h1><p role="status">${error ? esc(error) : '正在加载编辑器…'}</p>${error ? '<button data-studio-load-retry>重新加载</button>' : ''}</main>${footer()}`;
    },
  };
  document.addEventListener('click', (event) => {
    if (event.target.closest('[data-studio-load-retry]'))
      refresh()
        .then(() => renderKeepScroll())
        .catch((cause) => {
          error = cause.message;
          renderKeepScroll();
        });
  });
  window.LumoStorefrontSettings = { defaults, settings: shop, safeImage, asset, normalizeSettings, applyTheme };
  window.LumoStudio = stub;
  applyTheme();
})();
