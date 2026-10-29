const CommerceEnums = window.LumoConstants;
('use strict');
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const esc = (s) =>
  String(s ?? '').replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
  );
const money = (n) => Number(n || 0).toLocaleString('zh-CN', { maximumFractionDigits: 2 });
const price = (n, cls = '') => `<span class="price ${cls}"><small>¥</small>${money(n)}</span>`;
const paths = {
  home: 'M3 10l9-8 9 8M5 9v12h14V9M9 21v-8h6v8',
  bell: 'M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4',
  search: 'M21 21l-5-5M18 10.5a7.5 7.5 0 1 1-15 0 7.5 7.5 0 0 1 15 0',
  cart: 'M2 3h3l2.5 12h11L21 7H6M9 21h.01M18 21h.01',
  heart: 'M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z',
  bag: 'M5 7h14l1 14H4L5 7ZM8 8V6a4 4 0 0 1 8 0v2',
  user: 'M20 21v-2a7 7 0 0 0-14 0v2M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0',
  arrow: 'M5 12h14m-5-5 5 5-5 5',
  chevron: 'm9 5 7 7-7 7',
  back: 'm15 5-7 7 7 7',
  check: 'm5 12 4 4L19 6',
  close: 'm6 6 12 12M18 6 6 18',
  trash: 'M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7',
  minus: 'M5 12h14',
  plus: 'M12 5v14M5 12h14',
  shield: 'M12 3 4 6v6c0 5 8 9 8 9s8-4 8-9V6l-8-3Zm-4 9 3 3 5-6',
  truck: 'M1 5h13v12H1V5Zm13 5h4l4 4v3h-8M5 20a3 3 0 1 0 0-6 3 3 0 0 0 0 6m13 0a3 3 0 1 0 0-6 3 3 0 0 0 0 6',
  return: 'M3 10h11a6 6 0 0 1 0 12h-4M7 4l-6 6 6 6',
  support: 'M3 14v-3a9 9 0 0 1 18 0v3M3 12h3v7H3v-7Zm15 0h3v7h-3v-7Zm3 6c0 3-4 4-7 4',
  filter: 'M4 5h16l-6 7v6l-4 2v-8L4 5Z',
  sort: 'M8 4v16m-4-4 4 4 4-4M16 20V4m-4 4 4-4 4 4',
  pin: 'M20 10c0 6-8 12-8 12S4 16 4 10a8 8 0 0 1 16 0ZM15 10a3 3 0 1 1-6 0 3 3 0 0 1 6 0',
  ticket: 'M3 4h18v5a3 3 0 0 0 0 6v5H3v-5a3 3 0 0 0 0-6V4Zm12 2v3m0 3v3m0 3v1',
  coin: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M15 8h-4a2 2 0 0 0 0 4h2a2 2 0 0 1 0 4H9m3-10v12',
  chip: 'M6 6h12v12H6V6Zm3 3h6v6H9V9ZM9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4',
  camera: 'M3 7h4l2-3h6l2 3h4v14H3V7Zm13 6a4 4 0 1 1-8 0 4 4 0 0 1 8 0',
  layers: 'm12 3 10 6-10 6L2 9l10-6Zm-9 11 9 5 9-5m-18 5 9 5 9-5',
  sparkles: 'm12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3Z',
  info: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M12 11v6m0-10v.1',
  clock: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M12 7v5l3 2',
  edit: 'm15 4 5 5M3 21l5-1L21 7l-5-5L3 15v6Z',
};
const icon = (name, cls = '') =>
  `<svg class="${cls}" viewBox="0 0 24 24" aria-hidden="true"><path d="${paths[name] || paths.bag}"/></svg>`;
