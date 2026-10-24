(function () {
  'use strict';
  const C = window.LumoConstants;
  const engine = () => window.LumoPromotions;
  const decode = (value) => {
    try {
      return decodeURIComponent(value);
    } catch (_) {
      return '';
    }
  };
  const labels = {
    [C.PromotionType.Flash]: '限时秒杀',
    [C.PromotionType.Group]: '2 人拼团',
    [C.PromotionType.Exchange]: '加价换购',
    [C.CampaignKind.Reduction]: '满额立减',
    [C.CampaignKind.Discount]: '折扣特惠',
    [C.CampaignKind.QuantityDiscount]: '多件优惠',
    [C.CampaignKind.Direct]: '限量直降',
  };
  const campaignTypes = [
    C.CampaignKind.Reduction,
    C.CampaignKind.Discount,
    C.CampaignKind.QuantityDiscount,
    C.CampaignKind.Direct,
  ];
  const campaignGlyphs = {
    [C.CampaignKind.Reduction]: 'ticket',
    [C.CampaignKind.Discount]: 'heart',
    [C.CampaignKind.QuantityDiscount]: 'bag',
    [C.CampaignKind.Direct]: 'coin',
  };
  const demoNote = '活动资格、库存与优惠以服务端报价和下单确认为准。模拟支付不产生真实扣款。';
  const selectedCart = () => (state.cart || []).filter((item) => item.selected);
  const yuan = (number) => `¥${money(Math.max(0, Number(number) || 0))}`;
  const PAGE_SIZE = 6;
  const pagingScope = () =>
    JSON.stringify([
      window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '',
      window.LumoAPI?.getToken?.() || '',
      window.LumoAPI?.getBase?.() || location.origin,
    ]);
  let pagingKey = '',
    pagingOwner = '',
    displayPage = 1,
    pageTotal = 0,
    pagingRevision = 0;
  function resetPaging() {
    pagingKey = '';
    pagingOwner = '';
    displayPage = 1;
    pageTotal = 0;
    pagingRevision++;
  }
  function beginPage(tab, session) {
    const owner = pagingScope(),
      key = JSON.stringify([tab, session, owner]);
    if (key !== pagingKey) {
      resetPaging();
      pagingKey = key;
      pagingOwner = owner;
    }
  }
  function pagination() {
    const pages = Math.max(1, Math.ceil(pageTotal / PAGE_SIZE));
    if (pages === 1) return '';
    const button = (page, label, content, disabled = false, current = false) =>
      `<button type="button" data-promo-page="${page}" data-promo-page-revision="${pagingRevision}" aria-label="${label}"${disabled ? ' disabled' : ''}${current ? ' class="active" aria-current="page"' : ''}>${content}</button>`;
    const visible = [
      ...new Set([1, displayPage - 1, displayPage, displayPage + 1, pages].filter((n) => n >= 1 && n <= pages)),
    ].sort((a, b) => a - b);
    let previous = 0;
    const numbers = visible
      .map((page) => {
        const gap = previous && page - previous > 1 ? '<span class="promo-page-gap" aria-hidden="true">…</span>' : '';
        previous = page;
        return gap + button(page, `第 ${page} 页活动商品`, String(page), false, page === displayPage);
      })
      .join('');
    return `<nav class="promo-pagination is-bottom" aria-label="活动商品分页（底部）">${button(displayPage - 1, '上一页活动商品', icon('back'), displayPage === 1)}${numbers}<span class="promo-page-count">第 <strong>${displayPage}</strong> / ${pages} 页</span>${button(displayPage + 1, '下一页活动商品', icon('chevron'), displayPage === pages)}</nav>`;
  }
  function pagedList(items, card, className = 'promo-grid', empty = '当前暂无活动商品。', groups = false) {
    pageTotal = items.length;
    displayPage = Math.max(1, Math.min(displayPage, Math.ceil(pageTotal / PAGE_SIZE) || 1));
    if (!pageTotal)
      return `<div class="promo-results-head"><p role="status">共 0 ${groups ? '个拼团' : '件好物'}</p></div><div class="promo-empty">${empty}</div>`;
    const start = (displayPage - 1) * PAGE_SIZE,
      shown = items.slice(start, start + PAGE_SIZE),
      unit = groups ? '个' : '件';
    return `<div class="promo-results-head"><p role="status">共 ${pageTotal} ${groups ? '个拼团' : '件好物'}<span> · 当前显示 ${start + 1}–${start + shown.length} ${unit}</span></p></div><div class="${className}">${shown.map(card).join('')}</div>${pagination()}`;
  }
  function timer(endAt) {
    if (!(Number(endAt) > 0)) return '以活动配置为准';
    const seconds = Math.max(0, Math.floor((Number(endAt) - Date.now()) / 1000));
    return [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60]
      .map((n) => String(n).padStart(2, '0'))
      .join(':');
  }
  const countdown = (endAt) =>
    `<span class="promo-clock" data-promo-countdown="${Number(endAt) || 0}">${timer(endAt)}</span>`;
  const ruleLink = (type) =>
    `<button class="promo-rule-link" data-promo-action="show-rules" data-type="${esc(type)}">活动规则 ${icon('chevron')}</button>`;
  function inactive(offer) {
    return offer.status !== C.PromotionOfferStatus.Active || offer.remaining <= 0 || offer.limitReached;
  }
  function flashButton(offer, detail = false) {
    const disabled = inactive(offer);
    const text =
      offer.status === C.PromotionOfferStatus.Upcoming
        ? '即将开抢'
        : offer.status === C.PromotionOfferStatus.Ended
          ? '本场已结束'
          : offer.limitReached
            ? '今日已抢购'
            : offer.remaining <= 0
              ? '本场已抢完'
              : detail
                ? '秒杀购买'
                : '立即抢购';
    return `<button class="primary promo-card-cta" data-promo-action="buy-flash" data-offer="${esc(offer.id)}" ${offer.status === C.PromotionOfferStatus.Upcoming ? 'data-session="' + C.PromotionSession.Tomorrow + '"' : ''} ${disabled ? 'disabled' : ''}>${text}${!disabled ? icon('arrow') : ''}</button>`;
  }
  function image(product, className = 'promo-card-image') {
    return `<a class="${className}" href="#/product/${esc(product.id)}" aria-label="查看${esc(product.name)}"><img src="${esc(asset(product.image))}" alt="${esc(product.name)}" loading="lazy"></a>`;
  }
  function pricing(offer, label) {
    return `<div class="promo-card-pricing"><span class="promo-card-price"><small>${esc(label || labels[offer.type] || '活动价')}</small>${yuan(offer.price)}</span>${offer.discount > 0 ? `<span class="promo-card-original">${yuan(offer.regularPrice)}</span><span class="promo-card-save">省 ¥${money(offer.discount)}</span>` : ''}</div>`;
  }
  function stock(offer) {
    const percent = Math.min(
      100,
      Math.max(0, offer.stock ? ((offer.stock - offer.remaining) / offer.stock) * 100 : 100),
    );
    return `<div class="promo-stock"><div class="promo-stock-track" aria-hidden="true"><i style="width:${percent}%"></i></div><span>剩余 ${Math.max(0, offer.remaining)} / ${Math.max(0, offer.stock)} 件</span></div>`;
  }
  function flashCard(offer) {
    const product = offer.product;
    return `<article class="promo-card is-flash">${image(product)}<span class="promo-card-label">${offer.status === C.PromotionOfferStatus.Upcoming ? '未来预告' : '当前秒杀'}</span><div class="promo-card-body"><a class="promo-card-title" href="#/product/${esc(product.id)}">${esc(product.name)}</a><p class="promo-card-sub">${esc(product.sub)}</p>${pricing(offer, '秒杀价')}${stock(offer)}<div class="promo-card-meta"><span>每个活动每人限 ${offer.limit || 1} 件</span>${offer.endAt > 0 || offer.startAt > 0 ? `<span>${offer.status === C.PromotionOfferStatus.Upcoming ? '距离开场' : '本场剩余'} ${countdown(offer.status === C.PromotionOfferStatus.Upcoming ? offer.startAt : offer.endAt)}</span>` : '<span>活动开放中</span>'}</div>${flashButton(offer)}</div></article>`;
  }
  function teamRow(team, offer, compact = false) {
    const joinable =
      team.status === C.GroupViewStatus.Waiting && team.occupied < team.required && Number(team.endAt) > Date.now();
    return `<div class="promo-group-row"><span class="promo-avatar">${esc(String(team.leader || '林').slice(0, 1))}</span><div class="promo-team-info"><span class="promo-team-name">${esc(team.leader || '好物伙伴')}${/团$/.test(team.leader || '') ? '' : team.mine ? '的拼团' : '的拼团'}</span><span class="promo-team-note">${joinable ? `还差 ${team.required - team.joined} 人 · ` + countdown(team.endAt) : team.status === C.GroupViewStatus.Formed ? '已成团' : '已结束'}</span></div>${joinable && !team.mine ? `<button class="secondary" data-promo-action="join-group" data-offer="${esc(offer.id)}" data-team="${esc(team.id)}">去参团</button>` : compact ? '' : `<span class="promo-order-status ${esc(team.status)}">${team.joined}/${team.required} 人</span>`}</div>`;
  }
  function groupCard(offer) {
    const teams = (engine().teams(offer.id) || []).filter(
      (team) => team.status === C.GroupViewStatus.Waiting && !team.mine && team.endAt > Date.now(),
    );
    return `<article class="promo-card is-group">${image(offer.product)}<span class="promo-card-label">${offer.teamSize || 2} 人成团</span><div class="promo-card-body"><a class="promo-card-title" href="#/product/${esc(offer.productId)}">${esc(offer.product.name)}</a><p class="promo-card-sub">一起选好物，分享好价格</p>${pricing(offer, '拼团价')}<div class="promo-card-meta"><span>30 分钟内成团</span>${ruleLink(C.PromotionType.Group)}</div>${teams[0] ? teamRow(teams[0], offer, true) : '<p class="promo-exchange-hint">当前暂无可参团的团，试试自己开团。</p>'}<button class="primary promo-card-cta" data-promo-action="create-group" data-offer="${esc(offer.id)}" ${offer.status !== C.PromotionOfferStatus.Active ? 'disabled' : ''}>发起拼团 ${icon('user')}</button></div></article>`;
  }
  function exchangeCard(offer, stateView, compact = false) {
    const selected = stateView.selectedId === offer.id;
    const allowed = stateView.eligible && !stateView.hasRestricted && offer.status === C.PromotionOfferStatus.Active;
    return `<article class="promo-exchange-card ${selected ? 'selected' : ''}">${image(offer.product, 'promo-exchange-image')}<div class="promo-exchange-info"><a class="promo-card-title" href="#/product/${esc(offer.productId)}">${esc(offer.product.name)}</a><div class="promo-exchange-pricing"><strong>+ ${yuan(offer.price)}</strong><del>${yuan(offer.regularPrice)}</del></div><span class="promo-card-sub">${compact ? '限换 1 件' : '达到门槛后，每单可任选 1 件'}</span></div><button class="${selected ? 'secondary' : 'outline'} promo-exchange-choose" data-promo-action="${selected ? 'remove-exchange' : 'select-exchange'}" ${selected ? '' : `data-offer="${esc(offer.id)}"`} ${!selected && !allowed ? 'disabled' : ''}>${selected ? '已选 · 移除' : allowed ? '选择换购' : '满额可换'}</button></article>`;
  }
  function campaignEntry(activity) {
    return `<a class="promo-more-entry is-${esc(activity.kind)}" href="#/promotions/${esc(activity.kind)}"><span>${icon(campaignGlyphs[activity.kind])}</span><div><strong>${esc(activity.name || labels[activity.kind])}</strong><small>${esc(engine().campaignRule(activity))}</small></div>${icon('arrow')}</a>`;
  }
  function campaignProducts(activity) {
    const P = engine();
    return pagedList(
      products.filter((p) => p.visible !== false),
      (product) => {
        const quantity = Math.max(
          1,
          Number(activity.min_quantity) || 1,
          Math.ceil(Number(activity.threshold_cents || 0) / Math.max(1, Math.round(product.price * 100))),
        );
        const stock = window.LumoInventory?.stockFor(product) || 0;
        const allowed =
          quantity <= 10 && stock >= quantity && Number(activity.quota) > Number(activity.held) + Number(activity.used);
        return `<article class="promo-card promo-campaign-product is-${esc(activity.kind)}">${image(product)}<span class="promo-card-label">${esc(labels[activity.kind])}</span><div class="promo-card-body"><a class="promo-card-title" href="#/product/${esc(product.id)}">${esc(product.name)}</a><p class="promo-card-sub">${esc(product.sub || product.category || '')}</p><div class="promo-campaign-product-price"><span>${yuan(product.price)}</span><small>商品原价 / 件</small></div><p class="promo-campaign-rule">${esc(P.campaignRule(activity))}</p><button class="primary promo-card-cta" data-promo-action="buy-campaign" data-campaign="${esc(activity.id)}" data-product="${esc(product.id)}" ${allowed ? '' : 'disabled'}>${allowed ? (quantity > 1 ? `选购 ${quantity} 件参加` : '活动购买') : '暂不可参加'}${allowed ? icon('arrow') : ''}</button><p class="promo-campaign-fineprint">按订单合计计算优惠，结算时确认应付</p></div></article>`;
      },
    );
  }
  function campaigns(items, placement = 'cart') {
    const P = engine(),
      activities = P?.campaigns?.() || [];
    if (
      !activities.length ||
      (items || []).some((item) => [C.PromotionType.Flash, C.PromotionType.Group].includes(item.promotion?.type))
    )
      return '';
    const state = P.campaignState(items);
    return `<section class="promo-campaign-selector in-${esc(placement)}" aria-label="选择商品活动"><div class="promo-campaign-selector-head"><div><span class="promo-kicker">MORE WAYS TO SAVE</span><h2>选一份合适的优惠</h2></div><span>每单任选一种</span></div><div class="promo-campaign-options">${activities
      .map((activity) => {
        const selected = state.selectedId === activity.id,
          eligibility = P.campaignEligibility(activity, items);
        return `<button type="button" class="promo-campaign-option is-${esc(activity.kind)} ${selected ? 'selected' : ''}" data-promo-action="${selected ? 'remove-campaign' : 'select-campaign'}" data-campaign="${esc(activity.id)}" aria-pressed="${selected}" ${!selected && !eligibility.eligible ? 'disabled' : ''}><span class="promo-campaign-option-icon">${icon(campaignGlyphs[activity.kind])}</span><span><strong>${esc(activity.name || labels[activity.kind])}</strong><small>${esc(P.campaignRule(activity))}</small><em>${selected ? '已选择 · 点击取消' : eligibility.eligible ? '点击使用' : esc(eligibility.reason)}</em></span>${selected ? icon('check') : ''}</button>`;
      })
      .join(
        '',
      )}</div><p class="promo-campaign-fineprint">${state.hasRestricted ? '换购与其他活动不可叠加，移除换购后可选择。' : '活动不与秒杀、拼团或换购叠加；符合条件的券和积分仍可使用。'}最终优惠由服务端报价确认。</p></section>`;
  }
  function campaignDetail(product, selection) {
    const P = engine(),
      activities = P?.campaigns?.() || [];
    if (!activities.length) return '';
    const items = [{ ...selection, id: product.id, qty: Number(selection?.qty) || 1 }];
    return `<section class="promo-campaign-detail" aria-label="更多商品活动"><div class="promo-campaign-selector-head"><h2>更多优惠活动</h2><span>按当前规格及数量</span></div><div class="promo-campaign-detail-options">${activities
      .map((activity) => {
        const eligibility = P.campaignEligibility(activity, items);
        return `<div class="promo-campaign-detail-row"><span class="promo-campaign-option-icon is-${esc(activity.kind)}">${icon(campaignGlyphs[activity.kind])}</span><div><strong>${esc(activity.name || labels[activity.kind])}</strong><small>${esc(P.campaignRule(activity))}</small></div><button class="outline" type="button" data-promo-action="buy-campaign" data-campaign="${esc(activity.id)}" data-product="${esc(product.id)}" data-detail="true" ${eligibility.eligible ? '' : 'disabled'}>${eligibility.eligible ? '活动购买' : esc(eligibility.reason)}</button></div>`;
      })
      .join(
        '',
      )}</div><p class="promo-campaign-fineprint">每单选择一种活动，具体优惠及资格以结算报价为准。</p></section>`;
  }
  function home() {
    const P = engine();
    if (!P) return '';
    const status = P.status?.();
    if (status && !status.loaded && !status.error && !status.hasData)
      return `<section class="promo-home" aria-label="商城优惠活动">${window.LumoSkeleton?.render('cards', { count: 3, label: '正在加载商城活动' }) || ''}</section>`;
    const entries = [
      [C.PromotionType.Flash, 'clock', '限时秒杀', '好价限时，好物不等人', 'DAILY FLASH DEALS'],
      [C.PromotionType.Group, 'user', '2 人拼团', '一起选好物，分享好价格', 'BETTER TOGETHER'],
      [C.PromotionType.Exchange, 'ticket', '满额换购', '满 ¥1,000，低价加一件', 'A LITTLE EXTRA JOY'],
    ];
    const picked = [
      P.flashOffers(C.PromotionSession.Today)[0],
      P.groupOffers()[0],
      P.exchangeOffers(selectedCart())[0],
    ].filter(Boolean);
    return `<section class="promo-home" aria-label="商城优惠活动"><div class="promo-home-grid">${entries.map(([type, glyph, title, sub, kicker]) => `<a class="promo-entry is-${type}" href="#/promotions/${type}"><span class="promo-entry-icon">${icon(glyph)}</span><div class="promo-entry-copy"><span class="promo-entry-label">${kicker}</span><h2 class="promo-entry-title">${title}</h2><p class="promo-entry-sub">${sub}</p></div><span class="promo-entry-arrow">${icon('arrow')}</span></a>`).join('')}</div>${P.campaigns?.().length ? `<div class="promo-more-grid" aria-label="更多优惠活动">${P.campaigns().map(campaignEntry).join('')}</div>` : ''}${picked.length ? `<div class="promo-home-featured">${picked.map((offer) => `<a class="promo-mini-card is-${offer.type}" href="#/promotions/${offer.type}"><span class="promo-mini-image"><img src="${esc(asset(offer.product.image))}" alt="" loading="lazy"></span><span class="promo-mini-copy"><small class="promo-kicker">${labels[offer.type]}</small><strong>${esc(offer.product.name)}</strong><span class="promo-mini-price">${yuan(offer.price)} ${offer.discount > 0 ? `<del>${yuan(offer.regularPrice)}</del>` : ''}</span></span>${icon('chevron')}</a>`).join('')}</div>` : ''}</section>`;
  }
  function exchange(baseItems, placement = 'cart') {
    const P = engine();
    if (!P) return '';
    const base = (baseItems || []).filter((item) => item.promotion?.type !== C.PromotionType.Exchange);
    const stateView = P.exchangeState(base);
    const offers = P.exchangeOffers(base);
    if (!offers.length)
      return placement === 'page' ? pagedList([], () => '', 'promo-exchange-grid', '当前暂无换购商品。') : '';
    const restricted =
      stateView.hasRestricted ||
      base.some((item) => [C.PromotionType.Flash, C.PromotionType.Group].includes(item.promotion?.type));
    if (restricted && placement === 'checkout') return '';
    const progress = Math.min(100, Math.max(0, (stateView.baseAmount / stateView.threshold) * 100));
    return `<section class="promo-exchange-box in-${esc(placement)}" aria-label="满额加价换购"><div class="promo-exchange-head"><div><span class="promo-kicker">A LITTLE EXTRA JOY</span><h2>满额换购 <span>好物再加一件</span></h2></div>${ruleLink(C.PromotionType.Exchange)}</div><div class="promo-exchange-status">${restricted ? '<strong>本单为秒杀或拼团订单，暂不参加换购</strong>' : stateView.eligible ? `<strong>${icon('check')}已满足 ¥${money(stateView.threshold)} 换购门槛</strong><span>每单任选 1 件，换购金额不计入门槛</span>` : `<strong>再购 ¥${money(stateView.remaining)}，即可低价换购</strong><span>已选普通商品 ¥${money(stateView.baseAmount)} / ¥${money(stateView.threshold)}</span>`}</div><div class="promo-exchange-progress" aria-hidden="true"><i style="width:${restricted ? 0 : progress}%"></i></div>${placement === 'page' ? pagedList(offers, (offer) => exchangeCard(offer, { ...stateView, hasRestricted: restricted }, true), 'promo-exchange-grid') : `<div class="promo-exchange-grid">${offers.map((offer) => exchangeCard(offer, { ...stateView, hasRestricted: restricted }, true)).join('')}</div>`}${stateView.selectedId ? '<p class="promo-exchange-selected">换购商品已加入本次结算，可在上方移除或更换。</p>' : '<p class="promo-exchange-hint">主商品优惠前金额满足门槛后可加价换购，券和积分以服务端报价为准。</p>'}</section>`;
  }
  function page(tab = C.PromotionType.Flash, session = C.PromotionSession.Today) {
    const P = engine();
    if (!P)
      return `${header(true)}<main class="container promo-page" id="main"><div class="promo-empty">活动准备中，请稍后再来。</div></main>${footer()}`;
    tab = [
      C.PromotionType.Flash,
      C.PromotionType.Group,
      C.PromotionType.Exchange,
      'mygroups',
      ...campaignTypes,
    ].includes(tab)
      ? tab
      : C.PromotionType.Flash;
    const requestedSession = session;
    const invitedTeam =
      tab === C.PromotionType.Group &&
      session &&
      ![C.PromotionSession.Today, C.PromotionSession.Tomorrow].includes(session)
        ? P.myGroups()
            .concat(P.groupOffers().flatMap((offer) => P.teams(offer.id)))
            .find((team) => team.id === decode(session))
        : null;
    session = session === C.PromotionSession.Tomorrow ? C.PromotionSession.Tomorrow : C.PromotionSession.Today;
    beginPage(tab, tab === C.PromotionType.Flash ? session : tab === C.PromotionType.Group ? requestedSession : '');
    let content = '';
    if (tab === C.PromotionType.Flash) {
      const offers = P.flashOffers(session);
      content = `<div class="promo-section-head"><div><h2>限时秒杀</h2><p>喜欢的好物，遇见心动的价格。</p></div>${ruleLink(C.PromotionType.Flash)}</div><div class="promo-session-tabs"><a class="${session === C.PromotionSession.Today ? 'active' : ''}" href="#/promotions/flash">当前场 <span>服务端活动</span></a><a class="${session === C.PromotionSession.Tomorrow ? 'active' : ''}" href="#/promotions/flash/tomorrow">未来场 <span>等待发布</span></a>${offers[0] && (offers[0].endAt > 0 || offers[0].startAt > 0) ? `<span class="promo-countdown">${icon('clock')}${session === C.PromotionSession.Today ? '距离结束' : '距离开抢'} ${countdown(session === C.PromotionSession.Today ? offers[0].endAt : offers[0].startAt)}</span>` : ''}</div>${pagedList(offers, flashCard, 'promo-grid', '本场好物暂未上架，稍后再来看看。')}`;
    } else if (tab === C.PromotionType.Group) {
      const offers = P.groupOffers();
      content = `<div class="promo-section-head"><div><h2>好物一起买</h2><p>2 人成团 · 30 分钟内邀请一位伙伴，共享拼团价。</p></div>${ruleLink(C.PromotionType.Group)}</div>${invitedTeam ? `<section class="promo-checkout-note"><strong>好友邀请你参团</strong>${groupPanel(invitedTeam)}${!invitedTeam.mine && invitedTeam.status === C.GroupViewStatus.Waiting ? `<button class="primary" data-promo-action="join-group" data-offer="${esc(invitedTeam.offerId)}" data-team="${esc(invitedTeam.id)}">加入这个拼团</button>` : ''}</section>` : ''}${pagedList(offers, groupCard, 'promo-grid', '当前暂无拼团商品。')}<div class="promo-checkout-note">${icon('info')}开团与参团后需完成服务端支付确认。两位不同用户支付成功，且订单资源确认后进入待发货。</div>`;
    } else if (tab === C.PromotionType.Exchange) {
      content = `<div class="promo-section-head"><div><h2>多一点心动，少一点花费</h2><p>普通商品满 ¥1,000，即可任选 1 件好物加价换购。</p></div><a class="outline" href="#/cart">查看购物车 ${icon('arrow')}</a></div>${exchange(selectedCart(), 'page')}`;
    } else if (campaignTypes.includes(tab)) {
      const activity = P.campaigns(tab)[0];
      content = activity
        ? `<div class="promo-section-head"><div><h2>${esc(activity.name || labels[tab])}</h2><p>${esc(activity.description || '多一点喜欢，少一点花费。')}</p></div>${ruleLink(tab)}</div><section class="promo-campaign-banner is-${esc(tab)}"><span>${icon(campaignGlyphs[tab])}</span><div><strong>${esc(P.campaignRule(activity))}</strong><p>普通商品均可参加 · 每单任选一种活动 · 每人每活动限一次</p></div><a class="outline" href="#/cart">购物车使用 ${icon('arrow')}</a></section>${campaignProducts(activity)}`
        : '<div class="promo-empty">本活动暂未开放，请稍后再来。</div>';
    } else {
      const groups = P.myGroups();
      content = `<div class="promo-section-head"><div><h2>我的拼团</h2><p>查看成团进度，和伙伴分享心动好物。</p></div><a class="outline" href="#/promotions/group">发现拼团好物 ${icon('arrow')}</a></div>${pagedList(groups, (team) => groupPanel(team), 'promo-my-groups', icon('user') + '<h3>还没有参与拼团</h3><p>挑一件喜欢的好物，和伙伴一起享好价。</p><a class="primary" href="#/promotions/group">去看看拼团商品</a>', true)}`;
    }
    const status = P.status?.();
    if (
      status &&
      (status.ready ? !status.ready[campaignTypes.includes(tab) ? 'campaign' : tab] : !status.loaded && !status.hasData)
    )
      content = status.error
        ? ''
        : window.LumoSkeleton?.render(tab === 'mygroups' ? 'rows' : 'products', {
            count: 6,
            label: '正在加载活动商品',
          }) || '';
    return `${header(true)}<main class="container promo-page" id="main"><div class="breadcrumb"><a href="#/">首页</a>${icon('chevron')}<span>好物福利社</span></div><section class="promo-hero"><div class="promo-hero-copy"><span class="promo-hero-badge">LUMO GOOD THINGS CLUB</span><h1>好物有约，心动有价</h1><p>秒杀、拼团、满减与多件好价，找到适合你的优惠。</p></div><div class="promo-hero-art" aria-hidden="true">${icon('ticket')}<span>GOOD THINGS<br>COME TOGETHER.</span></div></section><nav class="promo-tabs" aria-label="优惠活动">${[[C.PromotionType.Flash, 'clock', '限时秒杀'], [C.PromotionType.Group, 'user', '2 人拼团'], [C.PromotionType.Exchange, 'ticket', '满额换购'], ...campaignTypes.map((type) => [type, campaignGlyphs[type], labels[type]]), ['mygroups', 'bag', '我的拼团']].map(([key, glyph, name]) => `<a class="${tab === key ? 'active' : ''}" href="#/promotions/${key}">${icon(glyph)}${name}</a>`).join('')}</nav><section class="promo-content">${P.status?.().error ? `<div class="promo-checkout-note" role="alert">${esc(P.status().error)}<button class="outline" data-promo-action="refresh-promotions">重新加载活动</button></div>` : ''}${content}</section><p class="promo-footnote">${icon('info')}${demoNote}</p></main>${footer()}`;
  }
  function detailView(product, selection) {
    const P = engine();
    if (!P) return '';
    const offer = P.offerFor(C.PromotionType.Flash, product.id) || P.offerFor(C.PromotionType.Group, product.id);
    const additional = campaignDetail(product, selection);
    if (!offer) return additional;
    const regular = P.regularPrice({ ...selection, id: product.id });
    const chosenPrice = P.getPrice(
      { ...selection, id: product.id, promotion: { type: offer.type, offerId: offer.id } },
      regular,
    );
    const group = offer.type === C.PromotionType.Group;
    const waiting = group
      ? (P.teams(offer.id) || []).find(
          (team) => team.status === C.GroupViewStatus.Waiting && !team.mine && team.endAt > Date.now(),
        )
      : null;
    return `<section class="promo-detail is-${offer.type}" aria-label="${labels[offer.type]}活动"><div class="promo-detail-top"><span class="promo-detail-label">${icon(group ? 'user' : 'clock')}${labels[offer.type]}</span>${ruleLink(offer.type)}</div><div class="promo-detail-price"><strong>${yuan(chosenPrice)}</strong><span>活动指定规格价格</span>${!group ? `<small>剩余 ${offer.remaining} 件 · ${countdown(offer.endAt)}</small>` : '<small>2 人成团 · 30 分钟有效</small>'}</div><p class="promo-detail-rules">每个活动每人限购 1 件，最终应付金额以服务端报价为准。</p><div class="promo-detail-actions">${group ? `<button class="primary" data-promo-action="create-group" data-offer="${esc(offer.id)}" ${offer.status !== C.PromotionOfferStatus.Active ? 'disabled' : ''}>发起拼团</button>${waiting ? `<button class="secondary" data-promo-action="join-group" data-offer="${esc(offer.id)}" data-team="${esc(waiting.id)}">参与拼团</button>` : ''}` : flashButton(offer, true)}<a href="#/promotions/${offer.type}" class="promo-rule-link">查看活动 ${icon('arrow')}</a></div></section>${additional}`;
  }
  function itemBadge(item) {
    const type = item?.promotion?.type;
    return labels[type] ? `<span class="promo-item-badge is-${type}">${labels[type]}</span>` : '';
  }
  function checkout(items) {
    const chosen = engine()?.campaignState?.(items)?.activity;
    if (chosen)
      return `<div class="promo-checkout-note">${icon('ticket')}<span>本单参加${esc(chosen.name || labels[chosen.kind])}：${esc(engine().campaignRule(chosen))}。优惠按订单计算，最终金额以服务端报价为准。</span></div>`;
    const kinds = new Set((items || []).map((item) => item.promotion?.type).filter(Boolean));
    if (!kinds.size) return '';
    let copy = kinds.has(C.PromotionType.Group)
      ? '本单参加2人拼团。服务端确认收款后计入已支付人数，成团及资源确认后安排发货。'
      : kinds.has(C.PromotionType.Flash)
        ? '本单通过秒杀队列创建订单，库存与活动名额由后端预占及确认。'
        : '换购商品已计入订单，享换购专属价。普通商品仍可使用符合条件的优惠券与积分。';
    return `<div class="promo-checkout-note">${icon('ticket')}<span>${copy}</span></div>`;
  }
  function groupPanel(team, compact = false) {
    if (!team) return '';
    const product = team.product || products.find((p) => p.id === team.productId);
    const expired = team.status === C.GroupViewStatus.Expired;
    const formed = team.status === C.GroupViewStatus.Formed;
    const status = formed ? C.GroupViewStatus.Formed : expired ? C.GroupViewStatus.Expired : C.GroupViewStatus.Waiting;
    const missing = Math.max(0, team.required - team.joined);
    return `<section class="promo-order-panel ${compact ? 'is-compact' : ''}"><div class="promo-order-head"><div><span class="promo-kicker">BETTER TOGETHER</span><h3>${product ? esc(product.name) : '好物拼团'}</h3></div><span class="promo-order-status ${status}">${formed ? '已成团' : expired ? '拼团已结束' : '等待成团'}</span></div><div class="promo-order-progress"><div class="promo-order-avatars">${Array.from({ length: Math.max(1, Number(team.required) || 2) }, (_, i) => `<span class="${i < team.joined ? 'promo-avatar' : 'promo-avatar-gap'}">${i < team.joined ? (i === 0 ? '我' : '友') : '+'}</span>`).join('')}</div><div class="promo-order-copy"><strong>${team.joined} / ${team.required} 人${formed ? ' · 好物拼团成功' : expired ? ' · 本团未完成' : ` · 还差 ${missing} 位伙伴`}</strong><p>${formed ? '拼团已完成，可在订单中查看本次选购。' : expired ? '此团已结束，相关订单的关闭与退款进度请在订单及售后中查看。' : '邀请一位伙伴，享受一起买的好价格。'}</p>${!formed && !expired ? `<small>成团剩余 ${countdown(team.endAt)}</small>` : ''}</div></div>${!formed && !expired ? `<div class="promo-order-actions"><button class="primary" data-promo-action="copy-invite" data-team="${esc(team.id)}">复制邀请链接 ${icon('arrow')}</button></div>` : ''}<p class="promo-order-fineprint">${team.mine ? '本次拼团' : '商城拼团'} · 人数与结果来自服务端，${formed ? '订单资源确认后进入履约。' : '成团前不会进入履约。'}</p></section>`;
  }
  function success(order) {
    const P = engine();
    if (!P || !order) return '';
    const campaignKind = order.activityKind || order.quote?.activity_kind;
    if (campaignTypes.includes(campaignKind)) {
      const name = order.activityName || order.quote?.activity_name || labels[campaignKind];
      const discount = Number(order.quote?.campaign_discount_cents || 0) / 100;
      return `<div class="promo-checkout-note">${icon('check')}<span>${esc(name)}已随本单确认${discount > 0 ? `，活动优惠 ¥${money(discount)}` : ''}。<a href="#/promotions/${esc(campaignKind)}">查看活动 ${icon('arrow')}</a></span></div>`;
    }
    const team = P.groupForOrder(order);
    if (team) return groupPanel(team, true);
    const items = order.items || [];
    const flash = items.some((item) => item.promotion?.type === C.PromotionType.Flash);
    const exchanged = items.find((item) => item.promotion?.type === C.PromotionType.Exchange);
    if (!flash && !exchanged) return '';
    return `<div class="promo-checkout-note">${icon('check')}<span>${flash ? '秒杀好价已锁定，本次活动购买记录已保存。' : '换购商品已随本单一并购入，已享受专属换购价。'}<a href="#/promotions/${flash ? C.PromotionType.Flash : C.PromotionType.Exchange}">继续发现活动好物 ${icon('arrow')}</a></span></div>`;
  }
  document.addEventListener('click', (event) => {
    const button = event.target.closest?.('[data-promo-page]');
    if (!button || button.disabled || !/^#\/promotions(?:\/|$)/.test(location.hash)) return;
    event.preventDefault();
    if (pagingOwner !== pagingScope()) {
      resetPaging();
      if (typeof renderKeepScroll === 'function') renderKeepScroll();
      return;
    }
    if (button.dataset.promoPageRevision !== undefined && Number(button.dataset.promoPageRevision) !== pagingRevision)
      return;
    const requested = Number(button.dataset.promoPage);
    if (!Number.isSafeInteger(requested)) return;
    const next = Math.max(1, Math.min(requested, Math.ceil(pageTotal / PAGE_SIZE) || 1));
    if (next === displayPage) return;
    displayPage = next;
    if (typeof renderKeepScroll === 'function') renderKeepScroll();
    document.querySelector('.promo-results-head')?.scrollIntoView?.({ block: 'start', behavior: 'auto' });
  });
  window.addEventListener('hashchange', () => {
    if (!/^#\/promotions(?:\/|$)/.test(location.hash)) resetPaging();
  });
  for (const event of ['lumo:login', 'lumo:logout', 'lumo:session-expired'])
    window.addEventListener(event, resetPaging);
  window.LumoPromoView = {
    home,
    page,
    detail: detailView,
    exchange,
    campaigns,
    checkout,
    itemBadge,
    success,
    order: success,
  };
})();
