(function () {
  'use strict';
  const C = window.LumoConstants;
  const pageNames = {
    overview: '个人中心',
    orders: '我的订单',
    addresses: '地址管理',
    coupons: '优惠券',
    favorites: '我的收藏',
    history: '浏览足迹',
    points: '我的积分',
    aftersales: '售后服务',
    settings: '账户设置',
    messages: '消息中心',
    invoices: '发票中心',
    support: '客服中心',
  };
  const orderTabs = [
    ['all', '全部订单'],
    [C.OrderViewStatus.Pending, '待付款'],
    [C.OrderViewStatus.Group, '待成团'],
    [C.OrderViewStatus.Shipping, '待发货'],
    [C.OrderViewStatus.Receiving, '待收货'],
    [C.OrderViewStatus.Review, '待评价'],
    [C.OrderViewStatus.Completed, '已完成'],
    [C.OrderViewStatus.Refunded, '退款完成'],
    [C.OrderViewStatus.Closed, '已关闭'],
  ];
  let orderQuery = '';
  const member = () => window.LumoMember;
  const model = () => member().model();
  const date = (value, time = false) =>
    value
      ? new Date(value).toLocaleString('zh-CN', {
          timeZone: 'Asia/Shanghai',
          year: 'numeric',
          month: '2-digit',
          day: '2-digit',
          ...(time ? { hour: '2-digit', minute: '2-digit', hour12: false } : {}),
        })
      : '—';
  const safeProduct = (id) => products.find((p) => p.id === id);
  const smallLink = (href, label) => `<a class="mem-text-button" href="${href}">${esc(label)}${icon('chevron')}</a>`;
  const button = (label, action, id = '', primary = false, extra = '') =>
    `<button class="${primary ? 'mem-primary' : 'mem-outline'}" data-member-action="${esc(action)}" ${id ? `data-id="${esc(id)}"` : ''} ${extra}>${esc(label)}</button>`;
  const skeleton = (type, options = {}) => window.LumoSkeleton?.render(type, options) || '';
  const resourceState = (key) =>
    (key === 'orders' ? window.LumoCommerce?.readState?.('orders') : member()?.readState?.(key)) || {
      loading: false,
      loaded: true,
      error: '',
    };
  const readDependencies = {
    overview: ['profile', 'points', 'coupons', 'favorites', 'history', 'addresses', 'aftersales', 'orders'],
    orders: ['orders'],
    addresses: ['addresses'],
    coupons: ['coupons'],
    favorites: ['favorites'],
    history: ['history'],
    points: ['points'],
    aftersales: ['aftersales'],
    settings: ['profile'],
  };
  function resourceError(resource, error) {
    return `<section class="mem-panel" role="alert"><p>${esc(error || '账户信息暂未加载成功，请重试。')}</p><button class="mem-outline" data-member-action="retry-read" data-value="${resource === 'orders' ? 'orders' : 'member'}">重新加载</button></section>`;
  }
  function loadingContent(tab) {
    const options = { label: pageNames[tab] + '加载中' };
    if (tab === 'overview')
      return (
        skeleton('profile', { ...options, count: 1 }) +
        skeleton('summary', { count: 4 }) +
        skeleton('rows', { count: 3 })
      );
    if (['favorites', 'history'].includes(tab)) return skeleton('products', { ...options, count: 6 });
    if (tab === 'coupons') return skeleton('coupons', { ...options, count: 6 });
    if (tab === 'addresses') return skeleton('cards', { ...options, count: 2 });
    if (tab === 'settings') return skeleton('form', { ...options, count: 1 });
    if (tab === 'points') return skeleton('summary', { ...options, count: 1 }) + skeleton('rows', { count: 4 });
    return skeleton('rows', { ...options, count: 3 });
  }
  function guardedView(tab, renderView) {
    const keys = readDependencies[tab];
    if (!keys) return renderView();
    const states = keys.map((key) => ({ key, ...resourceState(key) })),
      unloaded = states.filter((value) => !value.loaded);
    if (unloaded.length) {
      const pending = unloaded.some((value) => !value.error),
        failed = unloaded.find((value) => value.error);
      return (
        pageHead(pageNames[tab], '正在同步服务端信息，稍候即可查看。') +
        (failed ? resourceError(failed.key, failed.error) : '') +
        (pending ? loadingContent(tab) : '')
      );
    }
    const failed = states.find((value) => value.error);
    return (failed ? resourceError(failed.key, failed.error) : '') + renderView();
  }
  const initials = (value) =>
    String(value || 'L')
      .trim()
      .slice(0, 1)
      .toUpperCase();
  const mask = (value) =>
    /^1[3-9]\d{9}$/.test(value || '') ? value.slice(0, 3) + '****' + value.slice(-4) : value || '未填写';
  function emptyState(title, description, href = '#/', label = '去逛逛', symbol = 'bag', compact = false) {
    return `<div class="mem-empty ${compact ? 'mem-empty-inline' : ''}"><span class="mem-empty-icon">${icon(symbol)}</span><h3>${esc(title)}</h3><p>${esc(description)}</p>${href ? `<a class="mem-outline" href="${href}">${esc(label)}${icon('arrow')}</a>` : ''}</div>`;
  }
  function sidebar(tab, data, counts) {
    const groups = [
      [
        '我的商城',
        [
          ['overview', 'user', '会员首页'],
          ['orders', 'bag', '我的订单'],
          ['messages', 'bell', '消息中心'],
          ['aftersales', 'return', '退款 / 售后'],
        ],
      ],
      [
        '我的资产',
        [
          ['coupons', 'ticket', '优惠券'],
          ['points', 'coin', '我的积分'],
          ['favorites', 'heart', '我的收藏'],
          ['history', 'clock', '浏览足迹'],
        ],
      ],
      [
        '账户管理',
        [
          ['addresses', 'pin', '收货地址'],
          ['invoices', 'edit', '发票中心'],
          ['support', 'support', '客服中心'],
          ['settings', 'shield', '账户设置'],
        ],
      ],
    ];
    const profileState = resourceState('profile'),
      identity = profileState.loaded
        ? `<span class="mem-avatar mem-avatar-small">${esc(initials(data.profile.nickname))}</span><span><strong>${esc(data.profile.nickname)}</strong><small>${esc(data.profile.level || 'LUMO 生活会员')}</small></span>`
        : profileState.error
          ? '<span>账户资料暂不可用</span>'
          : skeleton('rows', { count: 1, label: '正在加载会员资料', className: 'sk-member-identity' });
    return `<aside class="mem-sidebar"><a class="mem-sidebar-user" href="#/member/overview">${identity}</a><nav class="mem-side-nav" aria-label="个人中心导航">${groups.map(([label, links]) => `<div class="mem-nav-group"><span>${label}</span>${links.map(([id, symbol, title]) => `<a href="#/member/${id}" class="${tab === id ? 'active' : ''}" ${tab === id ? 'aria-current="page"' : ''}>${icon(symbol)}${title}${['coupons', 'favorites'].includes(id) && counts[id] ? `<small class="mem-count">${counts[id]}</small>` : ''}</a>`).join('')}</div>`).join('')}</nav><a class="mem-sidebar-shop" href="#/">${icon('arrow')}继续发现好物</a></aside>`;
  }
  function pageHead(title, description, action = '') {
    return `<div class="mem-pagehead"><div><span class="mem-kicker">MY LUMO, MY EVERYDAY</span><h1>${esc(title)}</h1><p>${esc(description)}</p></div>${action}</div>`;
  }
  function miniProducts(ids, limit = 4) {
    return `<div class="mem-preview-grid">${ids
      .map((id) => safeProduct(id))
      .filter((p) => p && p.visible !== false)
      .slice(0, limit)
      .map(
        (p) =>
          `<a class="mem-mini-product" href="#/product/${esc(p.id)}"><span class="mem-mini-image ${['iphone', 'macbook'].includes(p.id) ? 'mem-official-image' : ''}"><img src="${esc(asset(p.image))}" alt="${esc(p.name)}" loading="lazy"></span><strong>${esc(p.name)}</strong>${price(p.price)}</a>`,
      )
      .join('')}</div>`;
  }
  function orderImage(item) {
    const p = safeProduct(item.id);
    return `<span class="mem-order-image ${item.id === 'iphone' ? 'mem-phone-image' : ''}"><img src="${esc(asset(item.image || p?.image || 'hero.jpg'))}" alt="${esc(item.name || p?.name || '商品')}" loading="lazy"></span>`;
  }
  function recentOrder(order) {
    const first = order.items[0],
      p = safeProduct(first?.id);
    if (!first) return '';
    const status = member().orderState(order);
    return `<div class="mem-recent-order">${orderImage(first)}<div class="mem-recent-copy"><h3>${esc(first.name || p?.name || '精选好物')}${order.items.length > 1 ? `<small> 等 ${order.items.length} 款</small>` : ''}</h3><p>${date(order.createdAt || order.paidAt)} · ${esc(order.id)}</p><span class="mem-order-state">${esc(member().orderLabel(order))}</span></div><div class="mem-recent-action">${price(order.totals.total)}${status === C.OrderViewStatus.Pending ? `<button data-member-action="resume-payment" data-id="${esc(order.id)}">继续付款</button>` : status === C.OrderViewStatus.Closed && !order.paidAt ? '<a href="#/member/orders/closed">查看订单</a>' : `<button data-action="order-details" data-id="${esc(order.id)}">查看详情</button>`}</div></div>`;
  }
  function overview(data, counts) {
    const hour = Number(
      new Intl.DateTimeFormat('zh-CN', { hour: 'numeric', hour12: false, timeZone: 'Asia/Shanghai' }).format(
        new Date(),
      ),
    );
    const greeting = hour < 11 ? '早上好' : hour < 14 ? '中午好' : hour < 18 ? '下午好' : '晚上好';
    const recent = member().listOrders('all').slice(0, 3);
    const favorites = state.favorites.filter((id) => safeProduct(id)?.visible !== false && safeProduct(id));
    const history = data.history.filter((row) => safeProduct(row.id)?.visible !== false && safeProduct(row.id));
    const statusLinks = [
      [C.OrderViewStatus.Pending, 'clock', '待付款'],
      [C.OrderViewStatus.Shipping, 'bag', '待发货'],
      [C.OrderViewStatus.Receiving, 'truck', '待收货'],
      [C.OrderViewStatus.Review, 'edit', '待评价'],
      [C.OrderViewStatus.Group, 'layers', '待成团'],
      ['aftersale', 'return', '退款 / 售后'],
    ];
    return `<section class="mem-hero"><div class="mem-hero-profile"><span class="mem-avatar">${esc(initials(data.profile.nickname))}</span><div><span class="mem-kicker">WELCOME TO YOUR EVERYDAY</span><h1>${greeting}，${esc(data.profile.nickname)}</h1><p>把喜欢的日常，慢慢装进生活里。</p><button class="mem-hero-edit" data-member-action="edit-profile">编辑个人资料${icon('edit')}</button></div></div><div class="mem-hero-actions"><span class="mem-badge mem-gold-badge">${icon('sparkles')}${esc(data.profile.level || 'LUMO 生活会员')}</span><button class="mem-primary" data-member-action="signin" ${data.signedToday ? 'disabled' : ''}>${icon(data.signedToday ? 'check' : 'coin')}${data.signedToday ? '今日已签到' : '签到领 20 积分'}</button><small>最近签到 ${data.signInDays} 天</small></div></section>
      <section class="mem-panel mem-wallet" aria-label="我的资产">${[
        ['coupons', 'ticket', '可用优惠券', counts.coupons, '张好券，留给心动时刻'],
        ['points', 'coin', '可用积分', data.points, '100 积分可抵 ¥1'],
        ['favorites', 'heart', '收藏好物', counts.favorites, '每一次心动都值得收藏'],
        ['history', 'clock', '浏览足迹', counts.history, '找回刚刚喜欢的那一件'],
      ]
        .map(
          ([route, symbol, label, value, caption]) =>
            `<a class="mem-wallet-item" href="#/member/${route}"><span class="mem-wallet-icon">${icon(symbol)}</span><div><span>${label}</span><strong>${money(value)}<small>${route === 'coupons' ? '张' : route === 'points' ? '分' : '件'}</small></strong><p>${caption}</p></div>${icon('chevron')}</a>`,
        )
        .join('')}</section>
      <section class="mem-panel"><div class="mem-section-head"><h2>我的订单</h2>${smallLink('#/member/orders', '全部订单')}</div><div class="mem-status-grid">${statusLinks.map(([id, symbol, label]) => `<a class="mem-status-item" href="#/member/${id === 'aftersale' ? 'aftersales' : 'orders/' + id}"><span>${icon(symbol)}${counts[id] ? `<i>${counts[id]}</i>` : ''}</span><strong>${label}</strong></a>`).join('')}</div></section>
      <div class="mem-dashboard-columns"><section class="mem-panel"><div class="mem-section-head"><h2>最近订单</h2>${smallLink('#/member/orders', '订单管理')}</div>${recent.length ? recent.map(recentOrder).join('') : emptyState('第一份美好，等你开启', '选一件心仪好物，开启你的品质生活。', '#/', '发现好物', 'bag', true)}</section><div class="mem-stack"><section class="mem-member-card"><span class="mem-kicker">LUMO MEMBERSHIP</span><h2>让每一份喜欢<br>都有专属礼遇</h2><p>${esc(data.profile.level || '生活会员')} · 累计已省 ¥${money(data.totalSaved || 0)}</p><div class="mem-benefit-row"><span>${icon('ticket')}会员好券</span><span>${icon('coin')}积分回馈</span><span>${icon('support')}贴心服务</span></div><button data-member-action="membership">查看会员权益${icon('arrow')}</button></section><section class="mem-panel"><div class="mem-section-head"><h2>常用服务</h2></div><div class="mem-shortcuts">${[
        ['pin', '地址管理', '#/member/addresses'],
        ['ticket', '领券中心', '#/coupons'],
        ['layers', '我的拼团', '#/promotions/mygroups'],
      ]
        .map(([symbol, label, href]) => `<a href="${href}">${icon(symbol)}<span>${label}</span></a>`)
        .join('')}${[
        ['invoice', 'edit', '发票中心'],
        ['logistics', 'truck', '物流服务'],
        ['support', 'support', '客服中心'],
      ]
        .map(
          ([value, symbol, label]) =>
            `<button data-member-action="service-help" data-value="${value}">${icon(symbol)}<span>${label}</span></button>`,
        )
        .join('')}</div></section></div></div>
      <section class="mem-panel"><div class="mem-section-head"><div><h2>心动收藏</h2><p>喜欢的好物，下次也能轻松找到。</p></div>${smallLink('#/member/favorites', '查看全部')}</div>${favorites.length ? miniProducts(favorites) : emptyState('把心动留在这里', '点击商品上的爱心，就能收藏喜欢的好物。', '#/', '去发现心动', 'heart', true)}</section>
      <section class="mem-panel"><div class="mem-section-head"><div><h2>最近浏览</h2><p>好物不必错过，随时回来看看。</p></div>${smallLink('#/member/history', '全部足迹')}</div>${history.length ? miniProducts(history.map((row) => row.id)) : emptyState('日常灵感，从这里开始', '浏览过的商品会出现在这里。', '#/', '探索好物', 'clock', true)}</section>`;
  }
  function orderActions(order, code) {
    const id = order.id,
      active = member().activeAftersale?.(id);
    let actions = '';
    if (code === C.OrderViewStatus.Pending)
      return `${button('取消订单', 'cancel-pending', id)}${button('继续付款', 'resume-payment', id, true)}`;
    if (order.paidAt)
      actions += `<button class="mem-outline" data-action="order-details" data-id="${esc(id)}">订单详情</button>`;

    if (code === C.OrderViewStatus.Receiving && !active)
      actions +=
        button('查看物流', 'service-help', id, false, 'data-value="logistics"') +
        button('确认收货', 'receive-order', id, true);
    if (code === C.OrderViewStatus.Review && !active) actions += button('评价订单', 'review-order', id, true);
    if (code === C.OrderViewStatus.Completed) actions += button('查看评价', 'review-order', id);
    if (code === C.OrderViewStatus.Group)
      actions += '<a class="mem-primary" href="#/promotions/mygroups">查看拼团进度</a>';
    if (active) actions += '<a class="mem-outline" href="#/member/aftersales">售后处理中</a>';
    else if (
      [
        C.OrderViewStatus.Shipping,
        C.OrderViewStatus.Receiving,
        C.OrderViewStatus.Review,
        C.OrderViewStatus.Completed,
      ].includes(code)
    )
      actions += button('申请售后', 'aftersale', id);
    actions += window.LumoServices?.orderActions(order) || '';
    if (code !== C.OrderViewStatus.Group) actions += button('再次购买', 'buy-again', id);
    return actions;
  }
  function ordersView(filter, counts) {
    if (!orderTabs.some(([id]) => id === filter)) filter = 'all';
    let orders = member().listOrders(filter);
    if (orderQuery)
      orders = orders.filter((order) =>
        `${order.id} ${order.items.map((item) => item.name || safeProduct(item.id)?.name || '').join(' ')}`
          .toLowerCase()
          .includes(orderQuery.toLowerCase()),
      );
    return `${pageHead('我的订单', '从心动下单到收货评价，每一步都清晰可见。')}<section class="mem-panel"><nav class="mem-tabs" aria-label="订单分类">${orderTabs.map(([id, label]) => `<a href="#/member/orders/${id}" class="${filter === id ? 'active' : ''}" ${filter === id ? 'aria-current="page"' : ''}>${label}${counts[id] ? `<span>${counts[id]}</span>` : ''}</a>`).join('')}</nav><div class="mem-toolbar"><form id="mem-orders-search" role="search">${icon('search')}<input name="query" value="${esc(orderQuery)}" aria-label="搜索订单号或商品名称" placeholder="搜索订单号 / 商品名称" maxlength="100"><button type="submit">搜索</button>${orderQuery ? '<button type="button" data-member-view-action="reset-search" aria-label="清空订单搜索">重置</button>' : ''}</form><small>${orders.length} 笔订单</small></div>${
      orders.length
        ? `<div class="mem-order-list">${orders
            .map((order) => {
              const code = member().orderState(order);
              return `<article class="mem-order-card"><div class="mem-order-head"><span>${date(order.createdAt || order.paidAt, true)}<small>订单号：${esc(order.id)}</small></span><strong class="mem-order-state ${code === C.OrderViewStatus.Closed ? 'is-muted' : ''}">${esc(member().orderLabel(order))}</strong></div><div class="mem-order-body"><div class="mem-order-products">${order.items
                .slice(0, 3)
                .map(
                  (item) =>
                    `<div class="mem-order-product"><a href="#/product/${esc(item.id)}" aria-label="查看${esc(item.name || safeProduct(item.id)?.name || '商品')}">${orderImage(item)}</a><div><h3>${esc(item.name || safeProduct(item.id)?.name || '商品')}</h3>${window.LumoPromoView?.itemBadge(item) || ''}<p>${esc(item.color)} / ${esc(item.option)}</p><span>¥${money(itemPrice(item))} × ${item.qty}</span></div></div>`,
                )
                .join(
                  '',
                )}${order.items.length > 3 ? `<p class="mem-section-note">另有 ${order.items.length - 3} 款商品，可在订单详情查看。</p>` : ''}</div><div class="mem-order-amount"><span>${code === C.OrderViewStatus.Pending ? '应付款' : code === C.OrderViewStatus.Closed && !order.paidAt ? '订单金额' : '实付款'}</span>${price(order.totals.total)}<small>共 ${order.items.reduce((sum, item) => sum + item.qty, 0)} 件商品</small></div></div><div class="mem-order-foot"><small>${code === C.OrderViewStatus.Group ? '成团后进入待发货状态' : code === C.OrderViewStatus.Closed ? '订单已结束' : '订单状态由服务端同步'}</small><div>${orderActions(order, code)}</div></div></article>`;
            })
            .join('')}</div>`
        : emptyState(
            orderQuery ? '没有找到相关订单' : '这里暂时没有订单',
            orderQuery ? '换个商品名称或订单号试试。' : '买过的好物，会在这里留下美好记录。',
            orderQuery ? '' : '#/',
            '去发现好物',
            'bag',
          )
    }</section>`;
  }
  function addressesView(data) {
    return `${pageHead('收货地址', '管理常用收货地址，下次下单更轻松。', button('新增收货地址', 'add-address', '', true))}<div class="mem-notice">${icon('pin')}共 ${data.addresses.length} 个地址 · 默认地址用于快捷下单，结算时可自由切换。</div>${data.addresses.length ? `<div class="mem-address-grid">${data.addresses.map((a) => `<article class="mem-address-card ${a.isDefault ? 'active' : ''}"><div class="mem-address-head"><strong>${esc(a.name)}</strong><span>${esc(mask(a.phone))}</span>${a.label ? `<i>${esc(a.label)}</i>` : ''}</div><div class="mem-address-body"><p>${esc(a.region)}</p><strong>${esc(a.detail)}</strong></div><div class="mem-address-actions">${a.isDefault ? '<span class="mem-badge">默认地址</span>' : `<button data-member-action="default-address" data-id="${esc(a.id)}" aria-label="将${esc(a.name)}的地址设为默认">设为默认</button>`}<span></span><button data-member-action="edit-address" data-id="${esc(a.id)}" aria-label="编辑${esc(a.name)}的地址">${icon('edit')}编辑</button><button data-member-action="delete-address" data-id="${esc(a.id)}" aria-label="删除${esc(a.name)}的地址">${icon('trash')}删除</button></div></article>`).join('')}<button class="mem-address-add" data-member-action="add-address">${icon('plus')}<strong>添加新地址</strong><span>让好物送到每一个日常</span></button></div>` : `<section class="mem-panel">${emptyState('添加你的第一个地址', '保存常用地址，下单时即可快速选择。', '', '', 'pin')}${button('新增收货地址', 'add-address', '', true)}</section>`}<p class="mem-section-note">地址保存到账户，历史订单保留提交时的收货快照。</p>`;
  }
  function couponsView(filter) {
    const tabs = [
      [C.CouponViewStatus.Available, '可使用'],
      [C.CouponViewStatus.Frozen, '订单占用'],
      [C.CouponViewStatus.Used, '已使用'],
      [C.CouponViewStatus.Expired, '已过期'],
    ];
    if (!tabs.some(([id]) => id === filter)) filter = C.CouponViewStatus.Available;
    const coupons = member().coupons(),
      visible = coupons.filter((c) => c.status === filter);
    return `${pageHead('我的优惠券', '每一张好券，都让喜欢更近一点。', '<button class="mem-text-button" data-member-action="coupon-rules">使用规则' + icon('info') + '</button>')}<section class="mem-claim-banner"><div><span class="mem-kicker">A LITTLE GIFT FOR YOU</span><h2>好券都在领券中心</h2><p>限时好券与会员专享券，满足条件即可领取。</p></div><a class="mem-primary" href="#/coupons">去领券中心${icon('arrow')}</a></section><section class="mem-panel"><nav class="mem-tabs" aria-label="优惠券状态">${tabs.map(([id, label]) => `<a href="#/member/coupons/${id}" class="${filter === id ? 'active' : ''}" ${filter === id ? 'aria-current="page"' : ''}>${label}<span>${coupons.filter((c) => c.status === id).length}</span></a>`).join('')}</nav>${visible.length ? `<div class="mem-coupon-grid">${visible.map((c) => `<article class="mem-coupon-card ${c.status !== C.CouponViewStatus.Available ? 'is-disabled' : ''}"><div class="mem-coupon-value">${price(c.value)}<span>满 ¥${money(c.min)} 可用</span></div><div class="mem-coupon-main"><span class="mem-badge">普通商品专享</span><h3>${esc(c.title)}</h3><p>有效期至 ${date(c.expiresAt)}</p><small>每单限 1 张 · 不适用于拼团、秒杀</small></div><div class="mem-coupon-action">${c.status === C.CouponViewStatus.Available ? button(c.selected ? '已选 · 去使用' : '去使用', 'use-coupon', c.id) : `<span>${c.status === C.CouponViewStatus.Used ? '已使用' : c.status === C.CouponViewStatus.Frozen ? '订单占用' : '已过期'}</span>`}</div></article>`).join('')}</div>` : emptyState(filter === C.CouponViewStatus.Used ? '还没有使用过的优惠券' : filter === C.CouponViewStatus.Expired ? '暂时没有过期优惠券' : '好券正在路上', filter === C.CouponViewStatus.Available ? '去领券中心领取好券，再挑选喜欢的好物。' : '优惠券使用状态会自动更新在这里。', '', '', 'ticket')}</section><p class="mem-section-note">优惠券按普通商品优惠前金额判断门槛；换购品不参与满减计算。</p>`;
  }
  function productForMember(p) {
    if (p.visible !== false) return productCard(p);
    return `<article class="product-card mem-unavailable-product"><div class="product-image"><img src="${esc(asset(p.image))}" alt="${esc(p.name)}" loading="lazy"><span class="product-label">${p.metadataUnavailable ? '商品已不可用' : '暂时下架'}</span></div><div class="product-info"><strong class="product-title">${esc(p.name)}</strong><p class="product-sub">${p.metadataUnavailable ? '商品不存在或已下架，可移除此记录' : '商品恢复上架后即可选购'}</p><div class="price-line">${p.metadataUnavailable ? '' : price(p.price)}<span class="mem-chip">暂不可购买</span></div></div></article>`;
  }
  function productsView(tab, data) {
    const isHistory = tab === 'history';
    const rows = isHistory ? data.history : state.favorites.map((id) => ({ id }));
    const list = rows.map((row) => ({ ...row, product: safeProduct(row.id) })).filter((row) => row.product);
    return `${pageHead(isHistory ? '浏览足迹' : '我的收藏', isHistory ? '最近看过的好物，帮你记在这里。' : '把每一次心动，都留在触手可及的地方。', isHistory && list.length ? '<button class="mem-text-button" data-member-action="clear-history">' + icon('trash') + '清空足迹</button>' : '')}<section class="mem-panel"><div class="mem-section-head"><h2>${isHistory ? '最近浏览' : '收藏好物'} <small>${list.length} 件</small></h2>${smallLink('#/', '继续逛逛')}</div>${list.length ? `<div class="mem-product-grid">${list.map((row) => `<div class="mem-product-wrap">${productForMember(row.product)}<div class="mem-product-meta"><span>${row.product.visible === false ? '商品暂时下架' : isHistory ? date(row.at, true) : '心动好物，随时回来看看'}</span><button data-member-action="${isHistory ? 'remove-history' : 'remove-favorite'}" data-id="${esc(row.id)}" aria-label="${isHistory ? '移除足迹' : '取消收藏'}${esc(row.product.name)}">${icon('trash')}</button></div></div>`).join('')}</div>` : emptyState(isHistory ? '还没有浏览足迹' : '收藏夹等你填满', isHistory ? '浏览商品详情后，就能在这里重新找到。' : '点击商品卡片上的爱心，即可收藏。', '#/', '发现喜欢的好物', isHistory ? 'clock' : 'heart')}</section>`;
  }
  function pointsView(data) {
    return `${pageHead('我的积分', '每一次参与，都积攒一点美好。')}<section class="mem-points-hero"><div><span class="mem-kicker">LUMO REWARDS</span><p>当前可用积分</p><strong>${money(data.points)}<small>积分</small></strong><span>100 积分抵 ¥1，最多抵扣商品金额的 10%</span><p>冻结 ${money(data.frozenPoints || 0)} 分 · 待回收 ${money(data.debtPoints || 0)} 分</p></div><div><button class="mem-primary" data-member-action="signin" ${data.signedToday ? 'disabled' : ''}>${icon(data.signedToday ? 'check' : 'coin')}${data.signedToday ? '今天已签到' : '每日签到 +20'}</button><p>最近签到 ${data.signInDays} 天</p></div></section><div class="mem-points-rules">${[
      ['coin', '每日签到', '每天签到一次，可获得 20 积分。'],
      ['bag', '购物回馈', '订单完成后，按实付每 ¥1 奖励 1 积分。'],
      ['ticket', '积分抵扣', '每 100 积分抵 ¥1，最多抵扣 10%。'],
    ]
      .map(([symbol, title, description]) => `<div>${icon(symbol)}<h3>${title}</h3><p>${description}</p></div>`)
      .join(
        '',
      )}</div><section class="mem-panel"><div class="mem-section-head"><h2>积分明细</h2><small>共 ${data.pointLog.length} 条记录</small></div>${data.pointLog.length ? `<div class="mem-table-wrap"><table class="mem-table"><thead><tr><th>积分来源</th><th>时间</th><th>积分变化</th></tr></thead><tbody>${data.pointLog.map((row) => `<tr><td><strong>${esc(row.title)}</strong>${row.orderId ? `<small>订单 ${esc(row.orderId)}</small>` : ''}</td><td>${date(row.at, true)}</td><td class="${row.delta >= 0 ? 'mem-positive' : 'mem-negative'}">${row.delta > 0 ? '+' : ''}${row.delta}</td></tr>`).join('')}</tbody></table></div>` : emptyState('积分明细会记录在这里', '现在签到，获得今天的第一份积分。', '', '', 'coin')}</section><p class="mem-section-note">积分按服务端流水记录，100 积分抵 ¥1，最多抵扣商品金额的 10%；订单完成后发放奖励。</p>`;
  }
  function aftersalesView(data) {
    const names = {
      processing: '待审核',
      approved: '审核通过',
      returning: '退回处理中',
      completed: '已完成',
      rejected: '审核未通过',
      cancelled: '已撤销',
    };
    const active = [C.AftersaleViewStatus.Processing, C.AftersaleViewStatus.Approved, C.AftersaleViewStatus.Returning];
    return `${pageHead('退款 / 售后', '每一步售后进度，都能在这里查看。', '<a class="mem-primary" href="#/member/orders">选择订单申请售后' + icon('arrow') + '</a>')}<section class="mem-panel"><div class="mem-section-head"><h2>我的售后单</h2><span class="mem-badge">${data.aftersales.filter((a) => active.includes(a.status)).length} 笔处理中</span></div>${
      data.aftersales.length
        ? data.aftersales
            .map((a) => {
              const order = member()
                  .listOrders('all')
                  .find((o) => o.id === a.orderId),
                progress = member().aftersaleProgress?.(a) || [];
              return `<article class="mem-aftersale-card"><div class="mem-order-head"><span>${date(a.at, true)}<small>售后单号：${esc(a.id)}</small></span><strong class="mem-order-state">${names[a.status] || a.status}</strong></div><div class="mem-aftersale-body"><span class="mem-badge">${esc(a.type)}</span><h3>${esc(a.reason)}</h3><p>关联订单：${esc(a.orderId)}</p><p>服务状态：${esc(a.backendStatus || a.status)}</p>${a.note ? `<p>补充说明：${esc(a.note)}</p>` : ''}${order ? `<small>订单实付 ¥${money(order.totals.total)} · ${order.items.length} 款商品</small>` : ''}<ol class="mall-after-progress">${progress.map((step) => `<li class="${step.done ? 'done' : ''}"><i>${step.done ? '✓' : ''}</i><span>${esc(step.label)}${step.at ? `<small>${date(step.at, true)}</small>` : ''}</span></li>`).join('')}</ol>${a.status === C.AftersaleViewStatus.Completed && a.type !== '换货' ? `<div class="mall-refund-summary"><strong>退款金额 ¥${money(a.refundAmount || order?.totals.total || 0)}</strong><span>券与积分返还请查看账户最新流水</span></div>` : ''}</div><div class="mem-order-foot"><small>${a.status === C.AftersaleViewStatus.Completed ? '售后处理完成' : '处理进度由商家审核与仓库、支付服务同步'}</small><div>${button('查看详情', 'aftersale-details', a.id)}${a.status === C.AftersaleViewStatus.Approved && a.type !== '仅退款' ? button('填写寄回单号', 'return-aftersale', a.id, true) : ''}${a.refund_id ? button('退款进度', 'refund-status', a.id) : ''}${[C.AftersaleViewStatus.Processing, C.AftersaleViewStatus.Approved].includes(a.status) ? button('撤销申请', 'cancel-aftersale', a.id) : ''}</div></div></article>`;
            })
            .join('')
        : emptyState(
            '暂时没有售后申请',
            '可在已付款订单中申请退款、退货或换货。',
            '#/member/orders',
            '查看我的订单',
            'return',
          )
    }</section><section class="mem-panel"><div class="mem-section-head"><h2>需要帮助？</h2><a class="mem-outline" href="#/member/support">联系客服${icon('chevron')}</a></div><p class="mem-section-note">退款与换货由服务端审核处理；当前演示渠道不产生真实资金或配送。</p></section>`;
  }
  function settingsView(data) {
    const profile = data.profile;
    return `${pageHead('账户设置', '完善个人资料，管理你的购物偏好。')}<section class="mem-panel"><div class="mem-section-head"><h2>个人资料</h2></div><div class="mem-setting-profile"><span class="mem-avatar">${esc(initials(profile.nickname))}</span><div><strong>${esc(profile.nickname)}</strong><p>${esc(profile.level || 'LUMO 生活会员')} · 头像随昵称更新</p></div>${button('编辑资料', 'edit-profile')}</div><div class="mem-info-grid"><dl><dt>会员昵称</dt><dd>${esc(profile.nickname)}</dd></dl><dl><dt>联系手机</dt><dd>${esc(mask(profile.phone))}</dd></dl><dl><dt>性别</dt><dd>${esc(profile.gender || '未填写')}</dd></dl><dl><dt>生日</dt><dd>${esc(profile.birthday || '未填写')}</dd></dl></div></section><section class="mem-panel"><div class="mem-section-head"><h2>消息偏好</h2><small>账户消息偏好</small></div>${[
      ['order', '订单与服务提醒', '接收订单状态、物流与售后相关提示。'],
      ['promotion', '优惠与活动信息', '保留新品、会员券和限时活动的消息偏好。'],
    ]
      .map(
        ([value, title, description]) =>
          `<div class="mem-setting-row"><div><h3>${title}</h3><p>${description}</p></div><button class="mem-toggle ${data.notifications[value] ? 'on' : ''}" data-member-action="toggle-notification" data-value="${value}" role="switch" aria-checked="${!!data.notifications[value]}" aria-label="${title}"><i></i></button></div>`,
      )
      .join(
        '',
      )}</section><section class="mem-panel"><div class="mem-section-head"><h2>账户与服务</h2></div><div class="mem-setting-row"><div><h3>收货地址管理</h3><p>维护常用地址，结算时一键选择。</p></div>${smallLink('#/member/addresses', '管理地址')}</div><div class="mem-setting-row"><div><h3>隐私与数据</h3><p>个人资料、地址与订单保存在服务端账户中。</p></div><button class="mem-text-button" data-action="privacy">查看说明${icon('chevron')}</button></div><div class="mem-setting-row"><div><h3>购物帮助</h3><p>了解订单、优惠、积分和售后规则。</p></div>${button('帮助中心', 'service-help', '', false, 'data-value="faq"')}</div></section><section class="mem-panel auth-exit-panel"><div><h2>登录管理</h2><p>退出会撤销当前服务端会话；再次登录后可查看账户记录。</p></div><button class="mem-outline" data-auth-action="logout">退出登录</button></section>`;
  }
  function page(tab = 'overview', filter = 'all') {
    if (!member()) return '';
    if (!pageNames[tab]) tab = 'overview';
    const data = model(),
      counts = member().counts();
    const views = {
      overview: () => overview(data, counts),
      orders: () => ordersView(filter, counts),
      addresses: () => addressesView(data),
      coupons: () => couponsView(filter),
      favorites: () => productsView(tab, data),
      history: () => productsView(tab, data),
      points: () => pointsView(data),
      aftersales: () => aftersalesView(data),
      settings: () => settingsView(data),
      messages: () => window.LumoServices?.content('messages', filter) || '',
      invoices: () => window.LumoServices?.content('invoices', filter) || '',
      support: () => window.LumoServices?.content('support', filter) || '',
    };
    return `${header()}<main class="container mem-shell" id="main"><div class="mem-breadcrumb"><a href="#/">首页</a>${icon('chevron')}<a href="#/member/overview">个人中心</a>${tab !== 'overview' ? icon('chevron') + `<span>${pageNames[tab]}</span>` : ''}</div><div class="mem-layout">${sidebar(tab, data, counts)}<div class="mem-content">${guardedView(tab, views[tab])}</div></div><p class="mem-bottom-note">你的品质生活，从每一次用心选择开始。 · 账户资料与订单状态由服务端同步</p></main>${footer()}`;
  }
  document.addEventListener('submit', (event) => {
    if (event.target.id !== 'mem-orders-search') return;
    event.preventDefault();
    orderQuery = String(new FormData(event.target).get('query') || '')
      .trim()
      .slice(0, 100);
    renderKeepScroll();
  });
  document.addEventListener('click', (event) => {
    if (event.target.closest('[data-member-view-action="reset-search"]')) {
      orderQuery = '';
      renderKeepScroll();
    }
  });
  window.LumoMemberView = { page };
})();
