/* Shared feedback for the administrative application. */
(function () {
  'use strict';
  const escape = (v) =>
    String(v ?? '').replace(
      /[&<>"']/g,
      (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
    );
  const icons = {
    error:
      '<path d="M12 8v5m0 3h.01M10.3 3.9 2 18.2A1.8 1.8 0 0 0 3.6 21h16.8a1.8 1.8 0 0 0 1.6-2.8L13.7 3.9a2 2 0 0 0-3.4 0Z"/>',
    confirm: '<path d="M12 8v4m0 4h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"/>',
    prompt: '<path d="m16 3 5 5M4 20l4-1 13-13-4-4L4 15l-1 6Z"/>',
    success: '<path d="m8 12 3 3 5-6M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"/>',
  };
  let dialog,
    current = null,
    sequence = 0,
    toastTimer;
  const queue = [],
    recent = new Map(),
    forwarded = new WeakMap();
  const svg = (kind) =>
    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true">${icons[kind]}</svg>`;
  function ensure() {
    if (dialog) return dialog;
    dialog = document.createElement('dialog');
    dialog.className = 'admin-feedback-dialog';
    dialog.dataset.adminFeedback = '';
    dialog.addEventListener('cancel', (event) => {
      event.preventDefault();
      finish(false);
    });
    dialog.addEventListener('click', (event) => {
      if (event.target.closest('[data-feedback-close],[data-feedback-cancel]')) finish(false);
      if (event.target.closest('[data-feedback-retry]')) {
        const retry = current?.retry;
        finish(false);
        if (retry)
          Promise.resolve()
            .then(retry)
            .catch((error) => showError(error));
      }
    });
    dialog.addEventListener('submit', (event) => {
      event.preventDefault();
      if (!current) return;
      const input = dialog.querySelector('[data-admin-dialog-input]');
      finish(current.kind === 'prompt' ? String(input.value).trim() : true);
    });
    dialog.addEventListener('pointerdown', (event) => {
      if (event.target === dialog) {
        const r = dialog.getBoundingClientRect();
        if (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom)
          finish(false);
      }
    });
    document.body.append(dialog);
    return dialog;
  }
  function finish(value) {
    if (!current) return;
    const task = current;
    if (task.kind === 'error') recent.clear();
    current = null;
    dialog.close();
    task.resolve(task.kind === 'prompt' && value === false ? null : value);
    if (task.focus?.isConnected) task.focus.focus({ preventScroll: true });
    else document.getElementById('admin-main')?.focus({ preventScroll: true });
    queueMicrotask(next);
  }
  function next() {
    if (current || !queue.length) return;
    current = queue.shift();
    const task = current,
      d = ensure(),
      id = ++sequence;
    d.dataset.kind = task.kind;
    d.setAttribute('role', task.kind === 'error' ? 'alertdialog' : 'dialog');
    d.setAttribute('aria-labelledby', 'admin-feedback-title-' + id);
    d.setAttribute('aria-describedby', 'admin-feedback-description-' + id);
    d.innerHTML = `<form method="dialog" class="admin-feedback-content"><button type="button" class="admin-feedback-close" data-feedback-close aria-label="关闭提示">×</button><div class="admin-feedback-icon">${svg(task.kind)}</div><div class="admin-feedback-copy"><span class="admin-feedback-eyebrow">${task.kind === 'error' ? '操作提示' : '请确认'}</span><h2 id="admin-feedback-title-${id}">${escape(task.title)}</h2><p id="admin-feedback-description-${id}">${escape(task.message)}</p></div>${task.kind === 'prompt' ? `<label class="field admin-feedback-field">${escape(task.label || '填写内容')}<textarea data-admin-dialog-input rows="3" maxlength="${task.maxLength || 1000}" ${task.required === false ? '' : 'required'}>${escape(task.value || '')}</textarea></label>` : ''}${task.trace ? `<details class="admin-feedback-trace"><summary>查看问题追踪编号</summary><code>${escape(task.trace)}</code><p>反馈问题时可提供此编号，便于定位。</p></details>` : ''}<footer class="admin-feedback-actions">${task.kind !== 'error' ? '<button type="button" class="outline" data-feedback-cancel>取消</button>' : ''}${task.retry ? `<button type="button" class="outline" data-feedback-retry>${escape(task.retryLabel || '重新加载')}</button>` : ''}<button class="primary" type="submit" autofocus>${escape(task.confirmLabel || (task.kind === 'error' ? '我知道了' : '确认'))}</button></footer></form>`;
    d.showModal();
    if (task.kind === 'prompt') d.querySelector('textarea').focus({ preventScroll: true });
  }
  function enqueue(task) {
    return new Promise((resolve) => {
      queue.push({ ...task, resolve, focus: document.activeElement });
      next();
    });
  }
  function showError(value, options = {}) {
    const raw = String(value?.message || value || '操作未完成，请稍后重试');
    const trace =
      value?.traceId ||
      value?.traceID ||
      options.trace ||
      raw.match(/(?:链路\s+|Trace ID:\s*)([a-f0-9]{16,64})/i)?.[1] ||
      '';
    const message = raw
      .replace(/（链路\s+[a-f0-9]{16,64}）/gi, '')
      .replace(/Trace ID:\s*[a-f0-9]{16,64}/gi, '')
      .trim();
    if (!message) return Promise.resolve();
    const now = Date.now(),
      key = message.replace(/\s+/g, ' ').slice(0, 1500);
    if (
      (current?.kind === 'error' && current.message === message) ||
      queue.some((task) => task.kind === 'error' && task.message === message) ||
      (recent.has(key) && now - recent.get(key) < 2500)
    ) {
      if (current?.message === message && options.retry && !current.retry) {
        current.retry = options.retry;
        const retry = document.createElement('button');
        retry.type = 'button';
        retry.className = 'outline';
        retry.dataset.feedbackRetry = '';
        retry.textContent = options.retryLabel || '重新加载';
        dialog.querySelector('footer').prepend(retry);
      }
      return Promise.resolve();
    }
    recent.set(key, now);
    if (recent.size > 100) recent.delete(recent.keys().next().value);
    return enqueue({
      kind: 'error',
      title: options.title || '操作未完成',
      message,
      trace,
      retry: options.retry,
      retryLabel: options.retryLabel,
    });
  }
  function success(message) {
    let el = document.getElementById('admin-toast');
    if (!el) return;
    clearTimeout(toastTimer);
    el.innerHTML = `${svg('success')}<span>${escape(message)}</span>`;
    el.dataset.kind = 'success';
    el.style.display = 'flex';
    toastTimer = setTimeout(() => {
      el.style.display = 'none';
    }, 3500);
  }
  // Older editors also expose local validation through these error regions.
  // Forward them into the same modal without changing their draft/state handling.
  const observer = new MutationObserver(() => {
    document
      .querySelectorAll(
        '.admin-body .admin-error,.admin-body .api-config-error,.admin-body [role="alert"],.admin-body [data-operation-error],.admin-body .ce-dialog-error,.admin-body .pa-error',
      )
      .forEach((el) => {
        const text = el.textContent.trim();
        if (!text || forwarded.get(el) === text) return;
        forwarded.set(el, text);
        el.dataset.feedbackForwarded = '';
        el.hidden = true;
        const retry = el.querySelector('button');
        void showError(text, { retry: retry ? () => retry.isConnected && retry.click() : undefined });
      });
  });
  observer.observe(document.body, { childList: true, subtree: true, characterData: true });
  const invalidFields = new Set();
  document.addEventListener(
    'invalid',
    (event) => {
      if (!event.target.closest('#admin-main') || event.target.closest('[data-admin-feedback]')) return;
      event.preventDefault();
      invalidFields.add(event.target);
      if (invalidFields.size > 1) return;
      queueMicrotask(() => {
        const fields = [...invalidFields];
        invalidFields.clear();
        const message = fields
          .map((field) => {
            const label =
              field.closest('label')?.childNodes[0]?.textContent?.trim() ||
              field.getAttribute('aria-label') ||
              '表单内容';
            return `请检查${label}：${field.validationMessage}`;
          })
          .join('\n');
        void showError(message, {
          title: '请完善表单内容',
          retryLabel: '定位字段',
          retry: () => fields[0]?.isConnected && fields[0].focus(),
        });
      });
    },
    true,
  );
  const header = document.querySelector('.admin-header');
  if (header && typeof ResizeObserver !== 'undefined')
    new ResizeObserver(() =>
      document.body.style.setProperty(
        '--admin-header-height',
        Math.round(header.getBoundingClientRect().height) + 'px',
      ),
    ).observe(header);
  window.LumoAdminFeedback = {
    error: showError,
    success,
    confirm: (message, options = {}) =>
      enqueue({ kind: 'confirm', title: options.title || '确认操作', message, ...options }),
    prompt: (message, value = '', options = {}) =>
      enqueue({ kind: 'prompt', title: options.title || '补充处理信息', message, value, ...options }),
  };
})();
