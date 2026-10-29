(function () {
  'use strict';
  const C = window.LumoConstants;
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const pageSize = 20;
  let adapter = {},
    owner = '',
    revision = 0,
    pendingWrite = null,
    rows = [],
    total = 0,
    page = 1,
    ready = false,
    busy = false,
    loadingDraft = false,
    error = '',
    draft = null,
    original = null,
    changed = new Set(),
    search = '',
    status = '';
  const authorized = () => Boolean(adapter.isAdmin?.());
  const currentScope = () => adapter.scope?.() || '';
  const draw = () => adapter.draw?.();
  const active = () => Boolean(document.querySelector('#product-admin-root'));
  const current = (ticket, scope) => ticket === revision && scope === currentScope() && authorized();
  function runWrite(operation) {
    if (pendingWrite) return pendingWrite;
    const promise = Promise.resolve(operation());
    pendingWrite = promise;
    const release = () => {
      if (pendingWrite === promise) pendingWrite = null;
    };
    promise.then(release, release);
    return promise;
  }

  const money = (cents) => (Number(cents || 0) / 100).toFixed(2);
  function reset() {
    revision++;
    pendingWrite = null;
    owner = currentScope();
    rows = [];
    total = 0;
    page = 1;
    ready = false;
    busy = false;
    loadingDraft = false;
    error = '';
    draft = null;
    original = null;
    changed = new Set();
    search = '';
    status = '';
  }
  function checkScope() {
    if (owner !== currentScope()) reset();
  }
  function safeImage(value) {
    if (typeof value !== 'string') return '';
    if (
      /^(?:assets\/)?[a-zA-Z0-9_-]+\.(?:png|jpe?g|webp)$/i.test(value) ||
      /^(?:assets\/)?demo-[a-z0-9-]+\.svg$/.test(value)
    )
      return value;
    if (/^data:image\/(?:png|jpeg|webp);base64,[A-Za-z0-9+/=]+$/.test(value)) return value;
    try {
      const url = new URL(value);
      return url.protocol === 'https:' && !url.username && !url.password ? value : '';
    } catch (_) {
      return '';
    }
  }
  function asset(value) {
    const src = safeImage(value);
    return /^(?:https:|data:|assets\/)/i.test(src) ? src : src ? 'assets/' + src : '';
  }
  function cents(value, label) {
    if (!/^(?:0|[1-9]\d*)(?:\.\d{1,2})?$/.test(String(value))) throw Error(label + '请填写最多两位小数的金额');
    const [whole, fraction = ''] = String(value).split('.');
    const amount = Number(whole) * 100 + Number(fraction.padEnd(2, '0'));
    if (!Number.isSafeInteger(amount) || amount < 1 || amount > 1000000000)
      throw Error(label + '应为 0.01 至 10,000,000 元');
    return amount;
  }
  async function readPage(nextPage) {
    const result = await adapter.call(C.Service.Catalog, C.Action.List, {
      data: { include_disabled: true, limit: pageSize, offset: (nextPage - 1) * pageSize },
    });
    if (!Array.isArray(result.items) || !Number.isSafeInteger(Number(result.total)) || Number(result.total) < 0)
      throw Error('商品列表返回格式无效');
    return { rows: clone(result.items), total: Number(result.total) };
  }
  async function refresh() {
    checkScope();
    if (!authorized()) return false;
    if (pendingWrite) {
      // Returning to this tab must observe the in-flight write rather than
      // invalidate its confirmation and restore a stale creation form.
      const ticket = revision,
        scope = owner,
        writing = pendingWrite;
      await writing;
      return current(ticket, scope) && !error;
    }
    const ticket = ++revision,
      scope = owner;
    busy = true;
    error = '';
    try {
      const result = await readPage(page);
      if (!current(ticket, scope)) return false;
      rows = result.rows;
      total = result.total;
      ready = true;
      return true;
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '商品读取失败';
      return false;
    } finally {
      if (current(ticket, scope)) busy = false;
    }
  }
  function makeDraft(row) {
    return {
      id: row.id,
      name: row.name || '',
      brand: row.brand || '',
      category: row.category || '',
      sub: row.sub || '',
      image: row.image || '',
      price: money(row.price_cents),
      old: String(row.old ?? Number(row.price_cents || 0) / 100),
      label: row.label || '',
      active: row.active !== false && row.status !== C.CatalogStatus.Disabled,
    };
  }
  function newProduct() {
    original = null;
    changed.clear();
    draft = {
      id:
        'product-' +
        (crypto.randomUUID ? crypto.randomUUID() : Date.now().toString(36) + Math.random().toString(36).slice(2)),
      name: '',
      brand: '',
      category: '',
      sub: '',
      image: 'hero.jpg',
      price: '',
      old: '',
      label: '',
      active: false,
    };
    error = '';
    draw();
    window.scrollTo?.(0, 0);
  }
  async function edit(id) {
    const ticket = ++revision,
      scope = owner;
    busy = true;
    loadingDraft = true;
    error = '';
    draw();
    try {
      const row = await adapter.call(C.Service.Catalog, C.Action.Get, { id });
      if (!current(ticket, scope)) return;
      if (row.id !== id || !Array.isArray(row.colors) || !Array.isArray(row.options))
        throw Error('商品规格读取不完整，暂时不能编辑');
      original = clone(row);
      draft = makeDraft(row);
      changed.clear();
      window.scrollTo?.(0, 0);
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '商品详情读取失败';
    } finally {
      if (current(ticket, scope)) {
        busy = false;
        loadingDraft = false;
        draw();
      }
    }
  }
  function field(label, key, wide = false, type = 'text') {
    return `<label class="field ${wide ? 'wide' : ''}">${label}<input name="${key}" data-pd-field="${key}" type="${type}" value="${esc(draft[key])}" ${['name', 'category', 'image', 'price', 'old'].includes(key) ? 'required' : ''} ${['price', 'old'].includes(key) ? 'inputmode="decimal"' : `maxlength="${key === 'image' ? 2500000 : key === 'sub' ? 140 : key === 'label' ? 16 : 100}"`} ${busy ? 'disabled' : ''}></label>`;
  }
  function editor() {
    const image = asset(draft.image),
      colors = original?.colors || ['经典款'],
      options = original?.options || ['标准版'];
    return `<form id="product-admin-form"><div class="pd-editor-grid"><section class="admin-section"><div class="admin-panel-heading"><h2>基本资料</h2><span>${original ? '编辑商品' : '新建单规格商品'}</span></div><div class="admin-form"><label class="field wide">商品编号<input readonly value="${esc(draft.id)}"><small>编号用于关联订单与库存，保存后保持不变。</small></label>${field('商品名称', 'name', true)}${field('品牌', 'brand')}${field('商品分类', 'category')}${field('商品简介', 'sub', true)}${field('商品图片地址', 'image', true)}<p class="sa-field-help wide">支持 HTTPS 图片地址或现有商城素材文件名。分类名称应与分类导航中维护的名称一致。</p>${field('默认规格售价（元）', 'price')}${field('展示原价（元）', 'old')}${field('展示标签', 'label', true)}<label class="pd-check wide"><input type="checkbox" data-pd-field="active" ${draft.active ? 'checked' : ''} ${busy ? 'disabled' : ''}>在商城中上架展示</label></div></section><aside><section class="admin-section"><div class="admin-panel-heading"><h2>商品图片</h2></div><div class="pd-image-preview" data-pd-preview>${image ? `<img src="${esc(image)}" alt="商品图片预览">` : '<span>请填写有效图片地址</span>'}</div></section><section class="admin-section"><h2>规格与库存</h2><dl class="pd-specs"><dt>颜色</dt><dd>${colors.map(esc).join(' / ')}</dd><dt>规格</dt><dd>${options.map(esc).join(' / ')}</dd><dt>默认规格</dt><dd>${esc(original?.defaultOption || '标准版')}</dd></dl><p class="sa-field-help">${original ? '现有颜色和规格保持原样。调整默认售价时，其他规格按相同金额差调整，保留原有价差。' : '新商品按单颜色、单规格建立，初始为未上架。'}商品资料与库存独立保存，修改资料不会重置库存。</p></section></aside></div><div class="sa-savebar"><span>${original ? '已有规格与库存不会重置' : '保存后请前往库存管理设置可售库存'}</span><div><button type="button" class="outline" data-pd-action="back" ${busy ? 'disabled' : ''}>取消</button><button type="submit" class="primary" ${busy ? 'disabled' : ''}>${busy ? '正在保存…' : '保存商品'}</button></div></div></form>`;
  }
  function filteredRows() {
    const keyword = search.trim().toLocaleLowerCase();
    return rows.filter(
      (row) =>
        (!status || (row.active ? C.CatalogStatus.Active : C.CatalogStatus.Disabled) === status) &&
        (!keyword ||
          [row.id, row.name, row.brand, row.category].some((value) =>
            String(value || '')
              .toLocaleLowerCase()
              .includes(keyword),
          )),
    );
  }
  function listRows() {
    const visible = filteredRows();
    return `<div class="admin-list-summary"><span>本页匹配 ${visible.length} / ${rows.length} 件</span><span>目录共 ${total} 件</span></div>${
      visible.length
        ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table pd-table"><thead><tr><th scope="col">商品</th><th scope="col">分类 / 售价</th><th scope="col">状态</th><th scope="col">管理操作</th></tr></thead><tbody>${visible
            .map((row) => {
              const image = asset(row.image);
              return `<tr><td><div class="pd-product">${image ? `<img src="${esc(image)}" alt="">` : '<i></i>'}<div><strong>${esc(row.name)}</strong><small>${esc(row.id)}</small><small>${esc(row.brand || '')}</small></div></div></td><td><span>${esc(row.category || '未分类')}</span><strong class="admin-amount">¥${money(row.price_cents)}</strong></td><td><span class="admin-state ${row.active ? 'success' : 'neutral'}"><i></i>${row.active ? '已上架' : '未上架'}</span></td><td><div class="admin-actions"><button data-pd-action="edit" data-pd-id="${esc(row.id)}" ${busy ? 'disabled' : ''}>编辑商品</button><button data-pd-action="toggle" data-pd-id="${esc(row.id)}" ${busy ? 'disabled' : ''}>${row.active ? '下架' : '上架'}</button><button data-pd-action="inventory" data-pd-id="${esc(row.id)}" ${busy ? 'disabled' : ''}>管理库存</button></div></td></tr>`;
            })
            .join('')}</tbody></table></div>`
        : `<div class="admin-empty"><strong>${rows.length ? '本页没有符合条件的商品' : '暂无商品'}</strong><span>${rows.length ? '筛选只作用于当前已加载页面，可调整条件或翻页。' : '创建商品资料后，再到库存管理设置数量。'}</span></div>`
    }<div class="admin-pagination"><span>第 ${page} / ${Math.max(1, Math.ceil(total / pageSize))} 页 · 每页 ${pageSize} 件</span><div><button class="outline" data-pd-action="prev" ${busy || page === 1 ? 'disabled' : ''}>上一页</button><button class="outline" data-pd-action="next" ${busy || page * pageSize >= total ? 'disabled' : ''}>下一页</button></div></div>`;
  }
  function list() {
    return `<section class="admin-list-panel"><div class="admin-list-toolbar"><label class="admin-search"><input type="search" data-pd-filter="search" value="${esc(search)}" placeholder="在本页搜索名称、编号、品牌" aria-label="搜索本页商品" ${busy ? 'disabled' : ''}></label><label class="admin-status-filter">状态<select data-pd-filter="status" ${busy ? 'disabled' : ''}><option value="">全部状态</option><option value="${C.CatalogStatus.Active}" ${status === C.CatalogStatus.Active ? 'selected' : ''}>已上架</option><option value="${C.CatalogStatus.Disabled}" ${status === C.CatalogStatus.Disabled ? 'selected' : ''}>未上架</option></select></label></div><div id="product-admin-list">${listRows()}</div></section>`;
  }
  function render() {
    checkScope();
    if (!authorized()) return '';
    return `<div id="product-admin-root"><div class="admin-title"><div><h1>${draft ? (original ? '编辑商品' : '新建商品') : '商品管理'}</h1><p>维护商品资料、价格与上下架，库存独立管理。</p></div><div class="admin-title-actions">${draft ? `<button class="outline" data-pd-action="back" ${busy ? 'disabled' : ''}>返回列表（放弃未保存修改）</button>` : `<button class="outline" data-pd-action="reload" ${busy ? 'disabled' : ''}>刷新</button><button class="primary" data-pd-action="new" ${busy || !ready ? 'disabled' : ''}>新建商品</button>`}</div></div>${error ? `<div class="admin-error" role="alert">${esc(error)}</div>` : ''}${loadingDraft ? window.LumoSkeleton?.render('form', { count: 6, label: '正在读取商品详情' }) || '' : draft ? editor() : !ready ? (error ? '' : window.LumoSkeleton?.render('table', { count: 6, label: '正在读取商品目录' }) || '') : list()}</div>`;
  }
  async function save() {
    let price, old;
    try {
      price = cents(draft.price, '售价');
      old = !original || changed.has('old') ? cents(draft.old, '原价') : Number(draft.old) * 100;
      if ((!original || changed.has('old') || changed.has('price')) && old < price) throw Error('展示原价不能低于售价');
      if (!draft.name.trim() || draft.name.length > 100 || !draft.category.trim()) throw Error('请填写商品名称与分类');
      if (!safeImage(draft.image)) throw Error('请使用 HTTPS 图片地址或现有商城素材文件名');
      const form = document.querySelector('#product-admin-form');
      if (form?.reportValidity && !form.reportValidity()) return;
    } catch (cause) {
      error = cause.message;
      draw();
      return;
    }
    const ticket = ++revision,
      scope = owner,
      values = clone(draft),
      edits = new Set(changed),
      wasExisting = Boolean(original);
    busy = true;
    error = '';
    draw();
    try {
      let output;
      if (wasExisting) {
        output = await adapter.call(C.Service.Catalog, C.Action.Get, { id: values.id });
        if (!current(ticket, scope)) return;
        if (
          output.id !== values.id ||
          !Array.isArray(output.colors) ||
          !Array.isArray(output.options) ||
          !Array.isArray(output.prices)
        )
          throw Error('商品规格读取不完整，请重新加载');
      } else
        output = { colors: ['经典款'], options: ['标准版'], defaultOption: '标准版', prices: [price / 100], sales: 0 };
      for (const key of ['name', 'brand', 'category', 'sub', 'image', 'label', 'active'])
        if (!wasExisting || edits.has(key)) output[key] = values[key];
      if (!wasExisting || edits.has('price')) output.price_cents = price;
      if (!wasExisting || edits.has('old')) output.old = old / 100;
      await adapter.call(C.Service.Catalog, C.Action.Upsert, { id: values.id, data: output });
      if (!current(ticket, scope)) return;
      const confirmed = await adapter.call(C.Service.Catalog, C.Action.Get, { id: values.id });
      if (!current(ticket, scope)) return;
      if (confirmed.id !== values.id) throw Error('商品保存结果尚未确认');
      const result = await readPage(page);
      if (!current(ticket, scope)) return;
      rows = result.rows;
      total = result.total;
      ready = true;
      draft = null;
      original = null;
      changed.clear();
      adapter.notify?.(
        wasExisting ? '商品资料已保存，库存保持不变' : `商品已创建，请到库存管理设置 SKU ${values.id} 的库存`,
      );
      window.scrollTo?.(0, 0);
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '保存失败，输入已保留';
    } finally {
      if (current(ticket, scope)) {
        busy = false;
        draw();
      }
    }
  }
  async function toggle(id) {
    const ticket = ++revision,
      scope = owner;
    busy = true;
    error = '';
    draw();
    try {
      const latest = await adapter.call(C.Service.Catalog, C.Action.Get, { id });
      if (!current(ticket, scope)) return;
      if (
        latest.id !== id ||
        !Array.isArray(latest.colors) ||
        !Array.isArray(latest.options) ||
        !Array.isArray(latest.prices)
      )
        throw Error('商品规格读取不完整，请重试');
      await adapter.call(C.Service.Catalog, C.Action.Upsert, { id, data: { ...latest, active: !latest.active } });
      if (!current(ticket, scope)) return;
      const result = await readPage(page);
      if (!current(ticket, scope)) return;
      rows = result.rows;
      total = result.total;
      ready = true;
      adapter.notify?.(latest.active ? '商品已下架' : '商品已上架');
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '状态更新失败';
    } finally {
      if (current(ticket, scope)) {
        busy = false;
        draw();
      }
    }
  }
  function input(event) {
    checkScope();
    if (!active() || !authorized() || busy) return;
    const filter = event.target.dataset?.pdFilter;
    if (filter && ['search', 'status'].includes(filter)) {
      if (filter === 'search') search = event.target.value;
      else status = event.target.value;
      const target = document.querySelector('#product-admin-list');
      if (target) target.innerHTML = listRows();
      return;
    }
    const key = event.target.dataset?.pdField;
    if (!draft || !['name', 'brand', 'category', 'sub', 'image', 'price', 'old', 'label', 'active'].includes(key))
      return;
    draft[key] = key === 'active' ? event.target.checked : event.target.value;
    changed.add(key);
    if (key === 'image') {
      const target = document.querySelector('[data-pd-preview]'),
        image = asset(draft.image);
      if (target)
        target.innerHTML = image ? `<img src="${esc(image)}" alt="商品图片预览">` : '<span>请填写有效图片地址</span>';
    }
  }
  document.addEventListener('input', input);
  document.addEventListener('change', (event) => {
    if (event.target.dataset?.pdFilter === 'status' || event.target.dataset?.pdField === 'active') input(event);
  });
  document.addEventListener('click', async (event) => {
    const target = event.target.closest?.('[data-pd-action]');
    if (!target) return;
    checkScope();
    if (!active() || !authorized() || busy || target.disabled) return;
    event.preventDefault();
    const action = target.dataset.pdAction;
    if (action === 'new' && ready) newProduct();
    if (action === 'back') {
      draft = null;
      original = null;
      changed.clear();
      error = '';
      draw();
      window.scrollTo?.(0, 0);
    }
    if (action === 'edit' && rows.some((row) => row.id === target.dataset.pdId)) await edit(target.dataset.pdId);
    if (action === 'toggle' && rows.some((row) => row.id === target.dataset.pdId))
      await runWrite(() => toggle(target.dataset.pdId));
    if (action === 'inventory' && rows.some((row) => row.id === target.dataset.pdId)) {
      if (adapter.navigate) adapter.navigate('inventory', { skuId: target.dataset.pdId });
      else adapter.notify?.('请在库存管理中查询 SKU：' + target.dataset.pdId);
    }
    if (action === 'reload') {
      await refresh();
      draw();
    }
    if ((action === 'prev' && page > 1) || (action === 'next' && page * pageSize < total)) {
      const oldPage = page;
      page += action === 'prev' ? -1 : 1;
      if (!(await refresh())) page = oldPage;
      draw();
    }
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'product-admin-form') return;
    event.preventDefault();
    checkScope();
    if (active() && authorized() && !busy && draft) await runWrite(save);
  });
  window.LumoCatalogAdmin = {
    configure(value) {
      adapter = { ...adapter, ...value };
      checkScope();
    },
    refresh,
    render,
    reset,
  };
})();
