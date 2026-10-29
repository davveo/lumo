(function () {
  'use strict';
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char],
    );
  const safeLink = (value) =>
    typeof value === 'string' &&
    /^#\/(?:coupons|member(?:\/(?:coupons|points|growth))?|promotions(?:\/(?:flash|group|exchange|reduction|discount|quantity_discount|direct)(?:\/[A-Za-z0-9_-]+)?)?|product\/(?:[A-Za-z0-9_.:-]|%[A-Fa-f0-9]{2}){1,180})?$/.test(
      value,
    );
  const safeImage = (value) =>
    typeof value === 'string' &&
    (/^(?:assets\/)?(?:[A-Za-z0-9_-]+\.(?:jpg|jpeg|png|webp)|demo-[a-z0-9-]+\.svg)$/.test(value) ||
      /^data:image\/(?:png|jpeg|webp);base64,[A-Za-z0-9+/=]+$/.test(value) ||
      (() => {
        try {
          const url = new URL(value);
          return url.protocol === 'https:' && url.hostname && !url.username && !url.password;
        } catch (_) {
          return false;
        }
      })());
  const asset = (value) => (/^assets\/|^https:|^data:/.test(value) ? value : 'assets/' + value);
  const defaults = {
    headerSlogan: '品质生活，好物相伴',
    brandTagline: 'CURATED FOR EVERYDAY',
    searchPlaceholder: '搜索商品，发现好物',
    heroButton: '立即探索',
    heroLink: '#/',
    heroInterval: 6,
    heroSlides: [],
    showCategories: true,
    showPicks: true,
    showMember: true,
    showCoupons: true,
    showActivities: true,
    showRecommendations: true,
    announcement: '',
    announcementLink: '#/coupons',
    footerText: '© 2026 LUMO Mall · 订单与资产由服务端保存 · 演示支付渠道',
    contactPhone: '',
    contactHours: '',
  };
  function normalize(raw) {
    const out = { ...defaults, heroSlides: [] };
    for (const key of Object.keys(defaults)) {
      if (typeof defaults[key] === 'string' && typeof raw?.[key] === 'string')
        out[key] = raw[key].slice(0, key === 'footerText' ? 160 : 120);
      if (typeof defaults[key] === 'boolean' && typeof raw?.[key] === 'boolean') out[key] = raw[key];
    }
    for (const key of ['heroLink', 'announcementLink']) if (!safeLink(out[key])) out[key] = defaults[key];
    out.heroInterval =
      Number.isInteger(raw?.heroInterval) &&
      (raw.heroInterval === 0 || (raw.heroInterval >= 3 && raw.heroInterval <= 30))
        ? raw.heroInterval
        : 6;
    out.heroSlides = Array.isArray(raw?.heroSlides)
      ? raw.heroSlides
          .slice(0, 4)
          .filter((slide) => slide && safeImage(slide.image) && safeLink(slide.link))
          .map((slide) => ({
            title: String(slide.title || '').slice(0, 60),
            subtitle: String(slide.subtitle || '').slice(0, 120),
            eyebrow: String(slide.eyebrow || '').slice(0, 80),
            image: slide.image,
            button: String(slide.button || '查看详情').slice(0, 20),
            link: slide.link,
          }))
      : [];
    return out;
  }
  let index = 0,
    signature = '',
    timer = null,
    paused = false;
  function hero(settings) {
    const config = normalize(settings);
    const slides = [
      {
        title: settings.heroTitle,
        subtitle: settings.heroSubtitle,
        eyebrow: settings.heroEyebrow,
        image: settings.heroImage,
        button: config.heroButton,
        link: config.heroLink,
      },
      ...config.heroSlides,
    ];
    const next = JSON.stringify(slides);
    if (next !== signature) {
      signature = next;
      index = 0;
    }
    index = Math.min(index, slides.length - 1);
    clearTimeout(timer);
    Promise.resolve().then(() => schedule(config.heroInterval, slides.length));
    return `<section class="hero configured-hero" aria-label="商城精选轮播" data-store-hero>${slides.map((slide, i) => `<div class="store-hero-slide" data-store-slide="${i}" ${i === index ? '' : 'hidden'}><div class="hero-copy"><span class="eyebrow">${esc(slide.eyebrow)}</span><h1>${esc(slide.title).replace(/\n/g, '<br>')}</h1><p>${esc(slide.subtitle)}</p>${slide.link === '#/' ? `<button class="primary" data-action="explore">${esc(slide.button)} →</button>` : `<a class="primary" href="${esc(slide.link)}">${esc(slide.button)} →</a>`}</div><img class="hero-image" src="${esc(asset(slide.image))}" alt="${esc(slide.title)}" ${i ? 'loading="lazy"' : ''}></div>`).join('')}${slides.length > 1 ? `<div class="store-hero-controls" role="group" aria-label="切换轮播图">${slides.map((_, i) => `<button type="button" data-store-slide-to="${i}" aria-label="第${i + 1}张轮播图" aria-pressed="${i === index}"></button>`).join('')}${config.heroInterval > 0 ? `<button type="button" data-store-pause aria-label="${paused ? '继续自动轮播' : '暂停自动轮播'}" aria-pressed="${paused}">${paused ? '▶' : 'Ⅱ'}</button>` : ''}</div>` : '<span class="home-hero-label">LUMO · 每日灵感</span>'}</section>`;
  }
  function move(next) {
    const root = document.querySelector('[data-store-hero]');
    if (!root) return;
    const slides = root.querySelectorAll('[data-store-slide]');
    index = (next + slides.length) % slides.length;
    slides.forEach((slide, i) => {
      slide.hidden = i !== index;
    });
    root
      .querySelectorAll('[data-store-slide-to]')
      .forEach((button, i) => button.setAttribute('aria-pressed', String(i === index)));
  }
  function schedule(seconds, total) {
    clearTimeout(timer);
    if (
      !seconds ||
      total < 2 ||
      !document.querySelector('[data-store-hero]') ||
      window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    )
      return;
    timer = setTimeout(() => {
      const root = document.querySelector('[data-store-hero]');
      if (!root) return;
      if (
        !paused &&
        document.visibilityState !== 'hidden' &&
        !root.contains(document.activeElement) &&
        !root.matches(':hover')
      )
        move(index + 1);
      schedule(seconds, total);
    }, seconds * 1000);
  }
  document.addEventListener('click', (event) => {
    const button = event.target.closest?.('[data-store-slide-to],[data-store-pause]');
    if (!button) return;
    if (button.hasAttribute('data-store-pause')) {
      paused = !paused;
      button.setAttribute('aria-pressed', String(paused));
      button.setAttribute('aria-label', paused ? '继续自动轮播' : '暂停自动轮播');
      button.textContent = paused ? '▶' : 'Ⅱ';
    } else move(Number(button.dataset.storeSlideTo));
  });
  window.LumoStorefrontLayout = { defaults, normalize, safeLink, safeImage, hero };
})();
