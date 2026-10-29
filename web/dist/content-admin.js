(function () {
  'use strict';
  const C = window.LumoConstants;
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const kinds = [
    [C.ContentKind.Notice, '商城公告'],
    [C.ContentKind.Faq, '帮助与常见问题'],
  ];
  const pageSize = 20;
  let adapter = {},
    owner = '',
    revision = 0,
    pendingWrite = null,
    rows = [],
    ready = false,
    busy = false,
    error = '',
    kind = C.ContentKind.Notice,
    page = 1,
    hasNext = false,
    draft = null;
  const authorized = () => Boolean(adapter.isAdmin?.());
  const currentScope = () => adapter.scope?.() || '';
  const draw = () => adapter.draw?.();
  const active = () => Boolean(document.querySelector('#content-admin-root'));
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

  function reset() {
    revision++;
    pendingWrite = null;
    owner = currentScope();
    rows = [];
    ready = false;
    busy = false;
    error = '';
    kind = C.ContentKind.Notice;
    page = 1;
    hasNext = false;
    draft = null;
  }
  function checkScope() {
    if (owner !== currentScope()) reset();
  }
  async function fetchPage(nextPage) {
    const result = await adapter.call(C.Service.Content, C.Action.List, {
      data: { kind, limit: pageSize + 1, offset: (nextPage - 1) * pageSize },
    });
    if (!Array.isArray(result.items)) throw Error('内容列表返回格式无效');
    return { rows: clone(result.items.slice(0, pageSize)), hasNext: result.items.length > pageSize };
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
      const result = await fetchPage(page);
      if (!current(ticket, scope)) return false;
      rows = result.rows;
      hasNext = result.hasNext;
      ready = true;
      return true;
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '内容读取失败';
      return false;
    } finally {
      if (current(ticket, scope)) busy = false;
    }
  }
  const kindName = () => kinds.find(([key]) => key === kind)?.[1] || '';
  function list() {
    return `<div class="sa-content-tabs" role="group" aria-label="内容分类">${kinds.map(([key, label]) => `<button class="${kind === key ? 'active' : ''}" data-cn-kind="${key}" ${busy ? 'disabled' : ''}>${label}</button>`).join('')}</div><section class="admin-list-panel"><div class="admin-list-summary"><span>${esc(kindName())}</span><span>本页 ${rows.length} 条</span></div>${rows.length ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table sa-content-table"><thead><tr><th scope="col">内容标题</th><th scope="col">正文摘要</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead><tbody>${rows.map((row) => `<tr><td><strong>${esc(row.title)}</strong><small>${esc(row.id)}</small></td><td><p class="sa-content-excerpt">${esc(row.body)}</p></td><td><span class="admin-state success"><i></i>已发布</span></td><td><div class="admin-actions"><button data-cn-action="edit" data-cn-id="${esc(row.id)}" ${busy ? 'disabled' : ''}>编辑内容</button></div></td></tr>`).join('')}</tbody></table></div>` : '<div class="admin-empty"><strong>暂无已发布内容</strong><span>新建公告或常见问题，帮助顾客了解商城。</span></div>'}<div class="admin-pagination"><span>第 ${page} 页 · 每页最多 ${pageSize} 条</span><div><button class="outline" data-cn-action="prev" ${page === 1 || busy ? 'disabled' : ''}>上一页</button><button class="outline" data-cn-action="next" ${!hasNext || busy ? 'disabled' : ''}>下一页</button></div></div></section>`;
  }
  function render() {
    checkScope();
    if (!authorized()) return '';
    const heading = `<div class="admin-title"><div><h1>${draft ? (draft.isNew ? '发布新内容' : '编辑内容') : '内容管理'}</h1><p>维护商城公告与帮助文档，保存后立即发布。</p></div><div class="admin-title-actions">${draft ? `<button class="outline" data-cn-action="back" ${busy ? 'disabled' : ''}>返回列表（放弃未发布修改）</button>` : `<button class="outline" data-cn-action="reload" ${busy ? 'disabled' : ''}>刷新</button><button class="primary" data-cn-action="new" ${busy || !ready ? 'disabled' : ''}>新建内容</button>`}</div></div>`;
    const content = draft
      ? `<form id="content-admin-form" class="admin-section sa-content-form"><div class="admin-form"><label class="field">内容类型<select name="kind" data-cn-field="kind" ${busy || !draft.isNew ? 'disabled' : ''}>${kinds.map(([key, label]) => `<option value="${key}" ${draft.kind === key ? 'selected' : ''}>${label}</option>`).join('')}</select></label><label class="field">内容编号<input value="${esc(draft.id)}" readonly><small>自动生成，重复保存不会创建多条记录</small></label><label class="field wide">标题<input name="title" data-cn-field="title" value="${esc(draft.title)}" maxlength="120" required ${busy ? 'disabled' : ''}></label><label class="field wide">正文<textarea name="body" data-cn-field="body" maxlength="20000" required ${busy ? 'disabled' : ''}>${esc(draft.body)}</textarea><small>纯文本内容，最多 20,000 字节；FAQ 会在帮助中心展示。</small></label><div class="admin-form-footer"><span>保存后立即对顾客展示</span><button type="submit" class="primary" ${busy ? 'disabled' : ''}>${busy ? '正在发布…' : '保存并发布'}</button></div></div></form>`
      : !ready
        ? error
          ? ''
          : window.LumoSkeleton?.render('table', { count: 6, label: '正在读取商城内容' }) || ''
        : list();
    return `<div id="content-admin-root">${heading}${error ? `<div class="admin-error" role="alert">${esc(error)}</div>` : ''}${content}</div>`;
  }
  async function save() {
    if (!kinds.some(([key]) => key === draft.kind) || !draft.title.trim() || !draft.body.trim()) {
      error = '请选择内容类型并填写标题、正文';
      draw();
      return;
    }
    if (draft.title.length > 120 || new TextEncoder().encode(draft.body).length > 20000) {
      error = '标题或正文超过长度限制';
      draw();
      return;
    }
    const form = document.querySelector('#content-admin-form');
    if (form?.reportValidity && !form.reportValidity()) return;
    const ticket = ++revision,
      scope = owner,
      outgoing = clone(draft);
    busy = true;
    error = '';
    draw();
    try {
      await adapter.call(C.Service.Content, C.Action.Upsert, {
        id: outgoing.id,
        data: { kind: outgoing.kind, title: outgoing.title.trim(), body: outgoing.body },
      });
      if (!current(ticket, scope)) return;
      // Confirm the published record instead of trusting local form values.
      const confirmed = await adapter.call(C.Service.Content, C.Action.Get, { id: outgoing.id });
      if (!current(ticket, scope)) return;
      if (confirmed.id !== outgoing.id || confirmed.kind !== outgoing.kind) throw Error('发布结果尚未确认，请重试读取');
      kind = outgoing.kind;
      page = 1;
      const result = await fetchPage(page);
      if (!current(ticket, scope)) return;
      rows = result.rows;
      hasNext = result.hasNext;
      ready = true;
      draft = null;
      adapter.notify?.('内容已保存并发布');
      window.scrollTo?.(0, 0);
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '发布失败，输入已保留';
    } finally {
      if (current(ticket, scope)) {
        busy = false;
        draw();
      }
    }
  }
  document.addEventListener('input', (event) => {
    checkScope();
    const key = event.target.dataset?.cnField;
    if (!active() || !authorized() || busy || !draft || !['title', 'body'].includes(key)) return;
    draft[key] = event.target.value;
  });
  document.addEventListener('change', (event) => {
    checkScope();
    if (!active() || !authorized() || busy || !draft || !draft.isNew || event.target.dataset?.cnField !== 'kind')
      return;
    if (kinds.some(([key]) => key === event.target.value)) draft.kind = event.target.value;
  });
  document.addEventListener('click', async (event) => {
    const target = event.target.closest?.('[data-cn-action],[data-cn-kind]');
    if (!target) return;
    checkScope();
    if (!active() || !authorized() || busy || target.disabled) return;
    event.preventDefault();
    if (target.dataset.cnKind) {
      if (!kinds.some(([key]) => key === target.dataset.cnKind)) return;
      kind = target.dataset.cnKind;
      page = 1;
      ready = false;
      rows = [];
      await refresh();
      draw();
      return;
    }
    const action = target.dataset.cnAction;
    if (action === 'new' && ready) {
      draft = {
        id:
          'content-' +
          (crypto.randomUUID ? crypto.randomUUID() : Date.now().toString(36) + Math.random().toString(36).slice(2)),
        kind,
        title: '',
        body: '',
        isNew: true,
      };
      error = '';
      draw();
      window.scrollTo?.(0, 0);
    }
    if (action === 'edit') {
      const row = rows.find((value) => value.id === target.dataset.cnId);
      if (row) {
        draft = { id: row.id, kind: row.kind, title: row.title || '', body: row.body || '', isNew: false };
        error = '';
        draw();
        window.scrollTo?.(0, 0);
      }
    }
    if (action === 'back') {
      draft = null;
      error = '';
      draw();
      window.scrollTo?.(0, 0);
    }
    if (action === 'reload') {
      await refresh();
      draw();
    }
    if ((action === 'prev' && page > 1) || (action === 'next' && hasNext)) {
      const oldPage = page;
      page += action === 'prev' ? -1 : 1;
      if (!(await refresh())) page = oldPage;
      draw();
    }
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'content-admin-form') return;
    event.preventDefault();
    checkScope();
    if (active() && authorized() && !busy && draft) await runWrite(save);
  });
  window.LumoContentAdmin = {
    configure(value) {
      adapter = { ...adapter, ...value };
      checkScope();
    },
    refresh,
    render,
    reset,
  };
})();
