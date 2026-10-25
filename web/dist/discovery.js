(function () {
  'use strict';
  const C = window.LumoConstants;
  const PAGE_SIZE = 6;
  const MAX_COMPARE = 3;
  let catalogPage = 1;
  let catalogSignature = '';
  let lastTotal = 0;
  let activeSuggestion = -1;
  let comparison = [],
    history = [],
    remoteIds = new Set(),
    querySignature = '',
    queryLoading = false,
    queryError = '',
    resolvedSearch = '',
    resolvedOwner = '',
    queryOwner = '',
    queryFlight = null,
    queryRevision = 0,
    suggestionTimer,
    suggestionRevision = 0,
    discoveryOwner = '',
    discoveryOffset = 0;
  const api = (service, action, command, options) => window.LumoAPI.call(service, action, command, options);
  const principal = () => window.LumoAuth?.isLoggedIn() === true;
  const currentOwner = () => {
    const user = window.LumoAuth?.user?.();
    return JSON.stringify([
      user?.id || user?.user_id || '',
      window.LumoAPI.getBase?.() || location.origin,
      window.LumoAPI.getToken?.() || '',
    ]);
  };
  const searchSignature = () => JSON.stringify([ui.query, ui.category, data().filters, ui.sort]);
  function refreshSearch(force = false) {
    const signature = searchSignature(),
      owner = currentOwner();
    if (!force && queryFlight?.signature === signature && queryFlight.owner === owner) return queryFlight.promise;
    const revision = ++queryRevision;
    querySignature = signature;
    queryOwner = owner;
    queryLoading = true;
    queryError = '';
    const selected = () => querySignature === signature && owner === currentOwner() && revision === queryRevision;
    if (!String(ui.query || '').trim() && ui.category === '全部好物') {
      queryFlight = null;
      window.LumoRecommendations?.restoreItems?.(C.ProductPool.Search, []);
      remoteIds = new Set(products.filter((p) => p.visible !== false).map((p) => p.id));
      queryLoading = false;
      resolvedSearch = signature;
      resolvedOwner = owner;
      return Promise.resolve();
    }
    const ticket = window.LumoInventory?.hydrationTicket?.(),
      flight = { signature, owner, promise: null };
    queryFlight = flight;
    flight.promise = (async () => {
      try {
        const result = await api(
          C.Service.Search,
          C.Action.Query,
          {
            data: {
              query: ui.query || '',
              category: ui.category === '全部好物' ? '' : ui.category,
              brand: data().filters.brand === 'all' ? '' : data().filters.brand,
              in_stock: data().filters.inStock,
              sort:
                {
                  价格低: C.CatalogSort.PriceAsc,
                  价格高: C.CatalogSort.PriceDesc,
                  销量: C.CatalogSort.Sales,
                  新品: C.CatalogSort.Newest,
                }[ui.sort] || C.CatalogSort.Default,
              limit: 100,
            },
          },
          { auth: false },
        );
        if (selected()) {
          resolvedSearch = signature;
          resolvedOwner = owner;
          remoteIds = new Set((result.items || []).map((p) => p.product_id || p.id));
          if (window.LumoRecommendations?.mapItems)
            window.LumoRecommendations.mapItems(result, C.ProductPool.Search, [], ticket);
          else {
            for (const raw of (result.items || []).slice(0, 100)) {
              const product = window.LumoCommerce?.normalizeProduct?.(raw);
              if (product && !products.some((p) => p.id === product.id)) products.push(product);
            }
            window.LumoInventory?.prime?.(result.inventory_items || [], ticket);
          }
        }
      } catch (error) {
        if (selected()) {
          remoteIds = new Set();
          queryError = error.message;
        }
        throw error;
      } finally {
        if (selected()) queryLoading = false;
        if (queryFlight === flight) queryFlight = null;
      }
    })();
    return flight.promise;
  }
  function selectCategory(category, query = '', label = '', brand = '') {
    if (!categories.includes(category)) return;
    data().filters = { brand: brand || 'all', inStock: false };
    ui.category = category;
    ui.query = typeof query === 'string' ? [...query.trim()].slice(0, 120).join('') : '';
    ui.categoryLabel = cleanText(label);
    ui.onlyFavorites = false;
    catalogPage = 1;
    catalogSignature = '';
    closeSearch();
    const request = refreshSearch(true);
    go('');
    return request
      .then(() => renderKeepScroll())
      .catch((error) => {
        toast(error.message);
        renderKeepScroll();
      });
  }
  async function refresh() {
    const owner = currentOwner();
    // Public browsing does not depend on the private comparison/history reads.
    // Start both together so an account-specific failure cannot strand the grid.
    const search = refreshSearch();
    const personal = principal()
      ? Promise.allSettled(
          [C.EngagementKind.Compare, C.EngagementKind.Search].map((kind) =>
            api(C.Service.Engagement, C.Action.List, { data: { kind, limit: 100 } }),
          ),
        )
      : Promise.resolve([]);
    const [searchResult, personalResult] = await Promise.allSettled([search, personal]);
    if (owner !== currentOwner()) return;
    discoveryOwner = owner;
    let personalFailure = null;
    if (principal()) {
      const [compares, searches] = personalResult.status === 'fulfilled' ? personalResult.value : [];
      if (compares?.status === 'fulfilled') {
        const ids = (compares.value.items || []).map((row) => row.sku_id);
        await window.LumoCommerce?.hydrateProducts?.(ids, { active: () => owner === currentOwner() });
        if (owner !== currentOwner()) return;
        comparison = ids;
      }
      if (searches?.status === 'fulfilled')
        history = (searches.value.items || [])
          .map((row) => row.query)
          .filter(Boolean)
          .slice(0, 8);
      personalFailure =
        personalResult.status === 'rejected'
          ? personalResult.reason
          : [compares, searches].find((result) => result?.status === 'rejected')?.reason;
    } else {
      comparison = [];
      history = [];
    }
    if (searchResult.status === 'rejected') throw searchResult.reason;
    if (personalFailure) throw personalFailure;
    return searchResult.value;
  }
  const cleanText = (value) => (typeof value === 'string' ? value.trim().slice(0, 80) : '');
  const productFor = (id) =>
    products.find(
      (p) =>
        (p.id === id || p.skuId === id || p.variants?.some((variant) => variant.sku_id === id)) &&
        (p.visible !== false || p.metadataUnavailable),
    );
  const brandFor = (p) =>
    cleanText(p.brand) ||
    { iphone: 'Apple', macbook: 'Apple', headphones: 'Sony', sneakers: 'Nike', perfume: 'CHANEL' }[p.id] ||
    'LUMO';
  function available(p) {
    if (!p || p.visible === false) return false;
    const inventory = window.LumoInventory;
    if (typeof inventory?.stockFor === 'function') return Number(inventory.stockFor(p)) > 0;
    return Number.isFinite(Number(p.available ?? p.stock)) && Number(p.available ?? p.stock) > 0;
  }
  function data() {
    if (discoveryOwner !== currentOwner()) {
      comparison = [];
      history = [];
      discoveryOffset = 0;
      discoveryOwner = currentOwner();
    }
    const raw =
      state.discovery && typeof state.discovery === 'object' && !Array.isArray(state.discovery) ? state.discovery : {};
    const compareIds = [...new Set(comparison.map((id) => productFor(id)?.id).filter(Boolean))].slice(0, MAX_COMPARE);
    const filters = { brand: cleanText(raw.filters?.brand) || 'all', inStock: raw.filters?.inStock === true };
    state.discovery = { history, compareIds, filters };
    return state.discovery;
  }
  async function recordSearch(query) {
    const value = cleanText(query);
    if (!value) return;
    catalogPage = 1;
    if (principal()) {
      const owner = currentOwner();
      await api(C.Service.Engagement, C.Action.Put, { data: { kind: C.EngagementKind.Search, query: value } });
      if (owner !== currentOwner()) return;
      history = [value, ...history.filter((row) => row.toLocaleLowerCase() !== value.toLocaleLowerCase())].slice(0, 8);
    }
  }
  function recordSearchBehavior(query) {
    if (!principal()) return;
    const clean = query
      .replace(/[^\s@]+@[^\s@]+\.[^\s@]+/g, '')
      .replace(/\d{7,}/g, '')
      .trim();
    if (!clean) return;
    const key = 'search-' + (crypto.randomUUID?.() || Date.now().toString(36));
    api(C.Service.Analytics, C.Action.Record, {
      idempotency_key: key,
      data: { type: C.AnalyticsBehavior.Search, query: clean },
    }).catch(() => {});
  }
  function commitSearch(query) {
    const value = cleanText(query);
    if (!value) return;
    recordSearch(value).catch((error) => toast(error.message));
    recordSearchBehavior(value);
    const current = data();
    current.filters = { brand: 'all', inStock: false };
    ui.query = value;
    ui.category = '全部好物';
    ui.categoryLabel = '';
    ui.onlyFavorites = false;
    ui.min = '';
    ui.max = '';
    closeSearch();
    const request = refreshSearch();
    go('');
    request
      .then(() => renderKeepScroll())
      .catch((error) => {
        toast(error.message);
        renderKeepScroll();
      });
  }
  function filter(list) {
    const filters = data().filters;
    return list.filter(
      (p) =>
        querySignature === searchSignature() &&
        remoteIds.has(p.id) &&
        (filters.brand === 'all' || brandFor(p) === filters.brand) &&
        (!filters.inStock || available(p)),
    );
  }
  function resetFilters() {
    data().filters = { brand: 'all', inStock: false };
    catalogPage = 1;
    catalogSignature = '';
    save();
  }
  function controls() {
    const current = data(),
      brands = [...new Set(products.filter((p) => p.visible !== false).map(brandFor))].sort();
    const active = current.filters.brand !== 'all' || current.filters.inStock;
    return `${queryError ? `<p class="disc-search-label" role="alert">${esc(queryError)}</p>` : ''}<div class="disc-controls"><label class="disc-brand-filter"><span>品牌</span><select data-discovery-filter="brand" aria-label="按品牌筛选商品"><option value="all">全部品牌</option>${brands.map((brand) => `<option value="${esc(brand)}" ${current.filters.brand === brand ? 'selected' : ''}>${esc(brand)}</option>`).join('')}</select></label><label class="disc-stock-filter"><input type="checkbox" data-discovery-filter="stock" ${current.filters.inStock ? 'checked' : ''}>仅看有货</label>${active ? '<button class="disc-clear-filters" data-discovery-action="reset-filters">清除筛选</button>' : ''}<a class="disc-compare-entry" href="#/compare">${icon('layers')}商品对比${current.compareIds.length ? `<span>${current.compareIds.length}</span>` : ''}</a></div>`;
  }
  function paginate(list) {
    const current = data();
    const signature = JSON.stringify([
      ui.category,
      ui.query,
      ui.onlyFavorites,
      ui.min,
      ui.max,
      ui.sort,
      current.filters,
      list.map((p) => [p.id, p.price]),
    ]);
    if (signature !== catalogSignature) {
      catalogPage = 1;
      catalogSignature = signature;
    }
    lastTotal = list.length;
    catalogPage = Math.max(1, Math.min(catalogPage, Math.ceil(lastTotal / PAGE_SIZE) || 1));
    return list.slice((catalogPage - 1) * PAGE_SIZE, catalogPage * PAGE_SIZE);
  }
  function pagination(total = lastTotal) {
    const pages = Math.ceil(total / PAGE_SIZE);
    if (pages < 2) return '';
    const visible = [
      ...new Set([1, catalogPage - 1, catalogPage, catalogPage + 1, pages].filter((n) => n >= 1 && n <= pages)),
    ].sort((a, b) => a - b);
    let previous = 0;
    const buttons = visible
      .map((n) => {
        const gap = previous && n - previous > 1 ? '<span class="disc-page-gap">…</span>' : '';
        previous = n;
        return `${gap}<button data-discovery-action="page" data-value="${n}" ${n === catalogPage ? 'class="active" aria-current="page"' : ''} aria-label="第 ${n} 页">${n}</button>`;
      })
      .join('');
    return `<nav class="disc-pagination" aria-label="商品分页"><span>共 ${total} 件好物</span><div><button data-discovery-action="page" data-value="${catalogPage - 1}" ${catalogPage <= 1 ? 'disabled' : ''} aria-label="上一页">${icon('back')}</button>${buttons}<button data-discovery-action="page" data-value="${catalogPage + 1}" ${catalogPage >= pages ? 'disabled' : ''} aria-label="下一页">${icon('chevron')}</button></div><small>第 ${catalogPage} / ${pages} 页</small></nav>`;
  }
  function compareButton(p) {
    if (!p || p.visible === false) return '';
    const selected = data().compareIds.includes(p.id);
    return `<button class="disc-compare-button ${selected ? 'selected' : ''}" data-discovery-action="toggle-compare" data-id="${esc(p.id)}" aria-label="${selected ? '移出对比' : '加入对比'}${esc(p.name)}" aria-pressed="${selected}">${icon(selected ? 'check' : 'layers')}<span>对比</span></button>`;
  }
  function compareBar() {
    const route = (location.hash || '#/').slice(2).split('/')[0];
    if (!['', 'product'].includes(route)) return '';
    const rows = data().compareIds.map(productFor).filter(Boolean);
    if (!rows.length) return '';
    return `<aside class="disc-compare-bar" aria-label="已选对比商品"><div class="disc-compare-bar-title">${icon('layers')}<div><strong>商品对比</strong><small>已选 ${rows.length} / ${MAX_COMPARE} 件</small></div></div><div class="disc-compare-bar-products">${rows.map((p) => `<div><img src="${esc(asset(p.image))}" alt="${esc(p.name)}"><span>${esc(p.name)}</span><button data-discovery-action="toggle-compare" data-id="${esc(p.id)}" aria-label="移出对比${esc(p.name)}">${icon('close')}</button></div>`).join('')}</div><div class="disc-compare-bar-actions"><a class="primary" href="#/compare">开始对比${icon('arrow')}</a><button data-discovery-action="clear-compare">清空</button></div></aside>`;
  }
  function compareRow(title, rows, content) {
    return `<tr><th scope="row">${title}</th>${rows.map((p) => `<td>${content(p)}</td>`).join('')}</tr>`;
  }
  function page() {
    const rows = data().compareIds.map(productFor).filter(Boolean);
    const compareTable = rows.length
      ? `<section class="disc-compare-table-panel"><div class="disc-compare-table-scroll" tabindex="0" role="region" aria-label="商品参数对比表"><table class="disc-compare-table"><caption>并排查看喜欢的好物，选出更适合自己的那一件</caption><thead><tr><th scope="col">商品信息</th>${rows.map((p) => `<th scope="col"><button class="disc-remove-product" data-discovery-action="toggle-compare" data-id="${esc(p.id)}" aria-label="移出对比${esc(p.name)}">${icon('close')}</button><a class="disc-compare-image ${p.id === 'iphone' ? 'disc-phone-image' : ''}" href="#/product/${esc(p.id)}"><img src="${esc(asset(p.image))}" alt="${esc(p.name)}"></a><a class="disc-compare-product-name" href="#/product/${esc(p.id)}">${esc(p.name)}</a><span>${esc(p.sub)}</span></th>`).join('')}</tr></thead><tbody>${compareRow('当前售价', rows, (p) => (p.metadataUnavailable ? '商品已不可用' : price(p.price)))}${compareRow('品牌', rows, (p) => esc(brandFor(p)))}${compareRow('商品分类', rows, (p) => esc(p.category))}${compareRow('颜色款式', rows, (p) => (p.colors || []).map((value) => `<span class="disc-spec-tag">${esc(value)}</span>`).join(''))}${compareRow('可选规格', rows, (p) => (p.options || []).map((value) => `<span class="disc-spec-tag">${esc(value)}</span>`).join(''))}${compareRow('产品亮点', rows, (p) => `<ul>${(p.highlights || []).map((value, index) => `<li><strong>${esc(value)}</strong>${p.highSubs?.[index] ? `<small>${esc(p.highSubs[index])}</small>` : ''}</li>`).join('')}</ul>`)}${compareRow('商品状态', rows, (p) => `<span class="disc-availability ${available(p) ? '' : 'is-out'}"><i></i>${p.metadataUnavailable ? '商品已不可用' : available(p) ? '有货' : '暂时缺货'}</span>`)}${compareRow('选购好物', rows, (p) => `<div class="disc-compare-buy"><button class="primary" data-action="quick-add" data-id="${esc(p.id)}" ${available(p) ? '' : 'disabled'}>${icon('cart')}加入购物车</button><a class="outline" href="#/product/${esc(p.id)}">选择规格${icon('chevron')}</a></div>`)}</tbody></table></div></section>`
      : `<section class="disc-compare-empty"><span>${icon('layers')}</span><h2>把喜欢的好物放在一起看看</h2><p>在商品卡片点击「对比」，最多可选 ${MAX_COMPARE} 件商品。</p><a class="primary" href="#/">去挑选好物${icon('arrow')}</a></section>`;
    return `${header()}<main class="container disc-compare-page" id="main"><div class="breadcrumb"><a href="#/">首页</a>${icon('chevron')}<span>商品对比</span></div><div class="disc-compare-page-head"><div><span class="disc-kicker">FIND YOUR PERFECT MATCH</span><h1>好物对比 <small>${rows.length} / ${MAX_COMPARE}</small></h1></div><div><a class="outline" href="#/">${icon('plus')}继续添加</a>${rows.length ? '<button class="disc-clear-all" data-discovery-action="clear-compare">清空对比</button>' : ''}</div></div>${compareTable}</main>${footer()}`;
  }
  function searchPanel() {
    return document.querySelector('#disc-search-panel');
  }
  function inSearchContext(target) {
    return target?.closest?.('#search-form, #disc-search-panel');
  }
  function positionSearchPanel(input, panel) {
    const form = input.closest('#search-form');
    if (!form) return;
    const anchor = form.getBoundingClientRect();
    const top = anchor.bottom - 1;
    const viewportBottom =
      (window.visualViewport?.height || window.innerHeight) + (window.visualViewport?.offsetTop || 0);
    panel.style.setProperty('--search-panel-left', `${-form.clientLeft}px`);
    panel.style.setProperty('--search-panel-top', `${top - anchor.top - form.clientTop}px`);
    panel.style.setProperty('--search-panel-width', `${anchor.width}px`);
    panel.style.setProperty('--search-panel-space', `${Math.max(0, viewportBottom - top - 12)}px`);
  }
  function positionOpenSearch() {
    const panel = searchPanel(),
      input = document.querySelector('#search-input');
    if (panel && !panel.hidden && input) positionSearchPanel(input, panel);
  }
  function closeSearch() {
    suggestionRevision++;
    clearTimeout(suggestionTimer);
    const panel = searchPanel(),
      input = document.querySelector('#search-input');
    if (panel) panel.hidden = true;
    input?.closest('#search-form')?.classList.remove('is-search-open');
    if (input) {
      input.setAttribute('aria-expanded', 'false');
      input.removeAttribute('aria-activedescendant');
    }
    activeSuggestion = -1;
  }
  function searchOption(query, title, detailText = '', image = '') {
    return `<button type="button" class="disc-search-option" role="option" aria-selected="false" data-discovery-action="search" data-value="${esc(query)}">${image ? `<img src="${esc(asset(image))}" alt="">` : icon('search')}<span><strong>${esc(title)}</strong>${detailText ? `<small>${esc(detailText)}</small>` : ''}</span>${icon('chevron')}</button>`;
  }
  function discoveryTerms() {
    const ordered = window.LumoRecommendations?.orderHome
      ? window.LumoRecommendations.orderHome(products.filter((p) => p.visible !== false))
      : products;
    const seen = new Set(),
      terms = [];
    for (const product of ordered) {
      if (product.visible === false) continue;
      const term = cleanText(product.name).split(/\s+[·|｜]\s+/)[0];
      const key = term.toLocaleLowerCase();
      if (!term || seen.has(key)) continue;
      seen.add(key);
      terms.push(term);
      if (terms.length === 80) break;
    }
    return terms;
  }
  function discoveryContent(matches = []) {
    const current = data(),
      terms = discoveryTerms();
    const visible = terms.length
      ? Array.from({ length: Math.min(8, terms.length) }, (_, index) => terms[(discoveryOffset + index) % terms.length])
      : [];
    const suggestions = matches.length
      ? `<section class="disc-search-suggestions" role="listbox" aria-label="匹配商品">${matches
          .slice(0, 5)
          .map((p, index) =>
            searchOption(p.name, p.name, `${p.category} · ¥${money(p.price)}`, p.image).replace(
              'role="option"',
              `role="option" id="disc-search-option-${index}"`,
            ),
          )
          .join('')}</section>`
      : '';
    return `${suggestions}<section class="disc-search-section"><div class="disc-search-section-head"><h3>搜索历史</h3>${current.history.length ? `<button type="button" class="disc-search-clear" data-discovery-action="clear-search" aria-label="清空搜索历史">${icon('trash')}<span>删除</span></button>` : ''}</div><div class="disc-search-history">${current.history.length ? current.history.map((query) => `<button type="button" class="disc-search-history-chip" data-discovery-action="search" data-value="${esc(query)}">${esc(query)}</button>`).join('') : '<p class="disc-search-empty">暂无搜索历史</p>'}</div></section><section class="disc-search-section"><div class="disc-search-section-head"><h3>搜索发现</h3>${terms.length > 1 ? '<button type="button" class="disc-search-refresh" data-discovery-action="refresh-search-discover" aria-label="换一换搜索发现">换一换</button>' : ''}</div><div class="disc-search-discover">${visible.length ? visible.map((term) => `<button type="button" class="disc-search-discover-item" data-discovery-action="search" data-value="${esc(term)}">${esc(term)}</button>`).join('') : '<p class="disc-search-empty">暂无可推荐搜索</p>'}</div></section>`;
  }
  function showSearch(input, panel, matches = []) {
    panel.innerHTML = discoveryContent(matches);
    panel.hidden = false;
    input.closest('#search-form')?.classList.add('is-search-open');
    positionSearchPanel(input, panel);
    input.setAttribute('aria-expanded', 'true');
    input.removeAttribute('aria-activedescendant');
    activeSuggestion = -1;
  }
  function drawSuggestions(input) {
    const panel = searchPanel();
    if (!panel) return;
    const query = cleanText(input.value);
    closeSearch();
    showSearch(input, panel);
    if (!query) return;
    const revision = suggestionRevision,
      owner = currentOwner();
    suggestionTimer = setTimeout(async () => {
      try {
        const result = await api(C.Service.Search, C.Action.Query, { data: { query, limit: 5 } }, { auth: false });
        if (
          revision !== suggestionRevision ||
          owner !== currentOwner() ||
          !input.isConnected ||
          document.activeElement !== input ||
          cleanText(input.value) !== query
        )
          return;
        const matches = (result.items || [])
          .map((raw) => products.find((p) => p.id === (raw.product_id || raw.id)))
          .filter((p) => p && p.visible !== false);
        showSearch(input, panel, matches);
      } catch (_) {
        /* History and discovery remain usable when suggestions fail. */
      }
    }, 250);
  }
  function afterRender() {
    if (
      (!window.LumoConnection || window.LumoConnection.ready) &&
      (querySignature !== searchSignature() || queryOwner !== currentOwner())
    )
      refreshSearch()
        .then(() => renderKeepScroll())
        .catch((error) => {
          toast(error.message);
          renderKeepScroll();
        });
    document.body?.classList.toggle('has-disc-compare', !!document.querySelector('.disc-compare-bar'));
    const form = document.querySelector('#search-form'),
      input = document.querySelector('#search-input');
    if (!form || !input) return;
    if (!form.querySelector('#disc-search-panel')) {
      const panel = document.createElement('div');
      panel.id = 'disc-search-panel';
      panel.className = 'disc-search-panel';
      panel.setAttribute('role', 'dialog');
      panel.setAttribute('aria-label', '商品搜索');
      panel.hidden = true;
      form.append(panel);
    }
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-haspopup', 'dialog');
    input.setAttribute('aria-autocomplete', 'list');
    input.setAttribute('aria-controls', 'disc-search-panel');
    input.setAttribute('aria-expanded', 'false');
  }
  document.addEventListener('pointerdown', (event) => {
    if (event.target.closest?.('#disc-search-panel [data-discovery-action]')) event.preventDefault();
    else if (!inSearchContext(event.target)) closeSearch();
  });
  document.addEventListener('focusin', (event) => {
    if (event.target.id === 'search-input') drawSuggestions(event.target);
  });
  document.addEventListener('input', (event) => {
    if (event.target.id === 'search-input') drawSuggestions(event.target);
  });
  document.addEventListener('focusout', (event) => {
    if (inSearchContext(event.target) && !inSearchContext(event.relatedTarget)) closeSearch();
  });
  window.addEventListener?.('resize', positionOpenSearch);
  window.addEventListener?.('hashchange', closeSearch);
  window.visualViewport?.addEventListener('resize', positionOpenSearch);
  window.addEventListener?.('scroll', closeSearch, { passive: true });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      if (inSearchContext(event.target) && event.target.id !== 'search-input')
        document.querySelector('#search-input')?.focus();
      closeSearch();
      return;
    }
    if (event.target.id !== 'search-input') return;
    const panel = searchPanel();
    if (!['ArrowDown', 'ArrowUp', 'Enter'].includes(event.key)) return;
    if (event.key !== 'Enter' && (!panel || panel.hidden)) drawSuggestions(event.target);
    const options = [...(searchPanel()?.querySelectorAll('[role="option"]') || [])];
    if (!options.length || searchPanel().hidden) return;
    if (event.key === 'Enter') {
      if (activeSuggestion >= 0 && options[activeSuggestion]) {
        event.preventDefault();
        options[activeSuggestion].click();
      }
      return;
    }
    event.preventDefault();
    activeSuggestion =
      activeSuggestion < 0
        ? event.key === 'ArrowDown'
          ? 0
          : options.length - 1
        : (activeSuggestion + (event.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length;
    options.forEach((option, index) => option.setAttribute('aria-selected', String(index === activeSuggestion)));
    event.target.setAttribute('aria-activedescendant', options[activeSuggestion].id);
    options[activeSuggestion].scrollIntoView({ block: 'nearest' });
  });
  document.addEventListener('change', (event) => {
    if (!event.target.dataset.discoveryFilter) return;
    const current = data();
    if (event.target.dataset.discoveryFilter === 'brand') current.filters.brand = event.target.value;
    if (event.target.dataset.discoveryFilter === 'stock') current.filters.inStock = event.target.checked;
    catalogPage = 1;
    save();
    renderKeepScroll();
  });
  document.addEventListener('click', async (event) => {
    if (event.target.id === 'search-input' && searchPanel()?.hidden) {
      drawSuggestions(event.target);
      return;
    }
    const button = event.target.closest('[data-discovery-action]');
    if (!button) {
      if (!inSearchContext(event.target)) closeSearch();
      return;
    }
    if (button.disabled) return;
    const action = button.dataset.discoveryAction,
      id = button.dataset.id;
    if (action === 'search') {
      commitSearch(button.dataset.value);
      return;
    }
    if (action === 'refresh-search-discover') {
      const terms = discoveryTerms();
      discoveryOffset = terms.length ? (discoveryOffset + (terms.length > 8 ? 8 : 1)) % terms.length : 0;
      const input = document.querySelector('#search-input'),
        panel = searchPanel();
      if (input && panel && !panel.hidden) showSearch(input, panel);
      return;
    }
    if (action === 'clear-search') {
      const owner = currentOwner();
      button.disabled = true;
      try {
        if (principal()) await api(C.Service.Engagement, C.Action.Clear, { data: { kind: C.EngagementKind.Search } });
        if (owner !== currentOwner()) return;
        history = [];
      } catch (error) {
        if (owner === currentOwner()) {
          toast(error.message);
          button.disabled = false;
        }
        return;
      }
      const input = document.querySelector('#search-input'),
        panel = searchPanel();
      if (input && panel && !panel.hidden && owner === currentOwner()) showSearch(input, panel);
      return;
    }
    if (action === 'reset-filters') {
      resetFilters();
      renderKeepScroll();
      return;
    }
    if (action === 'page') {
      catalogPage = Math.max(1, Math.min(Math.ceil(lastTotal / PAGE_SIZE) || 1, Number(button.dataset.value) || 1));
      renderKeepScroll();
      document.getElementById('catalog')?.scrollIntoView({ block: 'start', behavior: 'smooth' });
      return;
    }
    if (action === 'toggle-compare') {
      if (!window.LumoAuth?.requireLogin(location.hash)) return;
      const selectedSKU = comparison.find((sku) => productFor(sku)?.id === id);
      if (!productFor(id) || (!selectedSKU && productFor(id).visible === false)) {
        toast('商品已下架，无法加入对比');
        return;
      }
      button.disabled = true;
      const owner = currentOwner();
      try {
        await api(C.Service.Engagement, selectedSKU ? C.Action.Delete : C.Action.Put, {
          data: {
            kind: C.EngagementKind.Compare,
            sku_id: selectedSKU || window.LumoCommerce?.skuForItem?.(id) || id,
          },
        });
        const result = await api(C.Service.Engagement, C.Action.List, {
          data: { kind: C.EngagementKind.Compare, limit: 100 },
        });
        if (owner !== currentOwner()) return;
        await window.LumoCommerce?.hydrateProducts?.(
          (result.items || []).map((row) => row.sku_id),
          {
            active: () => owner === currentOwner(),
          },
        );
        if (owner !== currentOwner()) return;
        comparison = (result.items || []).map((row) => row.sku_id);
        renderKeepScroll();
      } catch (error) {
        toast(error.message);
        button.disabled = false;
      }
      return;
    }
    if (action === 'clear-compare') {
      if (!window.LumoAuth?.requireLogin(location.hash)) return;
      try {
        await api(C.Service.Engagement, C.Action.Clear, { data: { kind: C.EngagementKind.Compare } });
        comparison = [];
        renderKeepScroll();
      } catch (error) {
        toast(error.message);
      }
    }
  });
  for (const event of ['lumo:login', 'lumo:logout', 'lumo:session-expired'])
    window.addEventListener?.(event, () => {
      comparison = [];
      history = [];
      discoveryOffset = 0;
      discoveryOwner = '';
      closeSearch();
    });
  window.LumoDiscovery = {
    readState: () => ({
      loading: queryLoading && queryOwner === currentOwner(),
      loaded: resolvedSearch === searchSignature() && resolvedOwner === currentOwner(),
      error: querySignature === searchSignature() && queryOwner === currentOwner() ? queryError : '',
    }),
    selectCategory,
    afterRender,
    recordSearch,
    commitSearch,
    filter,
    controls,
    paginate,
    pagination,
    compareButton,
    compareBar,
    page,
    resetFilters,
    brandFor,
    refresh,
    refreshSearch,
  };
})();
