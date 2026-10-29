(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc, money, date } = U;
  const pageSize = 8,
    historySize = 5;
  function variants(product) {
    return Array.isArray(product.variants) && product.variants.length
      ? product.variants
      : [
          {
            sku_id: product.id,
            color: product.colors?.[0] || '默认',
            option: product.defaultOption || product.options?.[0] || '标准规格',
            price_cents: product.price_cents,
          },
        ];
  }
  const m = U.controller(
    'inventory-admin-root',
    () => ({
      items: [],
      total: 0,
      page: 1,
      ready: false,
      busy: false,
      error: '',
      keyword: '',
      selected: null,
      selectedSKU: '',
      stocks: {},
      adjust: null,
      history: [],
      historySKU: '',
      historyPage: 1,
      filter: '',
      searchMode: false,
    }),
    async (module) => {
      const s = module.state;
      return readCatalog(module, s.page);
    },
    view,
  );
  async function readCatalog(module, page, keyword = module.state.searchMode ? module.state.keyword : '') {
    const s = module.state;
    const result = await module.adapter.call(C.Service.Catalog, keyword ? C.Action.AdminList : C.Action.List, {
      data: keyword
        ? { keyword, page, page_size: pageSize }
        : { include_disabled: true, limit: pageSize, offset: (page - 1) * pageSize },
    });
    if (!Array.isArray(result.items) || !Number.isSafeInteger(Number(result.total)) || Number(result.total) < 0)
      throw Error('商品列表返回格式无效');
    const total = Number(result.total || 0),
      lastPage = Math.max(1, Math.ceil(total / pageSize));
    if (page > lastPage) return readCatalog(module, lastPage, keyword);
    const products = [...(result.items || []), ...(s.selected ? [s.selected] : [])];
    const stocks = await readStocks(products);
    return {
      items: result.items || [],
      total,
      page,
      stocks,
      ready: true,
      searchMode: !!keyword,
    };
  }
  async function readStocks(products) {
    const ids = [...new Set(products.flatMap((product) => variants(product).map((v) => v.sku_id)))];
    const stocks = {};
    for (let offset = 0; offset < ids.length; offset += 500) {
      const result = await m.adapter.call(C.Service.Inventory, C.Action.BatchGet, {
        data: { ids: ids.slice(offset, offset + 500) },
      });
      for (const row of result.items || []) stocks[row.sku_id] = row;
    }
    return stocks;
  }
  function stock(id) {
    return m.state.stocks[id] || { sku_id: id, on_hand: 0, reserved: 0, allocated: 0, available: 0, missing: true };
  }
  function stats(row) {
    return `<div class="iv-stock-grid">${[
      ['在库', 'on_hand'],
      ['预占', 'reserved'],
      ['已分配', 'allocated'],
      ['可售', 'available'],
    ]
      .map(([name, key]) => `<div><span>${name}</span><strong>${esc(row[key] ?? 0)}</strong></div>`)
      .join('')}</div>`;
  }
  function view(module) {
    const s = module.state;
    return (
      U.heading(
        '商品库存',
        '先选择商品和具体规格，再核对预占、分配与可售数量。',
        `<button class="outline" data-iv="reload" ${s.busy ? 'disabled' : ''}>刷新库存</button>`,
      ) +
      `<section class="admin-section"><form data-iv-search class="iv-search"><label class="field">搜索商品或定位 SKU<input name="id" placeholder="输入商品名称，或完整商品 / SKU 编号" value="${esc(s.keyword)}"></label><button class="primary" ${s.busy ? 'disabled' : ''}>搜索商品</button><button class="outline" type="button" data-iv="clear">返回商品目录</button></form><p class="admin-muted">目录与名称搜索均覆盖全部上下架商品，完整编号可直接定位规格。</p></section><div class="iv-layout ${s.selected ? 'iv-has-selection' : ''}"><section class="admin-list-panel iv-catalog"><div class="admin-list-toolbar"><label class="field">筛选本页商品<input data-iv-filter type="search" value="${esc(s.filter)}" placeholder="名称、品牌、分类"></label><span>${(s.searchMode ? '匹配 ' : '目录共 ') + s.total.toLocaleString('zh-CN') + ' 件'}</span></div>${!s.ready ? (s.error && !s.busy ? U.empty('数据读取失败', '请点击上方重试。') : window.LumoSkeleton.render('rows', { count: 6 })) : `<div class="iv-product-scroll" data-iv-products tabindex="0" role="region" aria-label="商品库存列表">${renderProducts(s)}</div>`}<div data-iv-pagination>${productPager(s)}</div></section><aside id="iv-selection" class="admin-section iv-selected" tabindex="-1" aria-label="商品规格库存详情">${s.selected ? selection(s) : U.empty('选择具体商品规格', '点击商品的“管理规格”，再选择要查看的 SKU。')}</aside></div>`
    );
  }
  function filteredProducts(s) {
    return s.items.filter(
      (row) =>
        !s.filter ||
        `${row.name} ${row.id} ${row.brand} ${row.category}`.toLowerCase().includes(s.filter.toLowerCase()),
    );
  }
  function productPager(s) {
    return U.pager(s.total, s.page, pageSize, 'iv', s.busy);
  }
  function renderProducts(s) {
    const rows = filteredProducts(s);
    if (!rows.length) return U.empty('本页暂无匹配商品');
    return `<div class="iv-products">${rows
      .map((product) => {
        const vs = variants(product),
          available = vs.reduce((sum, v) => sum + Number(stock(v.sku_id).available || 0), 0);
        const stocks = vs.map((v) => stock(v.sku_id)),
          missing = stocks.some((row) => row.missing);
        return `<article class="iv-product ${s.selected?.id === product.id ? 'selected' : ''}"><div class="iv-product-info"><strong>${esc(product.name || product.id)}</strong><small>${esc(product.category || '未分类')} · ${esc(product.brand || '—')} · ${vs.length} 规格 · ${product.active ? '已上架' : '未上架'}</small><code>${esc(product.id)}</code></div><div class="iv-product-stock"><strong class="iv-available">${missing ? '—' : available}</strong><small>${missing ? '库存待建档' : '可售合计'}</small></div><button class="outline" data-iv="select" data-id="${esc(product.id)}" ${s.busy ? 'disabled' : ''} ${s.selected?.id === product.id ? 'aria-current="true"' : ''}>管理规格</button></article>`;
      })
      .join('')}</div>`;
  }
  function selection(s) {
    const product = s.selected,
      vs = variants(product),
      v = vs.find((v) => v.sku_id === s.selectedSKU) || vs[0],
      row = stock(v.sku_id);
    return `<div class="admin-panel-heading"><h2>${esc(product.name || product.id)}</h2><button class="outline" data-iv="close">返回列表</button></div><label class="field iv-sku-picker">选择规格 · 共 ${vs.length} 个 SKU<select data-iv-sku ${s.busy ? 'disabled' : ''}>${vs.map((item) => `<option value="${esc(item.sku_id)}" ${item.sku_id === v.sku_id ? 'selected' : ''}>${esc(item.color || '默认')} / ${esc(item.option || '标准规格')}</option>`).join('')}</select></label><div class="iv-variants"><article><div class="iv-sku-heading"><strong>${esc(v.color || '默认')} / ${esc(v.option || '标准规格')}</strong><span>${money(v.price_cents)}</span></div><code>${esc(v.sku_id)}</code>${row.missing ? '<small class="admin-muted">尚未建库存；设置在库数量后初始化</small>' : ''}${stats(row)}<div class="admin-actions"><button class="outline" data-iv="adjust" data-id="${esc(v.sku_id)}" ${s.busy ? 'disabled' : ''}>调整库存</button><button class="outline" data-iv="history" data-id="${esc(v.sku_id)}" ${s.busy ? 'disabled' : ''}>${s.historySKU ? '收起记录' : '调整记录'}</button></div>${s.adjust === v.sku_id ? `<form data-iv-adjust class="op-confirm"><label class="field">目标在库数量<input name="on_hand" type="number" min="${Number(row.reserved) + Number(row.allocated)}" max="9007199254740991" step="1" required value="${esc(row.on_hand)}"></label><label class="field">调整原因<input name="reason" required maxlength="240" placeholder="如采购到货、盘点修正"></label><p>预占与分配共 ${Number(row.reserved) + Number(row.allocated)} 件，可售数量将自动计算。</p><div class="admin-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>保存调整</button><button class="outline" type="button" data-iv="cancel">取消</button></div></form>` : ''}</article></div>${
      s.historySKU
        ? `<section class="iv-history"><h3>近期调整记录</h3><p class="admin-muted">最近最多 20 条记录</p>${
            s.history.length
              ? `<ol>${s.history
                  .slice((s.historyPage - 1) * historySize, s.historyPage * historySize)
                  .map(
                    (row) =>
                      `<li><strong>${esc(row.before)} → ${esc(row.after)}</strong><span>${esc(row.reason)}</span><small>${esc(row.actor_id)} · ${date(row.created_at)}</small></li>`,
                  )
                  .join('')}</ol>${U.pager(s.history.length, s.historyPage, historySize, 'iv-history', s.busy)}`
              : U.empty('暂无调整记录', '新提交的调整会记录数量变化、操作人和原因。')
          }</section>`
        : ''
    }`;
  }
  function replaceStocks(ids, rows) {
    const stocks = { ...m.state.stocks };
    for (const id of ids) delete stocks[id];
    for (const row of rows || []) stocks[row.sku_id] = row;
    return stocks;
  }
  async function searchProduct(query) {
    const exact = m.state.items.find((row) => row.id === query || (row.variants || []).some((v) => v.sku_id === query));
    if (exact) return await selectSKU(query);
    if (/^[A-Za-z0-9_.:-]+$/.test(query)) {
      try {
        return await selectSKU(query);
      } catch (cause) {
        if (cause.status !== 404) throw cause;
      }
    }
    return {
      ...(await readCatalog(m, 1, query)),
      selected: null,
      selectedSKU: '',
      adjust: null,
      history: [],
      historySKU: '',
      historyPage: 1,
      filter: '',
    };
  }
  async function select(product) {
    return m.run(async () => {
      const ids = variants(product).map((v) => v.sku_id);
      const result = await m.adapter.call(C.Service.Inventory, C.Action.BatchGet, { data: { ids } });
      return {
        selected: product,
        selectedSKU: variants(product)[0].sku_id,
        adjust: null,
        history: [],
        historySKU: '',
        historyPage: 1,
        stocks: replaceStocks(
          variants(product).map((v) => v.sku_id),
          result.items,
        ),
      };
    });
  }
  async function selectSKU(id) {
    const product = await m.adapter.call(C.Service.Catalog, C.Action.Get, { id });
    let parent = product;
    if (product.catalog_visible !== false && product.product_id && product.product_id !== product.id)
      parent = await m.adapter.call(C.Service.Catalog, C.Action.Get, { id: product.product_id });
    const result = await m.adapter.call(C.Service.Inventory, C.Action.BatchGet, {
      data: { ids: variants(parent).map((v) => v.sku_id) },
    });
    return {
      selected: parent,
      selectedSKU: variants(parent).some((v) => v.sku_id === id) ? id : variants(parent)[0].sku_id,
      adjust: null,
      historySKU: '',
      history: [],
      historyPage: 1,
      stocks: replaceStocks(
        variants(parent).map((v) => v.sku_id),
        result.items,
      ),
    };
  }
  function focusSelection() {
    const panel = document.getElementById('iv-selection');
    panel?.scrollIntoView({ block: 'nearest' });
    panel?.focus({ preventScroll: true });
  }
  m.selectSKU = async (id) => {
    const ok = await m.run(() => selectSKU(id));
    if (ok) focusSelection();
    return ok;
  };
  document.addEventListener('click', async (event) => {
    const el = event.target.closest('[data-iv],[data-iv-page],[data-iv-history-page]');
    if (!el || el.disabled || !m.active() || m.state.busy) return;
    const s = m.state,
      a = el.dataset.iv;
    if (el.dataset.ivPage) {
      const page = Number(el.dataset.ivPage),
        total = s.total;
      if (!Number.isInteger(page) || page < 1 || page > Math.max(1, Math.ceil(total / pageSize))) return;
      await m.run(() => readCatalog(m, page));
      return;
    }
    if (el.dataset.ivHistoryPage) {
      const page = Number(el.dataset.ivHistoryPage);
      if (!Number.isInteger(page) || page < 1 || page > Math.max(1, Math.ceil(s.history.length / historySize))) return;
      s.historyPage = page;
      m.draw();
      return;
    }
    if (a === 'reload') {
      if (s.selected) await m.selectSKU(s.selected.id);
      else await m.refresh();
    }
    if (a === 'select') {
      const product = s.items.find((p) => p.id === el.dataset.id);
      if (product && (await select(product))) focusSelection();
    }
    if (a === 'adjust') {
      s.adjust = el.dataset.id;
      s.historySKU = '';
      m.draw();
    }
    if (a === 'cancel') {
      s.adjust = null;
      m.draw();
    }
    if (a === 'clear') {
      s.selected = null;
      s.selectedSKU = '';
      s.filter = '';
      s.keyword = '';
      s.searchMode = false;
      s.page = 1;
      await m.refresh();
    }
    if (a === 'close') {
      s.selected = null;
      s.selectedSKU = '';
      s.adjust = null;
      s.historySKU = '';
      m.draw();
      document.querySelector('[data-iv-products]')?.focus({ preventScroll: true });
    }
    if (a === 'history') {
      if (s.historySKU === el.dataset.id) {
        s.historySKU = '';
        m.draw();
      } else
        await m.run(async () => ({
          history: (await m.adapter.call(C.Service.Inventory, C.Action.Adjustments, { id: el.dataset.id })).items || [],
          historySKU: el.dataset.id,
          historyPage: 1,
          adjust: null,
        }));
    }
  });
  document.addEventListener('change', (event) => {
    if (!m.active() || m.state.busy || !event.target.matches('[data-iv-sku]')) return;
    const s = m.state;
    if (!s.selected || !variants(s.selected).some((v) => v.sku_id === event.target.value)) return;
    s.selectedSKU = event.target.value;
    s.adjust = null;
    s.historySKU = '';
    s.history = [];
    s.historyPage = 1;
    m.draw();
    document.querySelector('[data-iv-sku]')?.focus({ preventScroll: true });
  });
  document.addEventListener('input', (event) => {
    if (m.active() && event.target.matches('[data-iv-filter]')) {
      m.state.filter = event.target.value;
      const list = document.querySelector('[data-iv-products]');
      if (list) list.innerHTML = renderProducts(m.state);
      const pager = document.querySelector('[data-iv-pagination]');
      if (pager) pager.innerHTML = productPager(m.state);
    }
  });
  document.addEventListener('submit', async (event) => {
    if (!m.active()) return;
    if (event.target.matches('[data-iv-search]')) {
      event.preventDefault();
      const id = String(new FormData(event.target).get('id') || '').trim();
      if (!id) return;
      m.state.keyword = id;
      const ok = await m.run(() => searchProduct(id));
      if (ok && m.state.selected) focusSelection();
    }
    if (event.target.matches('[data-iv-adjust]')) {
      event.preventDefault();
      if (m.state.busy) return;
      const form = new FormData(event.target),
        id = m.state.adjust,
        row = stock(id),
        qty = Number(form.get('on_hand'));
      if (!Number.isSafeInteger(qty) || qty < 0) return;
      const ok = await m.run(async () => {
        await m.adapter.call(C.Service.Inventory, C.Action.Adjust, {
          id,
          idempotency_key: crypto.randomUUID(),
          data: { on_hand: qty, expected_on_hand: row.on_hand, reason: String(form.get('reason') || '').trim() },
        });
        return await selectSKU(id);
      });
      if (ok) m.adapter.notify?.('库存已更新');
    }
  });
  window.LumoInventoryAdmin = m;
})();
