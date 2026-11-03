(function () {
  'use strict';
  const { esc } = LumoAdminUI;
  let adapter = {},
    revision = 0,
    rows = [],
    owner = '',
    saved = { recent: [], favorites: [] };
  const allowed = (row) => row && typeof row.id === 'string' && row.id.length <= 180 && adapter.allowed?.(row.tab);
  const panel = () => document.querySelector('#admin-search-results');
  const storageKey = () => 'lumo-admin-navigation-v1:' + adapter.identity?.();
  function sync() {
    const next = adapter.scope?.() || '';
    if (owner === next) return;
    owner = next;
    revision++;
    rows = [];
    saved = { recent: [], favorites: [] };
    if (adapter.authenticated?.()) {
      try {
        const raw = JSON.parse(localStorage.getItem(storageKey()) || '{}');
        saved.recent = (Array.isArray(raw.recent) ? raw.recent : []).filter(allowed).slice(0, 6);
        saved.favorites = (Array.isArray(raw.favorites) ? raw.favorites : []).filter(allowed).slice(0, 12);
      } catch (_) {}
    }
    if (panel()) panel().hidden = true;
    const input = document.querySelector('#admin-global-query');
    if (input) input.value = '';
  }
  function save() {
    try {
      localStorage.setItem(storageKey(), JSON.stringify(saved));
    } catch (_) {}
  }
  function render(groups, note = '') {
    const root = panel();
    if (!root) return;
    rows = [];
    root.hidden = false;
    root.innerHTML = `<div class="as-panel-heading"><strong>全局查找</strong><button type="button" data-global-close aria-label="关闭搜索">×</button></div>${note ? `<p role="status">${esc(note)}</p>` : ''}${groups
      .map(
        (group) =>
          `<section><h3>${esc(group.label)}</h3>${group.error ? `<p>${esc(group.error)}</p>` : ''}${group.items
            .filter(allowed)
            .map((row) => {
              const index = rows.push(row) - 1;
              const pinned = saved.favorites.some((item) => item.tab === row.tab && item.id === row.id);
              return `<div class="as-result"><button type="button" data-global-open="${index}"><strong>${esc(row.label || row.id)}</strong><small>${esc(row.id)}${row.reference ? ' · ' + esc(row.reference) : ''}</small></button><button type="button" data-global-pin="${index}" aria-label="${pinned ? '取消常用' : '设为常用'}" aria-pressed="${pinned}">${pinned ? '★' : '☆'}</button></div>`;
            })
            .join('')}</section>`,
      )
      .join('')}`;
  }
  function history() {
    sync();
    const groups = [];
    if (saved.favorites.length) groups.push({ label: '常用业务', items: saved.favorites });
    if (saved.recent.length) groups.push({ label: '最近访问', items: saved.recent });
    render(groups, '输入业务编号、商品或会员名称，按回车查询。');
  }
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'admin-global-search') return;
    event.preventDefault();
    sync();
    const query = String(new FormData(event.target).get('query') || '').trim();
    if (query.length < 2) {
      history();
      return;
    }
    const ticket = ++revision,
      scope = owner;
    render([], '正在查询有权限访问的业务…');
    try {
      const result = await adapter.call('management', 'global_search', { data: { query } });
      if (ticket !== revision || scope !== adapter.scope?.()) return;
      const groups = (result.groups || []).filter((group) => group.error || group.items?.length);
      render(groups, groups.length ? '' : '没有找到匹配业务，请核对编号或换个关键词。');
    } catch (error) {
      if (ticket === revision && scope === adapter.scope?.()) render([], error.message || '查询失败，请重试');
    }
  });
  document.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-global-open],[data-global-pin],[data-global-close]');
    if (!button) {
      if (!event.target.closest('#admin-global-search') && panel()) panel().hidden = true;
      return;
    }
    if (button.dataset.globalClose != null) {
      revision++;
      if (panel()) panel().hidden = true;
      return;
    }
    const row = rows[Number(button.dataset.globalOpen ?? button.dataset.globalPin)];
    if (!allowed(row)) return;
    if (button.dataset.globalPin != null) {
      const present = saved.favorites.some((item) => item.tab === row.tab && item.id === row.id);
      saved.favorites = saved.favorites.filter((item) => item.tab !== row.tab || item.id !== row.id);
      if (!present) saved.favorites.unshift(row);
      saved.favorites = saved.favorites.slice(0, 12);
      save();
      button.textContent = present ? '☆' : '★';
      button.setAttribute('aria-pressed', String(!present));
      button.setAttribute('aria-label', present ? '设为常用' : '取消常用');
      return;
    }
    saved.recent = [row, ...saved.recent.filter((item) => item.tab !== row.tab || item.id !== row.id)].slice(0, 6);
    save();
    revision++;
    if (panel()) panel().hidden = true;
    await adapter.navigate(row.tab, { recordId: row.id });
  });
  document.addEventListener('focusin', (event) => {
    if (event.target.id === 'admin-global-query') history();
  });
  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k' && adapter.authenticated?.()) {
      event.preventDefault();
      document.querySelector('#admin-global-query')?.focus();
    }
    if (event.key === 'Escape' && panel()) {
      revision++;
      panel().hidden = true;
    }
  });
  window.LumoAdminSearch = {
    configure(value) {
      adapter = value;
      sync();
    },
    sync,
  };
})();
