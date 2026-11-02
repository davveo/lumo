(function () {
  'use strict';
  const key = () => 'lumo-guest-cart-v1::' + (window.LumoAPI?.getBase?.() || location.origin);
  let flight = null;
  const empty = () => ({ items: [], revision: '', expires: 0 });
  function read() {
    try {
      const value = JSON.parse(localStorage.getItem(key()) || 'null');
      if (!value || !Number.isFinite(value.expires) || value.expires < Date.now()) return empty();
      return {
        ...value,
        items: (value.items || [])
          .filter(
            (r) =>
              typeof r.sku_id === 'string' &&
              r.sku_id.length <= 128 &&
              Number.isInteger(r.quantity) &&
              r.quantity > 0 &&
              r.quantity <= 10,
          )
          .slice(0, 100),
      };
    } catch {
      return empty();
    }
  }
  function write(value) {
    const next = { ...value, revision: crypto.randomUUID(), expires: Date.now() + 30 * 86400000 };
    localStorage.setItem(key(), JSON.stringify(next));
    return next;
  }
  async function rows(value = read()) {
    await hydrateProducts(value.items.map((r) => r.sku_id));
    return value.items.map((row) => {
      const item = normalizeItem(row);
      item.unavailable = actualProduct(item.sku_id)?.visible === false;
      return item;
    });
  }
  async function refresh() {
    if (window.LumoAuth?.isLoggedIn()) return [];
    const storageKey = key(),
      value = read(),
      items = await rows(value);
    if (!window.LumoAuth?.isLoggedIn() && key() === storageKey && read().revision === value.revision)
      state.cart = items;
    return items;
  }
  async function add(item) {
    const sku = skuForItem(item),
      value = read(),
      prior = value.items.find((r) => r.sku_id === sku),
      qty = (prior?.quantity || 0) + (Number(item.qty) || 1);
    const available = window.LumoInventory?.snapshot?.(sku)?.available;
    if (available != null && qty > Number(available)) throw Error('库存不足，请减少数量');
    if (qty > 10) throw Error('同一规格最多添加10件');
    if (!prior && value.items.length >= 100) throw Error('购物车最多保存100种规格');
    if (prior) prior.quantity = qty;
    else value.items.push({ sku_id: sku, quantity: qty });
    write(value);
    await refresh();
    renderKeepScroll();
    toast('已加入游客购物车，登录后自动合并');
    return true;
  }
  async function set(item, quantity) {
    const value = read(),
      row = value.items.find((r) => r.sku_id === skuForItem(item));
    if (!row) return;
    if (!Number.isInteger(quantity) || quantity < 1 || quantity > 10) throw Error('数量需为1至10件');
    row.quantity = quantity;
    write(value);
    await refresh();
    renderKeepScroll();
  }
  async function remove(items) {
    const ids = new Set(items.map(skuForItem)),
      value = read();
    value.items = value.items.filter((r) => !ids.has(r.sku_id));
    write(value);
    await refresh();
    closeModal();
    renderKeepScroll();
  }
  function merge() {
    if (flight) return flight;
    const owner = window.LumoAuth?.user()?.id,
      token = window.LumoAPI.getToken(),
      storageKey = key(),
      value = read();
    if (!owner || !value.items.length) return Promise.resolve();
    if (value.owner && value.owner !== owner) return Promise.reject(Error('请登录原账号，完成尚未同步的游客购物车'));
    const current = () =>
      key() === storageKey && window.LumoAPI.getToken() === token && window.LumoAuth?.user()?.id === owner;
    flight = (async () => {
      const items = await rows(value);
      if (!current()) throw Error('登录状态或服务地址已变化，请重新同步购物车');
      const latest = read();
      if (latest.owner && latest.owner !== owner) throw Error('请登录原账号，完成尚未同步的游客购物车');
      if (latest.revision !== value.revision) throw Error('购物车已在其他页面发生变化，请重试合并');
      value.owner = owner;
      value.mergeKey = latest.mergeKey || crypto.randomUUID();
      localStorage.setItem(storageKey, JSON.stringify(value));
      const valid = items.filter((r) => !r.unavailable).map((r) => ({ sku_id: r.sku_id, quantity: r.qty }));
      if (valid.length)
        await window.LumoAPI.call('cart', 'merge', { idempotency_key: value.mergeKey, data: { items: valid } });
      if (!current()) throw Error('登录状态或服务地址已变化，购物车已保留');
      const saved = read();
      if (saved.revision === value.revision && saved.owner === owner && saved.mergeKey === value.mergeKey)
        localStorage.removeItem(storageKey);
      if (valid.length < items.length) toast('部分商品已下架，可售商品已合并至账号购物车');
    })().finally(() => {
      flight = null;
    });
    return flight;
  }
  document.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-guest-merge]');
    if (!button) return;
    button.disabled = true;
    try {
      await merge();
      await window.LumoCommerce.refreshCart();
      renderKeepScroll();
    } catch (error) {
      toast(error.message);
    } finally {
      button.disabled = false;
    }
  });
  window.LumoGuestCart = {
    read,
    rows,
    refresh,
    add,
    set,
    remove,
    merge,
    count: () => read().items.reduce((n, r) => n + r.quantity, 0),
  };
  window.addEventListener('storage', (event) => {
    if (event.key === key() && !window.LumoAuth?.isLoggedIn())
      void refresh()
        .then(renderKeepScroll)
        .catch((e) => toast(e.message));
  });
})();
