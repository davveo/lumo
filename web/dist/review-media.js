(function () {
  'use strict';
  const C = window.LumoConstants,
    API = window.LumoAPI;
  const esc = (v) =>
    String(v ?? '').replace(
      /[&<>"']/g,
      (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
    );
  let adminBridge = null,
    lightbox = null,
    lightboxURL = '',
    lightboxRevision = 0;
  const uploads = new Map();
  const scope = () => JSON.stringify([API.getBase?.(), API.getToken?.()]);
  function imageURL(review, file, part = 'review') {
    const url = new URL((API.getBase?.() || location.origin) + '/api/v1/interaction/review_image');
    url.searchParams.set('id', review);
    url.searchParams.set('file_id', file);
    url.searchParams.set('scope', part);
    return url.href;
  }
  function grid(row, options = {}) {
    const ids = row.photo_ids || [];
    if (!ids.length) return '';
    const review = options.reviewId || row.review_id || row.id,
      part = options.part || 'review',
      published = options.published ?? row.status === C.InteractionStatus.Published;
    return `<div class="review-photo-grid">${ids.map((id, index) => `<button type="button" class="review-photo" data-review-photo="${esc(id)}" data-review-id="${esc(review)}" data-review-part="${part}" data-review-mode="${options.mode === 'admin' ? 'admin' : 'member'}" aria-label="查看第${index + 1}张商品实拍图">${published ? `<img src="${esc(imageURL(review, id, part))}" loading="lazy" decoding="async" alt="第${index + 1}张商品实拍图" data-review-thumb>` : `<span>实拍图 ${index + 1}<small>点击预览</small></span>`}</button>`).join('')}</div>`;
  }
  function close() {
    lightboxRevision++;
    if (lightboxURL) URL.revokeObjectURL(lightboxURL);
    lightboxURL = '';
    lightbox?.close?.();
    lightbox?.remove?.();
    lightbox = null;
  }
  async function openPhoto(button) {
    close();
    const ticket = lightboxRevision,
      currentScope = button.dataset.reviewMode === 'admin' ? adminBridge?.scope?.() : scope();
    const dialog = document.createElement('dialog');
    dialog.className = 'review-lightbox';
    dialog.setAttribute('aria-label', '商品实拍图预览');
    dialog.innerHTML =
      '<div class="review-lightbox-top"><strong>商品实拍图</strong><button type="button" data-review-close aria-label="关闭图片预览">×</button></div><div class="review-lightbox-body" role="status">正在读取图片…</div>';
    document.body.appendChild(dialog);
    lightbox = dialog;
    if (dialog.showModal) dialog.showModal();
    else dialog.setAttribute('open', '');
    dialog.addEventListener('cancel', (e) => {
      e.preventDefault();
      close();
    });
    dialog.addEventListener('click', (e) => {
      if (e.target === dialog) close();
    });
    try {
      const command = {
        id: button.dataset.reviewId,
        data: { file_id: button.dataset.reviewPhoto, scope: button.dataset.reviewPart || 'review' },
      };
      let result;
      if (button.dataset.reviewMode === 'admin') {
        if (!adminBridge?.call) throw Error('管理会话不可用');
        result = await adminBridge.call(C.Service.Interaction, C.Action.ReviewImage, command);
      } else
        result = await API.call(C.Service.Interaction, C.Action.ReviewImage, command, {
          auth: Boolean(API.getToken?.()),
        });
      const latest = button.dataset.reviewMode === 'admin' ? adminBridge?.scope?.() : scope();
      if (ticket !== lightboxRevision || currentScope !== latest || dialog !== lightbox) return;
      if (
        result.content_type !== C.MediaContentType.JPEG ||
        typeof result.file_content !== 'string' ||
        result.file_content.length > 3 * 1024 * 1024
      )
        throw Error('图片响应无效');
      const raw = atob(result.file_content),
        bytes = new Uint8Array(raw.length);
      for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
      lightboxURL = URL.createObjectURL(new Blob([bytes], { type: C.MediaContentType.JPEG }));
      dialog.querySelector('.review-lightbox-body').innerHTML = `<img src="${esc(lightboxURL)}" alt="商品实拍图">`;
    } catch (error) {
      if (ticket === lightboxRevision && dialog === lightbox)
        dialog.querySelector('.review-lightbox-body').textContent = error.message || '图片暂不可用';
    }
  }
  function uploader(ids = [], row = null, part = 'review') {
    const unique = [...new Set(ids)].slice(0, 5);
    return `<section class="review-upload-panel" data-review-upload-panel data-review-existing="${esc(JSON.stringify(unique))}"><div class="review-upload-heading"><strong>商品实拍图</strong><span>选填，最多5张</span></div>${unique.length ? `<div class="review-upload-existing">${unique.map((id, i) => `<div data-review-existing-item="${esc(id)}">${row ? grid({ ...row, photo_ids: [id] }, { part, published: false }) : ''}<button type="button" class="review-text-action" data-review-remove-existing="${esc(id)}">移除第${i + 1}张</button></div>`).join('')}</div>` : ''}<label class="review-upload-pick"><span>＋ 添加实拍图</span><small>支持JPG、PNG，自动压缩后上传</small><input type="file" accept="image/jpeg,image/png" multiple data-review-files aria-label="添加商品实拍图"></label><div class="review-upload-list" data-review-upload-list aria-live="polite"></div></section>`;
  }
  function uploadState(form) {
    form.dataset.reviewUploadKey ||= crypto.randomUUID();
    let state = uploads.get(form.dataset.reviewUploadKey);
    if (!state) {
      state = { form, scope: scope(), rows: [] };
      uploads.set(form.dataset.reviewUploadKey, state);
    }
    return state;
  }
  function existing(form) {
    try {
      return JSON.parse(form.querySelector('[data-review-upload-panel]')?.dataset.reviewExisting || '[]');
    } catch (_) {
      return [];
    }
  }
  function drawUploads(state) {
    if (state.scope !== scope() || !state.form.isConnected) return;
    const target = state.form.querySelector('[data-review-upload-list]');
    if (!target) return;
    target.innerHTML = state.rows
      .map(
        (row) =>
          `<div class="review-upload-item"><img src="${esc(row.preview)}" alt="待提交实拍图"><div><strong>${esc(row.name)}</strong><small>${row.error ? esc(row.error) : row.id ? '上传完成，提交后等待审核' : '正在压缩并上传…'}</small></div><div>${row.error ? `<button type="button" class="review-text-action" data-review-upload-retry="${row.key}">重试</button>` : ''}<button type="button" class="review-text-action" data-review-upload-remove="${row.key}">移除</button></div></div>`,
      )
      .join('');
  }
  async function compress(file) {
    if (![C.MediaContentType.JPEG, C.MediaContentType.PNG].includes(file.type) || file.size > 20 * 1024 * 1024)
      throw Error('请选择20MiB以内的JPG或PNG图片');
    const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
    try {
      const scale = Math.min(1, 1600 / Math.max(bitmap.width, bitmap.height)),
        canvas = document.createElement('canvas');
      canvas.width = Math.max(1, Math.round(bitmap.width * scale));
      canvas.height = Math.max(1, Math.round(bitmap.height * scale));
      const context = canvas.getContext('2d');
      context.fillStyle = '#fff';
      context.fillRect(0, 0, canvas.width, canvas.height);
      context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      const blob = await new Promise((resolve) => canvas.toBlob(resolve, C.MediaContentType.JPEG, 0.82));
      if (!blob || blob.size > 2 * 1024 * 1024) throw Error('图片压缩后仍过大，请换一张较小的图片');
      return new File([blob], file.name.replace(/\.[^.]+$/, '') + '.jpg', { type: C.MediaContentType.JPEG });
    } finally {
      bitmap.close();
    }
  }
  async function upload(state, row) {
    row.error = '';
    row.pending = true;
    drawUploads(state);
    try {
      row.compressed ||= await compress(row.file);
      if (state.scope !== scope() || row.removed) return;
      const file = row.compressed,
        meta = await API.call(C.Service.Media, C.Action.Create, {
          data: { file_name: file.name, content_type: file.type, size: file.size },
        });
      if (!meta.upload_url || meta.upload_supported === false) throw Error('图片存储暂不可用，请稍后重试或移除此图');
      if (state.scope !== scope() || row.removed) return;
      const body = new FormData();
      for (const [k, v] of Object.entries(meta.upload_fields || {})) body.append(k, v);
      body.append('file', file);
      const response = await fetch(meta.upload_url, { method: meta.upload_method || 'POST', body });
      if (!response.ok) throw Error('图片上传失败，请重试');
      if (state.scope !== scope() || row.removed) return;
      const id = meta.file_id || meta.id;
      await API.call(C.Service.Media, C.Action.Complete, { id });
      if (state.scope !== scope() || row.removed) return;
      row.id = id;
    } catch (error) {
      if (state.scope === scope() && !row.removed) row.error = error.message || '上传失败，请重试';
    } finally {
      row.pending = false;
      drawUploads(state);
    }
  }
  function ids(form) {
    const state = uploads.get(form.dataset.reviewUploadKey);
    if (state && state.scope !== scope()) throw Error('登录状态已变化，请重新选择图片');
    if (state?.rows.some((r) => r.pending)) throw Error('图片仍在上传，请稍候');
    if (state?.rows.some((r) => r.error || !r.id)) throw Error('请重试失败图片或移除后提交');
    return [...existing(form), ...(state?.rows || []).map((r) => r.id)];
  }
  document.addEventListener('change', async (e) => {
    const input = e.target.closest?.('[data-review-files]');
    if (!input) return;
    const form = input.closest('form');
    if (!form) return;
    const state = uploadState(form),
      files = [...input.files];
    input.value = '';
    if (existing(form).length + state.rows.length + files.length > 5) {
      window.toast?.('最多上传5张商品实拍图');
      return;
    }
    const rows = files.map((file) => ({
      key: crypto.randomUUID(),
      file,
      name: file.name,
      preview: URL.createObjectURL(file),
      pending: true,
      error: '',
      id: '',
    }));
    state.rows.push(...rows);
    drawUploads(state);
    await Promise.all(rows.map((row) => upload(state, row)));
  });
  document.addEventListener('click', async (e) => {
    const b = e.target.closest?.(
      '[data-review-photo],[data-review-close],[data-review-remove-existing],[data-review-upload-retry],[data-review-upload-remove]',
    );
    if (!b) return;
    e.preventDefault();
    if (b.hasAttribute('data-review-close')) {
      close();
      return;
    }
    if (b.dataset.reviewPhoto) {
      await openPhoto(b);
      return;
    }
    const form = b.closest('form');
    if (!form) return;
    if (b.dataset.reviewRemoveExisting) {
      const panel = form.querySelector('[data-review-upload-panel]');
      panel.dataset.reviewExisting = JSON.stringify(
        existing(form).filter((id) => id !== b.dataset.reviewRemoveExisting),
      );
      b.closest('[data-review-existing-item]')?.remove();
      return;
    }
    const state = uploadState(form),
      key = b.dataset.reviewUploadRetry || b.dataset.reviewUploadRemove,
      row = state.rows.find((r) => r.key === key);
    if (!row) return;
    if (b.dataset.reviewUploadRetry) {
      if (!row.pending) await upload(state, row);
    } else {
      row.removed = true;
      URL.revokeObjectURL(row.preview);
      state.rows = state.rows.filter((r) => r !== row);
      drawUploads(state);
    }
  });
  document.addEventListener(
    'error',
    (e) => {
      if (e.target?.matches?.('[data-review-thumb]')) {
        const label = document.createElement('span');
        label.textContent = '点击查看图片';
        e.target.replaceWith(label);
      }
    },
    true,
  );
  window.addEventListener('lumo:api-scope-changed', () => {
    close();
    for (const state of uploads.values())
      for (const row of state.rows) {
        row.removed = true;
        URL.revokeObjectURL(row.preview);
      }
    uploads.clear();
  });
  function release(form) {
    const state = uploads.get(form?.dataset?.reviewUploadKey);
    if (!state) return;
    for (const row of state.rows) {
      row.removed = true;
      URL.revokeObjectURL(row.preview);
    }
    uploads.delete(form.dataset.reviewUploadKey);
  }
  if (typeof MutationObserver === 'function')
    new MutationObserver(() => {
      for (const state of uploads.values()) if (!state.form.isConnected) release(state.form);
    }).observe(document.body, { childList: true, subtree: true });
  window.addEventListener('lumo:session-expired', close);
  window.LumoReviewMedia = {
    grid,
    uploader,
    release,
    ids,
    close,
    configureAdmin: (value) => {
      adminBridge = value;
    },
  };
})();
