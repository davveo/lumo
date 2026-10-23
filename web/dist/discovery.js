(function () {
  'use strict';
  const PAGE_SIZE = 6;
  const MAX_COMPARE = 3;
  let catalogPage = 1;
  let catalogSignature = '';
  let lastTotal = 0;
  let activeSuggestion = -1;
  let comparison = [], history = [], remoteIds = new Set(), querySignature = '', queryLoading = false, queryError = '', suggestionTimer, discoveryOwner = '';
  const api = (service, action, command, options) => window.LumoAPI.call(service, action, command, options);
  const principal = () => window.LumoAuth?.isLoggedIn() === true;
  const currentOwner = () => {const user=window.LumoAuth?.user?.();return user?.id || user?.user_id || '';};
  const searchSignature = () => JSON.stringify([ui.query,ui.category,data().filters,ui.sort]);
  async function refreshSearch() {
    const signature = searchSignature();
    querySignature = signature; queryLoading = true; queryError = '';
    try {
      const result = await api('search','query',{data:{query:ui.query || '', category:ui.category === '全部好物' ? '' : ui.category,
        brand:data().filters.brand === 'all' ? '' : data().filters.brand, in_stock:data().filters.inStock,
        sort:({价格低:'price_asc',价格高:'price_desc'})[ui.sort] || '', limit:100}}, {auth:principal()});
      if (querySignature === signature) remoteIds = new Set((result.items || []).map(p => p.product_id || p.id));
    } catch(error) { if(querySignature === signature) { remoteIds = new Set(); queryError = error.message; } throw error; }
    finally { if(querySignature === signature) queryLoading = false; }
  }
  async function refresh() {
    const owner=currentOwner();
    if(principal()) {
      const [compares,searches] = await Promise.all(['compare','search'].map(kind => api('engagement','list',{data:{kind,limit:100}})));
      if(owner!==currentOwner()) return;
      discoveryOwner=owner;
      comparison = (compares.items || []).map(row => row.sku_id);
      history = (searches.items || []).map(row => row.query).filter(Boolean).slice(0,8);
    } else { comparison=[];history=[];discoveryOwner=''; }
    return refreshSearch();
  }
  const cleanText = value => typeof value === 'string' ? value.trim().slice(0, 80) : '';
  const productFor = id => products.find(p => p.id === id && p.visible !== false);
  const brandFor = p => cleanText(p.brand) || ({ iphone: 'Apple', macbook: 'Apple', headphones: 'Sony', sneakers: 'Nike', perfume: 'CHANEL' })[p.id] || 'LUMO';
  function available(p) {
    if (!p || p.visible === false) return false;
    const inventory = window.LumoInventory;
    if (typeof inventory?.stockFor === 'function') return Number(inventory.stockFor(p)) > 0;
    return Number.isFinite(Number(p.available ?? p.stock)) && Number(p.available ?? p.stock) > 0;
  }
  function data() {
    if(discoveryOwner!==currentOwner()){comparison=[];history=[];discoveryOwner=currentOwner();}
    const raw = state.discovery && typeof state.discovery === 'object' && !Array.isArray(state.discovery) ? state.discovery : {};
    const compareIds = [...new Set(comparison.filter(id => productFor(id)))].slice(0,MAX_COMPARE);
    const filters = { brand: cleanText(raw.filters?.brand) || 'all', inStock: raw.filters?.inStock === true };
    state.discovery = { history, compareIds, filters };
    return state.discovery;
  }
  async function recordSearch(query) {
    const value = cleanText(query); if (!value) return;
    catalogPage = 1;
    if (principal()) {
      const owner=currentOwner();
      await api('engagement','put',{data:{kind:'search',query:value}});
      if(owner!==currentOwner())return;
      history = [value,...history.filter(row => row.toLocaleLowerCase() !== value.toLocaleLowerCase())].slice(0,8);
    }
  }
  function recordSearchBehavior(query) {
    if (!principal()) return;
    const clean = query.replace(/[^\s@]+@[^\s@]+\.[^\s@]+/g,'').replace(/\d{7,}/g,'').trim();
    if (!clean) return;
    const key='search-'+(crypto.randomUUID?.() || Date.now().toString(36));
    api('analytics','record',{idempotency_key:key,data:{type:'search',query:clean}}).catch(()=>{});
  }
  function commitSearch(query) {
    const value = cleanText(query);
    if (!value) return;
    recordSearch(value).catch(error => toast(error.message));
    recordSearchBehavior(value);
    const current = data();
    current.filters = { brand: 'all', inStock: false };
    ui.query = value; ui.category = '全部好物'; ui.onlyFavorites = false; ui.min = ''; ui.max = '';
    closeSearch(); go(''); refreshSearch().then(() => renderKeepScroll()).catch(error => {toast(error.message);renderKeepScroll();});
  }
  function filter(list) {
    const filters = data().filters;
    return list.filter(p => querySignature === searchSignature() && remoteIds.has(p.id) && (filters.brand === 'all' || brandFor(p) === filters.brand) && (!filters.inStock || available(p)));
  }
  function resetFilters() {
    data().filters = { brand: 'all', inStock: false };
    catalogPage = 1; catalogSignature = ''; save();
  }
  function controls() {
    const current = data(), brands = [...new Set(products.filter(p => p.visible !== false).map(brandFor))].sort();
    const active = current.filters.brand !== 'all' || current.filters.inStock;
    return `${queryLoading ? '<p class="disc-search-label" role="status">正在查询商城商品…</p>' : queryError ? `<p class="disc-search-label" role="alert">${esc(queryError)}</p>` : ''}<div class="disc-controls"><label class="disc-brand-filter"><span>品牌</span><select data-discovery-filter="brand" aria-label="按品牌筛选商品"><option value="all">全部品牌</option>${brands.map(brand => `<option value="${esc(brand)}" ${current.filters.brand === brand ? 'selected' : ''}>${esc(brand)}</option>`).join('')}</select></label><label class="disc-stock-filter"><input type="checkbox" data-discovery-filter="stock" ${current.filters.inStock ? 'checked' : ''}>仅看有货</label>${active ? '<button class="disc-clear-filters" data-discovery-action="reset-filters">清除筛选</button>' : ''}<a class="disc-compare-entry" href="#/compare">${icon('layers')}商品对比${current.compareIds.length ? `<span>${current.compareIds.length}</span>` : ''}</a></div>`;
  }
  function paginate(list) {
    const current = data();
    const signature = JSON.stringify([ui.category, ui.query, ui.onlyFavorites, ui.min, ui.max, ui.sort, current.filters, list.map(p => [p.id, p.price])]);
    if (signature !== catalogSignature) { catalogPage = 1; catalogSignature = signature; }
    lastTotal = list.length;
    catalogPage = Math.max(1, Math.min(catalogPage, Math.ceil(lastTotal / PAGE_SIZE) || 1));
    return list.slice((catalogPage - 1) * PAGE_SIZE, catalogPage * PAGE_SIZE);
  }
  function pagination(total = lastTotal) {
    const pages = Math.ceil(total / PAGE_SIZE);
    if (pages < 2) return '';
    const visible = [...new Set([1, catalogPage - 1, catalogPage, catalogPage + 1, pages].filter(n => n >= 1 && n <= pages))].sort((a, b) => a - b);
    let previous = 0;
    const buttons = visible.map(n => { const gap = previous && n - previous > 1 ? '<span class="disc-page-gap">…</span>' : ''; previous = n; return `${gap}<button data-discovery-action="page" data-value="${n}" ${n === catalogPage ? 'class="active" aria-current="page"' : ''} aria-label="第 ${n} 页">${n}</button>`; }).join('');
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
    return `<aside class="disc-compare-bar" aria-label="已选对比商品"><div class="disc-compare-bar-title">${icon('layers')}<div><strong>商品对比</strong><small>已选 ${rows.length} / ${MAX_COMPARE} 件</small></div></div><div class="disc-compare-bar-products">${rows.map(p => `<div><img src="${esc(asset(p.image))}" alt="${esc(p.name)}"><span>${esc(p.name)}</span><button data-discovery-action="toggle-compare" data-id="${esc(p.id)}" aria-label="移出对比${esc(p.name)}">${icon('close')}</button></div>`).join('')}</div><div class="disc-compare-bar-actions"><a class="primary" href="#/compare">开始对比${icon('arrow')}</a><button data-discovery-action="clear-compare">清空</button></div></aside>`;
  }
  function compareRow(title, rows, content) {
    return `<tr><th scope="row">${title}</th>${rows.map(p => `<td>${content(p)}</td>`).join('')}</tr>`;
  }
  function page() {
    const rows = data().compareIds.map(productFor).filter(Boolean);
    const compareTable = rows.length ? `<section class="disc-compare-table-panel"><div class="disc-compare-table-scroll" tabindex="0" role="region" aria-label="商品参数对比表"><table class="disc-compare-table"><caption>并排查看喜欢的好物，选出更适合自己的那一件</caption><thead><tr><th scope="col">商品信息</th>${rows.map(p => `<th scope="col"><button class="disc-remove-product" data-discovery-action="toggle-compare" data-id="${esc(p.id)}" aria-label="移出对比${esc(p.name)}">${icon('close')}</button><a class="disc-compare-image ${p.id === 'iphone' ? 'disc-phone-image' : ''}" href="#/product/${esc(p.id)}"><img src="${esc(asset(p.image))}" alt="${esc(p.name)}"></a><a class="disc-compare-product-name" href="#/product/${esc(p.id)}">${esc(p.name)}</a><span>${esc(p.sub)}</span></th>`).join('')}</tr></thead><tbody>${compareRow('当前售价', rows, p => price(p.price))}${compareRow('品牌', rows, p => esc(brandFor(p)))}${compareRow('商品分类', rows, p => esc(p.category))}${compareRow('颜色款式', rows, p => (p.colors || []).map(value => `<span class="disc-spec-tag">${esc(value)}</span>`).join(''))}${compareRow('可选规格', rows, p => (p.options || []).map(value => `<span class="disc-spec-tag">${esc(value)}</span>`).join(''))}${compareRow('产品亮点', rows, p => `<ul>${(p.highlights || []).map((value, index) => `<li><strong>${esc(value)}</strong>${p.highSubs?.[index] ? `<small>${esc(p.highSubs[index])}</small>` : ''}</li>`).join('')}</ul>`)}${compareRow('商品状态', rows, p => `<span class="disc-availability ${available(p) ? '' : 'is-out'}"><i></i>${available(p) ? '有货' : '暂时缺货'}</span>`)}${compareRow('选购好物', rows, p => `<div class="disc-compare-buy"><button class="primary" data-action="quick-add" data-id="${esc(p.id)}" ${available(p) ? '' : 'disabled'}>${icon('cart')}加入购物车</button><a class="outline" href="#/product/${esc(p.id)}">选择规格${icon('chevron')}</a></div>`)}</tbody></table></div></section>` : `<section class="disc-compare-empty"><span>${icon('layers')}</span><h2>把喜欢的好物放在一起看看</h2><p>在商品卡片点击「对比」，最多可选 ${MAX_COMPARE} 件商品。</p><a class="primary" href="#/">去挑选好物${icon('arrow')}</a></section>`;
    return `${header()}<main class="container disc-compare-page" id="main"><div class="breadcrumb"><a href="#/">首页</a>${icon('chevron')}<span>商品对比</span></div><div class="disc-compare-page-head"><div><span class="disc-kicker">FIND YOUR PERFECT MATCH</span><h1>好物对比 <small>${rows.length} / ${MAX_COMPARE}</small></h1></div><div><a class="outline" href="#/">${icon('plus')}继续添加</a>${rows.length ? '<button class="disc-clear-all" data-discovery-action="clear-compare">清空对比</button>' : ''}</div></div>${compareTable}</main>${footer()}`;
  }
  function searchPanel() { return document.querySelector('#disc-search-panel'); }
  function closeSearch() {
    const panel = searchPanel(), input = document.querySelector('#search-input');
    if (panel) panel.hidden = true;
    if (input) { input.setAttribute('aria-expanded', 'false'); input.removeAttribute('aria-activedescendant'); }
    activeSuggestion = -1;
  }
  function searchOption(query, title, detailText = '', image = '') {
    return `<button type="button" class="disc-search-option" role="option" aria-selected="false" data-discovery-action="search" data-value="${esc(query)}">${image ? `<img src="${esc(asset(image))}" alt="">` : icon('search')}<span><strong>${esc(title)}</strong>${detailText ? `<small>${esc(detailText)}</small>` : ''}</span>${icon('chevron')}</button>`;
  }
  function drawSuggestions(input) {
    const panel = searchPanel(); if (!panel) return;
    const query = cleanText(input.value), current = data();
    activeSuggestion = -1;
    if (query) {
      panel.innerHTML = `<div class="disc-search-label">正在搜索…</div>${searchOption(query, `搜索“${query}”`, '查看全部搜索结果')}`;
      clearTimeout(suggestionTimer);
      suggestionTimer = setTimeout(async () => {
        try {
          const result = await api('search','query',{data:{query,limit:5}}, {auth:principal()});
          if (!input.isConnected || input.value.trim() !== query || panel.hidden) return;
          const matches = (result.items || []).map(raw => products.find(p => p.id === (raw.product_id || raw.id))).filter(Boolean);
          panel.innerHTML = `<div class="disc-search-label">相关好物</div>${matches.map(p => searchOption(p.name,p.name,`${p.category} · ¥${money(p.price)}`,p.image)).join('')}${searchOption(query,`搜索“${query}”`,'查看全部搜索结果')}`;
          panel.querySelectorAll('[role="option"]').forEach((option,index) => option.id = 'disc-search-option-' + index);
        } catch(error) { if(input.isConnected && input.value.trim() === query) panel.innerHTML = `<div class="disc-search-label">${esc(error.message)}</div>`; }
      },250);
    } else {
      const hot = ['手机', '耳机', '咖啡', '沙发'];
      panel.innerHTML = `${current.history.length ? `<div class="disc-search-section"><div class="disc-search-section-head"><span>最近搜索</span><button type="button" data-discovery-action="clear-search" aria-label="清空搜索记录">${icon('trash')}清空</button></div><div class="disc-search-chips">${current.history.map(value => `<button type="button" role="option" aria-selected="false" data-discovery-action="search" data-value="${esc(value)}">${icon('clock')}${esc(value)}</button>`).join('')}</div></div>` : ''}<div class="disc-search-section"><div class="disc-search-section-head"><span>热门搜索</span>${icon('sparkles')}</div><div class="disc-search-chips">${hot.map((value, index) => `<button type="button" role="option" aria-selected="false" data-discovery-action="search" data-value="${value}"><i>${index + 1}</i>${value}</button>`).join('')}</div></div>`;
    }
    panel.querySelectorAll('[role="option"]').forEach((option, index) => option.id = 'disc-search-option-' + index);
    panel.hidden = false;
    input.setAttribute('aria-expanded', 'true'); input.removeAttribute('aria-activedescendant');
  }
  function afterRender() {
    if (querySignature !== searchSignature()) refreshSearch().then(() => renderKeepScroll()).catch(error => { toast(error.message); renderKeepScroll(); });
    document.body?.classList.toggle('has-disc-compare', !!document.querySelector('.disc-compare-bar'));
    const form = document.querySelector('#search-form'), input = document.querySelector('#search-input');
    if (!form || !input) return;
    if (!form.querySelector('#disc-search-panel')) {
      const panel = document.createElement('div'); panel.id = 'disc-search-panel'; panel.className = 'disc-search-panel'; panel.setAttribute('role', 'listbox'); panel.setAttribute('aria-label', '搜索建议'); panel.hidden = true; form.append(panel);
    }
    input.setAttribute('role', 'combobox'); input.setAttribute('aria-autocomplete', 'list'); input.setAttribute('aria-controls', 'disc-search-panel'); input.setAttribute('aria-expanded', 'false');
  }
  document.addEventListener('pointerdown', event => { if (event.target.closest?.('#disc-search-panel [data-discovery-action]')) event.preventDefault(); });
  document.addEventListener('focusin', event => { if (event.target.id === 'search-input') drawSuggestions(event.target); });
  document.addEventListener('input', event => { if (event.target.id === 'search-input') drawSuggestions(event.target); });
  document.addEventListener('focusout', event => {
    if (!event.target.closest?.('#search-form')) return;
    if (!event.relatedTarget?.closest?.('#search-form')) closeSearch();
  });
  document.addEventListener('keydown', event => {
    if (event.target.id !== 'search-input') return;
    const panel = searchPanel();
    if (event.key === 'Escape') { closeSearch(); return; }
    if (!['ArrowDown', 'ArrowUp', 'Enter'].includes(event.key)) return;
    if (event.key !== 'Enter' && (!panel || panel.hidden)) drawSuggestions(event.target);
    const options = [...(searchPanel()?.querySelectorAll('[role="option"]') || [])];
    if (!options.length || searchPanel().hidden) return;
    if (event.key === 'Enter') {
      if (activeSuggestion >= 0 && options[activeSuggestion]) { event.preventDefault(); options[activeSuggestion].click(); }
      return;
    }
    event.preventDefault();
    activeSuggestion = activeSuggestion < 0 ? (event.key === 'ArrowDown' ? 0 : options.length - 1) : (activeSuggestion + (event.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length;
    options.forEach((option, index) => option.setAttribute('aria-selected', String(index === activeSuggestion)));
    event.target.setAttribute('aria-activedescendant', options[activeSuggestion].id);
    options[activeSuggestion].scrollIntoView({ block: 'nearest' });
  });
  document.addEventListener('change', event => {
    if (!event.target.dataset.discoveryFilter) return;
    const current = data();
    if (event.target.dataset.discoveryFilter === 'brand') current.filters.brand = event.target.value;
    if (event.target.dataset.discoveryFilter === 'stock') current.filters.inStock = event.target.checked;
    catalogPage = 1; save(); renderKeepScroll();
  });
  document.addEventListener('click', async event => {
    const button = event.target.closest('[data-discovery-action]');
    if (!button) { if (!event.target.closest('#search-form')) closeSearch(); return; }
    if (button.disabled) return;
    const action = button.dataset.discoveryAction, id = button.dataset.id;
    if (action === 'search') { commitSearch(button.dataset.value); return; }
    if (action === 'clear-search') { try {if(principal()) await api('engagement','clear',{data:{kind:'search'}});history=[];} catch(error){toast(error.message);return;} const input = document.querySelector('#search-input'); if (input) { input.value = ''; drawSuggestions(input); } return; }
    if (action === 'reset-filters') { resetFilters(); renderKeepScroll(); return; }
    if (action === 'page') { catalogPage = Math.max(1, Math.min(Math.ceil(lastTotal / PAGE_SIZE) || 1, Number(button.dataset.value) || 1)); renderKeepScroll(); document.getElementById('catalog')?.scrollIntoView({ block: 'start', behavior: 'smooth' }); return; }
    if (action === 'toggle-compare') {
      if (!window.LumoAuth?.requireLogin(location.hash)) return;
      if (!productFor(id)) { toast('商品已下架，无法加入对比'); return; }
      button.disabled = true;
      const owner=currentOwner();
      try {
        const selected = comparison.includes(id);
        await api('engagement',selected ? 'delete':'put',{data:{kind:'compare',sku_id:id}});
        const result = await api('engagement','list',{data:{kind:'compare',limit:100}});
        if(owner!==currentOwner())return;
        comparison = (result.items || []).map(row => row.sku_id);renderKeepScroll();
      } catch(error) {toast(error.message);button.disabled=false;}
      return;
    }
    if (action === 'clear-compare') {
      if (!window.LumoAuth?.requireLogin(location.hash)) return;
      try {await api('engagement','clear',{data:{kind:'compare'}});comparison=[];renderKeepScroll();}catch(error){toast(error.message);}
    }
  });
  window.LumoDiscovery = { afterRender, recordSearch, commitSearch, filter, controls, paginate, pagination, compareButton, compareBar, page, resetFilters, brandFor, refresh, refreshSearch };
})();
