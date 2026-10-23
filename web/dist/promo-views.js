(function () {
  'use strict';
  const engine = () => window.LumoPromotions;
  const decode = value => { try { return decodeURIComponent(value); } catch (_) { return ''; } };
  const labels = { flash: '限时秒杀', group: '2 人拼团', exchange: '加价换购' };
  const demoNote = '活动资格、库存、成团与订单状态由后端确认。演示渠道须由管理员确认收款。';
  const selectedCart = () => (state.cart || []).filter(item => item.selected);
  const yuan = number => `¥${money(Math.max(0, Number(number) || 0))}`;
  function timer(endAt) {
    if (!(Number(endAt) > 0)) return '以活动配置为准';
    const seconds = Math.max(0, Math.floor((Number(endAt) - Date.now()) / 1000));
    return [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60].map(n => String(n).padStart(2, '0')).join(':');
  }
  const countdown = endAt => `<span class="promo-clock" data-promo-countdown="${Number(endAt) || 0}">${timer(endAt)}</span>`;
  const ruleLink = type => `<button class="promo-rule-link" data-promo-action="show-rules" data-type="${esc(type)}">活动规则 ${icon('chevron')}</button>`;
  function inactive(offer) { return offer.status !== 'active' || offer.remaining <= 0 || offer.limitReached; }
  function flashButton(offer, detail = false) {
    const disabled = inactive(offer);
    const text = offer.status === 'upcoming' ? '即将开抢' : offer.status === 'ended' ? '本场已结束' : offer.limitReached ? '今日已抢购' : offer.remaining <= 0 ? '本场已抢完' : detail ? '秒杀购买' : '立即抢购';
    return `<button class="primary promo-card-cta" data-promo-action="buy-flash" data-offer="${esc(offer.id)}" ${offer.status === 'upcoming' ? 'data-session="tomorrow"' : ''} ${disabled ? 'disabled' : ''}>${text}${!disabled ? icon('arrow') : ''}</button>`;
  }
  function image(product, className = 'promo-card-image') {
    return `<a class="${className}" href="#/product/${esc(product.id)}" aria-label="查看${esc(product.name)}"><img src="${esc(asset(product.image))}" alt="${esc(product.name)}" loading="lazy"></a>`;
  }
  function pricing(offer, label) {
    return `<div class="promo-card-pricing"><span class="promo-card-price"><small>${esc(label || labels[offer.type] || '活动价')}</small>${yuan(offer.price)}</span>${offer.discount > 0 ? `<span class="promo-card-original">${yuan(offer.regularPrice)}</span><span class="promo-card-save">省 ¥${money(offer.discount)}</span>` : ''}</div>`;
  }
  function stock(offer) {
    const percent = Math.min(100, Math.max(0, offer.stock ? (offer.stock - offer.remaining) / offer.stock * 100 : 100));
    return `<div class="promo-stock"><div class="promo-stock-track" aria-hidden="true"><i style="width:${percent}%"></i></div><span>剩余 ${Math.max(0, offer.remaining)} / ${Math.max(0, offer.stock)} 件</span></div>`;
  }
  function flashCard(offer) {
    const product = offer.product;
    return `<article class="promo-card is-flash">${image(product)}<span class="promo-card-label">${offer.status === 'upcoming' ? '未来预告' : '当前秒杀'}</span><div class="promo-card-body"><a class="promo-card-title" href="#/product/${esc(product.id)}">${esc(product.name)}</a><p class="promo-card-sub">${esc(product.sub)}</p>${pricing(offer, '秒杀价')}${stock(offer)}<div class="promo-card-meta"><span>每个活动每人限 ${offer.limit || 1} 件</span>${offer.endAt > 0 || offer.startAt > 0 ? `<span>${offer.status === 'upcoming' ? '距离开场' : '本场剩余'} ${countdown(offer.status === 'upcoming' ? offer.startAt : offer.endAt)}</span>` : '<span>活动开放中</span>'}</div>${flashButton(offer)}</div></article>`;
  }
  function teamRow(team, offer, compact = false) {
    const joinable = team.status === 'waiting' && team.occupied < team.required && Number(team.endAt) > Date.now();
    return `<div class="promo-group-row"><span class="promo-avatar">${esc(String(team.leader || '林').slice(0, 1))}</span><div class="promo-team-info"><span class="promo-team-name">${esc(team.leader || '好物伙伴')}${/团$/.test(team.leader || '') ? '' : team.mine ? '的拼团' : '的拼团'}</span><span class="promo-team-note">${joinable ? `还差 ${team.required - team.joined} 人 · ` + countdown(team.endAt) : team.status === 'formed' ? '已成团' : '已结束'}</span></div>${joinable && !team.mine ? `<button class="secondary" data-promo-action="join-group" data-offer="${esc(offer.id)}" data-team="${esc(team.id)}">去参团</button>` : compact ? '' : `<span class="promo-order-status ${esc(team.status)}">${team.joined}/${team.required} 人</span>`}</div>`;
  }
  function groupCard(offer) {
    const teams = (engine().teams(offer.id) || []).filter(team => team.status === 'waiting' && !team.mine && team.endAt > Date.now());
    return `<article class="promo-card is-group">${image(offer.product)}<span class="promo-card-label">${offer.teamSize || 2} 人成团</span><div class="promo-card-body"><a class="promo-card-title" href="#/product/${esc(offer.productId)}">${esc(offer.product.name)}</a><p class="promo-card-sub">一起选好物，分享好价格</p>${pricing(offer, '拼团价')}<div class="promo-card-meta"><span>30 分钟内成团</span>${ruleLink('group')}</div>${teams[0] ? teamRow(teams[0], offer, true) : '<p class="promo-exchange-hint">当前暂无可参团的团，试试自己开团。</p>'}<button class="primary promo-card-cta" data-promo-action="create-group" data-offer="${esc(offer.id)}" ${offer.status !== 'active' ? 'disabled' : ''}>发起拼团 ${icon('user')}</button></div></article>`;
  }
  function exchangeCard(offer, stateView, compact = false) {
    const selected = stateView.selectedId === offer.id;
    const allowed = stateView.eligible && !stateView.hasRestricted && offer.status === 'active';
    return `<article class="promo-exchange-card ${selected ? 'selected' : ''}">${image(offer.product, 'promo-exchange-image')}<div class="promo-exchange-info"><a class="promo-card-title" href="#/product/${esc(offer.productId)}">${esc(offer.product.name)}</a><div class="promo-exchange-pricing"><strong>+ ${yuan(offer.price)}</strong><del>${yuan(offer.regularPrice)}</del></div><span class="promo-card-sub">${compact ? '限换 1 件' : '达到门槛后，每单可任选 1 件'}</span></div><button class="${selected ? 'secondary' : 'outline'} promo-exchange-choose" data-promo-action="${selected ? 'remove-exchange' : 'select-exchange'}" ${selected ? '' : `data-offer="${esc(offer.id)}"`} ${!selected && !allowed ? 'disabled' : ''}>${selected ? '已选 · 移除' : allowed ? '选择换购' : '满额可换'}</button></article>`;
  }
  function home() {
    const P = engine(); if (!P) return '';
    const entries = [
      ['flash', 'clock', '限时秒杀', '好价限时，好物不等人', 'DAILY FLASH DEALS'],
      ['group', 'user', '2 人拼团', '一起选好物，分享好价格', 'BETTER TOGETHER'],
      ['exchange', 'ticket', '满额换购', '满 ¥1,000，低价加一件', 'A LITTLE EXTRA JOY']
    ];
    const picked = [P.flashOffers('today')[0], P.groupOffers()[0], P.exchangeOffers(selectedCart())[0]].filter(Boolean);
    return `<section class="promo-home" aria-label="商城优惠活动"><div class="promo-home-grid">${entries.map(([type, glyph, title, sub, kicker]) => `<a class="promo-entry is-${type}" href="#/promotions/${type}"><span class="promo-entry-icon">${icon(glyph)}</span><div class="promo-entry-copy"><span class="promo-entry-label">${kicker}</span><h2 class="promo-entry-title">${title}</h2><p class="promo-entry-sub">${sub}</p></div><span class="promo-entry-arrow">${icon('arrow')}</span></a>`).join('')}</div>${picked.length ? `<div class="promo-home-featured">${picked.map(offer => `<a class="promo-mini-card is-${offer.type}" href="#/promotions/${offer.type}"><span class="promo-mini-image"><img src="${esc(asset(offer.product.image))}" alt="" loading="lazy"></span><span class="promo-mini-copy"><small class="promo-kicker">${labels[offer.type]}</small><strong>${esc(offer.product.name)}</strong><span class="promo-mini-price">${yuan(offer.price)} ${offer.discount > 0 ? `<del>${yuan(offer.regularPrice)}</del>` : ''}</span></span>${icon('chevron')}</a>`).join('')}</div>` : ''}</section>`;
  }
  function exchange(baseItems, placement = 'cart') {
    const P = engine(); if (!P) return '';
    const base = (baseItems || []).filter(item => item.promotion?.type !== 'exchange');
    const stateView = P.exchangeState(base);
    const offers = P.exchangeOffers(base);
    if (!offers.length) return '';
    const restricted = stateView.hasRestricted || base.some(item => ['flash', 'group'].includes(item.promotion?.type));
    if (restricted && placement === 'checkout') return '';
    const progress = Math.min(100, Math.max(0, stateView.baseAmount / stateView.threshold * 100));
    return `<section class="promo-exchange-box in-${esc(placement)}" aria-label="满额加价换购"><div class="promo-exchange-head"><div><span class="promo-kicker">A LITTLE EXTRA JOY</span><h2>满额换购 <span>好物再加一件</span></h2></div>${ruleLink('exchange')}</div><div class="promo-exchange-status">${restricted ? '<strong>本单为秒杀或拼团订单，暂不参加换购</strong>' : stateView.eligible ? `<strong>${icon('check')}已满足 ¥${money(stateView.threshold)} 换购门槛</strong><span>每单任选 1 件，换购金额不计入门槛</span>` : `<strong>再购 ¥${money(stateView.remaining)}，即可低价换购</strong><span>已选普通商品 ¥${money(stateView.baseAmount)} / ¥${money(stateView.threshold)}</span>`}</div><div class="promo-exchange-progress" aria-hidden="true"><i style="width:${restricted ? 0 : progress}%"></i></div><div class="promo-exchange-grid">${offers.map(offer => exchangeCard(offer, { ...stateView, hasRestricted: restricted }, true)).join('')}</div>${stateView.selectedId ? '<p class="promo-exchange-selected">换购商品已加入本次结算，可在上方移除或更换。</p>' : '<p class="promo-exchange-hint">主商品优惠前金额满足门槛后可加价换购，券和积分以服务端报价为准。</p>'}</section>`;
  }
  function page(tab = 'flash', session = 'today') {
    const P = engine();
    if (!P) return `${header()}<main class="container promo-page" id="main"><div class="promo-empty">活动准备中，请稍后再来。</div></main>${footer()}`;
    tab = ['flash', 'group', 'exchange', 'mygroups'].includes(tab) ? tab : 'flash';
    const invitedTeam = tab === 'group' && session && !['today','tomorrow'].includes(session) ? P.myGroups().concat(P.groupOffers().flatMap(offer => P.teams(offer.id))).find(team => team.id === decode(session)) : null;
    session = session === 'tomorrow' ? 'tomorrow' : 'today';
    let content = '';
    if (tab === 'flash') {
      const offers = P.flashOffers(session);
      content = `<div class="promo-section-head"><div><h2>限时秒杀</h2><p>喜欢的好物，遇见心动的价格。</p></div>${ruleLink('flash')}</div><div class="promo-session-tabs"><a class="${session === 'today' ? 'active' : ''}" href="#/promotions/flash">当前场 <span>服务端活动</span></a><a class="${session === 'tomorrow' ? 'active' : ''}" href="#/promotions/flash/tomorrow">未来场 <span>等待发布</span></a>${offers[0] && (offers[0].endAt > 0 || offers[0].startAt > 0) ? `<span class="promo-countdown">${icon('clock')}${session === 'today' ? '距离结束' : '距离开抢'} ${countdown(session === 'today' ? offers[0].endAt : offers[0].startAt)}</span>` : ''}</div>${offers.length ? `<div class="promo-grid">${offers.map(flashCard).join('')}</div>` : '<div class="promo-empty">本场好物暂未上架，稍后再来看看。</div>'}`;
    } else if (tab === 'group') {
      const offers = P.groupOffers();
      content = `<div class="promo-section-head"><div><h2>好物一起买</h2><p>2 人成团 · 30 分钟内邀请一位伙伴，共享拼团价。</p></div>${ruleLink('group')}</div>${invitedTeam ? `<section class="promo-checkout-note"><strong>好友邀请你参团</strong>${groupPanel(invitedTeam)}${!invitedTeam.mine && invitedTeam.status === 'waiting' ? `<button class="primary" data-promo-action="join-group" data-offer="${esc(invitedTeam.offerId)}" data-team="${esc(invitedTeam.id)}">加入这个拼团</button>` : ''}</section>` : ''}${offers.length ? `<div class="promo-grid">${offers.map(groupCard).join('')}</div>` : '<div class="promo-empty">当前暂无拼团商品。</div>'}<div class="promo-checkout-note">${icon('info')}开团与参团后需完成服务端支付确认。两位不同用户支付成功，且订单资源确认后进入待发货。</div>`;
    } else if (tab === 'exchange') {
      content = `<div class="promo-section-head"><div><h2>多一点心动，少一点花费</h2><p>普通商品满 ¥1,000，即可任选 1 件好物加价换购。</p></div><a class="outline" href="#/cart">查看购物车 ${icon('arrow')}</a></div>${exchange(selectedCart(), 'page')}`;
    } else {
      const groups = P.myGroups();
      content = `<div class="promo-section-head"><div><h2>我的拼团</h2><p>查看成团进度，和伙伴分享心动好物。</p></div><a class="outline" href="#/promotions/group">发现拼团好物 ${icon('arrow')}</a></div>${groups.length ? `<div class="promo-my-groups">${groups.map(team => groupPanel(team)).join('')}</div>` : '<div class="promo-empty">' + icon('user') + '<h3>还没有参与拼团</h3><p>挑一件喜欢的好物，和伙伴一起享好价。</p><a class="primary" href="#/promotions/group">去看看拼团商品</a></div>'}`;
    }
    return `${header()}<main class="container promo-page" id="main"><div class="breadcrumb"><a href="#/">首页</a>${icon('chevron')}<span>好物福利社</span></div><section class="promo-hero"><div class="promo-hero-copy"><span class="promo-hero-badge">LUMO GOOD THINGS CLUB</span><h1>好物有约，心动有价</h1><p>限时好价、好友拼团与满额礼遇，让日常多一份惊喜。</p></div><div class="promo-hero-art" aria-hidden="true">${icon('ticket')}<span>GOOD THINGS<br>COME TOGETHER.</span></div></section><nav class="promo-tabs" aria-label="优惠活动">${[['flash', 'clock', '限时秒杀'], ['group', 'user', '2 人拼团'], ['exchange', 'ticket', '满额换购'], ['mygroups', 'bag', '我的拼团']].map(([key, glyph, name]) => `<a class="${tab === key ? 'active' : ''}" href="#/promotions/${key}">${icon(glyph)}${name}</a>`).join('')}</nav><section class="promo-content">${P.status?.().error ? `<div class="promo-checkout-note" role="alert">${esc(P.status().error)}<button class="outline" data-promo-action="refresh-promotions">重新加载活动</button></div>` : !P.status?.().loaded ? '<div class="promo-checkout-note" role="status">正在获取活动信息…</div>' : ''}${content}</section><p class="promo-footnote">${icon('info')}${demoNote}</p></main>${footer()}`;
  }
  function detailView(product, selection) {
    const P = engine(); if (!P) return '';
    const offer = P.offerFor('flash', product.id) || P.offerFor('group', product.id);
    if (!offer) return '';
    const regular = P.regularPrice({ ...selection, id: product.id });
    const chosenPrice = P.getPrice({ ...selection, id: product.id, promotion: { type: offer.type, offerId: offer.id } }, regular);
    const group = offer.type === 'group';
    const waiting = group ? (P.teams(offer.id) || []).find(team => team.status === 'waiting' && !team.mine && team.endAt > Date.now()) : null;
    return `<section class="promo-detail is-${offer.type}" aria-label="${labels[offer.type]}活动"><div class="promo-detail-top"><span class="promo-detail-label">${icon(group ? 'user' : 'clock')}${labels[offer.type]}</span>${ruleLink(offer.type)}</div><div class="promo-detail-price"><strong>${yuan(chosenPrice)}</strong><span>活动指定规格价格</span>${!group ? `<small>剩余 ${offer.remaining} 件 · ${countdown(offer.endAt)}</small>` : '<small>2 人成团 · 30 分钟有效</small>'}</div><p class="promo-detail-rules">每个活动每人限购 1 件，最终应付金额以服务端报价为准。</p><div class="promo-detail-actions">${group ? `<button class="primary" data-promo-action="create-group" data-offer="${esc(offer.id)}" ${offer.status !== 'active' ? 'disabled' : ''}>发起拼团</button>${waiting ? `<button class="secondary" data-promo-action="join-group" data-offer="${esc(offer.id)}" data-team="${esc(waiting.id)}">参与拼团</button>` : ''}` : flashButton(offer, true)}<a href="#/promotions/${offer.type}" class="promo-rule-link">查看活动 ${icon('arrow')}</a></div></section>`;
  }
  function itemBadge(item) {
    const type = item?.promotion?.type;
    return labels[type] ? `<span class="promo-item-badge is-${type}">${labels[type]}</span>` : '';
  }
  function checkout(items) {
    const kinds = new Set((items || []).map(item => item.promotion?.type).filter(Boolean));
    if (!kinds.size) return '';
    let copy = kinds.has('group') ? '本单参加2人拼团。服务端确认收款后计入已支付人数，成团及资源确认后安排发货。' : kinds.has('flash') ? '本单通过秒杀队列创建订单，库存与活动名额由后端预占及确认。' : '换购商品已计入订单，享换购专属价。普通商品仍可使用符合条件的优惠券与积分。';
    return `<div class="promo-checkout-note">${icon('ticket')}<span>${copy}</span></div>`;
  }
  function groupPanel(team, compact = false) {
    if (!team) return '';
    const product = team.product || products.find(p => p.id === team.productId);
    const expired = team.status === 'expired';
    const formed = team.status === 'formed';
    const status = formed ? 'formed' : expired ? 'expired' : 'waiting';
    const missing = Math.max(0, team.required - team.joined);
    return `<section class="promo-order-panel ${compact ? 'is-compact' : ''}"><div class="promo-order-head"><div><span class="promo-kicker">BETTER TOGETHER</span><h3>${product ? esc(product.name) : '好物拼团'}</h3></div><span class="promo-order-status ${status}">${formed ? '已成团' : expired ? '拼团已结束' : '等待成团'}</span></div><div class="promo-order-progress"><div class="promo-order-avatars">${Array.from({ length: Math.max(1, Number(team.required) || 2) }, (_, i) => `<span class="${i < team.joined ? 'promo-avatar' : 'promo-avatar-gap'}">${i < team.joined ? i === 0 ? '我' : '友' : '+'}</span>`).join('')}</div><div class="promo-order-copy"><strong>${team.joined} / ${team.required} 人${formed ? ' · 好物拼团成功' : expired ? ' · 本团未完成' : ` · 还差 ${missing} 位伙伴`}</strong><p>${formed ? '拼团已完成，可在订单中查看本次选购。' : expired ? '此团已结束，相关订单的关闭与退款进度请在订单及售后中查看。' : '邀请一位伙伴，享受一起买的好价格。'}</p>${!formed && !expired ? `<small>成团剩余 ${countdown(team.endAt)}</small>` : ''}</div></div>${!formed && !expired ? `<div class="promo-order-actions"><button class="primary" data-promo-action="copy-invite" data-team="${esc(team.id)}">复制邀请链接 ${icon('arrow')}</button></div>` : ''}<p class="promo-order-fineprint">${team.mine ? '本次拼团' : '商城拼团'} · 人数与结果来自服务端，${formed ? '订单资源确认后进入履约。' : '成团前不会进入履约。'}</p></section>`;
  }
  function success(order) {
    const P = engine(); if (!P || !order) return '';
    const team = P.groupForOrder(order);
    if (team) return groupPanel(team, true);
    const items = order.items || [];
    const flash = items.some(item => item.promotion?.type === 'flash');
    const exchanged = items.find(item => item.promotion?.type === 'exchange');
    if (!flash && !exchanged) return '';
    return `<div class="promo-checkout-note">${icon('check')}<span>${flash ? '秒杀好价已锁定，本次活动购买记录已保存。' : '换购商品已随本单一并购入，已享受专属换购价。'}<a href="#/promotions/${flash ? 'flash' : 'exchange'}">继续发现活动好物 ${icon('arrow')}</a></span></div>`;
  }
  window.LumoPromoView = { home, page, detail: detailView, exchange, checkout, itemBadge, success, order: success };
})();
