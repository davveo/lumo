(function () {
  'use strict';
  const C = window.LumoConstants;
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const defaults = {
    ...(window.LumoStorefrontLayout?.defaults || {}),
    brand: 'LUMO',
    brandSuffix: 'Mall',
    heroTitle: '智享生活\n从此不同',
    heroSubtitle: '甄选品质好物，享受更美好的生活',
    heroEyebrow: 'SMART LIVING, SIMPLY BETTER',
    heroImage: 'hero.jpg',
    accent: '#ff443d',
    sectionTitle: '今日推荐',
    sectionSubtitle: '精选好物，为你而来',
  };
  const limits = {
    brand: 20,
    brandSuffix: 12,
    heroTitle: 60,
    heroSubtitle: 120,
    heroEyebrow: 80,
    sectionTitle: 30,
    sectionSubtitle: 80,
    heroImage: 900000,
    accent: 7,
    headerSlogan: 60,
    brandTagline: 40,
    searchPlaceholder: 40,
    heroButton: 20,
    heroLink: 200,
    announcement: 120,
    announcementLink: 200,
    footerText: 160,
    contactPhone: 30,
    contactHours: 50,
  };
  let adapter = {},
    owner = '',
    revision = 0,
    pendingWrite = null,
    original = null,
    draft = null,
    changed = new Set(),
    busy = false,
    error = '',
    missing = false;
  const authorized = () => Boolean(adapter.isAdmin?.());
  const currentScope = () => adapter.scope?.() || '';
  const draw = () => adapter.draw?.();
  const active = () => Boolean(document.querySelector('#store-settings-admin-root'));
  function reset() {
    revision++;
    pendingWrite = null;
    owner = currentScope();
    original = null;
    draft = null;
    changed = new Set();
    busy = false;
    error = '';
    missing = false;
  }
  function checkScope() {
    if (owner !== currentScope()) reset();
  }
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
  const asset = (value) => {
    const safe = safeImage(value);
    return /^(?:https:|data:|assets\/)/i.test(safe) ? safe : safe ? 'assets/' + safe : '';
  };
  async function read() {
    try {
      const row = await adapter.call(C.Service.Content, C.Action.Get, { id: 'storefront-main' });
      const parsed = JSON.parse(row.body);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
        throw Error('商城配置应为对象，请先修复现有配置');
      return { value: parsed, missing: false };
    } catch (cause) {
      if (cause.status === 404) return { value: {}, missing: true };
      throw cause;
    }
  }
  function editable(value) {
    const result = {};
    for (const key of Object.keys(defaults)) {
      const fallback = defaults[key];
      result[key] = Array.isArray(fallback)
        ? clone(Array.isArray(value[key]) ? value[key] : fallback)
        : typeof value[key] === typeof fallback
          ? value[key]
          : fallback;
    }
    return result;
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
    if (changed.size) {
      error = '仍有未保存的修改，请先保存或撤销后再重新加载。';
      draw();
      return false;
    }
    const ticket = ++revision,
      scope = owner;
    busy = true;
    error = '';
    try {
      const value = await read();
      if (!current(ticket, scope)) return false;
      original = clone(value.value);
      draft = editable(original);
      missing = value.missing;
      return true;
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '商城配置读取失败';
      return false;
    } finally {
      if (current(ticket, scope)) busy = false;
    }
  }
  function field(label, key, type = 'text', wide = false) {
    return `<label class="field ${wide ? 'wide' : ''}">${label}${key === 'heroTitle' ? `<textarea data-ss-field="${key}" name="${key}" maxlength="${limits[key]}" rows="2" required ${busy ? 'disabled' : ''}>${esc(draft[key])}</textarea>` : `<input data-ss-field="${key}" name="${key}" type="${type}" value="${esc(draft[key])}" maxlength="${limits[key] || 200}" ${['announcement', 'contactPhone', 'contactHours'].includes(key) ? '' : 'required'} ${busy ? 'disabled' : ''}>`}</label>`;
  }
  function preview() {
    const image = asset(draft.heroImage),
      accent = /^#[a-f0-9]{6}$/i.test(draft.accent) ? draft.accent : defaults.accent;
    return `<div class="sa-preview-brand"><b style="color:${accent}">${esc(draft.brand)}</b><span>${esc(draft.brandSuffix)}</span></div><div class="sa-hero-preview">${image ? `<img src="${esc(image)}" alt="首页主视觉预览">` : ''}<div><span>${esc(draft.heroEyebrow)}</span><h2>${esc(draft.heroTitle).replace(/\n/g, '<br>')}</h2><p>${esc(draft.heroSubtitle)}</p></div></div><div class="sa-preview-section"><strong>${esc(draft.sectionTitle)}</strong><p>${esc(draft.sectionSubtitle)}</p></div>`;
  }
  function extraSections() {
    const flags = [
      ['showCategories', '分类侧栏'],
      ['showPicks', '精选好物'],
      ['showMember', '会员权益'],
      ['showCoupons', '领券预览'],
      ['showActivities', '活动快捷区'],
      ['showRecommendations', '推荐商品区'],
    ];
    return `<section class="admin-section"><div class="admin-panel-heading"><h2>页头与搜索</h2><span>全站品牌表达</span></div><div class="admin-form">${field('顶部标语', 'headerSlogan')}${field('品牌引导语', 'brandTagline')}${field('搜索框提示', 'searchPlaceholder', 'text', true)}${field('主视觉按钮文字', 'heroButton')}${field('主视觉跳转地址', 'heroLink')}</div></section><section class="admin-section"><div class="admin-panel-heading"><h2>首页轮播</h2><span>主视觉 + 最多4张附加轮播</span></div><div class="admin-form">${field('自动切换间隔（秒）', 'heroInterval', 'number')}<p class="sa-field-help">填写0关闭自动播放，或填写3–30秒；鼠标悬停、键盘焦点及减少动态效果偏好会暂停自动轮播。</p></div><div class="ss-slides">${draft.heroSlides
      .map(
        (slide, index) =>
          `<section><div class="admin-panel-heading"><h3>附加轮播 ${index + 1}</h3><div class="admin-actions"><button type="button" class="outline" data-ss-action="slide-up" data-index="${index}" ${index === 0 || busy ? 'disabled' : ''}>上移</button><button type="button" class="outline" data-ss-action="slide-remove" data-index="${index}" ${busy ? 'disabled' : ''}>移除</button></div></div><div class="admin-form">${[
            ['标题', 'title'],
            ['副标题', 'subtitle'],
            ['引导语', 'eyebrow'],
            ['图片地址', 'image'],
            ['按钮文字', 'button'],
            ['商城内跳转', 'link'],
          ]
            .map(
              ([label, key]) =>
                `<label class="field">${label}<input data-ss-slide="${index}.${key}" value="${esc(slide[key] || '')}" maxlength="${key === 'image' ? 1000 : key === 'link' ? 200 : 120}" ${busy ? 'disabled' : ''}></label>`,
            )
            .join('')}</div></section>`,
      )
      .join(
        '',
      )}</div><button type="button" class="outline" data-ss-action="slide-add" ${busy || draft.heroSlides.length >= 4 ? 'disabled' : ''}>＋ 添加轮播图</button></section><section class="admin-section"><div class="admin-panel-heading"><h2>首页模块</h2><span>控制首页区域的展示</span></div><div class="ss-modules">${flags.map(([key, label]) => `<label><input type="checkbox" data-ss-field="${key}" ${draft[key] ? 'checked' : ''} ${busy ? 'disabled' : ''}><span>${label}</span></label>`).join('')}</div></section><section class="admin-section"><div class="admin-panel-heading"><h2>顶部公告</h2><span>留空不展示</span></div><div class="admin-form">${field('公告内容', 'announcement', 'text', true)}${field('公告跳转地址', 'announcementLink', 'text', true)}</div></section><section class="admin-section"><div class="admin-panel-heading"><h2>页脚与客服</h2><span>全站展示</span></div><div class="admin-form">${field('页脚说明', 'footerText', 'text', true)}${field('客服电话（选填）', 'contactPhone')}${field('客服时间（选填）', 'contactHours')}</div></section>`;
  }
  function render() {
    checkScope();
    if (!authorized()) return '';
    const heading = `<div class="admin-title"><div><h1>商城装修</h1><p>统一维护品牌、轮播、首页模块、公告与客服展示。</p></div><div class="admin-title-actions"><button class="outline" data-ss-action="reload" ${busy ? 'disabled' : ''}>重新加载</button></div></div>`;
    if (!draft)
      return `<div id="store-settings-admin-root">${heading}${error ? `<div class="admin-error" role="alert">${esc(error)}</div>` : window.LumoSkeleton?.render('form', { count: 6, label: '正在读取商城设置' }) || ''}</div>`;
    return `<div id="store-settings-admin-root">${heading}${error ? `<div class="admin-error" role="alert">${esc(error)}</div>` : ''}${missing ? '<p class="sa-notice">尚未建立首页配置。当前展示商城默认值，保存后正式发布。</p>' : ''}<div class="sa-settings-grid"><form id="store-settings-admin-form"><section class="admin-section"><div class="admin-panel-heading"><h2>品牌信息</h2><span>展示在商城页头</span></div><div class="admin-form">${field('品牌名称', 'brand')}${field('品牌后缀', 'brandSuffix')}<label class="field wide">主题色<div class="sa-color-field"><input type="color" data-ss-field="accent" aria-label="选择商城主题色" value="${/^#[a-f0-9]{6}$/i.test(draft.accent) ? draft.accent : defaults.accent}" ${busy ? 'disabled' : ''}><input name="accent" data-ss-field="accent" aria-label="商城主题色" value="${esc(draft.accent)}" maxlength="7" required ${busy ? 'disabled' : ''}></div></label></div></section><section class="admin-section"><div class="admin-panel-heading"><h2>首页主视觉</h2><span>首屏标题与封面</span></div><div class="admin-form">${field('主标题', 'heroTitle', 'text', true)}${field('副标题', 'heroSubtitle', 'text', true)}${field('引导语', 'heroEyebrow', 'text', true)}${field('封面图片地址', 'heroImage', 'text', true)}<p class="sa-field-help wide">支持 HTTPS 图片地址或现有商城素材文件名。这里只调整主视觉，优惠活动请到活动管理中配置。</p></div></section><section class="admin-section"><div class="admin-panel-heading"><h2>推荐区文案</h2><span>商品展示区</span></div><div class="admin-form">${field('推荐区标题', 'sectionTitle')}${field('推荐区副标题', 'sectionSubtitle')}</div></section>${extraSections()}<div class="sa-savebar"><span data-ss-save-state>${changed.size ? '有未保存的修改' : '已与后端配置同步'}</span><div><button type="button" class="outline" data-ss-action="discard" ${busy || !changed.size ? 'disabled' : ''}>撤销修改</button><button class="primary" type="submit" ${busy || !changed.size ? 'disabled' : ''}>${busy ? '正在保存…' : '保存并发布'}</button></div></div></form><aside class="admin-section sa-preview"><div class="admin-panel-heading"><h2>首页预览</h2><span>随编辑更新</span></div><div data-ss-preview>${preview()}</div><p class="sa-field-help">预览用于检查文案与图片，实际布局以商城页面为准。</p></aside></div></div>`;
  }
  function validate() {
    for (const key of changed) {
      if (key === 'heroSlides') {
        if (!Array.isArray(draft.heroSlides) || draft.heroSlides.length > 4) throw Error('附加轮播最多4张');
        for (const slide of draft.heroSlides) {
          if (
            !slide.title?.trim() ||
            slide.title.length > 60 ||
            String(slide.subtitle || '').length > 120 ||
            String(slide.eyebrow || '').length > 80 ||
            !window.LumoStorefrontLayout.safeImage(slide.image) ||
            !window.LumoStorefrontLayout.safeLink(slide.link) ||
            !slide.button?.trim() ||
            slide.button.length > 20
          )
            throw Error('请检查轮播标题、图片、按钮与商城内跳转地址');
        }
        continue;
      }
      if (typeof defaults[key] === 'boolean') {
        if (typeof draft[key] !== 'boolean') throw Error('模块显示设置无效');
        continue;
      }
      if (key === 'heroInterval') {
        if (!Number.isInteger(draft[key]) || (draft[key] !== 0 && (draft[key] < 3 || draft[key] > 30)))
          throw Error('轮播间隔填写0或3–30秒');
        continue;
      }
      const optional = ['announcement', 'contactPhone', 'contactHours'];
      if (
        typeof draft[key] !== 'string' ||
        (!optional.includes(key) && !draft[key].trim()) ||
        draft[key].length > limits[key]
      )
        throw Error('请检查必填字段及长度限制');
      if (['heroLink', 'announcementLink'].includes(key) && !window.LumoStorefrontLayout.safeLink(draft[key]))
        throw Error('跳转地址只能填写商城内页面，如 #/coupons');
    }
    if (changed.has('heroImage') && !safeImage(draft.heroImage))
      throw Error('请使用 HTTPS 图片地址或现有商城素材文件名');
    if (changed.has('accent') && !/^#[a-f0-9]{6}$/i.test(draft.accent))
      throw Error('主题色请填写有效的六位十六进制色值');
    const form = document.querySelector('#store-settings-admin-form');
    if (form?.reportValidity && !form.reportValidity()) throw Error('请检查表单中标记的字段');
  }
  async function save() {
    try {
      validate();
    } catch (cause) {
      error = cause.message;
      draw();
      return;
    }
    if (!changed.size) return;
    const ticket = ++revision,
      scope = owner,
      edits = new Set(changed),
      values = clone(draft);
    busy = true;
    error = '';
    draw();
    try {
      // Preserve settings owned by other editors, including fields introduced
      // after this page was opened. Only explicitly edited keys are replaced.
      const latest = await read();
      if (!current(ticket, scope)) return;
      const output = latest.missing ? { ...defaults, ...latest.value } : clone(latest.value);
      for (const key of edits) output[key] = values[key];
      const body = JSON.stringify(output);
      if (new TextEncoder().encode(body).length > 900000)
        throw Error('首页配置超过容量限制，请缩小封面图片或改用 HTTPS 地址');
      await adapter.call(C.Service.Content, C.Action.Upsert, {
        id: 'storefront-main',
        data: { kind: C.ContentKind.Storefront, title: '商城首页配置', body },
      });
      if (!current(ticket, scope)) return;
      const confirmed = await read();
      if (!current(ticket, scope)) return;
      if (confirmed.missing) throw Error('保存已提交，但首页配置尚未确认，请重试保存');
      original = clone(confirmed.value);
      draft = editable(original);
      changed.clear();
      missing = confirmed.missing;
      adapter.notify?.('商城装修已保存并发布');
    } catch (cause) {
      if (current(ticket, scope)) error = cause.message || '保存失败，修改已保留';
    } finally {
      if (current(ticket, scope)) {
        busy = false;
        draw();
      }
    }
  }
  document.addEventListener('input', (event) => {
    checkScope();
    const slidePath = event.target.dataset?.ssSlide;
    if (slidePath && active() && authorized() && !busy && draft) {
      const [index, key] = slidePath.split('.');
      if (
        draft.heroSlides[Number(index)] &&
        ['title', 'subtitle', 'eyebrow', 'image', 'button', 'link'].includes(key)
      ) {
        draft.heroSlides[Number(index)][key] = event.target.value;
        changed.add('heroSlides');
        error = '';
        const state = document.querySelector('[data-ss-save-state]');
        if (state) state.textContent = '有未保存的修改';
        document.querySelectorAll('#store-settings-admin-form button').forEach((button) => {
          if (button.type === 'submit') button.disabled = false;
        });
      }
      return;
    }
    const key = event.target.dataset?.ssField;
    if (!active() || !authorized() || busy || !draft || !Object.hasOwn(defaults, key)) return;
    draft[key] =
      event.target.type === 'checkbox'
        ? event.target.checked
        : event.target.type === 'number'
          ? Number(event.target.value)
          : event.target.value;
    changed.add(key);
    error = '';
    if (key === 'accent' && /^#[a-f0-9]{6}$/i.test(draft.accent)) {
      document.querySelectorAll('[data-ss-field="accent"]').forEach((input) => {
        input.value = draft.accent;
      });
    }
    const target = document.querySelector('[data-ss-preview]');
    if (target) target.innerHTML = preview();
    const state = document.querySelector('[data-ss-save-state]');
    if (state) state.textContent = '有未保存的修改';
    document.querySelectorAll('#store-settings-admin-form button').forEach((button) => {
      button.disabled = false;
    });
  });
  document.addEventListener('click', async (event) => {
    const target = event.target.closest?.('[data-ss-action]');
    if (!target) return;
    checkScope();
    if (!active() || !authorized() || busy || target.disabled) return;
    event.preventDefault();
    if (target.dataset.ssAction.startsWith('slide-')) {
      const index = Number(target.dataset.index);
      if (target.dataset.ssAction === 'slide-add' && draft.heroSlides.length < 4)
        draft.heroSlides.push({
          title: '新的精选主题',
          subtitle: '为你的日常带来更多灵感',
          eyebrow: 'A LITTLE MORE FOR YOU',
          image: 'hero.jpg',
          button: '立即查看',
          link: '#/coupons',
        });
      if (target.dataset.ssAction === 'slide-remove') draft.heroSlides.splice(index, 1);
      if (target.dataset.ssAction === 'slide-up' && index > 0)
        [draft.heroSlides[index - 1], draft.heroSlides[index]] = [draft.heroSlides[index], draft.heroSlides[index - 1]];
      changed.add('heroSlides');
      draw();
      return;
    }
    if (target.dataset.ssAction === 'reload') {
      await refresh();
      draw();
    }
    if (target.dataset.ssAction === 'discard' && original) {
      draft = editable(original);
      changed.clear();
      error = '';
      draw();
    }
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'store-settings-admin-form') return;
    event.preventDefault();
    checkScope();
    if (active() && authorized() && !busy && draft) await runWrite(save);
  });
  window.LumoStoreSettingsAdmin = {
    configure(value) {
      adapter = { ...adapter, ...value };
      checkScope();
    },
    refresh,
    render,
    reset,
  };
})();
