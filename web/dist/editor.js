(function () {
  'use strict';
  const clone = value => JSON.parse(JSON.stringify(value));
  const defaultCatalog = clone(typeof presentationProducts !== 'undefined' ? presentationProducts : products);
  const defaults = {
    brand: 'LUMO', brandSuffix: 'Mall', heroTitle: '智享生活\n从此不同',
    heroSubtitle: '甄选品质好物，享受更美好的生活', heroEyebrow: 'SMART LIVING, SIMPLY BETTER',
    heroImage: 'hero.jpg', accent: '#ff443d', sectionTitle: '今日推荐', sectionSubtitle: '精选好物，为你而来'
  };
  const shop = { ...defaults };
  const studio = { tab: 'products', query: '', status: 'all', savedAt: '', loadWarning: '' };
  const el = selector => document.querySelector(selector);
  const safeText = (value, max, fallback = '') => typeof value === 'string' ? value.trim().slice(0, max) : fallback;
  function safeImage(value, fallback = 'hero.jpg') {
    if (typeof value !== 'string' || value.length > 2500000) return fallback;
    if (/^data:image\/(png|jpeg|webp);base64,[A-Za-z0-9+/=]+$/.test(value)) return value;
    if (/^https:\/\//i.test(value)) { try { const u = new URL(value); if (u.protocol === 'https:' && !u.username && !u.password) return u.href; } catch (_) {} }
    if (/^(?:assets\/)?[A-Za-z0-9_-]+\.(?:png|jpe?g|webp)$/i.test(value)) return value;
    return fallback;
  }
  function asset(value) { const src = safeImage(value); return /^https:|^data:|^assets\//i.test(src) ? src : 'assets/' + src; }
  function normalizeSettings(raw) {
    const output = { ...defaults };
    if(raw?.heroFileId) output.heroFileId=safeText(raw.heroFileId,128);
    const lengths = { brand: 20, brandSuffix: 12, heroTitle: 60, heroSubtitle: 120, heroEyebrow: 80, sectionTitle: 30, sectionSubtitle: 80 };
    for (const key of Object.keys(lengths)) if (raw && typeof raw[key] === 'string') output[key] = safeText(raw[key], lengths[key], defaults[key]) || defaults[key];
    output.heroImage = safeImage(raw?.heroImage, defaults.heroImage);
    output.accent = /^#[0-9a-f]{6}$/i.test(raw?.accent || '') ? raw.accent : defaults.accent;
    return output;
  }
  const numberValue = (value, min, max, fallback) => Number.isFinite(Number(value)) && Number(value) >= min && Number(value) <= max ? Math.round(Number(value)) : fallback;
  function normalizeProduct(raw) {
    if (!raw || typeof raw !== 'object' || !/^[a-zA-Z0-9_-]{1,80}$/.test(raw.id || '')) throw new Error('商品编号格式不正确');
    const original = defaultCatalog.find(p => p.id === raw.id);
    const name = safeText(raw.name, 100);
    if (!name) throw new Error('商品名称不能为空');
    const configured = window.LumoCategories?.configuration?.groups?.map(group => group.category) || categories.slice(1);
    if (!configured.includes(raw.category) && !products.some(product => product.id === raw.id && product.category === raw.category)) throw new Error('商品分类不正确');
    const productPrice = numberValue(raw.price, 1, 9999999, NaN);
    if (!Number.isFinite(productPrice)) throw new Error('商品售价应为 1 至 9,999,999 元');
    const stock = raw.stock === undefined ? 50 : typeof raw.stock === 'number' ? raw.stock : typeof raw.stock === 'string' && /^\d+$/.test(raw.stock.trim()) ? Number(raw.stock) : NaN;
    if (!Number.isSafeInteger(stock) || stock < 0 || stock > 999999) throw new Error('库存应为 0 至 999,999 的整数');
    const strings = (values, fallback, max = 12) => Array.isArray(values) && values.length ? values.slice(0, max).map(v => safeText(v, 70)).filter(Boolean) : fallback;
    const colors = strings(raw.colors, original?.colors || ['经典款']);
    const options = strings(raw.options, original?.options || ['标准版']);
    if (!colors.length || !options.length) throw new Error('商品颜色及规格不能为空');
    const output = {
      id: raw.id, name, sub: safeText(raw.sub, 140, ''), category: raw.category,
      image: safeImage(raw.image, original?.image || 'speaker.jpg'), image_file_id: safeText(raw.image_file_id,128), brand:safeText(raw.brand,80,original?.brand || 'LUMO'), price: productPrice,
      old: Math.max(productPrice, numberValue(raw.old, 1, 9999999, productPrice)),
      stock, sales: numberValue(raw.sales, 0, 100000000, 0), label: safeText(raw.label, 16),
      visible: raw.visible !== false, colors, options,
      defaultOption: options.includes(raw.defaultOption) ? raw.defaultOption : options[0],
      highlights: strings(raw.highlights, ['甄选品质', '简约设计', '用心之选', '日常好物'], 4),
      highSubs: strings(raw.highSubs, ['品质相伴', '融入生活', '安心选择', '自在日常'], 4)
    };
    while (output.highlights.length < 4) output.highlights.push('品质好物');
    while (output.highSubs.length < 4) output.highSubs.push('用心陪伴每一天');
    if ((!Array.isArray(raw.prices) || raw.prices.length !== options.length) && Array.isArray(raw.variants)) {
      raw = {...raw, prices: options.map(option => Number(raw.variants.find(v => v.option === option)?.price_cents ?? productPrice * 100) / 100)};
    }
    if (Array.isArray(raw.prices) && raw.prices.length === options.length) {
      output.prices = raw.prices.map(n => numberValue(n, 1, 9999999, productPrice));
      const offset = productPrice - output.prices[options.indexOf(output.defaultOption)];
      output.prices = output.prices.map(n => Math.max(1, n + offset));
    }
    if (Array.isArray(raw.specs)) output.specs = raw.specs.slice(0, 16).filter(row => Array.isArray(row) && row.length >= 2).map(row => [safeText(row[0], 50), row[0] === '商品名称' ? name : safeText(row[1], 200)]);
    return output;
  }
  function normalizeBackup(raw) {
    if (!raw || raw.version !== 1 || !Array.isArray(raw.catalog) || raw.catalog.length < 1 || raw.catalog.length > 150) throw new Error('请选择从本编辑器导出的有效 JSON 备份');
    const list = raw.catalog.map(normalizeProduct);
    if (new Set(list.map(p => p.id)).size !== list.length) throw new Error('备份存在重复商品编号');
    if (defaultCatalog.some(p => !list.some(row => row.id === p.id))) throw new Error('备份缺少初始商品，请使用完整备份文件');
    return { catalog: list, settings: normalizeSettings(raw.settings) };
  }
  const api = (service, action, command, options) => window.LumoAPI.call(service, action, command, options);
  function requireAdmin() {
    if (!window.LumoAuth?.isLoggedIn()) { window.LumoAuth?.requireLogin('#/editor'); return false; }
    if (window.LumoAuth.user()?.role !== 'admin') { toast('请使用管理员账号登录后编辑商城'); return false; }
    return true;
  }
  function productCommand(p) { return {...p, price_cents: Math.round(p.price*100), old_price_cents:Math.round(p.old*100), active:p.visible !== false}; }
  async function writeProduct(p, stockUpdates = null) {
    const result = await api('catalog','upsert',{id:p.id,data:productCommand(p)});
    try {
      if (stockUpdates) {
        for (const [sku,onHand] of stockUpdates) await api('inventory','adjust',{id:sku,data:{on_hand:onHand}});
      } else {
        // New catalog variants need their own stock row. Existing stock is never
        // reset when design, price, visibility or a backup is applied.
        const ids=[...new Set((result.variants?.length?result.variants:[{sku_id:result.id}]).map(v=>v.sku_id).filter(Boolean))];
        const batch=await api('inventory','batch_get',{data:{ids}});
        if(!Array.isArray(batch.items)||!Array.isArray(batch.missing_ids))throw Error('库存查询结果不完整，请刷新后重试');
        const existing=new Set(batch.items.map(row=>row.sku_id)),missing=new Set(batch.missing_ids);
        if(ids.some(id=>existing.has(id)===missing.has(id)))throw Error('库存查询结果不完整，请刷新后重试');
        for (const id of ids) if(missing.has(id)) await api('inventory','adjust',{id,data:{on_hand:p.stock ?? 50}});
      }
    } catch(error) { throw new Error('商品资料已保存，库存更新未完成：' + error.message); }
    return result;
  }
  async function refresh() {
    const result = await api('content','list',{data:{kind:'storefront',limit:100}},{auth:false});
    const item = (result.items || []).find(row => row.id === 'storefront-main');
    if (item) {
      const parsed = JSON.parse(item.body); Object.assign(shop,normalizeSettings(parsed));
      studio.savedAt = item.created_at || '';
    } else Object.assign(shop,defaults);
    if (window.LumoAuth?.user()?.role === 'admin' && /^#\/editor(?:\/|$)/.test(location.hash)) {
      const catalog = await api('catalog','list',{data:{limit:100,include_disabled:true}});
      if(window.LumoCommerce?.normalizeProduct) products.splice(0,products.length,...(catalog.items || []).map(window.LumoCommerce.normalizeProduct));
    }
    applyTheme();
  }
  async function commit(nextCatalog, nextSettings = shop, message = '修改已保存') {
    if(!requireAdmin()) return false;
    try {
      const ids = new Set(nextCatalog.map(p => p.id));
      for (const raw of nextCatalog) await writeProduct(normalizeProduct(raw));
      for (const old of products.filter(p => !ids.has(p.id))) await writeProduct(normalizeProduct({...old,visible:false}));
      const settings = normalizeSettings(nextSettings);
      await api('content','upsert',{id:'storefront-main',data:{kind:'storefront',title:'商城首页配置',body:JSON.stringify(settings)}});
      await window.LumoCommerce?.refreshCatalog?.(); await refresh(); await window.LumoInventory?.refresh?.();
      studio.savedAt = new Date().toISOString();
      if(message) toast(message); return true;
    } catch(error) { studio.loadWarning='部分请求可能已保存，请刷新确认后重试。' + error.message;toast(error.message);return false; }
  }
  function applyTheme() {
    document.documentElement.style.setProperty('--red', shop.accent);
    document.documentElement.style.setProperty('--red-dark', shop.accent);
    document.documentElement.style.setProperty('--pale', shop.accent + '12');
  }
  function savedTime() {
    if (!studio.savedAt) return '尚未修改';
    return '已保存于 ' + new Date(studio.savedAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false, timeZone: 'Asia/Shanghai' });
  }
  function field(label, name, value, type = 'text', extra = '') {
    return `<label class="studio-field">${label}<input type="${type}" name="${name}" value="${esc(value)}" ${extra}></label>`;
  }
  function productRows() {
    const rows = products.filter(p => (!studio.query || (p.name + p.category).toLowerCase().includes(studio.query.toLowerCase())) && (studio.status === 'all' || (studio.status === 'on' ? p.visible !== false : p.visible === false)));
    return rows.length ? rows.map(p => `<tr><td><div class="studio-product-cell"><img src="${esc(asset(p.image))}" alt=""><div><strong>${esc(p.name)}</strong><small>${esc(p.sub)}</small></div></div></td><td>${esc(p.category)}</td><td><strong>¥${money(p.price)}</strong><small class="studio-old-price">¥${money(p.old)}</small></td><td><strong>${money(window.LumoInventory?.stockFor(p) ?? (p.stock ?? 0))}</strong><small>${(window.LumoInventory?.stockFor(p) ?? (p.stock ?? 0)) > 0 ? '可售库存' : '暂时售罄'}</small></td><td><span class="studio-pill ${p.visible === false ? 'is-off' : ''}"><i></i>${p.visible === false ? '已下架' : '销售中'}</span></td><td><div class="studio-row-actions"><button data-editor-action="edit-product" data-id="${esc(p.id)}">${icon('edit')}编辑</button><button data-editor-action="toggle-product" data-id="${esc(p.id)}">${p.visible === false ? '上架' : '下架'}</button></div></td></tr>`).join('') : '<tr><td colspan="6"><div class="studio-empty">没有符合条件的商品，试试其他关键词。</div></td></tr>';
  }
  function productsView() {
    const available = products.filter(p => p.visible !== false).length;
    return `<div class="studio-pagehead"><div><span class="studio-section-label">YOUR CURATED COLLECTION</span><h1>商品管理</h1><p>让每一件好物，都有恰到好处的呈现。</p></div><button class="studio-primary" data-editor-action="add-product">${icon('plus')}添加商品</button></div><div class="studio-stats"><div class="studio-stat"><label>全部商品</label><strong>${products.length}<small>件</small></strong><span>你的精选好物</span></div><div class="studio-stat"><label>正在销售</label><strong>${available}<small>件</small></strong><span>在商城中展示</span></div><div class="studio-stat"><label>商品分类</label><strong>${new Set(products.map(p => p.category)).size}<small>类</small></strong><span>覆盖日常品质生活</span></div></div><section class="studio-card"><div class="studio-card-head"><h2>商品列表 <small>${products.length}</small></h2><span class="studio-saved">${icon('check')}${savedTime()}</span></div><div class="studio-toolbar"><form id="studio-search-form">${icon('search')}<input name="query" aria-label="搜索商品名称或分类" placeholder="搜索商品名称或分类" value="${esc(studio.query)}"><button type="submit">搜索</button></form><select id="studio-status-filter" aria-label="商品状态"><option value="all" ${studio.status === 'all' ? 'selected' : ''}>全部状态</option><option value="on" ${studio.status === 'on' ? 'selected' : ''}>销售中</option><option value="off" ${studio.status === 'off' ? 'selected' : ''}>已下架</option></select></div><div class="studio-table-wrap"><table class="studio-table"><thead><tr><th>商品信息</th><th>分类</th><th>售价 / 原价</th><th>库存</th><th>状态</th><th>操作</th></tr></thead><tbody>${productRows()}</tbody></table></div><div class="studio-table-foot">共 ${products.length} 件商品 · 下架商品会从商城列表隐藏，购物记录保留。</div></section><p class="studio-tip">${icon('info')}修改通过管理员接口保存到服务端。库存按 SKU 独立管理，订单记录保留。</p>`;
  }
  function designView() {
    return `<div class="studio-pagehead"><div><span class="studio-section-label">MAKE IT YOUR OWN</span><h1>页面装修</h1><p>从一句文案、一种颜色开始，打造你的商城。</p></div><span class="studio-saved">${icon('check')}${savedTime()}</span></div><div class="studio-grid"><section class="studio-card"><div class="studio-card-head"><h2>品牌与首页</h2></div><form id="studio-settings-form" class="studio-form"><div class="studio-form-row">${field('品牌名称', 'brand', shop.brand, 'text', 'required maxlength="20"')}${field('品牌后缀', 'brandSuffix', shop.brandSuffix, 'text', 'required maxlength="12"')}</div><label class="studio-field">首页主标题<textarea name="heroTitle" rows="2" maxlength="60" required>${esc(shop.heroTitle)}</textarea><small>可换行，建议不超过两行。</small></label>${field('首页副标题', 'heroSubtitle', shop.heroSubtitle, 'text', 'required maxlength="120"')}${field('英文引导语', 'heroEyebrow', shop.heroEyebrow, 'text', 'required maxlength="80"')}<div class="studio-form-row">${field('商品区标题', 'sectionTitle', shop.sectionTitle, 'text', 'required maxlength="30"')}${field('商品区副标题', 'sectionSubtitle', shop.sectionSubtitle, 'text', 'required maxlength="80"')}</div><label class="studio-field">首页封面图片<input name="heroImage" id="studio-hero-image" value="${esc(shop.heroImage)}" maxlength="2500000" required><small>填写 HTTPS 图片地址，或上传本地图片。</small></label><div class="studio-upload-actions"><label class="studio-outline">${icon('camera')}上传封面<input id="studio-hero-upload" type="file" accept="image/png,image/jpeg,image/webp" hidden></label><span>PNG / JPG / WebP，最大 10 MB</span></div><label class="studio-field">商城主题色<div class="studio-color-options">${['#ff443d', '#e85778', '#9354ca', '#337bbf', '#138f82', '#ba843e'].map(c => `<button type="button" style="background:${c}" data-editor-action="accent" data-value="${c}" class="${shop.accent === c ? 'active' : ''}" aria-label="主题色 ${c}" title="${c}">${shop.accent === c ? icon('check') : ''}</button>`).join('')}<input type="color" id="studio-accent" name="accent" value="${shop.accent}" aria-label="自定义主题色"></div></label><div class="studio-form-actions"><button class="studio-primary" type="submit">${icon('check')}保存修改</button><button class="studio-outline" type="submit" name="preview" value="true">保存并预览 ${icon('arrow')}</button></div></form></section><aside><div class="studio-preview-label">首页效果预览</div><div class="studio-preview"><div class="studio-preview-nav"><strong><b id="studio-preview-brand" style="color:${shop.accent}">${esc(shop.brand)}</b> <span id="studio-preview-suffix">${esc(shop.brandSuffix)}</span></strong>${icon('search')}</div><div class="studio-preview-hero"><img id="studio-preview-image" src="${esc(asset(shop.heroImage))}" alt="封面预览"><div class="studio-preview-copy"><span id="studio-preview-eyebrow">${esc(shop.heroEyebrow)}</span><h2 id="studio-preview-title">${esc(shop.heroTitle).replace(/\n/g, '<br>')}</h2><p id="studio-preview-subtitle">${esc(shop.heroSubtitle)}</p><i style="background:${shop.accent}">立即探索 →</i></div></div><div class="studio-preview-categories">${categories.slice(1, 5).map((category, i) => `<div><img src="${esc(asset(categoryImages[i] || 'hero.jpg'))}" alt=""><span>${esc(category)}</span></div>`).join('')}</div><div class="studio-preview-heading"><strong id="studio-preview-section">${esc(shop.sectionTitle)}</strong><small id="studio-preview-section-sub">${esc(shop.sectionSubtitle)}</small></div><div class="studio-preview-products">${products.filter(p => p.visible !== false).slice(0, 2).map(p => `<div><img src="${esc(asset(p.image))}" alt=""><strong>${esc(p.name)}</strong><span style="color:${shop.accent}">¥${money(p.price)}</span></div>`).join('')}</div></div><p class="studio-tip">${icon('info')}预览随文案输入更新。保存后，修改会应用到商城首页。</p></aside></div>`;
  }
  function backupView() {
    return `<div class="studio-pagehead"><div><span class="studio-section-label">KEEP YOUR IDEAS SAFE</span><h1>数据备份</h1><p>保存你的商品与页面设计，随时继续创作。</p></div></div><div class="studio-backup-grid"><section class="studio-card studio-backup-card"><div class="studio-backup-icon">${icon('bag')}</div><h2>导出商城内容</h2><p>将当前商品展示信息与页面设置保存为 JSON 文件。可用于备份，或转移到其他设备。</p><button class="studio-primary" data-editor-action="export">导出备份 ${icon('arrow')}</button><small>不包含库存账本、私有原图、购物车、收货地址和订单。</small></section><section class="studio-card studio-backup-card"><div class="studio-backup-icon">${icon('layers')}</div><h2>导入已有备份</h2><p>选择本编辑器导出的 JSON 文件，恢复商品信息与页面设置。导入前会再次确认。</p><label class="studio-outline">${icon('plus')}选择备份文件<input id="studio-import-file" type="file" accept="application/json,.json" hidden></label><small>仅支持完整备份，最大 12 MB。</small></section></div><section class="studio-card studio-reset-card"><div><h2>恢复初始设计</h2><p>将商品和页面装修恢复为初始内容。购物车与订单记录不受影响，建议先导出当前备份。</p></div><button class="studio-outline danger" data-editor-action="reset">恢复默认</button></section><div class="studio-storage-note">${icon('shield')}<div><strong>你的修改保存在后端服务</strong><p>商品与页面配置按服务端数据展示。导出的 JSON 可用于内容备份，库存变动和订单不包含在其中。</p></div></div>`;
  }
  function renderEditor() {
    if (window.LumoAuth?.user()?.role !== 'admin') return `${header()}<main id="main" class="container"><div class="panel" style="margin:40px 0;padding:40px"><h1>商城编辑工作台</h1><p>需要使用管理员账号登录后，才能管理商品、库存和首页装修。</p><p>如已登录普通账号，请先在账号菜单退出，再登录管理员账号。</p><a class="outline" href="#/">返回商城</a></div></main>${footer()}`;
    const tabs = [['products', 'bag', '商品管理'], ['design', 'layers', '页面装修'], ['categories', 'layers', '分类导航'], ['backup', 'shield', '数据备份']];
    return `<div class="studio-shell"><aside class="studio-sidebar"><a class="studio-logo" href="#/"><b>${esc(shop.brand)}</b><span>${esc(shop.brandSuffix)}</span><small>商城编辑工作台</small></a><div class="studio-nav-label">WORKSPACE</div><nav class="studio-nav" aria-label="编辑器导航">${tabs.map(([key, glyph, text]) => `<button data-editor-action="tab" data-value="${key}" class="${studio.tab === key ? 'active' : ''}">${icon(glyph)}${text}${studio.tab === key ? icon('chevron') : ''}</button>`).join('')}</nav><div class="studio-sidebar-note">${icon('sparkles')}<strong>好设计，随你而变。</strong><p>每个细节，都可以是你的表达。</p><a href="#/">返回商城 ${icon('arrow')}</a></div></aside><div class="studio-main"><header class="studio-topbar"><span>商城工作台 <i>/</i> ${tabs.find(t => t[0] === studio.tab)?.[2]}</span><a class="studio-outline" href="#/">${icon('back')}商城预览</a></header><main id="main" class="studio-content">${studio.loadWarning ? `<p class="studio-tip studio-warning">${icon('info')}${esc(studio.loadWarning)}</p>` : ''}${studio.tab === 'products' ? productsView() : studio.tab === 'design' ? designView() : studio.tab === 'categories' ? window.LumoCategoryEditor.render() : backupView()}<footer class="studio-footer">LUMO STUDIO · 为美好日常而设计</footer></main></div></div>`;
  }
  async function openProduct(id) {
    if(!requireAdmin()) return;
    let stockRows=[];
    const targetProduct=products.find(p=>p.id===id);
    try { if(targetProduct) { const variants=targetProduct.variants?.length?targetProduct.variants:[{sku_id:targetProduct.skuId||id,color:targetProduct.colors[0],option:targetProduct.defaultOption}];const batch=await api('inventory','batch_get',{data:{ids:[...new Set(variants.map(v=>v.sku_id))]}}),stocks=new Map((batch.items||[]).map(row=>[row.sku_id,row]));if(variants.some(v=>!stocks.has(v.sku_id)))throw Error('商品规格库存缺失，请刷新后重试');stockRows=variants.map(v=>({...v,...stocks.get(v.sku_id)}));} }
    catch(error){toast(error.message);return;}

    const original = products.find(p => p.id === id);
    const p = original ? {...original,stock:stockRows.find(v=>v.sku_id===original.skuId||v.sku_id===id)?.on_hand ?? 0} : { name: '', sub: '', category: categories[1] || '', price: '', old: '', label: '', image: 'speaker.jpg', visible: true };
    modal(original ? '编辑商品' : '添加商品', `<form id="studio-product-form" class="studio-form studio-modal-body" data-id="${esc(id || '')}"><p class="modal-desc">修改商品信息，保存后立即在商城中生效。</p>${field('商品名称', 'name', p.name, 'text', 'required maxlength="100" placeholder="如：无线降噪耳机"')}${field('商品简介', 'sub', p.sub, 'text', 'maxlength="140" placeholder="用一句话介绍这件好物"')}<div class="studio-form-row"><label class="studio-field">商品分类<select name="category">${[...new Set([p.category,...(window.LumoCategories?.configuration?.groups?.map(group => group.category) || categories.slice(1))].filter(Boolean))].map(c => `<option ${p.category === c ? 'selected' : ''}>${esc(c)}</option>`).join('')}</select></label>${field('商品标签', 'label', p.label || '', 'text', 'maxlength="16" placeholder="如：人气爆款"')}</div><div class="studio-form-row">${field('售价（元）', 'price', p.price, 'number', 'required min="1" max="9999999" step="1"')}${field('原价（元）', 'old', p.old, 'number', 'required min="1" max="9999999" step="1"')}</div>${field('默认规格账面库存（件）', 'stock', p.stock ?? 50, 'number', `required min="0" max="999999" step="1" data-original-stock="${p.stock ?? 50}"`)}<p class="studio-tip">可售库存由后端扣除预占及已分配数量；退款不会自动恢复实物库存。</p>${stockRows.filter(v=>v.sku_id!==p.skuId&&v.sku_id!==id).map((v,i)=>field(`${v.color} / ${v.option} 库存`, 'stock_variant_'+i,v.on_hand,'number',`min="0" max="999999" data-stock-sku="${esc(v.sku_id)}" data-original-stock="${v.on_hand}"`)).join('')}<p class="studio-tip">各颜色与规格分别记录库存</p><div class="studio-upload"><img id="studio-product-image-preview" src="${esc(asset(p.image))}" alt="商品图片预览"><div><label class="studio-field">商品图片<input name="image" id="studio-product-image" required value="${esc(p.image)}" placeholder="HTTPS 图片地址或本地上传"><small>也可保留现有素材文件名。</small></label><label class="studio-outline">${icon('camera')}上传图片<input id="studio-product-upload" type="file" accept="image/png,image/jpeg,image/webp" hidden></label></div></div><label class="studio-visible-check"><input type="checkbox" name="visible" ${p.visible !== false ? 'checked' : ''}>在商城中展示此商品</label><p class="studio-tip">${icon('info')}${original?.prices ? '修改售价时，其他规格价格会按相同差额调整。' : '原图私有上传，公开商品图会优化尺寸后由管理员发布。'}</p><div class="studio-form-actions"><button type="button" class="studio-outline" data-editor-action="close">取消</button><button type="submit" class="studio-primary">${icon('check')}保存商品</button></div></form>`);
  }
  let pendingImport = null;
  document.addEventListener('click', async event => {
    const target = event.target.closest('[data-editor-action]');
    if (!target || target.disabled) return;
    const action = target.dataset.editorAction;
    if (action === 'tab') { studio.tab = target.dataset.value; render(); window.scrollTo(0, 0); }
    if (action === 'add-product') openProduct();
    if (action === 'edit-product') openProduct(target.dataset.id);
    if (action === 'close') closeModal();
    if (action === 'toggle-product') {
      const list = clone(products), p = list.find(row => row.id === target.dataset.id);
      if (p) { p.visible = p.visible === false; if (await commit(list, shop, p.visible ? '商品已上架' : '商品已下架')) render(); }
    }
    if (action === 'accent') {
      const input = el('#studio-accent');
      if (input) { input.value = target.dataset.value; input.dispatchEvent(new Event('input', { bubbles: true })); }
    }
    if (action === 'export') {
      const blob = new Blob([JSON.stringify({ version: 1, savedAt: new Date().toISOString(), settings: shop, catalog: products }, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob), a = document.createElement('a'); a.href = url; a.download = 'lumo-mall-backup-' + new Date().toISOString().slice(0, 10) + '.json'; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); toast('备份已导出');
    }
    if (action === 'reset') modal('恢复初始设计？', '<p class="modal-desc">商品和页面设置将恢复默认。当前编辑内容将被替换，购物车及订单不受影响。</p><div class="studio-form-actions"><button class="studio-outline" data-editor-action="close">取消</button><button class="studio-primary" data-editor-action="confirm-reset">确认恢复</button></div>');
    if (action === 'confirm-reset') { if (await commit(defaultCatalog, defaults, '已恢复初始设计')) { closeModal(); render(); } }
    if (action === 'confirm-import' && pendingImport) { if (await commit(pendingImport.catalog, pendingImport.settings, '备份已成功导入')) { pendingImport = null; closeModal(); render(); } }
  });
  document.addEventListener('submit', async event => {
    if (event.target.id === 'studio-search-form') { event.preventDefault(); studio.query = new FormData(event.target).get('query').trim(); render(); }
    if (event.target.id === 'studio-product-form') {
      event.preventDefault(); if(!requireAdmin()) return;
      const form=event.target,data=Object.fromEntries(new FormData(form)),existing=products.find(p=>p.id===form.dataset.id),button=form.querySelector('button[type="submit"]');
      if(button)button.disabled=true;
      try {
        if(Number(data.old)<Number(data.price))throw new Error('原价不能低于售价');
        if(!safeImage(data.image,''))throw new Error('请使用 HTTPS 图片地址，或上传 PNG、JPG、WebP 图片');
        const p=normalizeProduct({...existing,...data,id:existing?.id||'custom-'+Date.now().toString(36),visible:data.visible==='on',image_file_id:form.querySelector('#studio-product-image')?.dataset.fileId || existing?.image_file_id});
        const updates = [];
        if(!existing || Number(data.stock)!==Number(form.querySelector('[name="stock"]')?.dataset.originalStock)) updates.push([existing?.skuId || p.id,Number(data.stock)]);
        form.querySelectorAll('[data-stock-sku]').forEach(input=>{if(Number(input.value)!==Number(input.dataset.originalStock))updates.push([input.dataset.stockSku,Number(input.value)]);});
        await writeProduct(p,updates);
        await window.LumoCommerce?.refreshCatalog?.();await refresh();await window.LumoInventory?.refresh?.();await window.LumoDiscovery?.refresh?.();
        closeModal();render();toast(existing?'商品修改已保存':'新商品已添加');
      } catch(error) {toast(error.message);if(button)button.disabled=false;}
    }
    if (event.target.id === 'studio-settings-form') {
      event.preventDefault();if(!requireAdmin())return;
      const data=Object.fromEntries(new FormData(event.target));
      if(!safeImage(data.heroImage,'')){toast('请使用 HTTPS 图片地址，或上传图片');return;}
      const button=event.submitter;if(button)button.disabled=true;
      try {
        data.heroFileId=el('#studio-hero-image')?.dataset.fileId || shop.heroFileId;
        await api('content','upsert',{id:'storefront-main',data:{kind:'storefront',title:'商城首页配置',body:JSON.stringify(normalizeSettings(data))}});
        await refresh();studio.savedAt=new Date().toISOString();
        if(button?.name==='preview')go('');else render();toast('页面设置已保存');
      } catch(error){toast(error.message);if(button)button.disabled=false;}
    }
  });
  document.addEventListener('input', event => {
    const form = event.target.closest('#studio-settings-form');
    if (form) {
      const data = Object.fromEntries(new FormData(form));
      const textMap = { brand: '#studio-preview-brand', brandSuffix: '#studio-preview-suffix', heroSubtitle: '#studio-preview-subtitle', heroEyebrow: '#studio-preview-eyebrow', sectionTitle: '#studio-preview-section', sectionSubtitle: '#studio-preview-section-sub' };
      for (const [key, selector] of Object.entries(textMap)) if (el(selector)) el(selector).textContent = data[key];
      if (el('#studio-preview-title')) el('#studio-preview-title').innerHTML = esc(data.heroTitle).replace(/\n/g, '<br>');
      if (safeImage(data.heroImage, '')) el('#studio-preview-image').src = asset(data.heroImage);
      if (/^#[0-9a-f]{6}$/i.test(data.accent)) { el('#studio-preview-brand').style.color = data.accent; const cta = el('.studio-preview-copy i'); if (cta) cta.style.background = data.accent; }
      document.querySelectorAll('[data-editor-action="accent"]').forEach(button => { button.classList.toggle('active', button.dataset.value === data.accent); button.innerHTML = button.dataset.value === data.accent ? icon('check') : ''; });
    }
    if (event.target.id === 'studio-product-image' && safeImage(event.target.value, '')) el('#studio-product-image-preview').src = asset(event.target.value);
  });
  async function readImage(file) {
    if (!file || !['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) throw new Error('请选择 PNG、JPG 或 WebP 图片');
    if (file.size > 10 * 1024 * 1024) throw new Error('图片大小不能超过 10 MB');
    return new Promise((resolve, reject) => {
      const reader = new FileReader(); reader.onerror = () => reject(new Error('图片读取失败，请重试'));
      reader.onload = () => { const image = new Image(); image.onerror = () => reject(new Error('图片无法识别，请换一张图片')); image.onload = () => { try { const ratio = Math.min(1, 1000 / Math.max(image.width, image.height)); const canvas = document.createElement('canvas'); canvas.width = Math.max(1, Math.round(image.width * ratio)); canvas.height = Math.max(1, Math.round(image.height * ratio)); canvas.getContext('2d').drawImage(image, 0, 0, canvas.width, canvas.height); const result=canvas.toDataURL('image/webp',0.82); if(result.length>450000)throw new Error('优化后的图片仍过大，请使用尺寸较小的图片'); resolve(result); } catch (_) { reject(new Error('图片处理失败，请重试')); } }; image.src = reader.result; }; reader.readAsDataURL(file);
    });
  }
  document.addEventListener('change', async event => {
    if (event.target.id === 'studio-status-filter') { studio.status = event.target.value; render(); }
    if (['studio-product-upload', 'studio-hero-upload'].includes(event.target.id)) {
      const input = event.target, file = input.files?.[0]; if (!file) return;
      const targetId = input.id === 'studio-product-upload' ? '#studio-product-image' : '#studio-hero-image';
      try {
        if(!requireAdmin())return;
        const src = await readImage(file);
        const upload = await api('media','create',{data:{file_name:file.name,content_type:file.type,size:file.size}});
        if(!upload.upload_supported || !upload.upload_url)throw new Error('对象存储尚未启用，无法上传图片；可填写公开 HTTPS 图片地址');
        const body=new FormData();Object.entries(upload.upload_fields || {}).forEach(([key,value])=>body.append(key,value));body.append('file',file);
        const response=await fetch(upload.upload_url,{method:upload.upload_method || 'POST',body});
        if(!response.ok)throw new Error('图片上传失败（HTTP '+response.status+'）');
        const fileId=upload.file_id || upload.id;
        await api('media','complete',{id:fileId});
        await api('media','download',{id:fileId});
 const target = el(targetId); if (target) { target.dataset.fileId=fileId; target.value = src; target.dispatchEvent(new Event('input', { bubbles: true })); toast('原图已私有上传；保存后发布优化图片'); } } catch (error) { toast(error.message); } finally { input.value = ''; }
    }
    if (event.target.id === 'studio-import-file') {
      const input = event.target, file = input.files?.[0]; if (!file) return;
      try {
        if (file.size > 12 * 1024 * 1024) throw new Error('备份文件不能超过 12 MB');
        pendingImport = normalizeBackup(JSON.parse(await file.text()));
        modal('导入商城备份？', `<p class="modal-desc">将导入 ${pendingImport.catalog.length} 件商品及页面设置，并替换当前编辑内容。购物车和订单记录不受影响。</p><div class="studio-form-actions"><button class="studio-outline" data-editor-action="close">取消</button><button class="studio-primary" data-editor-action="confirm-import">确认导入</button></div>`);
      } catch (error) { pendingImport = null; toast(error instanceof SyntaxError ? '文件不是有效的 JSON 备份' : error.message); } finally { input.value = ''; }
    }
  });
  function initialize() { applyTheme(); }
  window.LumoStudio = { settings: shop, renderEditor, asset, safeImage, refresh };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', initialize, { once: true }); else initialize();
})();
