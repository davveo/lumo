(function () {
  'use strict';
  const C = window.LumoConstants;
  const snapshots = new Map();
  const errors = new Map();
  // A late response from an older read must not replace newer stock or errors.
  const readVersions = new Map();
  let readClock = 0;
  let refreshPromise;
  const LIMIT = 10,
    BATCH_SIZE = 500;
  const find = (value) => {
    const id = typeof value === 'string' ? value : value?.id;
    return products.find((p) => p.id === id || (p.skuId || p.sku_id) === id);
  };
  const sku = (value) => {
    if (value && typeof value === 'object') {
      if (value.sku_id || value.skuId) return String(value.sku_id || value.skuId);
      if (window.LumoCommerce?.skuForItem) {
        try {
          return window.LumoCommerce.skuForItem(value);
        } catch (_) {
          return '';
        }
      }
    }
    const p = find(value);
    return p?.skuId || p?.sku_id || p?.id || (typeof value === 'string' ? value : '');
  };
  const beginRead = (key) => {
    const version = ++readClock;
    readVersions.set(key, version);
    return version;
  };
  const latestRead = (key, version) => readVersions.get(key) === version;
  function prime(rows, ticket) {
    if (!Number.isSafeInteger(ticket) || ticket < 0 || ticket > readClock || !Array.isArray(rows)) return 0;
    let loaded = 0;
    for (const row of rows.slice(0, BATCH_SIZE)) {
      const key = typeof row?.sku_id === 'string' ? row.sku_id : '';
      if (
        !key ||
        key.length > 150 ||
        key.trim() !== key ||
        !Number.isFinite(row.available) ||
        row.available < 0 ||
        (readVersions.get(key) || 0) > ticket
      )
        continue;
      beginRead(key);
      snapshots.set(key, { ...row });
      errors.delete(key);
      loaded++;
    }
    return loaded;
  }
  async function refreshOne(id) {
    const key = sku(id);
    if (!key) return;
    const version = beginRead(key);
    try {
      const row = await window.LumoAPI.call(C.Service.Inventory, C.Action.Get, { id: key }, { auth: false });
      if (latestRead(key, version)) {
        snapshots.set(key, row);
        errors.delete(key);
      }
      return row;
    } catch (e) {
      if (latestRead(key, version)) {
        snapshots.delete(key);
        errors.set(key, e);
      }
      throw e;
    }
  }
  async function refreshBatch(keys) {
    const versions = new Map(keys.map((key) => [key, beginRead(key)]));
    let result;
    try {
      result = await window.LumoAPI.call(
        C.Service.Inventory,
        C.Action.BatchGet,
        { data: { ids: keys } },
        { auth: false },
      );
    } catch (error) {
      for (const key of keys)
        if (latestRead(key, versions.get(key))) {
          snapshots.delete(key);
          errors.set(key, error);
        }
      return { loaded: 0, failed: keys.length, error };
    }
    const rows = new Map(
      (Array.isArray(result?.items) ? result.items : [])
        .filter((row) => row?.sku_id)
        .map((row) => [String(row.sku_id), row]),
    );
    const missing = new Set(Array.isArray(result?.missing_ids) ? result.missing_ids : []);
    let loaded = 0,
      failed = 0,
      error;
    for (const key of keys) {
      if (rows.has(key) && !missing.has(key)) {
        if (latestRead(key, versions.get(key))) {
          snapshots.set(key, rows.get(key));
          errors.delete(key);
        }
        loaded++;
      } else {
        const unavailable = new Error('未找到商品库存，请刷新后重试');
        unavailable.code = C.ApiErrorCode.NotFound;
        if (latestRead(key, versions.get(key))) {
          snapshots.delete(key);
          errors.set(key, unavailable);
        }
        failed++;
        error ||= unavailable;
      }
    }
    return { loaded, failed, error };
  }
  async function refresh(ids) {
    const keys = Array.from(new Set((Array.isArray(ids) ? ids : products).map(sku).filter(Boolean)));
    if (!ids && refreshPromise) return refreshPromise;
    const batches = [];
    for (let start = 0; start < keys.length; start += BATCH_SIZE)
      batches.push(refreshBatch(keys.slice(start, start + BATCH_SIZE)));
    const work = Promise.all(batches).then((results) => {
      const loaded = results.reduce((total, row) => total + row.loaded, 0),
        failed = keys.length - loaded;
      if (!loaded && failed) throw results.find((row) => row.error).error;
      return { loaded, failed };
    });
    if (ids) return work;
    refreshPromise = work.finally(() => {
      refreshPromise = null;
    });
    return refreshPromise;
  }
  function snapshot(value) {
    return snapshots.get(sku(value));
  }
  function stockFor(value) {
    const p = find(value);
    if (p?.visible === false) return 0;
    const row = snapshot(value);
    return row ? Math.max(0, Number(row.available) || 0) : 0;
  }
  function validate(items) {
    if (!Array.isArray(items) || !items.length) return '请先选择商品';
    const totals = new Map();
    for (const item of items) {
      const p = find(item),
        key = sku(item),
        q = Number(item.qty);
      if (!p || p.visible === false) return '商品不存在或已下架';
      if (!key) return '所选规格已变化，请重新选择';
      if (!Number.isInteger(q) || q < 1 || q > LIMIT) return '每种商品数量应为1至10件';
      if (!snapshot(item)) return errors.has(key) ? '库存服务暂不可用，请刷新后重试' : '正在获取库存，请稍后重试';
      totals.set(key, (totals.get(key) || 0) + q);
    }
    for (const [key, q] of totals) {
      if (q > LIMIT) return '每种商品每单限购10件';
      const remaining = stockFor(key);
      if (q > remaining) return '商品库存不足，当前可售' + remaining + '件';
    }
    return null;
  }
  function validateAdd(item) {
    const related = (state.cart || []).filter((row) => sku(row) === sku(item));
    const q = related.reduce((sum, row) => sum + (Number(row.qty) || 0), 0) + Number(item.qty);
    return validate([{ ...item, qty: q }]);
  }
  function limit(item, wanted) {
    const key = sku(item),
      other = (state.cart || []).includes(item)
        ? (state.cart || [])
            .filter((row) => row !== item && sku(row) === key)
            .reduce((sum, row) => sum + (Number(row.qty) || 0), 0)
        : 0;
    return Math.min(LIMIT, Math.max(0, stockFor(item) - other), Math.max(1, Math.floor(Number(wanted) || 1)));
  }
  function badge(value) {
    const p = find(value),
      row = snapshot(value),
      key = sku(value);
    const label =
      p?.visible === false
        ? '暂时下架'
        : !row
          ? errors.has(key)
            ? '库存暂不可用'
            : '正在查询库存'
          : stockFor(value) > 0
            ? '可售 ' + stockFor(value).toLocaleString('zh-CN') + ' 件 · 每单限购10件'
            : '暂时售罄';
    return (
      '<span class="stock inventory-stock' + (row && stockFor(value) <= 0 ? ' sold-out' : '') + '">' + label + '</span>'
    );
  }
  window.LumoInventory = {
    stockFor,
    validate,
    validateAdd,
    limit,
    badge,
    refresh,
    refreshOne,
    snapshot,
    hydrationTicket: () => readClock,
    prime,
  };
})();
