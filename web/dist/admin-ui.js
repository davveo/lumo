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
    `<div class="admin-title"><div><span class="admin-page-eyebrow">LUMO WORKSPACE</span><h1>${esc(name)}</h1>${note ? `<p class="admin-page-description">${esc(note)}</p>` : ''}</div><div class="admin-title-actions">${actions}</div></div>`;
  const pager = (total, page, size, prefix, busy = false) =>
    `<div class="admin-pagination"><span>共 ${Number(total).toLocaleString('zh-CN')} 条 · 每页 ${size} 条</span><div><button class="outline" data-${prefix}-page="${page - 1}" ${busy || page <= 1 ? 'disabled' : ''}>上一页</button><span>第 ${page} / ${Math.max(1, Math.ceil(total / size))} 页</span><button class="outline" data-${prefix}-page="${page + 1}" ${busy || page * size >= total ? 'disabled' : ''}>下一页</button></div></div>`;
  function createTabs(id, options) {
    let sections = [];
    const root = () => document.getElementById(id);
    function select(value, focus = false) {
      if (!options.active() || options.busy() || !sections.some(([key]) => key === value)) return;
      options.set(value);
      // Keep the existing panel DOM so switching tabs retains uncontrolled
      // form values, selections and scroll positions without another request.
      const host = root();
      let selected;
      for (const button of host?.querySelectorAll(`[data-admin-tabs="${id}"] [role="tab"]`) || []) {
        const current = button.dataset.adminSection === value;
        button.setAttribute('aria-selected', String(current));
        button.tabIndex = current ? 0 : -1;
        if (current) selected = button;
      }
      for (const panel of host?.querySelectorAll(`[data-admin-tab-group="${id}"]`) || []) {
        panel.hidden = panel.dataset.adminTabPanel !== value;
        panel.toggleAttribute('inert', panel.hidden);
      }
      if (!focus || !selected) return;
      selected.focus({ preventScroll: true });
      const list = selected.closest('[role="tablist"]'),
        offset = selected.getBoundingClientRect().left - list.getBoundingClientRect().left;
      if (offset < 0) list.scrollLeft += offset;
      else if (offset + selected.offsetWidth > list.clientWidth)
        list.scrollLeft += offset + selected.offsetWidth - list.clientWidth;
    }
    const target = (event) => {
      const button = event.target.closest?.('[data-admin-section]');
      return button?.closest('[data-admin-tabs]')?.dataset.adminTabs === id ? button : null;
    };
    document.addEventListener('click', (event) => {
      const button = target(event);
      if (button && !button.disabled) select(button.dataset.adminSection, true);
    });
    document.addEventListener('keydown', (event) => {
      const button = target(event);
      if (!button || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      const buttons = [...button.closest('[role="tablist"]').querySelectorAll('[role="tab"]')].filter(
        (item) => !item.disabled,
      );
      if (!buttons.length) return;
      event.preventDefault();
      const index = buttons.indexOf(button),
        next =
          event.key === 'Home'
            ? 0
            : event.key === 'End'
              ? buttons.length - 1
              : (index + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
      select(buttons[next].dataset.adminSection, true);
    });
    return {
      select,
      bar(items, label) {
        sections = items;
        if (!items.some(([key]) => key === options.get())) options.set(items[0]?.[0] || '');
        return `<div class="admin-page-tabs" data-admin-tabs="${id}" role="tablist" aria-label="${esc(label)}">${items.map(([key, title]) => `<button type="button" role="tab" id="${id}-tab-${key}" aria-controls="${id}-panel-${key}" aria-selected="${options.get() === key}" tabindex="${options.get() === key ? 0 : -1}" data-admin-section="${key}" ${options.busy() ? 'disabled' : ''}>${esc(title)}</button>`).join('')}</div>`;
      },
      panel(key, body) {
        return `<div class="admin-page-panel" role="tabpanel" id="${id}-panel-${key}" aria-labelledby="${id}-tab-${key}" data-admin-tab-group="${id}" data-admin-tab-panel="${key}" ${options.get() === key ? '' : 'hidden inert'}>${body}</div>`;
      },
    };
  }
  function controller(id, initial, fetch, view) {
    let adapter = {},
      owner = '',
      revision = 0,
      state = initial(),
      tabGroup;
    const tabs = () =>
      (tabGroup ||= createTabs(id, {
        get: () => module.state.sectionTab,
        set: (value) => (module.state.sectionTab = value),
        active: () => module.active(),
        busy: () => module.state.busy,
      }));
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
      tabs(items, label) {
        return tabs().bar(items, label);
      },
      panel(key, body) {
        return tabs().panel(key, body);
      },
      selectTab(key, focus = false) {
        tabs().select(key, focus);
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
            window.LumoAdminFeedback?.error(cause, { retry: () => module.refresh() });
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
        return `<div id="${id}" aria-busy="${Boolean(state.busy)}">${state.error && !window.LumoAdminFeedback ? `<div class="admin-error" role="alert">${esc(state.error)}<button class="outline" data-module-retry="${id}">重试</button></div>` : ''}${view(module)}</div>`;
      },
    };
    document.addEventListener('click', (event) => {
      if (event.target.closest(`[data-module-retry="${id}"]`) && module.active()) module.refresh();
    });
    return module;
  }
  window.LumoAdminUI = {
    feedback: window.LumoAdminFeedback,
    esc,
    money,
    date,
    parse,
    empty,
    pair,
    heading,
    pager,
    createTabs,
    controller,
  };
})();
