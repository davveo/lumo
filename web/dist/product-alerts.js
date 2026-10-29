(function () {
  'use strict';
  const C = window.LumoConstants;
  const esc = (v) =>
    String(v ?? '').replace(
      /[&<>"']/g,
      (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
    );
  const money = (v) => '¥' + (Number(v || 0) / 100).toFixed(2);
  const names = {
    [C.SubscriptionStatus.Active]: '等待触发',
    [C.SubscriptionStatus.Triggered]: '已提醒',
    [C.SubscriptionStatus.Cancelled]: '已取消',
    [C.SubscriptionStatus.Expired]: '已过期',
  };
  const scope = () => JSON.stringify([window.LumoAPI.getBase?.(), window.LumoAPI.getToken?.()]);
  const initial = () => ({
    loaded: false,
    busy: false,
    error: '',
    items: [],
    total: 0,
    page: 1,
    policy: null,
    policyAt: 0,
    policyFailed: false,
  });
  let owner = scope(),
    data = initial(),
    version = 0,
    flight = null;
  const call = (action, body = {}, options) => window.LumoAPI.call(C.Service.Engagement, action, body, options);
  const redraw = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  function check() {
    if (owner !== scope()) {
      owner = scope();
      data = initial();
      version++;
      flight = null;
    }
  }
  async function refresh() {
    check();
    if (flight) return flight;
    const ticket = ++version,
      who = owner;
    data.busy = true;
    data.error = '';
    flight = (async () => {
      try {
        const result = await call(C.Action.Subscriptions, { data: { limit: 10, offset: (data.page - 1) * 10 } });
        if (ticket === version && who === scope())
          Object.assign(data, { loaded: true, items: result.items || [], total: Number(result.total) || 0 });
      } catch (e) {
        if (ticket === version && who === scope()) data.error = e.message;
      } finally {
        if (ticket === version && who === scope()) {
          data.busy = false;
          flight = null;
          if (location.hash === '#/member/alerts') redraw();
        }
      }
    })();
    return flight;
  }
  async function policy() {
    check();
    if (data.policyAt && Date.now() - data.policyAt < 30000) return;
    data.policyAt = Date.now();
    const who = owner;
    try {
      const r = await call(C.Action.PolicyGet, {}, { auth: false });
      if (who === scope()) {
        data.policy = r.config;
        data.policyFailed = false;
        redraw();
      }
    } catch (_) {
      if (who === scope()) data.policyFailed = true;
    }
  }
  function actions(product) {
    check();
    const p = data.policy;
    if (!p) return '';
    const sku = typeof skuForItem === 'function' ? skuForItem(detail) : product.skuId || product.id;
    const stock = window.LumoInventory?.snapshot?.(sku);
    return `<div class="product-alert-actions">${p.price_enabled ? `<button type="button" data-alert-action="price" data-sku="${esc(sku)}">${icon('bell')}降价提醒</button>` : ''}${p.stock_enabled && stock && Number(stock.available) <= 0 ? `<button type="button" data-alert-action="stock" data-sku="${esc(sku)}">${icon('bell')}到货提醒</button>` : ''}<a href="#/member/alerts">管理我的提醒</a></div>`;
  }
  function page() {
    check();
    return `<div class="mem-pagehead"><div><h1>购物提醒</h1><p>订阅商品降价与到货动态，触发后会在消息中心提醒一次。</p></div><button class="mem-outline" data-alert-action="refresh" ${data.busy ? 'disabled' : ''}>刷新</button></div>${data.error ? `<p class="auth-error is-visible" role="alert">${esc(data.error)}</p>` : ''}<section class="mem-panel" aria-busy="${data.busy}">${!data.loaded && !data.error ? window.LumoSkeleton.render('rows', { count: 3 }) : data.items.length ? `<div class="alert-subscriptions">${data.items.map((row) => `<article><div><span class="mem-badge">${row.kind === C.SubscriptionKind.Price ? '降价提醒' : '到货提醒'}</span><h2>${esc(row.product_name)}</h2><p>商品 ${esc(row.sku_id)}${row.kind === C.SubscriptionKind.Price ? ' · 目标价格 ' + money(row.target_cents) : ''}</p><small>有效期至 ${new Date(row.expires_at).toLocaleDateString('zh-CN')}</small></div><div><strong>${esc(row.status === C.SubscriptionStatus.Active && Date.parse(row.expires_at) <= Date.now() ? '已过期' : names[row.status] || row.status)}</strong>${row.status === C.SubscriptionStatus.Active ? `<button class="mem-outline" data-alert-action="cancel" data-id="${esc(row.id)}" ${data.busy ? 'disabled' : ''}>取消提醒</button>` : ''}<a href="#/product/${esc((products.find((p) => p.skuId === row.sku_id || p.variants?.some((v) => v.sku_id === row.sku_id)) || {}).id || row.sku_id)}">查看商品</a></div></article>`).join('')}</div>` : '<div class="mem-empty"><h3>还没有购物提醒</h3><p>前往商品页设置目标价格，或订阅缺货商品的到货提醒。</p><a class="mem-outline" href="#/">去逛逛</a></div>'}<div class="alert-pagination"><span>共 ${data.total} 条</span><button class="mem-outline" data-alert-action="page" data-page="${data.page - 1}" ${data.busy || data.page <= 1 ? 'disabled' : ''}>上一页</button><span>第 ${data.page} 页</span><button class="mem-outline" data-alert-action="page" data-page="${data.page + 1}" ${data.busy || data.page * 10 >= data.total ? 'disabled' : ''}>下一页</button></div></section>`;
  }
  document.addEventListener('click', async (event) => {
    const el = event.target.closest('[data-alert-action]');
    if (!el) return;
    const action = el.dataset.alertAction;
    check();
    if (action === 'refresh' || action === 'page') {
      if (action === 'page') data.page = Math.max(1, Number(el.dataset.page) || 1);
      await refresh();
      return;
    }
    if (!window.LumoAuth?.requireLogin(location.hash)) return;
    if (action === 'cancel') {
      if (data.busy) return;
      data.busy = true;
      redraw();
      try {
        await call(C.Action.Unsubscribe, { id: el.dataset.id });
        await refresh();
      } catch (e) {
        data.error = e.message;
      } finally {
        data.busy = false;
        redraw();
      }
      return;
    }
    if (action === 'close') {
      window.LumoUI?.closeModal?.();
      return;
    }
    if (action === 'price' || action === 'stock') {
      const sku = el.dataset.sku,
        product = products.find((p) => p.skuId === sku || p.id === sku || p.variants?.some((v) => v.sku_id === sku));
      const variant = product?.variants?.find((v) => v.sku_id === sku),
        cents = Number(variant?.price_cents || product?.price_cents || Math.round((product?.price || 0) * 100));
      const modal = document.getElementById('modal-root');
      if (!modal) return;
      const body = `<p>${esc(product?.name || sku)}</p><form id="product-alert-form" data-sku="${esc(sku)}" data-kind="${action}">${action === 'price' ? `<label class="field">目标价格（当前 ${money(cents)}）<input type="number" name="target" min="0.01" max="${Math.max(0.01, (cents - 1) / 100).toFixed(2)}" step="0.01" required placeholder="降到此价格时提醒"></label>` : '<p>商品恢复库存时提醒一次。</p>'}<p class="muted">有效期 ${Number(data.policy?.ttl_days || 30)} 天，提醒将发送到站内消息中心。</p><p id="product-alert-error" role="alert"></p><button class="primary" type="submit">确认订阅</button></form>`;
      if (typeof globalThis.modal === 'function')
        globalThis.modal(action === 'price' ? '设置降价提醒' : '设置到货提醒', body);
    }
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'product-alert-form') return;
    event.preventDefault();
    const form = event.target,
      button = form.querySelector('[type=submit]');
    if (button?.disabled) return;
    const who = scope(),
      kind = form.dataset.kind,
      raw = new FormData(form).get('target');
    if (button) button.disabled = true;
    try {
      await call(C.Action.Subscribe, {
        data: {
          sku_id: form.dataset.sku,
          kind,
          target_cents: kind === C.SubscriptionKind.Price ? Math.round(Number(raw) * 100) : 0,
        },
      });
      if (who !== scope()) return;
      if (typeof closeModal === 'function') closeModal();
      data.loaded = false;
      toast('已订阅，触发后可在消息中心查看');
    } catch (e) {
      if (who === scope()) {
        const note = document.getElementById('product-alert-error');
        if (note) note.textContent = e.message;
      }
    } finally {
      if (button) button.disabled = false;
    }
  });
  window.LumoProductAlerts = {
    actions,
    page,
    refresh,
    afterRender() {
      check();
      if (location.hash.startsWith('#/product/')) void policy();
      if (location.hash === '#/member/alerts' && !data.loaded && !data.busy && !data.error) void refresh();
    },
  };
})();