const defaultShopSettings = {
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
const shop = () => ({
  ...defaultShopSettings,
  ...(window.LumoStorefrontLayout?.defaults || {}),
  ...(window.LumoStudio?.settings || {}),
});
const asset = (src) => {
  src = String(src || '');
  if (/^https:\/\//i.test(src) || /^data:image\/(png|jpeg|webp);base64,/i.test(src)) return src;
  if (/^(assets\/)?[a-zA-Z0-9_.-]+$/.test(src)) return src.startsWith('assets/') ? src : 'assets/' + src;
  return 'assets/hero.jpg';
};
const presentationProducts = [
  {
    id: 'iphone',
    name: 'Apple iPhone 15 Pro',
    sub: '钛金属设计 · Pro 级影像',
    category: '手机数码',
    image: 'iphone-black.png',
    price: 7999,
    old: 8999,
    sales: 21800,
    label: '人气爆款',
    colors: ['黑色钛金属', '蓝色钛金属', '白色钛金属'],
    options: ['128GB', '256GB', '512GB', '1TB'],
    defaultOption: '256GB',
    prices: [7499, 7999, 9999, 11999],
    highlights: ['A17 Pro', '4800 万', '钛金属', 'iOS 17'],
    highSubs: ['强劲芯片', '超清主摄', '轻盈坚固', '流畅体验'],
    specs: [
      ['商品名称', 'Apple iPhone 15 Pro'],
      ['屏幕尺寸', '6.1 英寸'],
      ['处理器', 'A17 Pro 芯片'],
      ['存储容量', '128GB / 256GB / 512GB / 1TB'],
      ['包装清单', '手机、USB-C 充电线、资料'],
    ],
  },
  {
    id: 'macbook',
    name: 'MacBook Air 13 英寸',
    sub: 'M3 芯片 · 轻盈，更有力量',
    category: '电脑办公',
    image: 'macbook-midnight.png',
    price: 8999,
    old: 9999,
    sales: 12600,
    label: '精选好物',
    colors: ['午夜色', '银色'],
    options: ['256GB', '512GB'],
    defaultOption: '256GB',
    prices: [8999, 10499],
    highlights: ['M3 芯片', '13.6 英寸', '轻至 1.24kg', '18 小时'],
    highSubs: ['澎湃性能', '绚丽显示屏', '轻装出行', '持久续航'],
  },
  {
    id: 'headphones',
    name: '索尼 WH-1000XM5 无线降噪耳机',
    sub: '沉浸聆听，让世界安静下来',
    category: '手机数码',
    image: 'headphones.jpg',
    price: 2299,
    old: 2799,
    sales: 18200,
    label: '品质之选',
    colors: ['黑色', '铂金银'],
    options: ['标准版'],
    defaultOption: '标准版',
    highlights: ['智能降噪', '30 小时', '轻盈佩戴', '清晰通话'],
    highSubs: ['专注每一刻', '长久陪伴', '舒适聆听', '连接自如'],
  },
  {
    id: 'watch',
    name: 'LUMO 简约智能腕表',
    sub: '简约设计，记录每一刻',
    category: '服饰箱包',
    image: 'watch.jpg',
    price: 1299,
    old: 1699,
    sales: 6300,
    colors: ['银白色', '经典黑'],
    options: ['38mm', '42mm'],
    defaultOption: '38mm',
    highlights: ['极简表盘', '日常记录', '轻盈设计', '舒适佩戴'],
    highSubs: ['纯粹美学', '陪伴生活', '轻装出行', '自在随行'],
  },
  {
    id: 'speaker',
    name: '便携蓝牙音箱',
    sub: '好声音，随时在身边',
    category: '家用电器',
    image: 'speaker.jpg',
    price: 699,
    old: 899,
    sales: 9200,
    label: '限时好价',
    colors: ['曜石黑'],
    options: ['标准版'],
    defaultOption: '标准版',
    highlights: ['蓝牙连接', '立体声', '轻巧便携', '长效续航'],
    highSubs: ['即刻连接', '饱满音质', '随心出行', '尽兴聆听'],
  },
  {
    id: 'sneakers',
    name: 'Nike Air Force 1 运动鞋',
    sub: '经典白色，轻松穿搭',
    category: '运动户外',
    image: 'sneakers-white.jpg',
    price: 899,
    old: 1099,
    sales: 15600,
    colors: ['经典白'],
    options: ['39码', '40码', '41码', '42码', '43码'],
    defaultOption: '42码',
    highlights: ['经典设计', '舒适脚感', '耐磨鞋底', '日常百搭'],
    highSubs: ['简约耐看', '轻松迈步', '安心出行', '自在表达'],
  },
  {
    id: 'perfume',
    name: 'CHANEL N°5 经典香水',
    sub: '经典花香，留住心动瞬间',
    category: '美妆个护',
    image: 'perfume.jpg',
    price: 1080,
    old: 1280,
    sales: 7800,
    colors: ['经典款'],
    options: ['50ml', '100ml'],
    defaultOption: '50ml',
    prices: [1080, 1580],
    highlights: ['经典花香', '精致瓶身', '细腻香调', '礼赠之选'],
    highSubs: ['优雅随行', '简约美学', '悠然绽放', '心意相伴'],
  },
  {
    id: 'camera',
    name: '复古微单数码相机',
    sub: '留住生活中的每一帧',
    category: '手机数码',
    image: 'camera.jpg',
    price: 5999,
    old: 6499,
    sales: 3800,
    colors: ['经典黑'],
    options: ['单机身', '镜头套装'],
    defaultOption: '单机身',
    prices: [5999, 6999],
    highlights: ['清晰成像', '快速对焦', '轻巧机身', '随心创作'],
    highSubs: ['记录细节', '捕捉精彩', '自在出行', '表达自我'],
  },
  {
    id: 'sofa',
    name: '云朵模块布艺沙发',
    sub: '给日常留一处柔软',
    category: '家居生活',
    image: 'sofa.jpg',
    price: 1899,
    old: 2399,
    sales: 2400,
    colors: ['森林绿'],
    options: ['三人位'],
    defaultOption: '三人位',
    highlights: ['柔软坐感', '简约造型', '舒适靠背', '自在组合'],
    highSubs: ['放松身心', '融入日常', '温柔支撑', '随心搭配'],
  },
  {
    id: 'bag',
    name: '城市轻行通勤背包',
    sub: '装下日常，轻松出发',
    category: '服饰箱包',
    image: 'bag.jpg',
    price: 299,
    old: 399,
    sales: 8600,
    colors: ['经典款'],
    options: ['标准版'],
    defaultOption: '标准版',
    highlights: ['轻量设计', '合理分区', '日常百搭', '耐用面料'],
    highSubs: ['轻松出行', '有序收纳', '简洁美观', '长久相伴'],
  },
  {
    id: 'coffee',
    name: '精选中度烘焙咖啡豆',
    sub: '从一杯香醇开始，唤醒日常',
    category: '食品生鲜',
    image: 'coffee.jpg',
    price: 99,
    old: 129,
    sales: 11900,
    label: '日常精选',
    colors: ['经典拼配'],
    options: ['250g', '500g'],
    defaultOption: '250g',
    prices: [99, 179],
    highlights: ['中度烘焙', '香醇口感', '精选拼配', '新鲜风味'],
    highSubs: ['均衡风味', '细腻回甘', '美好日常', '用心冲煮'],
  },
  {
    id: 'appliance',
    name: '复古迷你冰箱',
    sub: '为日常鲜活，留一点空间',
    category: '家用电器',
    image: 'appliance.jpg',
    price: 1299,
    old: 1599,
    sales: 4700,
    colors: ['薄荷绿'],
    options: ['标准版'],
    defaultOption: '标准版',
    highlights: ['复古设计', '精巧机身', '分区收纳', '日常好用'],
    highSubs: ['简约美学', '节省空间', '有序储鲜', '陪伴每一天'],
  },
];
const products = [];
const fallbackProduct = {
  id: '',
  name: '商品信息加载中',
  category: '',
  image: 'hero.jpg',
  sub: '',
  price: 0,
  old: 0,
  sales: 0,
  colors: ['默认'],
  options: ['标准版'],
  defaultOption: '标准版',
  highlights: [],
  highSubs: [],
};
const byId = (id) =>
  products.find((p) => p.id === id || p.skuId === id || p.variants?.some((v) => v.sku_id === id)) || {
    ...fallbackProduct,
    id: String(id || ''),
  };
const categories = ['全部好物'];
const categoryImages = [
  'iphone-black.png',
  'macbook-midnight.png',
  'speaker.jpg',
  'sofa.jpg',
  'perfume.jpg',
  'bag.jpg',
  'sneakers-white.jpg',
  'coffee.jpg',
];
const read = (key, fallback) => {
  try {
    return JSON.parse((window.LumoAuth || localStorage).getItem(key)) ?? fallback;
  } catch {
    return fallback;
  }
};
// Business state is a projection of service responses, never restored from the old demo cache.
let state = {
  cart: [],
  favorites: [],
  favoriteSKUs: [],
  address: null,
  orders: [],
  pending: null,
  points: false,
  coupon: 0,
  shipping: 0,
};
let ui = {
  category: '全部好物',
  categoryLabel: '',
  sort: '综合',
  query: '',
  min: '',
  max: '',
  onlyFavorites: false,
  filters: false,
  manage: false,
  detailTab: 0,
};
let detail = { id: 'iphone', color: '黑色钛金属', option: '256GB', qty: 1, thumb: 0 };
let selectedMethod = CommerceEnums.PaymentProvider.Demo,
  countdownTimer,
  toastTimer,
  checkoutMode = state.checkout?.mode || 'cart';
let directItem = state.checkout?.item || null;
const setCheckout = (mode, item = null) => {
  checkoutMode = mode;
  directItem = item;
  state.checkout = { mode, item };
  save();
};
const save = () => {}; // Address, cart, assets and orders are persisted by their own backend services.
const itemPrice = (item) => {
  if (Number.isFinite(item.unitPrice)) return item.unitPrice;
  const p = byId(item.id),
    v = p.variants?.find((v) => v.sku_id === item.sku_id || (v.color === item.color && v.option === item.option));
  return Number(v?.price_cents ?? p.price_cents ?? Math.round(p.price * 100)) / 100;
};
const cartCount = () => state.cart.reduce((a, b) => a + b.qty, 0);
function displayCartCount() {
  if (!window.LumoAuth?.isLoggedIn()) return window.LumoAuth?.model?.().checking ? null : 0;
  if (commerceReadState('cart').loaded) return cartCount();
  const summary = window.LumoAccountSummary?.model?.();
  return summary?.loaded && summary.cart_count != null && Number.isFinite(Number(summary.cart_count))
    ? Math.max(0, Number(summary.cart_count))
    : null;
}
const selectedItems = () => {
  let base =
    checkoutMode === CommerceEnums.OrderSource.Direct && directItem
      ? [directItem]
      : state.cart.filter((x) => x.selected);
  return window.LumoPromotions ? window.LumoPromotions.augmentItems(base) : base;
};
const subtotal = (items) => items.reduce((s, x) => s + itemPrice(x) * x.qty, 0);
const couponValue = (items) => {
  if (window.LumoMember) return window.LumoMember.couponValue(items);
  let amount = subtotal(items.filter((x) => !x.promotion));
  return state.coupon === 300 && amount >= 20000
    ? 300
    : state.coupon === 200 && amount >= 15000
      ? 200
      : state.coupon === 100 && amount >= 1000
        ? 100
        : 0;
};
const totals = (items) => {
  const q = commerceDisplayQuote(items);
  if (q) return normalizeTotals(q);
  const sub = subtotal(items);
  return { sub, original: sub, activitySavings: 0, coupon: 0, points: 0, shipping: 0, total: sub, estimated: true };
};
const imageFor = (item) =>
  item.image ||
  (item.id === 'iphone' && byId(item.id).image === 'iphone-black.png'
    ? `iphone-${String(item.color || '').includes('蓝') ? 'blue' : String(item.color || '').includes('白') ? 'white' : 'black'}.png`
    : byId(item.id).image);
const itemText = (item) => [item.color, item.option].filter(Boolean).join(' / ');
function toast(msg) {
  let el = $('#toast');
  if (!el) return;
  el.innerHTML = icon('check') + esc(msg);
  el.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove('show'), 2600);
}
function go(path) {
  if (location.hash === '#/' + path) {
    render();
    window.scrollTo(0, 0);
  } else location.hash = '/' + path;
}
function primaryNavigation() {
  const navigationCategories = window.LumoCategories?.categories || categories;
  const route = (location.hash || '#/').slice(2).split('/')[0];
  const section = route === 'promotions' ? 'promotions' : route === 'coupons' ? 'coupons' : 'catalog';
  const selected = (key) => (section === key ? ' active' : '');
  const current = (key) => (section === key ? ' aria-current="page"' : '');
  return `<nav class="container nav" aria-label="商品分类"><a class="nav-item${selected('promotions')}" href="#/promotions/flash"${current('promotions')}>${icon('ticket')}活动福利</a><a class="nav-item${selected('coupons')}" href="#/coupons"${current('coupons')}>领券中心</a>${navigationCategories
    .map((c) => {
      const active = section === 'catalog' && ui.category === c;
      return `<button type="button" data-action="category" data-value="${esc(c)}" class="nav-item${active ? ' active' : ''}"${active ? ' aria-current="page"' : ''}>${esc(c)}</button>`;
    })
    .join('')}</nav>`;
}
function positionPrimaryNavigation() {
  const nav = document.querySelector('.nav[aria-label="商品分类"]'),
    active = nav?.querySelector('[aria-current="page"]');
  if (!active) return;
  const bounds = nav.getBoundingClientRect(),
    item = active.getBoundingClientRect();
  if (item.left < bounds.left) nav.scrollLeft -= bounds.left - item.left;
  else if (item.right > bounds.right) nav.scrollLeft += item.right - bounds.right;
}
function canManageStore() {
  return window.LumoAuth?.isLoggedIn() === true && window.LumoAuth.user()?.role === CommerceEnums.Role.Admin;
}
function header(showNav = false) {
  const cartDisplay = displayCartCount();
  return `<div class="utility"><div class="container"><span>${esc(shop().headerSlogan || '品质生活，好物相伴')}</span><div class="utility-links"><a href="#/coupons">领券中心</a>${window.LumoAuthView?.headerUtility() || `<span>欢迎来到 ${esc(shop().brand)} ${esc(shop().brandSuffix)}</span><a href="#/member">个人中心</a>`}<button data-action="orders">我的订单</button><button data-action="help">帮助中心</button></div></div></div>${shop().announcement ? `<div class="store-announcement"><a href="${esc(shop().announcementLink || '#/coupons')}">${esc(shop().announcement)} →</a></div>` : ''}<header class="header"><div class="container header-main"><a class="brand" href="#/" aria-label="${esc(shop().brand + ' ' + shop().brandSuffix + ' 首页')}"><b>${esc(shop().brand)}</b><span>${esc(shop().brandSuffix)}</span><small>${esc(shop().brandTagline || 'CURATED FOR EVERYDAY')}</small></a><form class="search" id="search-form" role="search"><span class="disc-search-kind">商品</span><input id="search-input" name="query" aria-label="搜索商品" placeholder="${esc(shop().searchPlaceholder || '搜索商品，发现好物')}" value="${esc(ui.query)}" autocomplete="off"><button class="disc-search-submit" type="submit" aria-label="搜索">搜索</button></form><div class="header-actions">${canManageStore() ? `<a class="text-button" href="admin.html">运营台</a><a class="edit-store-button" href="#/editor" aria-label="编辑商城">${icon('edit')}<span>编辑商城</span></a>` : ''}<a class="icon-button mall-messages-action" href="#/member/messages" aria-label="消息中心">${icon('bell')}${window.LumoAuth?.isLoggedIn() && window.LumoServices?.unreadCount() ? `<span class="badge">${window.LumoServices.unreadCount()}</span>` : ''}</a><button class="icon-button favorites-action" data-action="favorites" aria-label="我的收藏">${icon('heart')}${state.favorites.length ? `<span class="badge">${state.favorites.length}</span>` : ''}</button><a class="icon-button cart-action" href="#/cart" aria-label="${cartDisplay == null ? '购物车' : `购物车 ${cartDisplay} 件商品`}">${icon('cart')}${cartDisplay > 0 ? `<span class="badge">${cartDisplay}</span>` : ''}<span class="header-label">购物车</span></a>${window.LumoAuthView?.headerAccount() || `<a class="icon-button member-action" href="#/member" aria-label="个人中心">${icon('user')}<span class="header-label">我的</span></a>`}</div></div>${showNav ? primaryNavigation() : ''}</header>`;
}
function footer() {
  return `<section class="service-strip"><div class="container services">${[
    ['shield', '甄选品质好物'],
    ['return', '7 天无理由退换'],
    ['truck', '全国配送服务'],
    ['support', '贴心售后支持'],
  ]
    .map(([i, t]) => `<div class="service">${icon(i)}<span>${t}</span></div>`)
    .join(
      '',
    )}</div></section><footer class="container footer"><a class="brand" href="#/"><b>${esc(shop().brand)}</b><span>${esc(shop().brandSuffix)}</span></a><span>${esc(shop().footerText || '© 2026 LUMO Mall · 订单与资产由服务端保存 · 演示支付渠道')}${shop().contactPhone ? `<small class="store-contact">客服 ${esc(shop().contactPhone)} ${esc(shop().contactHours || '')}</small>` : ''}</span><div class="footer-links">${canManageStore() ? '<a href="admin.html">商城管理</a>' : ''}<a href="#/member/support">客服中心</a><button data-action="help">购物帮助</button><button data-action="about">关于我们</button><button data-action="privacy">隐私说明</button></div></footer>`;
}
function productCard(p) {
  return `<article class="product-card"><a class="product-image ${['iphone', 'macbook'].includes(p.id) ? 'official-image' : ''}" href="#/product/${esc(p.id)}" aria-label="查看${esc(p.name)}"><img src="${esc(asset(p.image))}" alt="${esc(p.name)}" loading="lazy">${p.label ? `<span class="product-label">${esc(p.label)}</span>` : ''}</a>${window.LumoDiscovery?.compareButton(p) || ''}<button class="favorite-button ${state.favorites.includes(p.id) ? 'saved' : ''}" data-action="favorite" data-id="${esc(p.id)}" aria-label="${state.favorites.includes(p.id) ? '取消收藏' : '收藏'}${esc(p.name)}" aria-pressed="${state.favorites.includes(p.id)}">${icon('heart')}</button><div class="product-info"><a class="product-title" href="#/product/${esc(p.id)}">${esc(p.name)}</a><p class="product-sub">${esc(p.sub)}</p><div class="price-line">${price(p.price)}<span class="old-price">¥${money(p.old)}</span><button class="card-cart" data-action="quick-add" data-id="${esc(p.id)}" aria-label="将${esc(p.name)}加入购物车" ${p.visible === false || (window.LumoInventory && window.LumoInventory.stockFor(p) <= 0) ? 'disabled' : ''}>${icon('cart')}</button></div></div></article>`;
}
function homeMemberBenefits() {
  const loggedIn = !!window.LumoAuth?.isLoggedIn(),
    summary = window.LumoAccountSummary?.model?.(),
    ready = loggedIn && summary?.loaded;
  const name = ready ? summary.profile?.display_name : '';
  const metric = (field) =>
    ready && summary[field] != null && Number.isFinite(Number(summary[field])) ? Number(summary[field]) : '—';
  return `<section class="home-member-card" aria-label="会员权益"><div class="home-member-greeting"><span class="home-member-avatar">${icon('user')}</span><div><strong>${loggedIn ? esc(name || '欢迎回来') : 'Hi，欢迎来到 LUMO'}</strong><p>${loggedIn ? '你的品质生活，从这里继续' : '登录发现属于你的好物与福利'}</p></div></div><div class="home-member-actions">${loggedIn ? '<a href="#/member">会员中心</a><a href="#/member/orders">我的订单</a>' : '<a href="#/login">立即登录</a><a href="#/coupons">去领好券</a>'}</div><div class="home-member-metrics"><a href="#/member/coupons"><strong>${metric('coupon_count')}</strong><span>可用优惠券</span></a><a href="#/member/points"><strong>${metric('points')}</strong><span>我的积分</span></a><a href="#/member/favorites"><strong>${metric('favorite_count')}</strong><span>我的收藏</span></a></div></section>`;
}
function homeActivityCards() {
  const promotions = window.LumoPromotions,
    status = promotions?.status();
  const flash = promotions?.flashOffers().find((row) => row.status === CommerceEnums.PromotionOfferStatus.Active);
  const group = promotions?.groupOffers().find((row) => row.status === CommerceEnums.PromotionOfferStatus.Active);
  const exchange = promotions
    ?.exchangeOffers([])
    .find((row) => row.status === CommerceEnums.PromotionOfferStatus.Active);
  const campaign = promotions?.campaigns()[0];
  const cards = [
    { title: '限时秒杀', label: '限时好价', route: 'flash', ready: 'flash', offer: flash, tone: 'red' },
    { title: '一起拼好物', label: '拼团享优惠', route: 'group', ready: 'group', offer: group, tone: 'violet' },
    {
      title: '活动精选',
      label: campaign ? promotions.campaignRule(campaign) : '发现购物好福利',
      route: campaign?.kind || 'reduction',
      ready: 'campaign',
      campaign,
      tone: 'amber',
    },
    { title: '超值换购', label: '好物加价购', route: 'exchange', ready: 'exchange', offer: exchange, tone: 'blue' },
  ];
  return `<div class="home-activity-grid" aria-label="活动福利">${cards
    .map((card) => {
      const catalogPending = !products.length && !commerceState.catalogLoaded && !commerceState.catalogError;
      const pending =
        (!!status?.loading || !(status?.ready?.[card.ready] ?? status?.loaded) || catalogPending) && !status?.error;
      const body = card.offer
        ? `<span class="home-activity-product">${esc(card.offer.product.name)}</span><strong class="home-activity-price">¥${money(card.offer.price)}</strong><img src="${esc(asset(card.offer.product.image))}" alt="" loading="lazy">`
        : card.campaign
          ? `<span class="home-activity-product">${esc(card.campaign.name || '商城优惠活动')}</span><strong class="home-activity-rule">${esc(promotions.campaignRule(card.campaign))}</strong>`
          : pending
            ? commerceSkeleton('summary', { count: 1, label: '正在加载活动' })
            : `<span class="home-activity-empty">${status?.error ? '活动暂未加载，进入重试' : '更多活动准备中'}</span>`;
      return `<a class="home-activity-card is-${card.tone}" href="#/promotions/${esc(card.route)}"><h2>${card.title}${icon('chevron')}</h2><p>${esc(card.label)}</p>${body}</a>`;
    })
    .join('')}</div>`;
}
function homeLanding(list, busy, error, recommendation) {
  const featured = list.slice(0, 4);
  const directory =
    window.LumoCategories?.directory() ||
    `<aside class="category-directory" aria-label="正在加载商品分类"><div class="category-directory-head">${icon('layers')}全部商品分类</div>${commerceSkeleton('navigation', { count: 12, label: '正在加载分类' })}</aside>`;
  const picks =
    busy && !featured.length
      ? commerceSkeleton('products', { count: 4, label: '正在加载精选好物' })
      : featured.length
        ? `<div class="home-picks-grid">${featured.map((product) => `<a class="home-pick" href="#/product/${esc(product.id)}"><img src="${esc(asset(product.image))}" alt="${esc(product.name)}" loading="lazy"><strong>${esc(product.name)}</strong><span>¥${money(product.price)}</span></a>`).join('')}</div>`
        : `<div class="home-picks-empty"><span>${error ? '精选商品暂未加载' : '新好物正在上架'}</span><button data-action="${error ? 'commerce-refresh' : 'explore'}">${error ? '重新加载' : '探索全部好物'} ${icon('arrow')}</button></div>`;
  return `<section class="home-category-layout${shop().showCategories === false ? ' without-directory' : ''}${shop().showMember === false && shop().showCoupons === false ? ' without-benefits' : ''}" aria-label="商城精选与分类">${shop().showCategories === false ? '' : directory}<div class="home-feature-area${shop().showActivities === false ? ' without-activities' : ''}"><div class="home-feature-top${shop().showPicks === false ? ' without-picks' : ''}">${window.LumoStorefrontLayout?.hero(shop()) || `<section class="hero" aria-label="智享生活精选"><div class="hero-copy"><span class="eyebrow">${esc(shop().heroEyebrow)}</span><h1>${esc(shop().heroTitle).replace(/\n/g, '<br>')}</h1><p>${esc(shop().heroSubtitle)}</p><button class="primary" data-action="explore">立即探索 ${icon('arrow')}</button></div><img class="hero-image" src="${esc(asset(shop().heroImage))}" alt="品质生活精选"><span class="home-hero-label">LUMO · 每日灵感</span></section>`}<section class="home-picks" aria-label="精选好物" ${shop().showPicks === false ? 'hidden' : ''}><div class="home-card-heading"><h2>${recommendation.personalized ? '为你精选' : '发现好物'}</h2><button data-action="explore">去逛逛 ${icon('chevron')}</button></div>${picks}</section></div>${shop().showActivities === false ? '' : homeActivityCards()}</div><aside class="home-benefits" aria-label="会员与领券" ${shop().showMember === false && shop().showCoupons === false ? 'hidden' : ''}>${shop().showMember === false ? '' : homeMemberBenefits()}${shop().showCoupons === false ? '' : window.LumoCouponCenter?.home({ limit: 2 }) || `<section class="home-coupons-pending"><h2>${icon('ticket')}领券中心</h2>${commerceSkeleton('coupons', { count: 2, label: '正在加载好券', className: 'sk-coupons-compact' })}</section>`}</aside></section>`;
}
function storefrontNavigation() {
  return `<div class="home-discovery-bar">${primaryNavigation().replace('container nav', 'nav home-category-nav')}<div class="home-entry-links"><a href="#/member">${icon('user')}会员权益</a></div></div>`;
}
function storefrontPageStart(pageClass) {
  return `${header()}<main id="main" class="container storefront-page ${esc(pageClass)}">${storefrontNavigation()}`;
}
function catalog() {
  let list = products.filter(
    (p) =>
      p.visible !== false &&
      (ui.category === '全部好物' || p.category === ui.category) &&
      (!ui.query ||
        `${p.name} ${p.sub} ${p.category} ${window.LumoDiscovery?.brandFor(p) || p.brand || ''}`
          .toLowerCase()
          .includes(ui.query.toLowerCase())) &&
      (!ui.onlyFavorites || state.favorites.includes(p.id)) &&
      (!ui.min || p.price >= Number(ui.min)) &&
      (!ui.max || p.price <= Number(ui.max)),
  );
  if (window.LumoDiscovery) list = window.LumoDiscovery.filter(list);
  if (ui.sort === '价格低') list.sort((a, b) => a.price - b.price);
  if (ui.sort === '价格高') list.sort((a, b) => b.price - a.price);
  if (ui.sort === '销量') list.sort((a, b) => b.sales - a.sales);
  if (ui.sort === '新品' && !ui.query && ui.category === '全部好物')
    list.sort((a, b) => Date.parse(b.created_at || 0) - Date.parse(a.created_at || 0));
  const filtered =
    ui.query ||
    ui.category !== '全部好物' ||
    ui.onlyFavorites ||
    ui.min ||
    ui.max ||
    (state.discovery?.filters?.brand && state.discovery.filters.brand !== 'all') ||
    state.discovery?.filters?.inStock;
  const recommendationMode = !filtered && ui.sort === '综合' && window.LumoRecommendations?.homeModel().ready;
  const homeRecommendation = window.LumoRecommendations?.homeModel() || {};
  if (recommendationMode) list = window.LumoRecommendations.orderHome(list);
  const pageList = window.LumoDiscovery ? window.LumoDiscovery.paginate(list) : list;
  const searchRead = window.LumoDiscovery?.readState?.(),
    catalogBusy =
      (!products.length &&
        !commerceState.catalogLoaded &&
        !commerceState.catalogError &&
        !(filtered && searchRead?.loaded)) ||
      (searchRead && !searchRead.loaded && !searchRead.error && !commerceState.catalogError) ||
      (ui.onlyFavorites && !commerceReadState('favorites').loaded && !commerceReadState('favorites').error);
  const catalogError =
    searchRead?.error || commerceState.catalogError || (ui.onlyFavorites && commerceReadState('favorites').error);
  const catalogCount = catalogBusy ? '加载中' : list.length + ' 件好物';
  return `${storefrontPageStart(`catalog-page${!filtered ? ' home-page' : ''}`)}${!filtered ? homeLanding(list, catalogBusy, catalogError, homeRecommendation) : ''}<section class="catalog" id="catalog" ${!filtered && shop().showRecommendations === false ? 'hidden' : ''}><div class="section-head"><div><h2>${ui.onlyFavorites ? '我的收藏' : ui.categoryLabel ? esc(ui.categoryLabel) : ui.query ? '搜索结果' : ui.category === '全部好物' ? (recommendationMode ? (shop().sectionTitle !== defaultShopSettings.sectionTitle ? esc(shop().sectionTitle) : homeRecommendation.personalized ? '为你推荐' : '精选推荐') : esc(shop().sectionTitle)) : esc(ui.category)}</h2><p>${ui.onlyFavorites ? '每一次心动，都值得收藏' : recommendationMode ? (homeRecommendation.personalized ? '根据你的浏览、收藏、搜索和购买偏好挑选' : '精选当前可售好物，发现新的心动') : esc(shop().sectionSubtitle)}</p></div><span class="subtle">好设计，让日常多一点美好 ${icon('sparkles')}</span></div>${filtered ? `<div class="result-summary">${ui.query ? `“${esc(ui.query)}” · ` : ''}${catalogBusy ? '正在加载商品' : (ui.category !== '全部好物' ? '当前展示' : '共找到') + ' ' + list.length + ' 件好物'}<button data-action="reset-filters">查看全部好物</button></div>` : ''}${window.LumoDiscovery?.controls() || ''}<div class="filter-bar">${['综合', '销量', '价格', '新品'].map((s) => `<button data-action="sort" data-value="${s}" class="filter-tab ${ui.sort.startsWith(s) ? 'active' : ''}">${s}${s === '价格' ? icon(ui.sort === '价格高' ? 'sort' : 'chevron') : ''}</button>`).join('')}<div class="filter-end"><span>${catalogCount}</span><button class="filter-btn" data-action="filters" aria-expanded="${ui.filters}">筛选 ${icon('filter')}</button></div></div><form class="filter-panel" id="price-form" ${ui.filters ? '' : 'hidden'}><span>价格区间</span><input type="number" min="0" name="min" aria-label="最低价格" placeholder="最低价" value="${esc(ui.min)}"><span>—</span><input type="number" min="0" name="max" aria-label="最高价格" placeholder="最高价" value="${esc(ui.max)}"><button class="primary">确定</button><button class="outline" type="button" data-action="reset-price">重置</button></form>${catalogBusy ? commerceSkeleton('products', { count: 6, label: '正在加载商品' }) : list.length ? `<div class="product-grid">${pageList.map((p) => (recommendationMode ? window.LumoRecommendations.homeCard(p) : productCard(p))).join('')}</div>${window.LumoDiscovery?.pagination(list.length) || ''}` : empty(catalogError ? '商品加载失败' : '没有找到合适的好物', esc(catalogError || '') || '试试其他关键词，或看看全部商品。', catalogError ? (searchRead?.error ? 'commerce-search-retry' : 'commerce-refresh') : 'reset-filters', catalogError ? '重新加载' : '浏览全部好物', 'search')}</section>${!filtered ? `<section class="about-strip"><div><h2>生活的美好，藏在用心的选择里。</h2><p>数码灵感、日常所需与心动好物，都在这里。</p></div><a class="outline" href="#/coupons">去领好券 ${icon('arrow')}</a></section>` : ''}</main>${footer()}`;
}
let detailPageProduct = '';
function detailMedia(product, selectedImage) {
  const images = [
    selectedImage,
    ...(Array.isArray(product.images) ? product.images : []),
    ...(Array.isArray(product.gallery) ? product.gallery : []),
    ...(product.variants || []).map((variant) => variant.image),
    product.image,
  ]
    .map((image) => (typeof image === 'string' ? image : image?.url || image?.src))
    .filter(Boolean);
  const media = [...new Set(images)].map((src) => ({ type: 'image', src }));
  const video = product.video_url || product.videoUrl;
  if (typeof video === 'string' && (/^https:\/\//i.test(video) || /^assets\/[a-zA-Z0-9_.-]+\.(mp4|webm)$/i.test(video)))
    media.push({ type: 'video', src: video });
  return media;
}
function detailPage(id) {
  if (!actualProduct(id)) {
    const record = commerceProductReads.get(commerceOwner() + '|' + id),
      error = commerceState.catalogError || record?.error;
    return `${header()}<main id="main" class="container pdp-page">${error ? empty('商品暂不可用', esc(error), 'commerce-product-retry', '重新加载', 'bag') : commerceSkeleton('detail', { label: '正在加载商品详情' })}</main>${footer()}`;
  }
  const p = byId(id),
    variants = p.variants || [];
  if (detailPageProduct !== p.id) {
    detailPageProduct = p.id;
    ui.detailTab = 2;
  }
  if (
    detail.id !== p.id ||
    !p.colors.includes(detail.color) ||
    !p.options.includes(detail.option) ||
    (variants.length && !variants.some((variant) => variant.color === detail.color && variant.option === detail.option))
  ) {
    const initial =
      variants.find((variant) => variant.sku_id === p.skuId) ||
      variants.find((variant) => variant.color === p.colors[0] && variant.option === p.defaultOption) ||
      variants[0];
    detail = {
      id: p.id,
      color: initial?.color || p.colors[0],
      option: initial?.option || p.defaultOption,
      qty: 1,
      thumb: 0,
    };
    ui.detailTab = 2;
  }
  const currentPrice = itemPrice(detail),
    currentImage = imageFor(detail),
    media = detailMedia(p, currentImage);
  detail.thumb = Math.max(0, Math.min(media.length - 1, Number(detail.thumb) || 0));
  const selected = media[detail.thumb],
    selectedVariant = variants.find((variant) => variant.color === detail.color && variant.option === detail.option);
  const read = window.LumoProductExtras?.readState?.(p.id),
    reviews = window.LumoProductExtras?.reviewCount?.(p.id) || 0,
    questions = window.LumoProductExtras?.questionCount?.(p.id) || 0;
  const disabled =
    p.visible === false ||
    (variants.length > 0 && !selectedVariant) ||
    (window.LumoInventory && window.LumoInventory.stockFor(detail) <= 0);
  const specifications = Array.isArray(p.specs)
    ? p.specs
    : [
        ['商品名称', p.name],
        ['商品分类', p.category],
        ['品牌', p.brand || '—'],
        ['商品编号', selectedVariant?.sku_id || p.skuId],
        ['颜色', detail.color],
        ['规格', detail.option],
      ];
  const specTable = `<table class="spec-table">${specifications.map(([key, value]) => `<tr><th>${esc(key)}</th><td>${esc(value)}</td></tr>`).join('')}</table>`;
  const services = `<section class="pdp-service-panel"><h2>服务与售后</h2><div>${icon('truck')}<p><strong>配送说明</strong><span>配送费用与可配送范围，在确认收货地址后由服务端计算。</span></p></div><div>${icon('shield')}<p><strong>商品保障</strong><span>${esc(p.warranty || p.service_policy || '商品信息、价格和库存以当前规格及订单确认结果为准。')}</span></p></div><div>${icon('support')}<p><strong>售后服务</strong><span>可在「我的订单」中查看订单、申请售后，或联系在线客服。</span></p><a href="#/member/support">联系客服 ${icon('chevron')}</a></div></section>`;
  const tabs = [
    [2, '用户评价'],
    [0, '商品详情'],
    [1, '规格参数'],
    [4, '服务保障'],
    [5, '推荐好物'],
    [3, '商品问答'],
  ];
  const tabContent =
    ui.detailTab === 2
      ? window.LumoProductExtras?.reviews(p.id) || '<div class="pdp-tab-empty">暂无评价数据</div>'
      : ui.detailTab === 3
        ? window.LumoProductExtras?.questions(p.id) || '<div class="pdp-tab-empty">暂无问答数据</div>'
        : ui.detailTab === 4
          ? services
          : ui.detailTab === 5
            ? window.LumoProductExtras?.related(p) || '<div class="pdp-tab-empty">暂无推荐商品</div>'
            : ui.detailTab === 1
              ? specTable
              : `<section class="pdp-description"><h2>${esc(p.name)}</h2>${p.sub ? `<p>${esc(p.sub)}</p>` : ''}${p.highlights?.length ? `<div class="highlights">${p.highlights.map((highlight, index) => `<div class="highlight">${icon(['chip', 'camera', 'layers', 'sparkles'][index % 4])}<strong>${esc(highlight)}</strong>${p.highSubs?.[index] ? `<p>${esc(p.highSubs[index])}</p>` : ''}</div>`).join('')}</div>` : ''}<img src="${esc(asset(p.image))}" alt="${esc(p.name)}商品展示" loading="lazy">${specTable}</section>`;
  const address = window.LumoMember?.selectedAddress?.(),
    promotions = window.LumoPromoView?.detail(p, detail) || '';
  const activityPanel = promotions
    ? `<details class="pdp-promotions"><summary><span>商品活动</span><small>查看当前规格可参与的优惠</small>${icon('chevron')}</summary><div>${promotions}</div></details>`
    : '';
  return `${header()}<main class="container pdp-page" id="main"><div class="breadcrumb"><a href="#/">首页</a>${icon('chevron')}<button data-action="category" data-value="${esc(p.category)}">${esc(p.category)}</button>${icon('chevron')}<span>${esc(p.name)}</span></div><div class="pdp-layout"><div class="pdp-main"><section class="pdp-gallery" aria-label="商品图片与视频"><div class="pdp-thumbnails" aria-label="选择商品媒体">${media.map((item, index) => `<button type="button" class="pdp-thumbnail ${detail.thumb === index ? 'active' : ''}" data-action="thumb" data-value="${index}" aria-label="${item.type === 'video' ? '播放商品视频' : '查看商品图片 ' + (index + 1)}" aria-pressed="${detail.thumb === index}"><img src="${esc(asset(item.type === 'video' ? currentImage : item.src))}" alt="">${item.type === 'video' ? '<span class="pdp-video-mark" aria-hidden="true">▶</span>' : ''}</button>`).join('')}</div><div class="pdp-media">${selected?.type === 'video' ? `<video src="${esc(selected.src)}" poster="${esc(asset(currentImage))}" controls playsinline preload="metadata" aria-label="${esc(p.name)}商品视频"></video>` : `<img src="${esc(asset(selected?.src || currentImage))}" alt="${esc(p.name)} · ${esc(detail.color)}" fetchpriority="high">`}<span class="pdp-media-index">${detail.thumb + 1} / ${media.length}</span></div></section><section class="details-bottom pdp-details"><nav class="detail-tabs pdp-tabs" aria-label="商品详情内容">${tabs.map(([value, label]) => `<button class="${ui.detailTab === value ? 'active' : ''}" data-action="detail-tab" data-value="${value}" aria-pressed="${ui.detailTab === value}">${label}${value === 2 || value === 3 ? `<span>${read && !read.loaded && !read.error ? '加载中' : value === 2 ? reviews : questions}</span>` : ''}</button>`).join('')}</nav><div class="pdp-tab-content">${tabContent}</div></section></div><aside class="pdp-purchase" aria-label="商品选购"><div class="pdp-purchase-heading"><span>${esc(p.brand || p.category)}</span><a href="#/member/support">${icon('support')}在线客服</a></div><div class="pdp-product-heading"><h1>${p.label ? `<span class="pdp-product-label">${esc(p.label)}</span>` : ''}${esc(p.name)}</h1>${p.sub ? `<p>${esc(p.sub)}</p>` : ''}</div><section class="pdp-price" aria-label="商品价格"><div><span class="pdp-price-caption">商品价格</span><div>${price(currentPrice)}${Number(p.old) > currentPrice ? `<span class="old-price">¥${money(p.old)}</span>` : ''}</div></div><div class="pdp-price-meta">${p.sales > 0 ? `<span>已售 ${money(p.sales)} 件</span>` : ''}<button type="button" data-action="detail-tab" data-value="2">${reviews} 条评价 ${icon('chevron')}</button></div></section>${activityPanel}<div class="pdp-benefit-row"><span>优惠</span><button type="button" data-action="coupons">${icon('ticket')}查看可用优惠券 ${icon('chevron')}</button></div><div class="pdp-delivery"><span>配送</span><div><button type="button" data-action="address">${icon('pin')}${address ? esc([address.region, address.detail].filter(Boolean).join(' ')) : '选择收货地址'}${icon('chevron')}</button><small>运费与配送范围将在结算时确认</small></div></div><div class="pdp-selections"><div class="pdp-option-row"><h2>颜色</h2><div class="pdp-options">${p.colors.map((color) => `<button type="button" class="pdp-option ${color === detail.color ? 'active' : ''}" data-action="color" data-value="${esc(color)}" aria-pressed="${color === detail.color}">${esc(color)}</button>`).join('')}</div></div><div class="pdp-option-row"><h2>规格</h2><div class="pdp-options">${p.options.map((option) => `<button type="button" class="pdp-option ${option === detail.option ? 'active' : ''}" data-action="option" data-value="${esc(option)}" aria-pressed="${option === detail.option}" ${variants.length && !variants.some((variant) => variant.color === detail.color && variant.option === option) ? 'disabled' : ''}>${esc(option)}</button>`).join('')}</div></div><div class="pdp-selected"><span>已选</span><p>${esc(detail.color)} · ${esc(detail.option)}<small>商品编号：${esc(selectedVariant?.sku_id || p.skuId || p.id)}</small></p></div><div class="pdp-stock">${window.LumoInventory?.badge(detail) || '<span>库存信息将在选择规格后确认</span>'}</div></div><div class="pdp-buybar"><div class="pdp-quantity"><span>数量</span>${quantity(detail.qty, 'detail')}</div><div class="detail-ctas pdp-ctas"><button type="button" class="secondary" data-action="detail-add" ${disabled ? 'disabled' : ''}>${icon('cart')}加入购物车</button><button type="button" class="primary" data-action="buy-now" ${disabled ? 'disabled' : ''}>立即购买</button></div></div>${window.LumoProductExtras?.detailActions(p) || ''}${window.LumoProductAlerts?.actions(p) || ''}<div class="pdp-safe-note">${icon('shield')}价格及库存由服务端确认</div></aside></div></main>${footer()}`;
}
function quantity(q, scope, index = '') {
  const item = scope === 'detail' ? detail : state.cart[Number(index)],
    max = window.LumoInventory && item ? Math.max(1, window.LumoInventory.limit(item, 10)) : 10;
  return `<div class="quantity"><button data-action="quantity" data-scope="${scope}" data-index="${index}" data-value="-1" aria-label="减少数量" ${q <= 1 || item?.unavailable ? 'disabled' : ''}>−</button><output aria-label="数量">${q}</output><button data-action="quantity" data-scope="${scope}" data-index="${index}" data-value="1" aria-label="增加数量" ${q >= max || item?.unavailable ? 'disabled' : ''}>＋</button></div>`;
}
function empty(title, desc, action, label, i = 'cart') {
  return `<div class="empty-state">${icon(i)}<h2>${title}</h2><p>${desc}</p><button class="primary" data-action="${action}">${label}</button></div>`;
}
function cartBrand(item) {
  const product = actualProduct(item.id);
  return product?.brand || item.brand || product?.category || item.category || 'LUMO 精选';
}
function cartPage() {
  const read = commerceReadState('cart');
  const heading = `<div class="cart-page-heading"><div><span class="cart-kicker">SHOPPING BAG</span><h1>购物车 ${read.loaded || state.cart.length ? `<span>(${cartCount()})</span>` : ''}</h1></div><button class="cart-destination" data-action="choose-address">${icon('pin')}<span>配送至 ${esc(state.address?.region || '选择收货地址')}</span>${icon('chevron')}</button></div>`;
  if (!state.cart.length && (!read.loaded || read.error)) {
    return `${header()}<main id="main" class="container cart-page">${heading}${read.error ? empty('购物车加载失败', esc(read.error), 'commerce-refresh', '重新加载') : commerceSkeleton('rows', { count: 3, label: '正在加载购物车' })}</main>${footer()}`;
  }
  const checked = state.cart.filter((item) => item.selected);
  const totalItems = window.LumoPromotions?.augmentItems(checked) || checked;
  const total = subtotal(totalItems);
  const count = totalItems.reduce((sum, item) => sum + item.qty, 0);
  const all = state.cart.length > 0 && checked.length === state.cart.length;
  const brands = new Map();
  state.cart.forEach((item, index) => {
    const brand = cartBrand(item);
    if (!brands.has(brand)) brands.set(brand, []);
    brands.get(brand).push({ item, index });
  });
  const groups = [...brands]
    .map(
      ([brand, rows]) => `
    <section class="cart-brand-group" aria-label="${esc(brand)}商品">
      <div class="cart-brand-heading"><label class="check"><input type="checkbox" data-action="cart-select-brand" data-brand="${esc(brand)}" ${rows.every(({ item }) => item.selected) ? 'checked' : ''} aria-label="选择${esc(brand)}全部商品"><span class="cart-brand-mark">品牌</span><strong>${esc(brand)}</strong></label><span>${rows.length} 款商品</span></div>
      ${rows
        .map(({ item, index }) => {
          const product = byId(item.id);
          const name = item.name || product.name;
          return `<article class="cart-item cart-table-row">
          <label class="check cart-row-check"><input type="checkbox" data-action="select" data-index="${index}" ${item.selected ? 'checked' : ''} ${item.unavailable ? 'disabled' : ''} aria-label="选择${esc(name)}"></label>
          <a class="cart-product" href="#/product/${esc(product.id)}"><div class="cart-product-image ${['iphone', 'macbook'].includes(product.id) ? 'official-image' : ''}"><img src="${esc(asset(imageFor(item)))}" alt="${esc(name)}" loading="lazy"></div><div><h3>${esc(name)}</h3><p class="cart-product-category">${esc(product.category || item.category || '')}</p>${window.LumoPromoView?.itemBadge(item) || ''}</div></a>
          <div class="cart-variant"><span>${esc(item.color || '默认颜色')}</span><span>${esc(item.option || product.defaultOption || '标准规格')}</span><small>商品规格</small></div>
          <div class="cart-unit-price">${item.unavailable ? '<strong>商品已不可用</strong><small>请移除后重新选购</small>' : `${price(itemPrice(item))}<small>小计 ¥${money(itemPrice(item) * item.qty)}</small>`}</div>
          ${quantity(item.qty, 'cart', index)}
          <div class="cart-row-actions"><button data-action="cart-move-favorite" data-index="${index}">移入收藏</button><button data-action="delete" data-index="${index}" aria-label="删除${esc(name)}">删除</button></div>
        </article>`;
        })
        .join('')}
    </section>`,
    )
    .join('');
  const available = products.filter(
    (product) => product.visible !== false && !state.cart.some((item) => item.id === product.id),
  );
  const recommendations = (window.LumoRecommendations?.orderHome?.(available) || available).slice(0, 12);
  const selectedPreview = checked
    .slice(0, 4)
    .map((item) => `<img src="${esc(asset(imageFor(item)))}" alt="${esc(item.name || byId(item.id).name)}">`)
    .join('');
  return `${header()}<main id="main" class="container cart-page">${heading}
    <div class="cart-benefits-strip"><span>${icon('ticket')}好券搭配好物，结算时核验可用优惠</span><a href="#/coupons">去领券 ${icon('chevron')}</a></div>
    ${
      state.cart.length
        ? `<div class="cart-toolbar"><div><label class="check"><input type="checkbox" data-action="select-all" ${all ? 'checked' : ''} aria-label="全选商品">全选</label><button data-action="delete-selected" ${checked.length ? '' : 'disabled'}>删除</button><button data-action="cart-move-selected" ${checked.length ? '' : 'disabled'}>移入收藏</button><button data-action="cart-clear">清空购物车</button></div><nav aria-label="购物记录"><a href="#/member/history">${icon('clock')}刚看过</a><a href="#/member/favorites">${icon('heart')}我的收藏</a><a href="#/orders">${icon('bag')}我的订单</a></nav></div>
    <div class="cart-table-heading" aria-hidden="true"><span></span><span>商品信息</span><span>商品规格</span><span>单价 / 小计</span><span>数量</span><span>操作</span></div><div class="cart-items">${groups}</div>
    ${window.LumoPromoView?.campaigns(checked, 'cart') || ''}${window.LumoPromoView?.exchange(checked, 'cart') || ''}`
        : empty('购物车还是空的', '把喜欢的好物带回家吧。', 'home', '去逛逛')
    }
    <section class="cart-recs"><div class="section-head"><div><h2>猜你喜欢</h2><p>为你的购物清单，再添一点灵感</p></div><a class="text-button" href="#/">浏览全部好物 ${icon('chevron')}</a></div><div class="product-grid">${recommendations.map(productCard).join('')}</div></section>
    ${state.cart.length ? `<div class="cart-bottom-wrap"><div class="cart-bottom"><label class="check"><input type="checkbox" data-action="select-all" ${all ? 'checked' : ''} aria-label="全选商品">全选</label><div class="cart-selection-preview"><span>已选 ${count} 件</span>${selectedPreview}</div><div class="cart-summary"><div><span class="cart-summary-label">商品合计</span>${price(total)}<small>优惠和运费将在结算时由服务端确认${totalItems.some((item) => item.promotion?.type === CommerceEnums.PromotionType.Exchange) ? ' · 含换购商品' : ''}</small></div></div><button class="primary" data-action="checkout" ${count ? '' : 'disabled'}>去结算 <span>(${count})</span></button></div></div>` : ''}
  </main>${footer()}`;
}
function steps(active) {
  return `<div class="checkout-steps">${['确认订单', '支付订单', '完成'].map((s, i) => `${i ? '<span class="step-line"></span>' : ''}<div class="step ${i === active ? 'active' : i < active ? 'done' : ''}"><i>${i < active ? '✓' : i + 1}</i>${s}</div>`).join('')}</div>`;
}
function orderItems(items) {
  return `<div class="order-items">${items
    .map((item) => {
      let p = { ...byId(item.id), name: item.name || byId(item.id).name };
      return `<div class="order-item"><span class="order-product-image"><img src="${esc(asset(imageFor(item)))}" alt="${esc(p.name)}"></span><div class="order-item-info"><h3>${esc(p.name)}</h3>${window.LumoPromoView?.itemBadge(item) || ''}<p>${esc(itemText(item))}</p><div class="item-price">¥${money(itemPrice(item))}</div></div><span class="item-count">× ${item.qty}</span></div>`;
    })
    .join('')}</div>`;
}
function amountLines(t) {
  return `<div class="amounts"><div class="amount-line"><span>${t.activitySavings ? '商品原价' : '商品金额'}</span><span>¥${money(t.original ?? t.sub)}</span></div>${t.activitySavings ? `<div class="amount-line discount"><span>活动优惠</span><span>− ¥${money(t.activitySavings)}</span></div>` : ''}<div class="amount-line"><span>运费</span><span>${t.shipping ? '¥' + t.shipping : '免运费'}</span></div><div class="amount-line discount"><span>优惠券</span><span>− ¥${t.coupon}</span></div><div class="amount-line discount"><span>积分抵扣</span><span>− ¥${t.points}</span></div><div class="amount-line total"><span>实付款</span>${price(t.total)}</div></div>`;
}
function checkoutPage() {
  const items = selectedItems();
  if (!items.length && !readSubmitAttempt() && !commerceReadState('cart').loaded && !commerceReadState('cart').error)
    return `${header()}<main id="main" class="container">${steps(0)}${commerceSkeleton('detail', { label: '正在加载结算商品' })}</main>${footer()}`;
  if (!items.length && commerceReadState('cart').error)
    return `${header()}<main id="main" class="container">${steps(0)}${empty('结算商品加载失败', esc(commerceReadState('cart').error), 'commerce-refresh', '重新加载')}</main>${footer()}`;
  if (!items.length && readSubmitAttempt())
    return `${header()}<main id="main" class="container">${steps(0)}${commerceAttemptNotice()}</main>${footer()}`;
  if (!items.length)
    return `${header()}<main id="main" class="container">${steps(0)}${empty('请选择要结算的商品', '回到购物车，勾选喜欢的商品。', 'cart', '返回购物车')}</main>${footer()}`;
  const t = totals(items),
    a = state.address,
    points = window.LumoMember?.pointBalance() || 0,
    addressRead = window.LumoMember?.readState?.('addresses'),
    pointsRead = window.LumoMember?.readState?.('points');
  const addressPending = addressRead && !addressRead.loaded && !addressRead.error,
    pointsPending = pointsRead && !pointsRead.loaded && !pointsRead.error;
  return `${header()}<main id="main" class="container">${steps(0)}<div class="checkout-heading"><h1>确认订单</h1><a href="#/cart">${icon('back')} 返回购物车</a></div>${commerceAttemptNotice()}<div class="checkout-layout"><section class="panel checkout-panel"><div class="checkout-section"><h2>收货地址</h2>${addressPending ? commerceSkeleton('form', { label: '正在加载收货地址' }) : addressRead?.error && !a ? `<p class="api-config-error">${esc(addressRead.error)} <button class="text-button" data-action="commerce-member-retry">重试</button></p>` : a ? `<div class="address-card">${icon('pin')}<div><strong>${esc(a.name)}<span>${esc(String(a.phone || '').slice(0, 3) + '****' + String(a.phone || '').slice(-4))}</span></strong><p>${esc([a.region, a.detail].filter(Boolean).join(' '))}</p></div><span class="default-tag">${a.isDefault ? '默认' : '本次收货'}</span><button class="address-edit" data-action="address">编辑</button></div>` : ''}${!addressPending && !addressRead?.error ? `<button class="add-address" data-action="choose-address">${icon('plus')}${a ? '使用其他地址' : '添加收货地址'}</button>` : ''}</div><div class="checkout-section"><h2>配送方式</h2><div class="shipping-options"><button class="shipping-option active" type="button"><span class="radio-dot"></span>${esc(commerceState.shippingQuote?.provider || '标准配送')}<small>${commerceState.shippingQuote ? (commerceState.shippingQuote.serviceable ? '配送费用 · ' + (Number(commerceState.shippingQuote.shipping_cents) ? '¥' + money(Number(commerceState.shippingQuote.shipping_cents) / 100) : '免运费') : '当前地址暂不可配送') : '正在查询配送范围和运费'}</small></button></div></div><div class="checkout-section"><h2>支付优惠</h2><button class="benefit-row" data-action="coupons">${icon('ticket')}优惠券<span>${t.coupon ? '− ¥' + money(t.coupon) : '选择优惠券'}</span>${icon('chevron', 'arrow-icon')}</button>${pointsPending ? commerceSkeleton('rows', { count: 1, label: '正在加载可用积分' }) : pointsRead?.error && !pointsRead.loaded ? `<p class="api-config-error">${esc(pointsRead.error)} <button class="text-button" data-action="commerce-member-retry">重试</button></p>` : `<button class="benefit-row" data-action="points" role="switch" aria-checked="${!!state.points}" ${points <= 0 ? 'disabled' : ''}>${icon('coin', 'points')}积分（共 ${money(points)}）<span class="muted">100 积分抵 ¥1，最多抵商品金额的 ${Number(window.LumoMember?.model()?.pointsPolicy?.deduction_bps ?? 1000) / 100}%</span><div class="toggle ${state.points ? 'on' : ''}"><i></i></div></button>`}</div>${window.LumoPromoView?.checkout(items) || ''}${window.LumoPromoView?.campaigns(items, 'checkout') || ''}${window.LumoPromoView?.exchange(window.LumoPromotions?.baseItems() || items, 'checkout') || ''}${window.LumoServices?.checkoutFields() || ''}<div class="checkout-section"><h2>订单备注</h2><textarea class="note-input" id="order-note" maxlength="150" rows="2" aria-label="订单备注" placeholder="选填：配送时间、特殊说明等">${esc(state.note || '')}</textarea></div></section><aside class="panel order-summary checkout-summary"><div class="checkout-summary-heading"><h2>订单商品 <span>(${items.reduce((n, x) => n + x.qty, 0)})</span></h2><small>${items.length} 款好物</small></div><div class="checkout-product-list" tabindex="0" role="region" aria-label="订单商品列表，共 ${items.length} 款">${orderItems(items)}</div>${items.length > 3 ? `<p class="checkout-scroll-hint">${icon('chevron')}上下滚动查看全部 ${items.length} 款商品</p>` : ''}${!commerceDisplayQuote(items) && !commerceState.quoteError ? commerceSkeleton('summary', { label: '正在获取订单价格与优惠' }) : commerceDisplayQuote(items) ? amountLines(t) : ''}<p class="secure-note" role="status">${esc(commerceState.quoteError || (commerceQuoteFor(items) ? '价格及优惠已确认' : commerceDisplayQuote(items) ? '正在更新价格，暂显示上次报价…' : ''))}</p><button class="primary" data-action="submit-order" ${commerceState.submitting || !commerceQuoteFor(items) ? 'disabled' : ''}>${commerceState.submitting ? '正在提交…' : readSubmitAttempt() ? '确认并重试提交' : '提交订单'}</button><p class="secure-note">${icon('shield')}订单和收货地址由服务端保存</p></aside></div></main>${footer()}`;
}
function paymentPage() {
  const order = state.pending;
  if (!order && !commerceReadState('orders').loaded && !commerceReadState('orders').error)
    return `${header()}<main id="main" class="container">${steps(1)}${commerceSkeleton('summary', { label: '正在加载待支付订单' })}</main>${footer()}`;
  if (!order && commerceReadState('orders').error)
    return `${header()}<main id="main" class="container">${steps(1)}${empty('订单加载失败', esc(commerceReadState('orders').error), 'commerce-refresh', '重新加载', 'bag')}</main>${footer()}`;
  if (!order)
    return `${header()}<main id="main" class="container">${steps(1)}${empty('暂无待支付的订单', '可到我的订单查看最新状态。', 'orders', '查看订单', 'bag')}</main>${footer()}`;
  const expired = order.expiresAt && Date.now() >= order.expiresAt,
    p = commerceState.payment?.order_id === order.id ? commerceState.payment : null;
  const refreshOnly =
    expired ||
    (p && !(p.provider === CommerceEnums.PaymentProvider.Demo && p.status === CommerceEnums.PaymentStatus.Pending));
  const note =
    p?.status === CommerceEnums.PaymentStatus.Succeeded
      ? '模拟支付已成功，正在确认订单，请稍候。'
      : p
        ? '支付请求已创建，可提交模拟支付结果。'
        : '当前使用模拟支付渠道，不会产生真实扣款。';
  return `${header()}<main id="main" class="container">${steps(1)}<div class="payment-wrap panel payment-panel"><h1>订单支付</h1><p class="payment-intro">${expired ? '支付期限已到，请刷新确认服务端订单状态' : `请在 <strong id="countdown">--:--</strong> 内完成支付`}</p><div class="payment-amount"><span>待付金额</span><div>${price(order.totals.total)}</div><p>订单编号：${esc(order.id)}</p></div><div class="payment-methods">${commerceState.paymentMethodsReady ? (commerceState.paymentMethods.length ? commerceState.paymentMethods.map((method) => `<button type="button" class="payment-method ${method.id === selectedMethod ? 'active' : ''}" data-action="payment-method" data-value="${esc(method.id)}" aria-pressed="${method.id === selectedMethod}"><span class="method-icon" style="background:var(--red)">${esc(method.name.slice(0, 1))}</span>${esc(method.name)}</button>`).join('') : '<p class="demo-note">商城暂未开放支付方式，请稍后再试或联系客服。</p>') : '<p class="demo-note">正在读取可用支付方式…</p>'}</div>${commerceState.paymentMethodsError ? `<p class="api-config-error">${esc(commerceState.paymentMethodsError)}</p>` : ''}${p ? `<p class="demo-note">支付单 ${esc(p.id)} · ${esc(p.status)}</p>` : ''}<p class="demo-note">${note}</p><div class="payment-actions"><button type="button" class="primary" data-action="${refreshOnly ? 'commerce-payment-refresh' : 'pay'}" ${commerceState.paying || (!refreshOnly && (!commerceState.paymentMethodsReady || !commerceState.paymentMethods.length)) ? 'disabled' : ''}>${commerceState.paying ? '正在确认支付…' : refreshOnly ? '刷新支付结果' : p ? '模拟支付成功' : '模拟支付'}</button><button type="button" class="outline" data-action="commerce-cancel-order" data-id="${esc(order.id)}" ${commerceState.paying ? 'disabled' : ''}>取消订单</button></div><p class="secure-note" role="status">${esc(commerceState.paymentError || '支付结果由服务端确认后更新')}</p></div></main>${footer()}`;
}
function successPage() {
  if (!commerceOrders.length && !commerceReadState('orders').loaded && !commerceReadState('orders').error)
    return `${header()}<main id="main" class="container">${steps(2)}${commerceSkeleton('summary', { label: '正在确认订单状态' })}</main>${footer()}`;
  if (!commerceOrders.length && commerceReadState('orders').error)
    return `${header()}<main id="main" class="container">${steps(2)}${empty('订单状态加载失败', esc(commerceReadState('orders').error), 'commerce-refresh', '重新加载', 'bag')}</main>${footer()}`;
  const order =
    commerceOrders.find((o) => o.id === commerceState.successOrderId && o.paidAt) ||
    commerceOrders.find((o) => o.paidAt);
  if (!order)
    return `${header()}<main id="main" class="container">${steps(2)}${empty('暂未查询到已支付订单', '支付确认后，订单状态会在这里更新。', 'orders', '查看订单', 'bag')}</main>${footer()}`;
  const label = window.LumoMember?.orderLabel(order) || order.backendStatus;
  return `${header()}<main id="main" class="container"><div class="success-wrap panel"><section class="success-top"><div class="success-icon">${icon('check')}</div><h1>${order.backendStatus === CommerceEnums.OrderStatus.Refunded ? '退款已完成' : '支付已确认'}</h1><p>${esc(label)}</p>${price(order.totals.total)}<div class="order-number">订单编号：${esc(order.id)}</div><div class="success-actions"><button class="primary" data-action="order-details" data-id="${esc(order.id)}">查看订单</button><button class="outline" data-action="home">继续购物</button></div></section>${window.LumoPromoView?.success(order) || ''}<section class="timeline"><h2>订单状态</h2><div class="timeline-event current"><strong>${esc(label)}</strong><p>${esc(new Date(order.updatedAt || order.createdAt).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }))}</p></div><p class="secure-note">库存、优惠券、积分与配送状态由各服务确认后更新</p></section></div>${window.LumoRecommendations?.render(order) || ''}</main>${footer()}`;
}
function ordersPage() {
  const read = commerceReadState('orders');
  if (!commerceOrders.length && (!read.loaded || read.error))
    return `${header()}<main id="main" class="container"><div class="page-title"><h1>我的订单</h1></div>${read.error ? empty('订单加载失败', esc(read.error), 'commerce-refresh', '重新加载') : commerceSkeleton('rows', { count: 3, label: '正在加载订单' })}</main>${footer()}`;
  return `${header()}<main id="main" class="container"><div class="page-title"><h1>我的订单 <span>(${state.orders.length})</span></h1><a class="text-button" href="#/">继续购物 ${icon('chevron')}</a></div><div class="orders-list">${state.pending ? `<div class="cart-banner"><span>${icon('clock')}你有一笔待支付的订单</span><button data-action="resume-payment">继续支付</button></div>` : ''}${state.orders.length ? state.orders.map((o) => `<article class="order-card"><div class="order-card-header"><span>${esc(o.id)} · ${new Date(o.paidAt).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' })}</span><strong>${esc(window.LumoPromotions?.orderStatus(o) || '支付已确认')}</strong></div><div class="order-card-body">${orderItems(o.items)}<div class="order-card-total">实付款 ${price(o.totals.total)}<button class="outline" data-action="order-details" data-id="${o.id}">订单详情</button></div></div></article>`).join('') : empty('还没有订单', '你的美好生活，从第一件好物开始。', 'home', '发现好物', 'bag')}</div></main>${footer()}`;
}
function render() {
  clearInterval(countdownTimer);
  let accent = shop().accent;
  if (!/^#[0-9a-f]{6}$/i.test(accent)) accent = '#ff443d';
  document.documentElement.style.setProperty('--red', accent);
  document.documentElement.style.removeProperty?.('--red-dark');
  document.querySelector('meta[name=theme-color]')?.setAttribute('content', accent);
  let route = (location.hash || '#/').slice(2).split('/');
  const name = route[0] || '';
  if (window.LumoAuth && !window.LumoAuth.gate(route)) {
    $('#app').innerHTML =
      window.LumoAuthView?.page() ||
      '<main id="main" class="container auth-loading" role="status">正在打开登录页面…</main>';
    document.title = '登录 · ' + shop().brand + ' ' + shop().brandSuffix;
    window.LumoAuth?.afterRender();
    window.LumoSidebars?.afterRender();
    return;
  }
  if (name === 'login' && !window.LumoAuthView) {
    $('#app').innerHTML = '<main id="main" class="container auth-loading" role="status">正在打开登录页面…</main>';
    return;
  }
  let content =
    name === 'compare' && window.LumoDiscovery
      ? window.LumoDiscovery.page()
      : name === 'login' && window.LumoAuthView
        ? window.LumoAuthView.page()
        : name === 'member' && window.LumoMemberView
          ? window.LumoMemberView.page(route[1] || 'overview', route[2] || 'all')
          : name === 'orders' && window.LumoMemberView
            ? window.LumoMemberView.page('orders', 'all')
            : name === 'editor' && window.LumoStudio
              ? window.LumoStudio.renderEditor()
              : name === 'coupons' && window.LumoCouponCenter
                ? window.LumoCouponCenter.page()
                : name === 'promotions' && window.LumoPromoView
                  ? window.LumoPromoView.page(
                      route[1] || CommerceEnums.PromotionType.Flash,
                      route[2] || CommerceEnums.PromotionSession.Today,
                    )
                  : name === 'product'
                    ? detailPage(route[1])
                    : name === 'cart'
                      ? cartPage()
                      : name === 'checkout'
                        ? checkoutPage()
                        : name === 'payment'
                          ? paymentPage()
                          : name === 'success'
                            ? successPage()
                            : name === 'orders'
                              ? ordersPage()
                              : catalog();
  $('#app').innerHTML =
    content +
    (window.LumoDiscovery?.compareBar() || '') +
    (window.LumoSidebars ? window.LumoSidebars.render() : window.LumoServices?.supportLauncher() || '');
  document.title =
    ({
      cart: '购物车',
      checkout: '确认订单',
      payment: '订单支付',
      success: '支付已确认',
      orders: '我的订单',
      member: '个人中心',
      login: '登录',
      compare: '商品对比',
      promotions: '好物福利社',
      coupons: '领券中心',
      editor: '编辑商城',
      product: byId(route[1]).name,
    }[name] || '品质生活商城') +
    ' · ' +
    shop().brand +
    ' ' +
    shop().brandSuffix;
  if (name === 'payment' && state.pending) startCountdown();
  window.LumoPromotions?.afterRender();
  window.LumoMember?.afterRender();
  window.LumoAuth?.afterRender();
  window.LumoDiscovery?.afterRender();
  window.LumoCategories?.afterRender();
  window.LumoProductExtras?.afterRender();
  window.LumoProductAlerts?.afterRender();
  window.LumoServices?.afterRender();
  window.LumoCouponCenter?.afterRender();
  window.LumoSidebars?.afterRender();
  positionPrimaryNavigation();
  commerceAfterRender(name);
}
function renderKeepScroll() {
  let y = window.scrollY;
  render();
  window.scrollTo(0, y);
}
async function addToCart(item) {
  if (!window.LumoAuth?.requireLogin(location.hash)) return;
  const sku = skuForItem(item);
  if (commerceAdding.has(sku)) return false;
  const err = window.LumoInventory?.validateAdd(item);
  if (err) {
    toast(err);
    return;
  }
  commerceAdding.add(sku);
  try {
    await window.LumoAPI.call(CommerceEnums.Service.Cart, CommerceEnums.Action.Add, {
      data: { sku_id: skuForItem(item), quantity: Number(item.qty) || 1 },
    });
    await refreshCart();
    renderKeepScroll();
    toast('已加入购物车');
    return true;
  } catch (error) {
    commerceError(error);
    return false;
  } finally {
    commerceAdding.delete(sku);
  }
}
let modalReturnFocus = null;
let modalFocusTimer = null;
function modal(title, body) {
  if (!$('#modal-root').innerHTML) modalReturnFocus = document.activeElement;
  clearTimeout(modalFocusTimer);
  $('#modal-root').innerHTML =
    `<div class="modal-backdrop" data-backdrop="true"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title"><div class="modal-header"><h2 id="modal-title">${title}</h2><button class="icon-button" data-action="close-modal" aria-label="关闭">${icon('close')}</button></div>${body}</section></div>`;
  document.body.style.overflow = 'hidden';
  modalFocusTimer = setTimeout(() => $('.modal input, .modal button')?.focus(), 30);
}
function closeModal() {
  clearTimeout(modalFocusTimer);
  $('#modal-root').innerHTML = '';
  document.body.style.overflow = '';
  const trigger = modalReturnFocus;
  modalReturnFocus = null;
  if (trigger?.isConnected) trigger.focus({ preventScroll: true });
}
function showCoupons() {
  window.LumoMember?.showCoupons();
}
function addressModal() {
  if (window.LumoMember) {
    window.LumoMember.openAddress(window.LumoMember.selectedAddress()?.id || null, { checkout: true });
    return;
  }
  let a = state.address || {};
  modal(
    '收货地址',
    `<p class="modal-desc">用于体验下单流程，请填写示例信息。</p><form id="address-form"><div class="form-row"><label class="field">收货人<input name="name" required maxlength="20" placeholder="请输入收货人姓名" value="${esc(a.name || '')}"></label><label class="field">手机号码<input name="phone" type="tel" required pattern="1[3-9][0-9]{9}" maxlength="11" placeholder="11 位手机号码" value="${esc(a.phone || '')}"></label></div><label class="field">省市区<input name="region" required maxlength="60" placeholder="如：广东省 深圳市 南山区" value="${esc(a.region || '')}"></label><label class="field">详细地址<textarea name="detail" required maxlength="100" rows="2" placeholder="街道、门牌号等">${esc(a.detail || '')}</textarea></label><button class="primary">保存并使用</button></form>`,
  );
}
function startCountdown() {
  function tick() {
    const el = $('#countdown');
    if (!el || !state.pending) return;
    const sec = Math.max(0, Math.floor(((state.pending.expiresAt || Date.now()) - Date.now()) / 1000));
    el.textContent = String(Math.floor(sec / 60)).padStart(2, '0') + ':' + String(sec % 60).padStart(2, '0');
    if (sec === 0) clearInterval(countdownTimer);
  }
  tick();
  countdownTimer = setInterval(tick, 1000);
}
function showOrder(id) {
  let o = commerceOrders.find((x) => x.id === id) || state.orders.find((x) => x.id === id);
  if (!o) return;
  modal(
    '订单详情',
    `<p class="modal-desc">订单编号 ${esc(o.id)} · ${esc(window.LumoMember?.orderLabel(o) || window.LumoPromotions?.orderStatus(o) || '支付已确认')}</p>${orderItems(o.items)}${amountLines(o.totals)}${window.LumoPromoView?.order(o) || ''}<p class="modal-desc" style="margin:18px 0 0">收货人：${esc(o.address.name)} · ${esc(o.address.phone.slice(0, 3) + '****' + o.address.phone.slice(-4))}<br>收货地址：${esc(o.address.region + ' ' + o.address.detail)}<br>配送方式：${esc(o.shippingLabel || '标准配送')}<br>支付方式：${esc(o.methodLabel)}${o.note ? '<br>订单备注：' + esc(o.note) : ''}<br>订单以服务端记录为准；当前支付及物流采用演示适配器。</p>`,
  );
}
document.addEventListener('click', (e) => {
  if (e.target.matches('[data-backdrop]')) {
    closeModal();
    return;
  }
  let b = e.target.closest('[data-action]');
  if (!b || b.disabled) return;
  let a = b.dataset.action,
    v = b.dataset.value,
    i = Number(b.dataset.index),
    id = b.dataset.id;
  switch (a) {
    case 'commerce-search-retry':
      void window.LumoDiscovery?.refreshSearch?.()
        .then(() => renderKeepScroll())
        .catch(() => renderKeepScroll());
      renderKeepScroll();
      break;
    case 'commerce-member-retry':
      void window.LumoMember?.refresh?.()
        .then(() => renderKeepScroll())
        .catch(() => renderKeepScroll());
      renderKeepScroll();
      break;
    case 'commerce-product-retry': {
      const id = (location.hash || '').slice(2).split('/')[1];
      commerceProductReads.delete(commerceOwner() + '|' + id);
      if (commerceState.catalogError)
        void refreshCatalog()
          .then(() => renderKeepScroll())
          .catch(() => renderKeepScroll());
      else void ensureCommerceProduct(id);
      renderKeepScroll();
      break;
    }
    case 'commerce-refresh':
      void refreshCommerce()
        .then(() => renderKeepScroll())
        .catch(commerceError);
      break;
    case 'commerce-resume-submit':
      void resumeCommerceAttempt();
      break;
    case 'commerce-payment-refresh':
      void refreshCommercePayment().catch(() => {});
      break;
    case 'commerce-cancel-order':
      void cancelCommerceOrder(id);
      break;
    case 'home':
      ui = { ...ui, category: '全部好物', categoryLabel: '', query: '', onlyFavorites: false, min: '', max: '' };
      go('');
      break;
    case 'cart':
      go('cart');
      break;
    case 'category':
      if (window.LumoDiscovery?.selectCategory) {
        window.LumoDiscovery.selectCategory(v);
        break;
      }
      ui.category = v;
      ui.categoryLabel = '';
      ui.onlyFavorites = false;
      ui.query = '';
      go('');
      break;
    case 'explore':
      $('#catalog')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
      break;
    case 'sort':
      ui.sort = v === '价格' ? (ui.sort === '价格低' ? '价格高' : '价格低') : v;
      renderKeepScroll();
      break;
    case 'filters':
      ui.filters = !ui.filters;
      renderKeepScroll();
      break;
    case 'reset-price':
      ui.min = '';
      ui.max = '';
      renderKeepScroll();
      break;
    case 'reset-filters':
      window.LumoDiscovery?.resetFilters();
      ui = { ...ui, category: '全部好物', categoryLabel: '', query: '', onlyFavorites: false, min: '', max: '' };
      render();
      window.scrollTo(0, 0);
      break;
    case 'favorite':
      void toggleFavorite(id);
      break;
    case 'favorites':
      go('member/favorites');
      break;
    case 'quick-add': {
      let p = byId(id);
      addToCart({ id: p.id, color: p.colors[0], option: p.defaultOption, qty: 1 });
      break;
    }
    case 'color': {
      const product = byId(detail.id),
        variants = product.variants || [];
      if (!product.colors.includes(v)) break;
      detail.color = v;
      if (variants.length && !variants.some((variant) => variant.color === v && variant.option === detail.option))
        detail.option = variants.find((variant) => variant.color === v)?.option || detail.option;
      detail.thumb = 0;
      renderKeepScroll();
      break;
    }
    case 'option':
      if (
        byId(detail.id).variants?.length &&
        !byId(detail.id).variants.some((variant) => variant.color === detail.color && variant.option === v)
      )
        break;
      detail.option = v;
      renderKeepScroll();
      break;
    case 'thumb':
      detail.thumb = Number(v);
      renderKeepScroll();
      break;
    case 'detail-tab':
      ui.detailTab = Number(v);
      renderKeepScroll();
      break;
    case 'quantity': {
      const item = b.dataset.scope === 'detail' ? detail : state.cart[i];
      if (!item) break;
      const wanted = Math.max(1, Math.min(10, item.qty + Number(v)));
      if (b.dataset.scope === 'detail') {
        detail.qty = wanted;
        renderKeepScroll();
      } else void setCartQuantity(item, wanted);
      break;
    }
    case 'detail-add':
      addToCart(detail);
      break;
    case 'buy-now':
      {
        const err = window.LumoInventory?.validate([detail]);
        if (err) {
          toast(err);
          break;
        }
      }
      window.LumoPromotions?.clearExchange();
      window.LumoPromotions?.clearCampaign?.();
      setCheckout(CommerceEnums.OrderSource.Direct, {
        id: detail.id,
        color: detail.color,
        option: detail.option,
        qty: detail.qty,
        selected: true,
      });
      go('checkout');
      break;
    case 'select':
      state.cart[i].selected = !state.cart[i].unavailable && b.checked;
      save();
      renderKeepScroll();
      break;
    case 'select-all':
      state.cart.forEach((x) => (x.selected = !x.unavailable && b.checked));
      save();
      renderKeepScroll();
      break;
    case 'cart-select-brand':
      state.cart
        .filter((item) => cartBrand(item) === b.dataset.brand)
        .forEach((item) => (item.selected = !item.unavailable && b.checked));
      save();
      renderKeepScroll();
      break;
    case 'cart-move-favorite':
      void moveCartToFavorites([state.cart[i]]);
      break;
    case 'cart-move-selected':
      void moveCartToFavorites(state.cart.filter((item) => item.selected));
      break;
    case 'cart-clear':
      if (state.cart.length)
        modal(
          '清空购物车？',
          `<p class="modal-desc">将移除购物车中的全部 ${state.cart.length} 款商品。</p><div class="modal-actions"><button class="outline" data-action="close-modal">再想想</button><button class="primary" data-action="cart-confirm-clear" data-owner="${esc(commerceOwner())}">确认清空</button></div>`,
        );
      break;
    case 'cart-confirm-clear':
      if (b.dataset.owner === commerceOwner()) void removeCartItems([...state.cart]);
      else closeModal();
      break;
    case 'delete':
      modal(
        '移除这件好物？',
        `<p class="modal-desc">${esc(byId(state.cart[i].id).name)} 将从购物车移除。</p><div class="modal-actions"><button class="outline" data-action="close-modal">再想想</button><button class="primary" data-action="confirm-delete" data-index="${i}">确认移除</button></div>`,
      );
      break;
    case 'confirm-delete':
      void removeCartItems([state.cart[i]]);
      break;
    case 'delete-selected':
      if (!state.cart.some((x) => x.selected)) {
        toast('请先选择要删除的商品');
        break;
      }
      modal(
        '移除所选商品？',
        '<p class="modal-desc">所选商品将从购物车移除，你可以随时重新添加。</p><div class="modal-actions"><button class="outline" data-action="close-modal">再想想</button><button class="primary" data-action="confirm-delete-selected">确认移除</button></div>',
      );
      break;
    case 'confirm-delete-selected':
      void removeCartItems(state.cart.filter((x) => x.selected));
      break;
    case 'manage':
      ui.manage = !ui.manage;
      renderKeepScroll();
      if (ui.manage) toast('勾选商品后可批量删除');
      break;
    case 'checkout':
      setCheckout(CommerceEnums.OrderSource.Cart);
      go('checkout');
      break;
    case 'coupons':
      showCoupons();
      break;
    case 'coupon':
      if (window.LumoMember) {
        window.LumoMember.selectCoupon(v);
        break;
      }
      state.coupon = Number(v);
      save();
      closeModal();
      renderKeepScroll();
      toast(Number(v) ? '已选择 ¥' + v + ' 优惠券' : '本单不使用优惠券');
      break;
    case 'address':
      addressModal();
      break;
    case 'choose-address':
      if (window.LumoMember) window.LumoMember.chooseAddress();
      else addressModal();
      break;
    case 'points':
      state.points = !state.points;
      invalidateCommerceQuote();
      renderKeepScroll();
      break;
    case 'shipping':
      state.shipping = 0;
      invalidateCommerceQuote();
      renderKeepScroll();
      break;
    case 'submit-order':
      void submitCommerceOrder();
      break;
    case 'payment-method':
      selectedMethod = v;
      renderKeepScroll();
      break;
    case 'pay':
      void submitCommerceDemoPayment();
      break;
    case 'expired-order':
      void refreshCommercePayment();
      break;
    case 'resume-payment':
      if (id) state.pending = commerceOrders.find((o) => o.id === id) || state.pending;
      commerceState.payment = null;
      go('payment');
      break;
    case 'orders':
      go('member/orders');
      break;
    case 'order-details':
      showOrder(id);
      break;
    case 'close-modal':
      closeModal();
      break;
    case 'help':
      modal(
        '购物帮助',
        '<div class="help-list"><details open><summary>如何下单？</summary><p>登录后选择商品规格、地址和优惠，系统获取服务端报价后提交订单。支付请求由渠道确认后更新订单。</p></details><details><summary>优惠如何计算？</summary><p>可用优惠券、积分及活动折扣由后端实时校验，最终以提交前报价为准。</p></details><details><summary>数据会保存吗？</summary><p>地址、购物车、订单与资产保存在后端。当前部署提供演示支付和物流适配器，暂不产生真实扣款和配送。</p></details></div>',
      );
      break;
    case 'about':
      modal(
        '关于 LUMO Mall',
        '<p class="modal-desc">LUMO Mall 以简约设计呈现日常好物，探索数码、家居与生活方式。商城已连接独立后端服务，支付和物流当前采用演示适配器。</p>',
      );
      break;
    case 'privacy':
      modal(
        '隐私说明',
        '<p class="modal-desc">地址、订单、购物车、收藏及服务记录会发送到后端并按账号保存。登录凭据用于服务端认证；退出会撤销会话。此应用不要求银行卡号或支付密码。</p>',
      );
      break;
  }
});
document.addEventListener('submit', (e) => {
  if (e.target.id === 'search-form') {
    e.preventDefault();
    const query = $('#search-input').value.trim();
    if (query && window.LumoDiscovery) {
      window.LumoDiscovery.commitSearch(query);
      return;
    }
    ui.query = query;
    ui.category = '全部好物';
    ui.categoryLabel = '';
    ui.onlyFavorites = false;
    go('');
  }
  if (e.target.id === 'price-form') {
    e.preventDefault();
    let fd = new FormData(e.target);
    let min = String(fd.get('min')),
      max = String(fd.get('max'));
    if (min && max && Number(min) > Number(max)) {
      toast('最高价格不能低于最低价格');
      return;
    }
    ui.min = min;
    ui.max = max;
    renderKeepScroll();
  }
  if (e.target.id === 'address-form') {
    e.preventDefault();
    let fd = new FormData(e.target),
      a = Object.fromEntries(fd);
    for (let k in a) a[k] = a[k].trim();
    if (!a.name || !a.region || !a.detail) {
      toast('请填写完整的地址信息');
      return;
    }
    toast('请使用地址管理保存服务端地址');
  }
});
document.addEventListener('input', (e) => {
  if (e.target.id === 'order-note') {
    state.note = e.target.value;
    save();
  }
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeModal();
  if (e.key === 'Tab' && $('.modal')) {
    let els = $$('button:not(:disabled),input,textarea,select,a[href]', $('.modal'));
    let first = els[0],
      last = els[els.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
});
window.addEventListener('hashchange', () => {
  closeModal();
  render();
  window.scrollTo(0, 0);
});
window.addEventListener('resize', positionPrimaryNavigation);

// Commerce state contains only backend responses and transient UI state.
const commerceState = {
  catalogLoaded: false,
  catalogError: '',
  quote: null,
  shippingQuote: null,
  quoteKey: '',
  quoteCartKey: '',
  quoteError: '',
  quoteErrorKey: '',
  quoteLoadingKey: '',
  submitting: false,
  paying: false,
  payment: null,
  paymentError: '',
  paymentMethods: [],
  paymentMethodsReady: false,
  paymentMethodsError: '',
  paymentMethodsAt: 0,
  successOrderId: '',
  flashRequestId: '',
  lastPoll: 0,
};
let commerceOrders = [],
  commerceQuoteSequence = 0,
  commercePollTimer,
  commerceCartQueue = Promise.resolve(),
  commerceDetailSKU = '',
  commerceEpoch = 0;
const commerceReads = new Map(),
  commerceProductReads = new Map(),
  commerceMetadataReads = new Map(),
  commerceInventoryReads = new Map(),
  commerceHydratedProducts = new Map();
const commerceReadState = (resource) => {
  const row = commerceReads.get(resource);
  return { loading: row?.loading === true, loaded: row?.loaded === true, error: row?.error || '' };
};
const commerceSkeleton = (type, options = {}) => window.LumoSkeleton?.render(type, options) || '';
const actualProduct = (id) =>
  products.find((p) => p.id === id || p.skuId === id || p.variants?.some((v) => v.sku_id === id));
const commerceOwner = () =>
  String(window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '') +
  '@' +
  String(window.LumoAPI?.getBase?.() || '') +
  ':' +
  String(window.LumoAPI?.getToken?.() || '') +
  '#' +
  commerceEpoch;
const commerceAdding = new Set(),
  commerceCartMutating = new Set(),
  commerceCartMoving = new Set();
const legacyProductIds = {
  'sku-phone': 'iphone',
  'sku-headphones': 'headphones',
  'sku-watch': 'watch',
  'sku-coffee': 'coffee',
};
const commerceCall = (service, action, cmd = {}, options = {}) => window.LumoAPI.call(service, action, cmd, options);
function normalizeProduct(raw) {
  const v = { ...(raw.attributes || raw.presentation || {}), ...raw },
    id = String(v.product_id || legacyProductIds[v.id] || v.id || ''),
    presentation = presentationProducts.find((p) => p.id === id) || fallbackProduct;
  const variants = Array.isArray(v.variants)
    ? v.variants.map((x) => ({ ...x, sku_id: String(x.sku_id || x.id), price_cents: Number(x.price_cents) }))
    : [];
  const options = variants.length
    ? [...new Set(variants.map((x) => x.option).filter(Boolean))]
    : [String(v.option || v.defaultOption || v.default_option || '标准版')];
  const colors = variants.length
    ? [...new Set(variants.map((x) => x.color).filter(Boolean))]
    : [String(v.color || '默认')];
  const priceCents = Number(v.price_cents) || 0;
  return {
    ...presentation,
    ...v,
    id,
    skuId: String(v.id || v.sku_id || id),
    price_cents: priceCents,
    price: priceCents / 100,
    old: v.old != null ? Number(v.old) : Number(v.old_price_cents || v.original_price_cents || priceCents) / 100,
    visible: v.active !== false && v.status !== CommerceEnums.CatalogStatus.Disabled,
    sub: String(v.sub || v.description || ''),
    colors: colors.length ? colors : ['默认'],
    options: options.length ? options : ['标准版'],
    defaultOption: String(v.defaultOption || v.default_option || v.option || options[0] || '标准版'),
    variants,
    prices: undefined,
    sales: Number(v.sales || 0),
    highlights: Array.isArray(v.highlights) ? v.highlights : presentation.highlights,
    highSubs: Array.isArray(v.highSubs) ? v.highSubs : presentation.highSubs,
  };
}
async function readProductMetadata(ids, owner) {
  const unique = [...new Set(ids.filter(Boolean).map(String))],
    fresh = unique.filter((id) => !commerceMetadataReads.has(owner + '|' + id));
  for (let offset = 0; offset < fresh.length; offset += 500) {
    const batch = fresh.slice(offset, offset + 500),
      request = commerceCall(
        CommerceEnums.Service.Catalog,
        CommerceEnums.Action.BatchGet,
        { data: { ids: batch } },
        { auth: false },
      ).then((result) => new Map((result.items || []).map((row) => [String(row.id), row])));
    for (const id of batch) {
      const key = owner + '|' + id,
        read = request.then((rows) => rows.get(id) || null);
      commerceMetadataReads.set(key, read);
      const release = () => {
        if (commerceMetadataReads.get(key) === read) commerceMetadataReads.delete(key);
      };
      void read.then(release, release);
    }
  }
  return Promise.all(unique.map((id) => commerceMetadataReads.get(owner + '|' + id)));
}
async function hydrateProducts(ids, options = {}) {
  const owner = commerceOwner(),
    active = () => owner === commerceOwner() && (options.active?.() ?? true),
    unique = [...new Set(ids.filter(Boolean).map(String))],
    missing = unique.filter((id) => !actualProduct(id) || actualProduct(id).metadataUnavailable);
  const raw = (await readProductMetadata(missing, owner)).filter(Boolean);
  if (!active()) return [];
  const value = (row) => ({ ...(row.attributes || row.presentation || {}), ...row }),
    parentIds = [...new Set(raw.map((row) => value(row).product_id || row.parent_id).filter(Boolean))].filter(
      (id) => !actualProduct(id) && !raw.some((row) => row.id === id),
    );
  raw.push(...(await readProductMetadata(parentIds, owner)).filter(Boolean));
  if (!active()) return [];
  // A cart or engagement row is a SKU reference. Resolve its parent and retain
  // the selected variant instead of depending on the bounded home catalogue.
  const hydrated = new Map();
  for (const row of [...raw].sort(
    (a, b) => Number(normalizeProduct(a).id !== a.id) - Number(normalizeProduct(b).id !== b.id),
  )) {
    const product = normalizeProduct(row),
      existing = actualProduct(product.id),
      base = hydrated.get(product.id) || (existing?.metadataUnavailable ? null : existing);
    if (!base) hydrated.set(product.id, product);
    else if (String(row.id) === product.id) hydrated.set(product.id, product);
    else hydrated.set(product.id, { ...base, variants: [...(base.variants || [])] });
    const resolved = hydrated.get(product.id),
      fields = value(row);
    if (row.id !== product.id && !resolved.variants.some((variant) => variant.sku_id === row.id))
      resolved.variants.push({
        sku_id: String(row.id),
        color: String(fields.color || resolved.colors[0]),
        option: String(fields.option || resolved.defaultOption),
        price_cents: Number(fields.price_cents),
      });
  }
  const resolvedIds = new Set(raw.flatMap((row) => [String(row.id), normalizeProduct(row).id]));
  for (let index = products.length - 1; index >= 0; index--)
    if (products[index].metadataUnavailable && resolvedIds.has(products[index].id)) {
      commerceHydratedProducts.delete(products[index].id);
      products.splice(index, 1);
    }
  for (const product of hydrated.values()) {
    const index = products.findIndex((row) => row.id === product.id),
      prior = index < 0 ? null : products[index];
    if (index < 0) products.push(product);
    else products[index] = product;
    if (!prior || commerceHydratedProducts.get(product.id) === prior) commerceHydratedProducts.set(product.id, product);
  }
  for (const id of missing.filter((id) => !actualProduct(id))) {
    const product = {
      ...fallbackProduct,
      id,
      skuId: id,
      name: '商品已不可用',
      visible: false,
      metadataUnavailable: true,
      price_cents: null,
      price: null,
      old: null,
      colors: ['—'],
      options: ['—'],
      defaultOption: '—',
      variants: [],
    };
    products.push(product);
    commerceHydratedProducts.set(id, product);
  }
  const inventory = window.LumoInventory,
    stockIds = unique.filter((id) => {
      const product = actualProduct(id);
      return product && product.visible !== false && !inventory?.snapshot?.(id);
    });
  if (active() && stockIds.length && inventory?.refresh) {
    const fresh = stockIds.filter((id) => !commerceInventoryReads.has(owner + '|' + id));
    if (fresh.length) {
      const read = inventory.refresh(fresh);
      for (const id of fresh) {
        const key = owner + '|' + id;
        commerceInventoryReads.set(key, read);
        const release = () => {
          if (commerceInventoryReads.get(key) === read) commerceInventoryReads.delete(key);
        };
        void read.then(release, release);
      }
    }
    await Promise.all(stockIds.map((id) => commerceInventoryReads.get(owner + '|' + id)));
  }
  return active() ? unique.map(actualProduct) : [];
}
function skuForItem(item) {
  if (typeof item === 'string') return byId(item).skuId || item;
  if (!item) throw new Error('请选择商品规格');
  if (item.sku_id || item.skuId) return String(item.sku_id || item.skuId);
  const p = byId(item.id),
    variant = p.variants?.find((v) => v.color === item.color && v.option === item.option);
  if (p.variants?.length && !variant) throw new Error('所选规格已变更，请重新选择商品规格');
  return String(variant?.sku_id || p.skuId || item.id);
}
function normalizeItem(raw) {
  const sku = String(raw.sku_id || raw.skuId || raw.id || ''),
    p = byId(raw.product_id || sku),
    variant = p.variants?.find((v) => v.sku_id === sku),
    priceCents = raw.unit_price_cents ?? raw.price_cents ?? variant?.price_cents ?? p.price_cents;
  return {
    ...raw,
    id: p.id || raw.product_id || legacyProductIds[sku] || sku,
    sku_id: sku,
    rowId: raw.id,
    qty: Number(raw.quantity ?? raw.qty) || 1,
    name: raw.name || p.name,
    color: raw.color || variant?.color || p.colors[0],
    option: raw.option || variant?.option || p.defaultOption,
    image: raw.image || p.image,
    category: raw.category || p.category,
    unitPrice: Number(priceCents || 0) / 100,
    regularUnitPrice: Number(priceCents || 0) / 100,
    selected: raw.selected !== false,
  };
}
function normalizeTotals(q = {}) {
  const sub = Number(q.subtotal_cents || 0) / 100,
    activitySavings = Number(q.campaign_discount_cents || 0) / 100;
  return {
    sub: sub - activitySavings,
    original: sub,
    activitySavings,
    coupon: Number(q.coupon_discount_cents || 0) / 100,
    points: Number(q.points || 0) / 100,
    shipping: Number(q.shipping_cents || 0) / 100,
    total: Number(q.payable_cents || 0) / 100,
  };
}
function normalizeOrder(raw) {
  const q = raw.quote || {},
    a = raw.address || {},
    status = String(raw.status || ''),
    createdAt = Date.parse(raw.created_at) || 0,
    updatedAt = Date.parse(raw.updated_at) || createdAt;
  const paid =
      !!raw.payment_id &&
      ![
        CommerceEnums.OrderStatus.PendingPayment,
        CommerceEnums.OrderStatus.Creating,
        CommerceEnums.OrderStatus.CreateFailed,
        CommerceEnums.OrderStatus.Closing,
        CommerceEnums.OrderStatus.Closed,
      ].includes(status),
    items = (raw.items || q.items || []).map(normalizeItem);
  if (q.group_id || raw.group_id)
    items.forEach(
      (x) =>
        (x.promotion = {
          type: CommerceEnums.PromotionType.Group,
          teamId: q.group_id || raw.group_id,
          offerId: q.activity_id || 'group-demo',
        }),
    );
  else if (q.activity_kind === CommerceEnums.CampaignKind.Flash || q.activity_id === 'flash-demo')
    items.forEach((x) => (x.promotion = { type: CommerceEnums.PromotionType.Flash, offerId: q.activity_id }));
  if (q.addon_sku_id)
    items
      .filter((x) => x.sku_id === q.addon_sku_id)
      .forEach((x) => (x.promotion = { type: CommerceEnums.PromotionType.Exchange, offerId: q.activity_id }));
  return {
    ...raw,
    activityId: q.activity_id || raw.activity_id || '',
    activityKind: q.activity_kind || raw.activity_kind || '',
    activityName: q.activity_name || raw.activity_name || '',
    backendStatus: status,
    id: raw.id || raw.order_id,
    shippingLabel: q.shipping?.provider || q.shipping_policy?.name || '标准配送',
    createdAt,
    updatedAt,
    paidAt: paid ? Date.parse(raw.paid_at) || updatedAt : null,
    expiresAt: Date.parse(raw.expires_at) || 0,
    items,
    totals: normalizeTotals({ ...q, payable_cents: raw.payable_cents ?? q.payable_cents }),
    address: {
      ...a,
      id: a.id,
      name: a.recipient || a.name || '',
      phone: String(a.phone || ''),
      region: [a.province, a.city, a.district].filter(Boolean).join(' ') || a.region || '',
      detail: a.address || a.detail || '',
    },
    method: CommerceEnums.PaymentProvider.Demo,
    methodLabel: '演示支付渠道',
    couponId: q.coupon_id || raw.coupon_id || null,
    pointsUsed: Number(q.points || raw.points || 0),
    groupId: q.group_id || raw.group_id || '',
    lifecycle: {
      createdAt,
      shippedAt: [CommerceEnums.OrderStatus.Shipped, CommerceEnums.OrderStatus.Completed].includes(status)
        ? updatedAt
        : null,
      receivedAt: status === CommerceEnums.OrderStatus.Completed ? updatedAt : null,
    },
    source: raw.source || CommerceEnums.OrderDataSource.Backend,
  };
}
function readCommerce(resource, execute) {
  const owner = commerceOwner(),
    previous = commerceReads.get(resource);
  if (previous?.loading && previous.owner === owner) return previous.promise;
  const entry = {
    owner,
    loading: true,
    loaded: previous?.owner === owner && previous.loaded === true,
    error: '',
    promise: null,
  };
  commerceReads.set(resource, entry);
  const active = () => commerceReads.get(resource) === entry && owner === commerceOwner();
  entry.promise = (async () => {
    try {
      const result = await execute(active);
      if (active()) entry.loaded = true;
      return result;
    } catch (error) {
      if (active()) entry.error = error.message || '数据加载失败，请重试';
      throw error;
    } finally {
      if (active()) {
        entry.loading = false;
        entry.promise = null;
      }
    }
  })();
  return entry.promise;
}
function refreshCatalog() {
  return readCommerce('catalog', async (active) => {
    try {
      const result = await commerceCall(
        CommerceEnums.Service.Catalog,
        CommerceEnums.Action.List,
        { data: { limit: 100 } },
        { auth: false },
      );
      if (!active()) return [];
      const list = (result.items || []).map(normalizeProduct),
        publicIds = new Set(list.map((product) => product.id));
      for (const id of publicIds) commerceHydratedProducts.delete(id);
      products.splice(0, products.length, ...list, ...commerceHydratedProducts.values());
      commerceState.catalogLoaded = true;
      commerceState.catalogError = '';
      window.LumoRecommendations?.catalogChanged?.();
      return list;
    } catch (error) {
      if (active()) {
        commerceState.catalogLoaded = true;
        commerceState.catalogError = error.message || '无法连接商品服务';
      }
      throw error;
    }
  });
}
function refreshCart() {
  return readCommerce('cart', async (active) => {
    if (!window.LumoAuth?.isLoggedIn()) {
      state.cart = [];
      return [];
    }
    const rows = [],
      seen = new Set();
    let offset = 0;
    while (active()) {
      const result = await commerceCall(CommerceEnums.Service.Cart, CommerceEnums.Action.List, {
          data: { limit: 100, offset },
        }),
        page = result.items || [],
        total = Number.isFinite(Number(result.total)) && result.total != null ? Number(result.total) : null;
      if (!active()) return [];
      const fresh = [];
      for (const row of page) {
        const key = String(row.id || row.sku_id);
        if (!seen.has(key)) {
          seen.add(key);
          fresh.push(row);
        }
      }
      if (page.length && !fresh.length) throw Error('购物车分页数据重复，请重新加载');
      rows.push(...fresh);
      offset += page.length;
      if (total == null ? page.length < 100 : offset >= total) break;
      if (!page.length) throw Error('购物车数据不完整，请重新加载');
    }
    await hydrateProducts(
      rows.map((row) => row.sku_id || row.skuId || row.id),
      { active },
    );
    if (!active() || !window.LumoAuth?.isLoggedIn()) return [];
    const selected = new Map(state.cart.map((x) => [x.sku_id || skuForItem(x), x.selected]));
    state.cart = rows.map((row) => {
      const sku = String(row.sku_id || row.skuId || row.id || ''),
        item = normalizeItem({ ...row, product_id: actualProduct(sku)?.id || row.product_id });
      item.unavailable = actualProduct(item.sku_id)?.visible === false;
      item.selected = !item.unavailable && (selected.has(item.sku_id) ? selected.get(item.sku_id) : true);
      return item;
    });
    return state.cart;
  });
}
function refreshFavorites() {
  return readCommerce('favorites', async (active) => {
    if (!window.LumoAuth?.isLoggedIn()) {
      state.favorites = [];
      state.favoriteSKUs = [];
      return [];
    }
    const result = await commerceCall(CommerceEnums.Service.Engagement, CommerceEnums.Action.List, {
      data: { kind: CommerceEnums.EngagementKind.Favorite, limit: 100 },
    });
    if (!active()) return [];
    await hydrateProducts(
      (result.items || []).map((row) => row.sku_id),
      { active },
    );
    if (!active() || !window.LumoAuth?.isLoggedIn()) return [];
    state.favoriteSKUs = (result.items || []).map((x) => x.sku_id);
    state.favorites = (result.items || []).map((x) => byId(x.sku_id).id || legacyProductIds[x.sku_id] || x.sku_id);
    return state.favorites;
  });
}
function refreshOrders() {
  return readCommerce('orders', async (active) => {
    if (!window.LumoAuth?.isLoggedIn()) {
      commerceOrders = [];
      state.orders = [];
      state.pending = null;
      return [];
    }
    const result = await commerceCall(CommerceEnums.Service.Order, CommerceEnums.Action.List);
    if (!active() || !window.LumoAuth?.isLoggedIn()) return [];
    commerceOrders = (result.items || []).map(normalizeOrder).sort((a, b) => b.createdAt - a.createdAt);
    const prior = state.pending?.id;
    state.pending =
      commerceOrders.find((o) => o.id === prior && o.backendStatus === CommerceEnums.OrderStatus.PendingPayment) ||
      commerceOrders.find((o) => o.backendStatus === CommerceEnums.OrderStatus.PendingPayment) ||
      null;
    state.orders = commerceOrders.filter((o) => o.backendStatus !== CommerceEnums.OrderStatus.PendingPayment);
    return commerceOrders;
  });
}
function ensureCommerceProduct(id) {
  if (actualProduct(id) || !commerceState.catalogLoaded || commerceState.catalogError) return;
  const owner = commerceOwner(),
    key = owner + '|' + id,
    previous = commerceProductReads.get(key);
  if (previous) return previous.promise;
  const entry = { loading: true, error: '', promise: null };
  commerceProductReads.set(key, entry);
  entry.promise = (async () => {
    try {
      const result = await commerceCall(
        CommerceEnums.Service.Catalog,
        CommerceEnums.Action.Get,
        { id },
        { auth: false },
      );
      if (owner !== commerceOwner()) return;
      const product = normalizeProduct(result);
      if (!product.id || !result.id) throw Error('商品不存在或已下架');
      const index = products.findIndex((row) => row.id === product.id);
      if (index < 0) products.push(product);
      else products[index] = product;
    } catch (error) {
      if (owner === commerceOwner()) entry.error = error.message || '商品加载失败';
    } finally {
      if (owner === commerceOwner()) {
        entry.loading = false;
        if (location.hash === '#/product/' + id) renderKeepScroll();
      }
    }
  })();
  return entry.promise;
}
async function refreshCommerce() {
  await refreshCatalog();
  if (window.LumoAuth?.isLoggedIn()) await Promise.all([refreshCart(), refreshOrders(), refreshFavorites()]);
  else {
    state.cart = [];
    state.orders = [];
    state.pending = null;
    state.favorites = [];
    commerceOrders = [];
  }
  return commerceState;
}
async function addItems(items) {
  if (!window.LumoAuth?.requireLogin(location.hash)) return false;
  try {
    await refreshCart();
    const quantities = new Map(state.cart.map((x) => [skuForItem(x), x.qty]));
    for (const item of items) {
      const sku = skuForItem(item);
      quantities.set(sku, Math.min(10, (quantities.get(sku) || 0) + (Number(item.qty) || 1)));
    }
    const additions = [...quantities]
      .filter(([sku]) => items.some((item) => skuForItem(item) === sku))
      .map(([sku_id, quantity]) => ({ sku_id, quantity }));
    await Promise.all(additions.map((x) => window.LumoInventory?.refreshOne?.(x.sku_id)));
    for (const row of additions) {
      const available = window.LumoInventory?.snapshot?.(row.sku_id)?.available;
      if (available != null && row.quantity > Number(available)) throw new Error('库存不足，无法再次购买该数量');
    }
    await commerceCall(CommerceEnums.Service.Cart, CommerceEnums.Action.Merge, {
      idempotency_key: newCommerceKey(),
      data: { items: additions },
    });
    await refreshCart();
    renderKeepScroll();
    toast('已按当前商品信息加入购物车');
    return true;
  } catch (error) {
    commerceError(error);
    return false;
  }
}
function commerceError(error) {
  const trace = error.traceID || error.traceId || error.trace_id;
  toast(
    (error.message || '请求失败，请重试') +
      (trace && !String(error.message || '').includes(trace) ? ' · 链路 ' + trace : ''),
  );
}
async function setCartQuantity(item, quantity) {
  const sku = skuForItem(item);
  if (commerceCartMutating.has(sku)) return;
  commerceCartMutating.add(sku);
  commerceCartQueue = commerceCartQueue
    .catch(() => {})
    .then(async () => {
      await commerceCall(CommerceEnums.Service.Cart, CommerceEnums.Action.Set, {
        data: { sku_id: skuForItem(item), quantity },
      });
      await refreshCart();
      invalidateCommerceQuote();
      renderKeepScroll();
    });
  try {
    await commerceCartQueue;
  } catch (error) {
    commerceError(error);
  } finally {
    commerceCartMutating.delete(sku);
  }
}
async function removeCartItems(items) {
  const owner = commerceOwner();
  try {
    const rows = items.filter(Boolean);
    if (
      rows.length &&
      rows.length === state.cart.length &&
      state.cart.every((x) => rows.some((y) => skuForItem(y) === skuForItem(x)))
    )
      await commerceCall(CommerceEnums.Service.Cart, CommerceEnums.Action.Clear);
    else
      for (const item of rows) {
        if (owner !== commerceOwner()) return;
        await commerceCall(CommerceEnums.Service.Cart, CommerceEnums.Action.Remove, {
          id: item.rowId || skuForItem(item),
        });
      }
    if (owner !== commerceOwner()) return;
    await refreshCart();
    if (owner !== commerceOwner()) return;
    invalidateCommerceQuote();
    closeModal();
    renderKeepScroll();
    toast('已从购物车移除');
  } catch (error) {
    if (owner === commerceOwner()) commerceError(error);
  }
}
async function moveCartToFavorites(items) {
  if (!window.LumoAuth?.requireLogin(location.hash)) return;
  const owner = commerceOwner();
  const rows = items.filter(Boolean);
  if (!rows.length || commerceCartMoving.has(owner)) return;
  commerceCartMoving.add(owner);
  try {
    for (const item of rows) {
      if (owner !== commerceOwner()) return;
      if (!state.favorites.includes(item.id)) {
        await commerceCall(CommerceEnums.Service.Engagement, CommerceEnums.Action.Put, {
          data: { kind: CommerceEnums.EngagementKind.Favorite, sku_id: skuForItem(item) },
        });
      }
    }
    if (owner !== commerceOwner()) return;
    await refreshFavorites();
    if (owner !== commerceOwner()) return;
    await removeCartItems(rows);
  } catch (error) {
    if (owner === commerceOwner()) commerceError(error);
  } finally {
    commerceCartMoving.delete(owner);
  }
}
async function toggleFavorite(id) {
  if (!window.LumoAuth?.requireLogin(location.hash)) return;
  try {
    const p = byId(id);
    await commerceCall(
      CommerceEnums.Service.Engagement,
      state.favorites.includes(id) ? CommerceEnums.Action.Delete : CommerceEnums.Action.Put,
      { data: { kind: CommerceEnums.EngagementKind.Favorite, sku_id: p.skuId || id } },
    );
    await refreshFavorites();
    await window.LumoSidebars?.refresh?.();
    renderKeepScroll();
    toast(state.favorites.includes(id) ? '已收藏' : '已取消收藏');
  } catch (error) {
    commerceError(error);
  }
}
function commerceQuoteData(items = selectedItems()) {
  const promo = window.LumoPromotions?.quoteData?.(items) || {},
    meta = window.LumoMember?.checkoutMeta?.(items) || {};
  const lines = items
    .filter((x) => !(promo.addon_sku_id && x.promotion?.type === CommerceEnums.PromotionType.Exchange))
    .map((x) => ({ sku_id: skuForItem(x), quantity: Number(x.qty) }));
  const grouped = new Map();
  for (const line of lines) grouped.set(line.sku_id, (grouped.get(line.sku_id) || 0) + line.quantity);
  return {
    items: [...grouped].map(([sku_id, quantity]) => ({ sku_id, quantity })),
    coupon_id: meta.couponId || meta.coupon_id || '',
    coupon_mode: meta.couponMode || 'auto',
    points_mode: state.points ? 'max' : '',
    address_id: state.address?.id || '',
    address: { region: state.address?.region || '' },
    points: state.points ? Number(meta.pointsUsed || meta.points || 0) : 0,
    ...promo,
  };
}
function commerceQuoteKey(items) {
  try {
    return JSON.stringify({
      pricing: commerceQuoteData(items),
      address: state.address?.id || [state.address?.region, state.address?.detail].filter(Boolean).join(' '),
    });
  } catch {
    return '';
  }
}
function commerceQuoteFor(items) {
  return commerceState.quote &&
    commerceState.quoteKey === commerceQuoteKey(items) &&
    Date.parse(commerceState.quote.expires_at) > Date.now()
    ? commerceState.quote
    : null;
}
function commerceCartKey(items) {
  return JSON.stringify(
    items.map((item) => [skuForItem(item), Number(item.qty)]).sort((a, b) => a[0].localeCompare(b[0])),
  );
}
function commerceDisplayQuote(items) {
  return (
    commerceQuoteFor(items) || (commerceState.quoteCartKey === commerceCartKey(items) ? commerceState.quote : null)
  );
}
function invalidateCommerceQuote(discard = false) {
  commerceQuoteSequence++;
  if (discard) {
    commerceState.quote = null;
    commerceState.quoteCartKey = '';
  }
  commerceState.quoteKey = '';
  commerceState.quoteLoadingKey = '';
  commerceState.quoteError = '';
  commerceState.quoteErrorKey = '';
}
async function ensureCommerceQuote(force = false) {
  if (!window.LumoAuth?.isLoggedIn() || !selectedItems().length) return null;
  const data = commerceQuoteData(),
    key = commerceQuoteKey(selectedItems());
  if (!force && commerceQuoteFor(selectedItems())) return commerceState.quote;
  if (!force && commerceState.quoteLoadingKey === key) return null;
  const seq = ++commerceQuoteSequence;
  commerceState.quoteLoadingKey = key;
  commerceState.quoteError = '';
  try {
    const q = await commerceCall(CommerceEnums.Service.Coupon, CommerceEnums.Action.Calculate, { data });
    const shipping = q.shipping || { shipping_cents: q.shipping_cents, serviceable: true };
    if (seq !== commerceQuoteSequence) return null;
    if (shipping.serviceable === false) throw new Error('当前地址暂不可配送，请更换收货地址');
    if (Number(shipping.shipping_cents || 0) !== Number(q.shipping_cents || 0))
      throw new Error('配送费用与订单报价不一致，请重新确认');
    commerceState.shippingQuote = shipping;
    window.LumoMember?.applyCouponQuote?.(q);
    commerceState.quote = q;
    commerceState.quoteCartKey = commerceCartKey(selectedItems());
    commerceState.quoteKey = key;
    return q;
  } catch (error) {
    if (seq === commerceQuoteSequence) {
      commerceState.quoteError = error.message;
      commerceState.quoteErrorKey = key;
    }
    throw error;
  } finally {
    if (seq === commerceQuoteSequence) {
      commerceState.quoteLoadingKey = '';
      if (location.hash === '#/checkout') renderKeepScroll();
    }
  }
}
function submitStorageKey() {
  return (
    'lumo-submit-v3::' +
    JSON.stringify([
      window.LumoAPI.getBase() || location.origin,
      String(window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || 'guest'),
    ])
  );
}
function readSubmitAttempt() {
  try {
    return JSON.parse(sessionStorage.getItem(submitStorageKey()) || 'null');
  } catch {
    return null;
  }
}
function storeSubmitAttempt(attempt) {
  sessionStorage.setItem(submitStorageKey(), JSON.stringify(attempt));
}
function clearSubmitAttempt() {
  sessionStorage.removeItem(submitStorageKey());
}
function newCommerceKey() {
  return (
    'web-' + (globalThis.crypto?.randomUUID?.() || Date.now().toString(36) + '-' + Math.random().toString(36).slice(2))
  );
}
async function submitCommerceOrder() {
  if (commerceState.submitting || !window.LumoAuth?.requireLogin('#/checkout')) return;
  const items = selectedItems();
  if (!items.length) {
    toast('请先选择商品');
    return;
  }
  if (!state.address) {
    addressModal();
    return;
  }
  const validation = window.LumoMember?.validateCheckout(items) || window.LumoPromotions?.validate(items, 'submit');
  if (validation) {
    toast(validation);
    return;
  }
  commerceState.submitting = true;
  commerceState.quoteError = '';
  renderKeepScroll();
  try {
    const address = state.address.id
      ? { address_id: state.address.id }
      : {
          address: {
            recipient: state.address.name,
            phone: state.address.phone,
            address: [state.address.region, state.address.detail].filter(Boolean).join(' '),
          },
        };
    const input = commerceQuoteData(items),
      note = String(state.note || '').trim(),
      source =
        checkoutMode === CommerceEnums.OrderSource.Cart
          ? CommerceEnums.OrderSource.Cart
          : CommerceEnums.OrderSource.Direct,
      fingerprint = JSON.stringify({ input, address, note, source }),
      flash = items.some((x) => x.promotion?.type === CommerceEnums.PromotionType.Flash);
    let attempt = readSubmitAttempt();
    if (attempt && attempt.fingerprint !== fingerprint) {
      modal(
        '确认上次提交结果',
        '<p class="modal-desc">你有一笔提交请求尚未确认。请先恢复这笔请求，避免重复创建订单。</p><button class="primary" data-action="commerce-resume-submit">恢复上次请求</button>',
      );
      return;
    }
    if (!attempt) {
      const preview = commerceQuoteFor(items) || (await ensureCommerceQuote(true));
      if (!preview) throw new Error('报价已变更，请重新确认');
      const q = await commerceCall(CommerceEnums.Service.Pricing, CommerceEnums.Action.Get, { id: preview.quote_id });
      if (Date.parse(q.expires_at) <= Date.now()) throw new Error('quote expired; request a new quote');
      attempt = {
        fingerprint,
        key: newCommerceKey(),
        data: { quote_id: q.quote_id, ...address, note, source },
        flash,
        createdAt: Date.now(),
      };
      storeSubmitAttempt(attempt);
    }
    let result;
    if (attempt.flash) {
      result = await commerceCall(CommerceEnums.Service.Flash, CommerceEnums.Action.Request, {
        data: attempt.data,
        idempotency_key: attempt.key,
      });
      commerceState.flashRequestId = result.id;
      await pollFlashRequest(result.id);
      return;
    }
    result = await commerceCall(CommerceEnums.Service.Checkout, CommerceEnums.Action.Submit, {
      data: attempt.data,
      idempotency_key: attempt.key,
    });
    if (!result.order_id) throw new Error('订单仍在处理中，请沿用本次请求重试');
    await finishSubmittedOrder(result.order_id);
  } catch (error) {
    commerceState.quoteError = error.message;
    if (/quote expired|报价.*过期/i.test(error.message)) {
      clearSubmitAttempt();
      invalidateCommerceQuote();
    }
    commerceError(error);
  } finally {
    commerceState.submitting = false;
    renderKeepScroll();
  }
}
function commerceAttemptNotice() {
  return readSubmitAttempt()
    ? '<div class="cart-banner"><span>' +
        icon('clock') +
        '上次提交的结果尚待确认</span><button data-action="commerce-resume-submit">恢复上次订单请求</button></div>'
    : '';
}
async function resumeCommerceAttempt() {
  const attempt = readSubmitAttempt();
  if (!attempt || commerceState.submitting || !window.LumoAuth?.requireLogin('#/checkout')) return;
  commerceState.submitting = true;
  closeModal();
  renderKeepScroll();
  try {
    if (attempt.flash) {
      const result = await commerceCall(CommerceEnums.Service.Flash, CommerceEnums.Action.Request, {
        data: attempt.data,
        idempotency_key: attempt.key,
      });
      commerceState.flashRequestId = result.id;
      await pollFlashRequest(result.id);
    } else {
      const result = await commerceCall(CommerceEnums.Service.Checkout, CommerceEnums.Action.Submit, {
        data: attempt.data,
        idempotency_key: attempt.key,
      });
      if (!result.order_id) throw new Error('订单仍在处理中，请继续恢复原请求');
      await finishSubmittedOrder(result.order_id);
    }
  } catch (error) {
    if (/quote expired|报价.*过期/i.test(error.message)) {
      clearSubmitAttempt();
      invalidateCommerceQuote();
    }
    commerceError(error);
  } finally {
    commerceState.submitting = false;
    renderKeepScroll();
  }
}
async function finishSubmittedOrder(id) {
  const result = await commerceCall(CommerceEnums.Service.Order, CommerceEnums.Action.Get, { id });
  const order = normalizeOrder(result);
  await refreshOrders();
  state.pending = order.backendStatus === CommerceEnums.OrderStatus.PendingPayment ? order : state.pending;
  commerceState.payment = null;
  commerceState.paymentError = '';
  clearSubmitAttempt();
  window.LumoMember?.resetCheckoutCoupon?.();
  invalidateCommerceQuote(true);
  window.LumoPromotions?.clearCampaign?.();
  window.LumoPromotions?.clearExchange?.();
  if (order.backendStatus === CommerceEnums.OrderStatus.PendingPayment) go('payment');
  else {
    commerceState.successOrderId = order.id;
    go('member/orders');
  }
}
async function pollFlashRequest(id) {
  const deadline = Date.now() + 20000;
  let result;
  while (Date.now() < deadline) {
    result = await commerceCall(CommerceEnums.Service.Flash, CommerceEnums.Action.Get, { id });
    if (result.status === CommerceEnums.FlashRequestStatus.Succeeded && result.order_id) {
      await finishSubmittedOrder(result.order_id);
      return result;
    }
    if (result.status === CommerceEnums.FlashRequestStatus.Failed) {
      clearSubmitAttempt();
      throw new Error(result.error || '秒杀下单失败');
    }
    await new Promise((resolve) => setTimeout(resolve, 750));
  }
  toast('秒杀请求已入队，继续提交可查询同一请求');
  return result;
}
let paymentMethodsFlight = null;
async function loadPaymentMethods(force = false) {
  if (paymentMethodsFlight) return paymentMethodsFlight;
  if (!force && Date.now() - commerceState.paymentMethodsAt < 15000) return commerceState.paymentMethods;
  const owner = commerceOwner();
  paymentMethodsFlight = (async () => {
    try {
      const value = await commerceCall(CommerceEnums.Service.Payment, CommerceEnums.Action.Methods, {});
      if (owner !== commerceOwner()) return [];
      commerceState.paymentMethods = value.items || [];
      commerceState.paymentMethodsReady = true;
      commerceState.paymentMethodsError = '';
      if (!commerceState.paymentMethods.some((m) => m.id === selectedMethod))
        selectedMethod = commerceState.paymentMethods[0]?.id || '';
      return commerceState.paymentMethods;
    } catch (error) {
      if (owner === commerceOwner()) commerceState.paymentMethodsError = error.message;
      return [];
    } finally {
      if (owner === commerceOwner()) {
        commerceState.paymentMethodsAt = Date.now();
        if (location.hash === '#/payment') renderKeepScroll();
      }
      paymentMethodsFlight = null;
    }
  })();
  return paymentMethodsFlight;
}
async function createCommercePayment() {
  if (commerceState.paying || !state.pending || !window.LumoAuth?.requireLogin('#/payment')) return;
  const id = state.pending.id,
    owner = commerceOwner(),
    active = () => owner === commerceOwner() && window.LumoAuth?.isLoggedIn() && state.pending?.id === id;
  commerceState.paying = true;
  commerceState.paymentError = '';
  renderKeepScroll();
  try {
    const payment = await commerceCall(CommerceEnums.Service.Payment, CommerceEnums.Action.Create, {
      id,
      data: { provider: selectedMethod },
    });
    if (!active()) return;
    commerceState.payment = payment;
    toast('支付请求已创建，可继续模拟支付');
    await refreshCommercePayment(false);
  } catch (error) {
    if (active()) {
      commerceState.paymentError = error.message;
      commerceError(error);
    }
  } finally {
    if (owner === commerceOwner()) {
      commerceState.paying = false;
      renderKeepScroll();
    }
  }
}
async function submitCommerceDemoPayment() {
  if (commerceState.paying || !state.pending || !window.LumoAuth?.requireLogin('#/payment')) return;
  const id = state.pending.id,
    owner = commerceOwner(),
    active = () => owner === commerceOwner() && window.LumoAuth?.isLoggedIn() && state.pending?.id === id;
  commerceState.paying = true;
  commerceState.paymentError = '';
  renderKeepScroll();
  try {
    let payment = commerceState.payment?.order_id === id ? commerceState.payment : null;
    if (!payment) {
      payment = await commerceCall(CommerceEnums.Service.Payment, CommerceEnums.Action.Create, {
        id,
        data: { provider: selectedMethod },
      });
      if (!active()) return;
      commerceState.payment = payment;
    }
    if (payment.provider !== CommerceEnums.PaymentProvider.Demo) throw new Error('当前支付渠道不支持模拟提交');
    if (payment.status === CommerceEnums.PaymentStatus.Pending) {
      payment = await commerceCall(CommerceEnums.Service.Payment, CommerceEnums.Action.SubmitDemo, { id: payment.id });
      if (!active()) return;
      commerceState.payment = payment;
    }
    if (payment.status !== CommerceEnums.PaymentStatus.Succeeded)
      throw new Error('模拟支付未成功，请刷新订单状态后重试');
    toast('模拟支付已成功，正在确认订单');
    const deadline = Date.now() + 15000;
    do {
      const order = await refreshCommercePayment(false);
      if (!active() || order?.backendStatus !== CommerceEnums.OrderStatus.PendingPayment) return;
      if (Date.now() >= deadline) break;
      await new Promise((resolve) => setTimeout(resolve, 500));
    } while (active());
    if (active()) toast('模拟支付已成功，订单仍在确认中，可稍后刷新结果');
  } catch (error) {
    if (active()) {
      commerceState.paymentError = error.message;
      commerceError(error);
    }
  } finally {
    if (owner === commerceOwner()) {
      commerceState.paying = false;
      renderKeepScroll();
    }
  }
}
async function refreshCommercePayment(showError = true) {
  if (!state.pending || !window.LumoAuth?.isLoggedIn()) return;
  const id = state.pending.id,
    owner = commerceOwner(),
    active = () => owner === commerceOwner() && window.LumoAuth?.isLoggedIn() && state.pending?.id === id;
  try {
    if (commerceState.payment?.order_id === id) {
      const payment = await commerceCall(CommerceEnums.Service.Payment, CommerceEnums.Action.Get, {
        id: commerceState.payment.id,
      });
      if (!active()) return;
      commerceState.payment = payment;
    }
    const result = normalizeOrder(await commerceCall(CommerceEnums.Service.Order, CommerceEnums.Action.Get, { id }));
    if (!active()) return;
    commerceState.paymentError = '';
    const paid =
      !!result.payment_id &&
      ![
        CommerceEnums.OrderStatus.PendingPayment,
        CommerceEnums.OrderStatus.Creating,
        CommerceEnums.OrderStatus.Closing,
        CommerceEnums.OrderStatus.Closed,
        CommerceEnums.OrderStatus.CreateFailed,
      ].includes(result.backendStatus);
    if (paid) {
      commerceState.successOrderId = id;
      await Promise.all([refreshOrders(), refreshCart()]);
      if (owner !== commerceOwner() || !window.LumoAuth?.isLoggedIn()) return;
      void refreshOrderResources([result], false).catch(commerceError);
      go('success');
      return result;
    }
    if (result.backendStatus !== CommerceEnums.OrderStatus.PendingPayment) {
      await refreshOrders();
      go('member/orders');
      toast('订单状态：' + result.backendStatus);
      return result;
    }
    state.pending = result;
    renderKeepScroll();
    return result;
  } catch (error) {
    if (active()) {
      commerceState.paymentError = error.message;
      if (showError) commerceError(error);
    }
    throw error;
  }
}
async function cancelCommerceOrder(id) {
  const owner = commerceOwner(),
    previous = commerceOrders.find((order) => order.id === id) || (state.pending?.id === id ? state.pending : null);
  try {
    await commerceCall(CommerceEnums.Service.Checkout, CommerceEnums.Action.Cancel, { id });
    if (owner !== commerceOwner() || !window.LumoAuth?.isLoggedIn()) return;
    await refreshOrders();
    if (owner !== commerceOwner() || !window.LumoAuth?.isLoggedIn()) return;
    commerceState.payment = null;
    const order = commerceOrders.find((row) => row.id === id) || previous;
    void refreshOrderResources(order ? [order] : [], false).catch(commerceError);
    go('member/orders');
    toast('订单已取消，服务端正在释放预占资源');
  } catch (error) {
    if (owner === commerceOwner()) commerceError(error);
  }
}
async function refreshOrderResources(orders, includeCart = true) {
  const ids = [...new Set(orders.flatMap((order) => (order.items || []).map((item) => item.sku_id).filter(Boolean)))];
  await Promise.all([
    includeCart ? refreshCart() : null,
    window.LumoMember?.refresh?.(false, ['coupons', 'points']),
    ids.length ? window.LumoInventory?.refresh?.(ids) : null,
  ]);
}
async function refreshPostPaymentState() {
  if (!window.LumoAuth?.isLoggedIn() || location.hash !== '#/success') return;
  try {
    await readCommerce('post-payment', async (active) => {
      const signature = (order) =>
        JSON.stringify([order.backendStatus, order.payment_id, Number(order.refunded_total_cents || 0)]);
      const previous = new Map(commerceOrders.map((order) => [order.id, signature(order)]));
      await refreshOrders();
      if (!active() || location.hash !== '#/success') return;
      const changed = commerceOrders.filter((order) => order.paidAt && previous.get(order.id) !== signature(order));
      if (changed.length) await refreshOrderResources(changed);
    });
    if (location.hash === '#/success') renderKeepScroll();
  } catch (error) {
    commerceError(error);
    if (location.hash === '#/success') commercePollTimer = setTimeout(refreshPostPaymentState, 8000);
  }
}
function commerceAfterRender(name) {
  if (name === 'product' && !actualProduct((location.hash || '').slice(2).split('/')[1])) {
    void ensureCommerceProduct((location.hash || '').slice(2).split('/')[1]);
    commerceDetailSKU = '';
  } else if (name === 'product' && window.LumoInventory?.refreshOne) {
    const sku = skuForItem(detail);
    if (sku !== commerceDetailSKU) {
      commerceDetailSKU = sku;
      void window.LumoInventory.refreshOne(sku)
        .then(() => {
          if (location.hash.startsWith('#/product/')) renderKeepScroll();
        })
        .catch(commerceError);
    }
  } else commerceDetailSKU = '';
  if (name === 'checkout' && selectedItems().length && window.LumoAuth?.isLoggedIn()) {
    const key = commerceQuoteKey(selectedItems());
    if (key !== commerceState.quoteErrorKey) commerceState.quoteError = '';
    if (!commerceQuoteFor(selectedItems()) && commerceState.quoteLoadingKey !== key && !commerceState.quoteError)
      void ensureCommerceQuote().catch(() => {});
  }
  clearTimeout(commercePollTimer);
  if (name === 'payment' && window.LumoAuth?.isLoggedIn()) void loadPaymentMethods();
  if (name === 'payment' && state.pending && !commerceState.paying && window.LumoAuth?.isLoggedIn())
    commercePollTimer = setTimeout(
      () =>
        refreshCommercePayment(false).catch(() => {
          if (location.hash === '#/payment') renderKeepScroll();
        }),
      3000,
    );
  if (name === 'success' && window.LumoAuth?.isLoggedIn())
    commercePollTimer = setTimeout(refreshPostPaymentState, 5000);
}
window.LumoCommerce = {
  readState: commerceReadState,
  refresh: refreshCommerce,
  refreshCatalog,
  refreshCart,
  refreshOrders,
  refreshFavorites,
  hydrateProducts,
  normalizeProduct,
  normalizeItem,
  normalizeOrder,
  normalizeTotals,
  skuForItem,
  skuFor: skuForItem,
  allOrders: () => commerceOrders,
  addItem: addToCart,
  addItems,
  setQuantity: setCartQuantity,
  quoteData: commerceQuoteData,
  ensureQuote: ensureCommerceQuote,
  invalidateQuote: invalidateCommerceQuote,
  submit: submitCommerceOrder,
  resumeSubmit: resumeCommerceAttempt,
  createPayment: createCommercePayment,
  submitDemoPayment: submitCommerceDemoPayment,
  refreshPayment: refreshCommercePayment,
  cancelOrder: cancelCommerceOrder,
  get state() {
    return commerceState;
  },
  reset() {
    commerceEpoch++;
    for (const resource of ['cart', 'orders', 'favorites']) commerceReads.delete(resource);
    commerceProductReads.clear();
    commerceMetadataReads.clear();
    commerceInventoryReads.clear();
    for (let index = products.length - 1; index >= 0; index--)
      if (commerceHydratedProducts.get(products[index].id) === products[index]) products.splice(index, 1);
    commerceHydratedProducts.clear();
    clearTimeout(commercePollTimer);
    commerceOrders = [];
    state.cart = [];
    state.orders = [];
    state.pending = null;
    directItem = null;
    checkoutMode = CommerceEnums.OrderSource.Cart;
    delete state.checkout;
    state.favorites = [];
    state.favoriteSKUs = [];
    state.address = null;
    commerceState.payment = null;
    commerceState.paymentMethods = [];
    commerceState.paymentMethodsReady = false;
    commerceState.paymentMethodsAt = 0;
    commerceState.paymentMethodsError = '';
    commerceState.paymentError = '';
    commerceState.paying = false;
    commerceState.successOrderId = '';
    invalidateCommerceQuote(true);
  },
};
window.addEventListener('lumo:logout', () => window.LumoCommerce.reset());
window.addEventListener('lumo:login', () => window.LumoCommerce.reset());
window.addEventListener('lumo:api-scope-changed', () => {
  window.LumoCommerce.reset();
  commerceReads.clear();
  products.splice(0, products.length);
  commerceState.catalogLoaded = false;
  commerceState.catalogError = '';
});
render();
