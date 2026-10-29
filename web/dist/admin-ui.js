(function () {
  'use strict';
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const money = (value) => '¥' + (Number(value || 0) / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2 });
  const date = (value) =>
    value && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
  const parse = (value) => {
    try {
      return typeof value === 'string' ? JSON.parse(value) : value;
    } catch (_) {
      return null;
    }
  };
  const empty = (title, note = '调整筛选条件或稍后刷新。') =>
    `<div class="admin-empty"><strong>${esc(title)}</strong><span>${esc(note)}</span></div>`;
  const pair = (name, value) => `<div><dt>${esc(name)}</dt><dd>${esc(value ?? '—')}</dd></div>`;
  const heading = (name, note, actions = '') =>
    `<div class="admin-title"><div><h1>${esc(name)}</h1><p>${esc(note)}</p></div><div class="admin-title-actions">${actions}</div></div>`;
  const pager = (total, page, size, prefix, busy = false) =>
    `<div class="admin-pagination"><span>共 ${Number(total).toLocaleString('zh-CN')} 条 · 每页 ${size} 条</span><div><button class="outline" data-${prefix}-page="${page - 1}" ${busy || page <= 1 ? 'disabled' : ''}>上一页</button><span>第 ${page} / ${Math.max(1, Math.ceil(total / size))} 页</span><button class="outline" data-${prefix}-page="${page + 1}" ${busy || page * size >= total ? 'disabled' : ''}>下一页</button></div></div>`;
  function controller(id, initial, fetch, view) {
    let adapter = {},
      owner = '',
      revision = 0,
      state = initial();
    const valid = () => Boolean(adapter.isAdmin?.());
    function check() {
      const scope = adapter.scope?.() || '';
      if (scope !== owner) {
        revision++;
        state = initial();
        owner = scope;
      }
    }
    const module = {
      get state() {
        check();
        return state;
      },
      get adapter() {
        return adapter;
      },
      configure(value) {
        adapter = value;
        check();
      },
      reset() {
        revision++;
        state = initial();
        owner = adapter.scope?.() || '';
      },
      active() {
        return valid() && Boolean(document.getElementById(id));
      },
      draw() {
        if (module.active()) adapter.draw?.();
      },
      async run(operation, apply) {
        check();
        if (!valid() || state.busy) return false;
        const ticket = ++revision,
          scope = owner;
        state.busy = true;
        state.error = '';
        module.draw();
        try {
          const result = await operation();
          if (ticket !== revision || scope !== (adapter.scope?.() || '') || !valid()) return false;
          if (apply) apply(result);
          else if (result) Object.assign(state, result);
          return true;
        } catch (cause) {
          if (ticket === revision && scope === (adapter.scope?.() || '')) {
            state.error = cause.message || '读取失败，请重试';
          }
          return false;
        } finally {
          if (ticket === revision && scope === (adapter.scope?.() || '')) {
            state.busy = false;
            module.draw();
          }
        }
      },
      refresh() {
        return module.run(() => fetch(module));
      },
      render() {
        check();
        if (!valid()) return '';
        return `<div id="${id}" aria-busy="${Boolean(state.busy)}">${state.error ? `<div class="admin-error" role="alert">${esc(state.error)}<button class="outline" data-module-retry="${id}">重试</button></div>` : ''}${view(module)}</div>`;
      },
    };
    document.addEventListener('click', (event) => {
      if (event.target.closest(`[data-module-retry="${id}"]`) && module.active()) module.refresh();
    });
    return module;
  }
  window.LumoAdminUI = { esc, money, date, parse, empty, pair, heading, pager, controller };
})();
