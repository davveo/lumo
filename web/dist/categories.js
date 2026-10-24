(function () {
  'use strict';
  const C = window.LumoConstants;
  const DOCUMENT_ID = 'storefront-categories';
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const icons = new Set(
    'home bell search cart heart bag user arrow chevron back check close trash minus plus shield truck return support filter sort pin ticket coin chip camera layers sparkles info clock edit'.split(
      ' ',
    ),
  );
  let configuration = { version: 1, groups: [] },
    visibleGroups = [],
    loaded = false,
    lastError = null,
    activeGroup = '',
    closeTimer,
    revision = 0,
    pending = null,
    owner = '';
  const context = () =>
    JSON.stringify([
      window.LumoAPI?.getBase?.() || location.origin,
      window.LumoAPI?.getToken?.() || '',
      window.LumoAuth?.user?.()?.id || window.LumoAuth?.user?.()?.user_id || '',
    ]);
  const invalid = () => new Error('商品分类配置格式不正确，请联系管理员');
  function text(value, max, required = true) {
    if (typeof value !== 'string' || [...value].length > max || value !== value.trim() || (required && !value))
      throw invalid();
    return value;
  }
  function array(value, max, optional = false) {
    if (value === undefined && optional) return [];
    if (!Array.isArray(value) || value.length > max) throw invalid();
    return value;
  }
  function unique(values, key) {
    const keys = values.map(key);
    if (new Set(keys).size !== keys.length) throw invalid();
    return values;
  }
  function normalize(raw) {
    if (raw && typeof raw.body === 'string') {
      if (raw.id !== undefined && raw.id !== DOCUMENT_ID) throw invalid();
      try {
        raw = JSON.parse(raw.body);
      } catch (_) {
        throw invalid();
      }
    }
    if (!raw || raw.version !== 1 || !Array.isArray(raw.groups)) throw invalid();
    let rows = 0,
      items = 0;
    const groups = array(raw.groups, 40).map((value) => {
      if (
        !value ||
        typeof value !== 'object' ||
        !/^([a-z][a-z0-9_-]{0,63})$/.test(value.id || '') ||
        !icons.has(value.icon) ||
        (value.enabled !== undefined && typeof value.enabled !== 'boolean')
      )
        throw invalid();
      const category = text(value.category, 80);
      if (category === '全部好物') throw invalid();
      const output = {
        id: value.id,
        category,
        label: text(value.label, 80),
        title: text(value.title, 80),
        icon: value.icon,
        enabled: value.enabled !== false,
      };
      output.shortcuts = unique(
        array(value.shortcuts, 8, true).map((item) => ({
          label: text(item?.label, 80),
          query: text(item?.query ?? '', 120, false),
        })),
        (item) => item.label,
      );
      output.rows = unique(
        array(value.rows, 20).map((row) => {
          rows++;
          const title = text(row?.title, 80);
          return {
            title,
            query: text(row?.query === undefined || row.query === '' ? title : row.query, 120),
            items: unique(
              array(row?.items, 40).map((item) => {
                items++;
                if (item?.featured !== undefined && typeof item.featured !== 'boolean') throw invalid();
                const label = text(item?.label, 80);
                return {
                  label,
                  query: text(item?.query === undefined || item.query === '' ? label : item.query, 120),
                  featured: item?.featured === true,
                };
              }),
              (item) => item.label,
            ),
          };
        }),
        (row) => row.title,
      );
      output.brands = unique(
        array(value.brands, 16, true).map((brand) => ({
          label: text(brand?.label, 80),
          brand: text(brand?.brand, 80),
        })),
        (brand) => brand.brand,
      );
      return output;
    });
    if (rows > 400 || items > 2000) throw invalid();
    unique(groups, (item) => item.id);
    unique(groups, (item) => item.category);
    return { version: 1, groups };
  }
  function updateCategories() {
    categories.splice(0, categories.length, '全部好物', ...visibleGroups.map((group) => group.category));
    if (typeof ui !== 'undefined' && !categories.includes(ui.category)) {
      ui.category = '全部好物';
      ui.query = '';
      ui.categoryLabel = '';
      ui.onlyFavorites = false;
      if (typeof state !== 'undefined' && state.discovery?.filters)
        state.discovery.filters = { brand: 'all', inStock: false };
      Promise.resolve(window.LumoDiscovery?.refreshSearch?.()).catch(() => {});
    }
  }
  function clear() {
    close();
    configuration = { version: 1, groups: [] };
    visibleGroups = [];
    loaded = false;
    lastError = null;
    updateCategories();
  }
  function ensureContext() {
    const current = context();
    if (owner !== current) {
      owner = current;
      revision++;
      pending = null;
      clear();
    }
    return current;
  }
  function apply(raw) {
    const next = normalize(raw);
    ensureContext();
    revision++;
    pending = null;
    close();
    configuration = next;
    visibleGroups = next.groups.filter((value) => value.enabled);
    loaded = true;
    lastError = null;
    updateCategories();
    return true;
  }
  function refresh() {
    const current = ensureContext();
    if (pending?.owner === current) return pending.promise;
    const ticket = ++revision;
    const request = { owner: current, promise: null };
    pending = request;
    request.promise = (async () => {
      try {
        if (typeof window.LumoAPI?.call !== 'function') throw new Error('商品分类服务尚未连接');
        const response = await window.LumoAPI.call(
          C.Service.Content,
          C.Action.Get,
          { id: DOCUMENT_ID },
          { auth: false },
        );
        if (ticket !== revision || current !== context()) return;
        const next = normalize(response);
        close();
        configuration = next;
        visibleGroups = next.groups.filter((value) => value.enabled);
        loaded = true;
        lastError = null;
        updateCategories();
        return clone(next);
      } catch (error) {
        if (ticket === revision && current === context()) {
          clear();
          lastError = error;
          throw error;
        }
      } finally {
        if (pending === request) pending = null;
      }
    })();
    return request.promise;
  }

  const panel = () => document.querySelector('#category-mega-panel');
  const selection = () => ({
    category: ui.category,
    query: ui.query || '',
    label: ui.categoryLabel || '',
    brand: state.discovery?.filters?.brand || 'all',
  });
  function choice(label, category, query = '', className = 'category-leaf', brand = '') {
    return `<button type="button" class="${className}" data-category-select="${esc(category)}" data-category-query="${esc(query)}" data-category-label="${esc(label)}"${brand ? ` data-category-brand="${esc(brand)}"` : ''}>${esc(label)}</button>`;
  }
  function groupContent(value) {
    const brands = value.brands;
    return `<div class="category-mega-head"><div><span>精选品类</span><h2>${esc(value.title)}</h2></div><button type="button" class="category-mega-close" data-category-close aria-label="关闭品类面板">${icon('close')}</button></div><div class="category-mega-shortcuts">${value.shortcuts.map((item) => choice(item.label, value.category, item.query, 'category-shortcut')).join('')}</div><div class="category-mega-body${brands.length ? '' : ' no-brands'}"><div class="category-mega-rows">${value.rows.map((r) => `<section class="category-mega-row"><button type="button" class="category-row-title" data-category-select="${esc(value.category)}" data-category-query="${esc(r.query)}" data-category-label="${esc(r.title)}">${esc(r.title)}${icon('chevron')}</button><div class="category-row-links">${r.items.map((item) => choice(item.label, value.category, item.query, 'category-leaf' + (item.featured ? ' featured' : ''))).join('')}</div></section>`).join('')}</div>${brands.length ? `<aside class="category-brand-rail" aria-label="相关品牌"><h3>相关品牌</h3><div>${brands.map((brand) => choice(brand.label, value.category, '', 'category-brand', brand.brand)).join('')}</div></aside>` : ''}</div>`;
  }
  function directory() {
    ensureContext();
    return `<aside class="category-directory" aria-label="全部商品分类"><div class="category-directory-head">${icon('layers')}<span>全部商品分类</span><small>${!loaded && !lastError ? '加载中' : `${visibleGroups.length} 大品类`}</small></div><div class="category-directory-list">${visibleGroups.map((value) => `<button type="button" class="category-directory-item" data-category-group="${esc(value.id)}" aria-controls="category-mega-panel" aria-expanded="false">${icon(value.icon)}<span>${esc(value.label)}</span>${icon('chevron')}</button>`).join('')}${visibleGroups.length ? '' : !loaded && !lastError ? window.LumoSkeleton?.render('navigation', { count: 12, label: '正在加载商品分类' }) || '' : `<div class="category-directory-empty" role="${lastError ? 'alert' : 'status'}">${esc(lastError ? '品类配置加载失败，请重新连接重试' : loaded ? '暂无商品分类' : '正在加载商品分类…')}</div>`}</div><section id="category-mega-panel" class="category-mega-panel" role="region" aria-label="品类详情" hidden></section></aside>`;
  }
  function show(id) {
    ensureContext();
    clearTimeout(closeTimer);
    const target = panel(),
      value = visibleGroups.find((item) => item.id === id);
    if (!target || !value) return;
    if (activeGroup !== id || target.hidden) target.innerHTML = groupContent(value);
    activeGroup = id;
    target.hidden = false;
    for (const button of document.querySelectorAll('[data-category-group]')) {
      const active = button.dataset.categoryGroup === id;
      button.classList.toggle('active', active);
      button.setAttribute('aria-expanded', String(active));
    }
  }
  function close(restoreFocus = false) {
    clearTimeout(closeTimer);
    if (restoreFocus && activeGroup) document.querySelector(`[data-category-group="${activeGroup}"]`)?.focus();
    const target = panel();
    if (target) target.hidden = true;
    for (const button of document.querySelectorAll('[data-category-group]')) {
      button.classList.remove('active');
      button.setAttribute('aria-expanded', 'false');
    }
    activeGroup = '';
  }
  function select(category, query = '', label = '', brand = '') {
    ensureContext();
    if (!categories.includes(category)) return;
    close();
    if (window.LumoDiscovery?.selectCategory) return window.LumoDiscovery.selectCategory(category, query, label, brand);
    ui.category = category;
    ui.query = query;
    ui.categoryLabel = label;
    ui.onlyFavorites = false;
    go('');
  }
  function afterRender() {
    clearTimeout(closeTimer);
    activeGroup = '';
  }
  document.addEventListener('mouseover', (event) => {
    const button = event.target.closest?.('[data-category-group]');
    if (button) show(button.dataset.categoryGroup);
    if (event.target.closest?.('.category-directory')) clearTimeout(closeTimer);
  });
  document.addEventListener('mouseout', (event) => {
    if (event.target.closest?.('.category-directory') && !event.relatedTarget?.closest?.('.category-directory'))
      closeTimer = setTimeout(() => close(), 140);
  });
  document.addEventListener('focusin', (event) => {
    const button = event.target.closest?.('[data-category-group]');
    if (button) show(button.dataset.categoryGroup);
    else if (!event.target.closest?.('.category-directory')) close();
  });
  document.addEventListener('pointerdown', (event) => {
    if (!event.target.closest?.('.category-directory')) close();
  });
  document.addEventListener('click', (event) => {
    const target = event.target.closest?.('[data-category-group], [data-category-select], [data-category-close]');
    if (!target) return;
    if (target.hasAttribute('data-category-close')) {
      close(true);
      return;
    }
    if (target.dataset.categoryGroup) {
      show(target.dataset.categoryGroup);
      return;
    }
    select(
      target.dataset.categorySelect,
      target.dataset.categoryQuery || '',
      target.dataset.categoryLabel || '',
      target.dataset.categoryBrand || '',
    );
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      close(!!event.target.closest?.('.category-directory'));
      return;
    }
    const button = event.target.closest?.('[data-category-group]');
    if (!button) return;
    if (event.key === 'ArrowRight') {
      event.preventDefault();
      show(button.dataset.categoryGroup);
      panel()?.querySelector('.category-shortcut')?.focus();
      return;
    }
    if (!['ArrowDown', 'ArrowUp'].includes(event.key)) return;
    event.preventDefault();
    const buttons = [...document.querySelectorAll('[data-category-group]')],
      index = buttons.indexOf(button),
      next = buttons[(index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length];
    next?.focus();
    if (next) show(next.dataset.categoryGroup);
  });
  window.addEventListener?.('hashchange', () => close());
  for (const event of ['lumo:login', 'lumo:logout', 'lumo:session-expired'])
    window.addEventListener?.(event, () => ensureContext());
  window.LumoCategories = {
    refresh,
    apply,
    directory,
    afterRender,
    select,
    close,
    selection,
    get configuration() {
      ensureContext();
      return clone(configuration);
    },
    get groups() {
      ensureContext();
      return clone(visibleGroups);
    },
    get categories() {
      ensureContext();
      return categories.slice();
    },
    get loaded() {
      ensureContext();
      return loaded;
    },
    get error() {
      ensureContext();
      return lastError;
    },
  };
})();
