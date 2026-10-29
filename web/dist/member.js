(function () {
  'use strict';
  const C = window.LumoConstants;
  const now = () => Date.now();
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const text = (value, max = 100) => (typeof value === 'string' ? value.trim().slice(0, max) : '');
  const uid = (prefix) =>
    prefix + '-' + (crypto.randomUUID?.() || now().toString(36) + Math.random().toString(36).slice(2));
  const dayKey = () => new Date().toISOString().slice(0, 10); // check-in uses the server's UTC day
  const date = (value) => new Date(value).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' });
  const phoneMasked = (phone) => (phone ? phone.slice(0, 3) + '****' + phone.slice(-4) : '未设置');
  const findProduct = (id) =>
    products.find(
      (p) =>
        p.id === id || p.sku_id === id || p.skuId === id || (p.variants || []).some((v) => (v.sku_id || v.id) === id),
    );
  const skuFor = (item) =>
    window.LumoCommerce?.skuForItem?.(item) ||
    window.LumoCommerce?.skuFor?.(item) ||
    item?.sku_id ||
    findProduct(item?.id || item)?.skuId ||
    findProduct(item?.id || item)?.sku_id ||
    item?.id ||
    item;
  const viewId = (sku) => findProduct(sku)?.id || sku;
  const call = (service, action, command = {}) => window.LumoAPI.call(service, action, command);
  const loggedIn = () => !!window.LumoAuth?.isLoggedIn();
  const principal = () => window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '';
  const memberScope = () =>
    JSON.stringify([principal(), window.LumoAPI?.getToken?.() || '', window.LumoAPI?.getBase?.() || location.origin]);
  let memoryScope = memberScope();
  const redraw = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  function emptyMemory() {
    return {
      profile: { nickname: 'LUMO 生活家', phone: '', gender: '保密', birthday: '' },
      addresses: [],
      selectedAddressId: null,
      coupons: [],
      couponTemplates: [],
      selectedCouponId: null,
      couponMode: 'auto',
      quoteCouponOptions: [],
      claimedCoupon: false,
      points: 0,
      pointsPolicy: { deduction_bps: 1000 },
      frozenPoints: 0,
      debtPoints: 0,
      pointLog: [],
      history: [],
      reviews: [],
      aftersales: [],
      notifications: { order: true, promotion: false },
      signInDates: [],
      orderStates: {},
      growth: 0,
      level: '生活会员',
    };
  }
  let memory = emptyMemory();
  let lastTracked = null,
    tracking = null,
    historyRevision = 0,
    historyHydration = null,
    historyProductsKey = '';
  let refreshVersion = 0;
  let inFlight = null,
    dialogVersion = 0;
  const resourceNames = [
    'profile',
    'addresses',
    'coupons',
    'templates',
    'points',
    'history',
    'reviews',
    'aftersales',
    'favorites',
  ];
  const emptyReadStates = () =>
    Object.fromEntries(resourceNames.map((key) => [key, { loading: false, loaded: false, error: '' }]));
  let readStates = emptyReadStates();
  function readState(resource = '') {
    if (memoryScope !== memberScope()) reset();
    if (resource) return { ...(readStates[resource] || { loading: false, loaded: true, error: '' }) };
    const values = Object.values(readStates);
    return {
      loading: values.some((value) => value.loading),
      loaded: values.every((value) => value.loaded),
      error: values.find((value) => value.error)?.error || '',
    };
  }
  function reset() {
    memoryScope = memberScope();
    refreshVersion++;
    dialogVersion++;
    inFlight = null;
    readStates = emptyReadStates();
    memory = emptyMemory();
    lastTracked = null;
    tracking = null;
    historyRevision++;
    historyHydration = null;
    historyProductsKey = '';
    state.address = null;
    state.favorites = [];
    state.favoriteSKUs = [];
  }
  const timestamp = (value) => (value ? Date.parse(value) || Number(value) || 0 : 0);
  function mapAddress(a) {
    return {
      id: a.id,
      name: a.recipient || '',
      phone: a.phone || '',
      region: [a.province, a.city, a.district].filter(Boolean).join(' '),
      detail: a.address || '',
      label: '',
      isDefault: a.is_default === true,
    };
  }
  function mapCoupon(c) {
    return {
      id: c.id,
      templateId: c.template_id || '',
      rule: clone(c),
      value: Number(c.amount_cents || 0) / 100,
      min: Number(c.min_cents || 0) / 100,
      title: c.template_name || memory.couponTemplates.find((t) => t.id === c.template_id)?.name || '品质生活优惠券',
      expiresAt: timestamp(c.expires_at),
      backendStatus: c.status,
      status:
        c.status === C.CouponStatus.Available
          ? timestamp(c.expires_at) <= now()
            ? C.CouponViewStatus.Expired
            : C.CouponViewStatus.Available
          : [C.CouponStatus.Used, C.CouponHoldState.Confirmed].includes(c.status)
            ? C.CouponViewStatus.Used
            : [C.CouponHoldState.Reserved, C.CouponStatus.Held].includes(c.status)
              ? C.CouponViewStatus.Frozen
              : C.CouponViewStatus.Expired,
    };
  }
  const aftersaleNames = {
    [C.AftersaleStatus.Requested]: C.AftersaleViewStatus.Processing,
    [C.AftersaleStatus.Approved]: C.AftersaleViewStatus.Approved,
    [C.AftersaleStatus.Returning]: C.AftersaleViewStatus.Returning,
    [C.AftersaleStatus.Inspecting]: C.AftersaleViewStatus.Returning,
    [C.AftersaleStatus.Inspected]: C.AftersaleViewStatus.Returning,
    [C.AftersaleStatus.RefundRequested]: C.AftersaleViewStatus.Returning,
    [C.AftersaleStatus.Refunding]: C.AftersaleViewStatus.Returning,
    [C.AftersaleStatus.Exchanging]: C.AftersaleViewStatus.Returning,
    [C.AftersaleStatus.Completed]: C.AftersaleViewStatus.Completed,
    [C.AftersaleStatus.Rejected]: C.AftersaleViewStatus.Rejected,
    [C.AftersaleStatus.Cancelled]: C.AftersaleViewStatus.Cancelled,
  };
  function mapAftersale(a) {
    const at = timestamp(a.created_at),
      updated = timestamp(a.updated_at);
    return {
      ...a,
      orderId: a.order_id,
      type:
        {
          [C.AftersaleType.Refund]: '仅退款',
          [C.AftersaleType.Return]: '退货退款',
          [C.AftersaleType.Exchange]: '换货',
        }[a.type] || a.type,
      status: aftersaleNames[a.status] || C.AftersaleViewStatus.Processing,
      backendStatus: a.status,
      at,
      updatedAt: updated,
      approvedAt: null,
      returningAt: null,
      completedAt: a.status === C.AftersaleStatus.Completed ? updated : null,
      rejectedAt: a.status === C.AftersaleStatus.Rejected ? updated : null,
      cancelledAt: a.status === C.AftersaleStatus.Cancelled ? updated : null,
      refundAmount: Number(a.amount_cents || 0) / 100,
    };
  }
  async function refresh(force = false, resources = resourceNames) {
    if (memoryScope !== memberScope()) reset();
    if (!loggedIn()) {
      reset();
      return model();
    }
    const requested = new Set(resources.filter((key) => resourceNames.includes(key)));
    if (!requested.size) return model();
    if (inFlight) {
      if (!force && [...requested].every((key) => inFlight.resources.has(key))) return inFlight.promise;
      await inFlight.promise.catch(() => {});
      return refresh(force, [...requested]);
    }
    const version = refreshVersion,
      owner = memberScope(),
      historyReadRevision = historyRevision,
      pending = { resources: requested };
    inFlight = pending;
    [...requested].forEach((key) => {
      readStates[key] = { ...readStates[key], loading: true, error: '' };
    });
    pending.promise = (async () => {
      const keys = [
        'profile',
        'addresses',
        'coupons',
        'templates',
        'points',
        'history',
        'reviews',
        'aftersales',
        'favorites',
      ];
      const operations = [
        () => call(C.Service.Member, C.Action.Get),
        () => call(C.Service.Address, C.Action.List, { data: { limit: 100 } }),
        () => call(C.Service.Coupon, C.Action.List),
        () => call(C.Service.Coupon, C.Action.Templates, { data: { page: 1, page_size: 100 } }),
        () => call(C.Service.Points, C.Action.Get),
        () => call(C.Service.Engagement, C.Action.List, { data: { kind: C.EngagementKind.History, limit: 100 } }),
        () =>
          call(C.Service.Interaction, C.Action.List, {
            data: { kind: C.InteractionKind.Review, mine: true, limit: 100 },
          }),
        () => call(C.Service.Aftersale, C.Action.List),
        async () => {
          const favorites = await call(C.Service.Engagement, C.Action.List, {
            data: { kind: C.EngagementKind.Favorite, limit: 100 },
          });
          if (version === refreshVersion && owner === memberScope() && loggedIn())
            await window.LumoCommerce?.hydrateProducts?.(
              (favorites.items || []).map((row) => row.sku_id),
              {
                active: () => version === refreshVersion && owner === memberScope() && loggedIn(),
              },
            );
          return favorites;
        },
      ];
      const results = await Promise.allSettled(
        operations.map((execute, index) => (requested.has(keys[index]) ? execute() : null)),
      );
      if (version !== refreshVersion || owner !== memberScope() || !loggedIn()) return model();
      const values = results.map((result, index) => {
        const key = keys[index];
        if (!requested.has(key)) return null;
        readStates[key] = {
          loading: false,
          loaded: readStates[key].loaded || result.status === 'fulfilled',
          error: result.status === 'rejected' ? result.reason?.message || '账户信息暂未加载成功' : '',
        };
        return result.status === 'fulfilled' ? result.value : null;
      });
      const [profile, address, coupon, templates, points, history, reviews, aftersales, favorites] = values;
      if (profile) {
        const p = profile.preferences || {};
        memory.profile = {
          nickname: profile.display_name || principal() || 'LUMO 生活家',
          phone: p.phone || '',
          gender: p.gender || '保密',
          birthday: p.birthday || '',
          avatar: profile.avatar || '',
        };
        memory.notifications = {
          order: p.notifications?.order !== false,
          promotion: p.notifications?.promotion === true,
        };
        memory.growth = Number(profile.growth || 0);
        memory.level = profile.level === C.MemberLevel.Gold ? '金卡会员' : '生活会员';
      }
      if (address) {
        const selected = memory.selectedAddressId;
        memory.addresses = (address.items || []).map(mapAddress);
        memory.selectedAddressId = memory.addresses.some((a) => a.id === selected)
          ? selected
          : memory.addresses.find((a) => a.isDefault)?.id || memory.addresses[0]?.id || null;
        mirrorAddress();
      }
      if (templates) memory.couponTemplates = templates.items || [];
      if (coupon) {
        memory.coupons = (coupon.items || []).map(mapCoupon);
        if (
          memory.selectedCouponId &&
          !memory.coupons.some((c) => c.id === memory.selectedCouponId && c.status === C.CouponViewStatus.Available)
        )
          resetCheckoutCoupon();
        memory.claimedCoupon = memory.coupons.some((c) => c.templateId === 'welcome-100');
      }
      if (points) {
        memory.points = Number(points.available || 0);
        memory.pointsPolicy = points.policy || { deduction_bps: 1000 };
        memory.frozenPoints = Number(points.frozen || 0);
        memory.debtPoints = Number(points.debt || 0);
        memory.pointLog = (points.ledger || []).map((l) => ({
          id: l.id,
          title:
            {
              [C.PointsLedgerKind.Checkin]: '每日签到',
              [C.PointsLedgerKind.Freeze]: '下单冻结积分',
              [C.PointsLedgerKind.Confirmed]: '积分已抵扣',
              [C.PointsLedgerKind.Cancelled]: '取消订单释放积分',
              [C.PointsLedgerKind.Reward]: '订单完成奖励',
              [C.PointsLedgerKind.Refund]: '退款返还抵扣积分',
              [C.PointsLedgerKind.RewardReversal]: '退款收回奖励积分',
            }[l.kind] || l.kind,
          delta: [C.PointsLedgerKind.Confirmed].includes(l.kind) ? 0 : Number(l.amount || 0),
          at: timestamp(l.created_at),
          orderId: l.order_id,
          kind: l.kind,
        }));
        memory.signInDates = [
          ...new Set(
            (points.ledger || [])
              .filter((l) => l.kind === C.PointsLedgerKind.Checkin)
              .map((l) => (l.created_at || '').slice(0, 10)),
          ),
        ];
      }
      if (history) {
        const rows = (history.items || []).map(historyRow);
        memory.history = historyReadRevision === historyRevision ? rows : mergeHistory(memory.history, rows);
        historyProductsKey = '';
      }
      if (reviews)
        memory.reviews = (reviews.items || []).map((r) => ({
          id: r.id,
          orderId: r.order_id,
          productId: viewId(r.sku_id),
          sku_id: r.sku_id,
          rating: Number(r.rating || 5),
          text: r.body || '',
          at: timestamp(r.created_at),
          status: r.status,
        }));
      if (aftersales) memory.aftersales = (aftersales.items || []).map(mapAftersale);
      if (favorites) {
        state.favoriteSKUs = (favorites.items || []).map((f) => f.sku_id);
        state.favorites = (favorites.items || []).map((f) => viewId(f.sku_id));
      }
      const failed = results.find((result) => result.status === 'rejected');
      if (failed) throw failed.reason;
      return model();
    })().finally(() => {
      if (inFlight === pending) inFlight = null;
    });
    return pending.promise;
  }
  function refreshForView() {
    const route = (location.hash || '#/').slice(2).split('/');
    let resources = [];
    if (route[0] === 'checkout') resources = ['addresses', 'coupons', 'points'];
    else if (route[0] === 'cart') resources = ['addresses'];
    else if (route[0] === 'member') {
      const tab = route[1] || 'overview';
      resources =
        {
          overview: ['profile', 'points', 'coupons', 'favorites', 'history', 'addresses', 'aftersales'],
          orders: ['reviews', 'aftersales'],
          addresses: ['addresses'],
          coupons: ['coupons'],
          favorites: ['favorites'],
          history: ['history'],
          points: ['points'],
          aftersales: ['aftersales'],
          settings: ['profile'],
          reviews: ['reviews'],
        }[tab] || [];
    }
    return refresh(false, resources);
  }
  function mirrorAddress() {
    const a = memory.addresses.find((row) => row.id === memory.selectedAddressId);
    state.address = a ? { ...a } : null;
  }
  function addresses() {
    return clone(memory.addresses);
  }
  function selectedAddress() {
    return clone(memory.addresses.find((a) => a.id === memory.selectedAddressId) || null);
  }
  function coupons() {
    return memory.coupons.map((c) => ({ ...c, selected: c.id === memory.selectedCouponId }));
  }
  const restricted = (items) =>
    (items || []).some((x) => [C.PromotionType.Flash, C.PromotionType.Group].includes(x.promotion?.type));
  const ordinaryAmount = (items) =>
    (items || [])
      .filter((x) => !x.promotion)
      .reduce((sum, x) => sum + itemPrice(x) * Math.max(0, Number(x.qty) || 0), 0);
  function couponEstimate(c, items) {
    if (!window.LumoCouponBenefits)
      return ordinaryAmount(items) >= c.min ? Math.min(c.value, ordinaryAmount(items)) : 0;
    const lines = (items || [])
      .filter((item) => !item.promotion)
      .map((item) => {
        const p = findProduct(item.id) || {};
        return {
          sku_id: skuFor(item),
          product_id: p.product_id || p.id || item.id,
          category: p.category || '',
          amount_cents: Math.round(itemPrice(item) * Math.max(0, Number(item.qty) || 0) * 100),
        };
      });
    return (
      window.LumoCouponBenefits.estimate(
        c.rule || { amount_cents: Math.round(c.value * 100), min_cents: Math.round(c.min * 100) },
        Math.round(ordinaryAmount(items) * 100),
        lines,
      ) / 100
    );
  }
  function couponValue(items) {
    if (restricted(items)) return 0;
    const c = coupons().find((c) => c.selected && c.status === C.CouponViewStatus.Available);
    return c ? couponEstimate(c, items) : 0;
  }
  function selectCoupon(id) {
    if (id === 'auto') {
      memory.couponMode = 'auto';
      memory.selectedCouponId = null;
      state.coupon = 0;
      return true;
    }
    if (!id || id === '0') {
      memory.couponMode = 'none';
      memory.selectedCouponId = null;
      state.coupon = 0;
      return true;
    }
    const c = coupons().find(
      (c) => (c.id === id || c.value === Number(id)) && c.status === C.CouponViewStatus.Available,
    );
    if (!c) {
      toast('优惠券不可用，请刷新后重试');
      return false;
    }
    memory.couponMode = 'manual';
    memory.selectedCouponId = c.id;
    state.coupon = c.value;
    return true;
  }
  const pointBalance = () => memory.points;
  function pointsUsed(items) {
    return state.points && !restricted(items) && !memory.debtPoints
      ? Math.max(
          0,
          Math.min(
            memory.points,
            Math.floor((ordinaryAmount(items) * 100 * Number(memory.pointsPolicy.deduction_bps)) / 10000),
          ),
        )
      : 0;
  }
  function pointsDiscount(items) {
    return pointsUsed(items) / 100;
  }
  function checkoutMeta(items) {
    return {
      couponId: memory.couponMode === 'manual' ? memory.selectedCouponId : null,
      couponMode: memory.couponMode,
      pointsUsed: pointsUsed(items),
      addressId: memory.selectedAddressId,
    };
  }
  function validateCheckout() {
    return null;
  } // authoritative validation is performed by pricing and checkout services
  function resetCheckoutCoupon() {
    memory.couponMode = 'auto';
    memory.selectedCouponId = null;
    memory.quoteCouponOptions = [];
    state.coupon = 0;
  }
  function onPaid() {
    return refresh(true);
  }
  function orderState(order) {
    const s = order?.backendStatus || order?.status || '';
    if ([C.OrderStatus.Closed, C.OrderStatus.Closing, C.OrderStatus.Terminated, C.OrderStatus.CreateFailed].includes(s))
      return C.OrderViewStatus.Closed;
    if (s === C.OrderStatus.Refunded || isRefunded(order?.id)) return C.OrderViewStatus.Refunded;
    if (
      [
        C.OrderStatus.PendingPayment,
        C.OrderStatus.AwaitingPayment,
        C.OrderStatus.Created,
        C.OrderStatus.Creating,
      ].includes(s)
    )
      return C.OrderViewStatus.Pending;
    if (s === C.OrderStatus.PaidWaitGroup) return C.OrderViewStatus.Group;
    if (s === C.OrderStatus.Completed)
      return order.items?.every((item) =>
        memory.reviews.some((r) => r.orderId === order.id && r.sku_id === skuFor(item)),
      )
        ? C.OrderViewStatus.Completed
        : C.OrderViewStatus.Review;
    if (s === C.OrderStatus.Shipped) return C.OrderViewStatus.Receiving;
    if (s === C.OrderStatus.Delivered) return C.OrderViewStatus.Review;
    return C.OrderViewStatus.Shipping;
  }
  function orderLabel(order) {
    if ((order?.backendStatus || order?.status) === C.OrderStatus.PaidConfirming) return '支付已确认 · 资源处理中';
    return (
      {
        [C.OrderViewStatus.Pending]: '待付款',
        [C.OrderViewStatus.Shipping]: '待发货',
        [C.OrderViewStatus.Receiving]: '待收货',
        [C.OrderViewStatus.Review]: '待评价',
        [C.OrderViewStatus.Completed]: '已完成',
        [C.OrderViewStatus.Refunded]: '退款完成',
        [C.OrderViewStatus.Group]: '待成团',
        [C.OrderViewStatus.Closed]: '已关闭',
      }[orderState(order)] || '处理中'
    );
  }
  function listOrders(filter = 'all') {
    const source = window.LumoCommerce?.allOrders?.() || [...(state.pending ? [state.pending] : []), ...state.orders];
    return source
      .filter((o, i, a) => a.findIndex((x) => x.id === o.id) === i)
      .sort((a, b) => (b.createdAt || 0) - (a.createdAt || 0))
      .filter((o) => filter === 'all' || (filter === 'aftersale' ? !!activeAftersale(o.id) : orderState(o) === filter))
      .map((o) => ({
        ...clone(o),
        memberState: orderState(o),
        memberLabel: orderLabel(o),
        lifecycle: clone(o.lifecycle || {}),
      }));
  }
  function counts() {
    const result = {
      all: 0,
      pending: 0,
      shipping: 0,
      receiving: 0,
      review: 0,
      completed: 0,
      refunded: 0,
      group: 0,
      closed: 0,
      aftersale: memory.aftersales.filter(
        (a) =>
          ![C.AftersaleViewStatus.Completed, C.AftersaleViewStatus.Cancelled, C.AftersaleViewStatus.Rejected].includes(
            a.status,
          ),
      ).length,
      favorites: state.favorites.length,
      history: memory.history.length,
      coupons: coupons().filter((c) => c.status === C.CouponViewStatus.Available).length,
    };
    listOrders().forEach((o) => {
      result.all++;
      result[o.memberState] = (result[o.memberState] || 0) + 1;
    });
    return result;
  }
  function model() {
    if (memoryScope !== memberScope()) reset();
    const nextAt = memory.growth < 1000 ? 1000 : 5000;
    return {
      ...clone(memory),
      coupons: coupons(),
      ledger: clone(memory.pointLog),
      pointsPolicy: clone(memory.pointsPolicy),
      preferences: clone(memory.notifications),
      profile: { ...clone(memory.profile), level: memory.level },
      signedToday: memory.signInDates.includes(dayKey()),
      signInDays: memory.signInDates.length,
      membership: {
        level: memory.level,
        growth: memory.growth,
        nextAt,
        progress: Math.min(100, Math.round((memory.growth / nextAt) * 100)),
      },
      totalSaved: listOrders()
        .filter((o) => o.paidAt && !isRefunded(o.id))
        .reduce(
          (sum, o) => sum + (o.totals?.coupon || 0) + (o.totals?.points || 0) + (o.totals?.activitySavings || 0),
          0,
        ),
    };
  }
  function renderAddress(id = null, options = {}) {
    const a = memory.addresses.find((row) => row.id === id) || {
      name: '',
      phone: '',
      region: '',
      detail: '',
      label: '家',
      isDefault: !memory.addresses.length,
    };
    if (!id && memory.addresses.length >= 10) {
      toast('最多保存 10 个地址，请先整理现有地址');
      return;
    }
    modal(
      id ? '编辑收货地址' : '添加收货地址',
      `<p class="modal-desc">管理常用地址，下次购物更轻松。演示请使用示例信息。</p><form id="member-address-form" data-id="${esc(a.id || '')}" data-checkout="${options.checkout === true}"><div class="form-row"><label class="field">收货人<input name="name" required maxlength="20" value="${esc(a.name)}" placeholder="请输入收货人姓名"></label><label class="field">手机号码<input name="phone" type="tel" required pattern="1[3-9][0-9]{9}" maxlength="11" value="${esc(a.phone)}" placeholder="11 位手机号码"></label></div><label class="field">省 / 市 / 区<input name="region" required maxlength="60" value="${esc(a.region)}" placeholder="如：广东省 深圳市 南山区"></label><label class="field">详细地址<textarea name="detail" required rows="2" maxlength="100" placeholder="街道、楼栋、门牌号等">${esc(a.detail)}</textarea></label><label class="field">地址标签<select name="label">${['家', '公司', '学校', '其他'].map((label) => `<option ${a.label === label ? 'selected' : ''}>${label}</option>`).join('')}</select></label><label class="check" style="margin:14px 0 22px"><input name="isDefault" type="checkbox" ${a.isDefault ? 'checked' : ''}>设为默认收货地址</label><button class="primary" type="submit">${options.checkout ? '保存并使用' : '保存地址'}</button></form>`,
    );
  }
  async function readDialog(title, type, load, show, retryAction, id) {
    if (memoryScope !== memberScope()) reset();
    const owner = memberScope(),
      ticket = ++dialogVersion,
      marker = uid('member-read');
    modal(
      title,
      `<div data-member-loading="${marker}">${window.LumoSkeleton?.render(type, { count: 2, label: '正在读取' + title }) || ''}</div>`,
    );
    const current = () => {
      const root = document.querySelector('#modal-root');
      return owner === memberScope() && ticket === dialogVersion && (!root || String(root.innerHTML).includes(marker));
    };
    try {
      const result = await load();
      if (current()) show(result);
    } catch (error) {
      if (current())
        modal(
          title,
          `<div role="alert"><p>${esc(error?.message || '读取失败，请重试')}</p><button class="outline" data-member-action="${retryAction}" data-id="${esc(id)}"${retryAction === 'service-help' ? ' data-value="logistics"' : ''}>重新加载</button></div>`,
        );
    }
  }
  async function openAddress(id = null, options = {}) {
    if (!id) {
      renderAddress(null, options);
      return;
    }
    return readDialog(
      '编辑收货地址',
      'form',
      () => call(C.Service.Address, C.Action.Get, { id }),
      (raw) => {
        const fresh = mapAddress(raw),
          index = memory.addresses.findIndex((a) => a.id === id);
        if (index >= 0) memory.addresses[index] = fresh;
        else memory.addresses.push(fresh);
        renderAddress(id, options);
      },
      'edit-address',
      id,
    );
  }
  async function ensureResources(keys) {
    if (keys.every((key) => readState(key).loaded)) return;
    try {
      await refresh(false, keys);
    } catch (error) {
      if (keys.some((key) => !readState(key).loaded)) throw error;
    }
    const failed = keys.map((key) => readState(key)).find((value) => !value.loaded);
    if (failed) throw Error(failed.error || '账户信息尚未加载，请重试');
  }
  function chooseAddress() {
    if (!readState('addresses').loaded)
      return readDialog(
        '选择收货地址',
        'cards',
        () => ensureResources(['addresses']),
        chooseAddress,
        'choose-address',
        '',
      );
    modal(
      '选择收货地址',
      `<p class="modal-desc">选择本次订单的收货地址，已提交订单不受影响。</p><div class="mem-address-picker">${memory.addresses.map((a) => `<button class="mem-address-pick ${memory.selectedAddressId === a.id ? 'is-selected' : ''}" data-member-action="select-address" data-id="${esc(a.id)}"><strong>${esc(a.name)} <span>${esc(phoneMasked(a.phone))}</span> <small>${esc(a.label)}${a.isDefault ? ' · 默认' : ''}</small></strong><p>${esc(a.region + ' ' + a.detail)}</p><span>${memory.selectedAddressId === a.id ? '当前使用' : '使用此地址'}</span></button>`).join('') || '<p class="modal-desc">还没有收货地址，先添加一个吧。</p>'}</div><button class="outline" data-member-action="add-address" data-checkout="true" style="margin-top:18px">${icon('plus')}添加新地址</button>`,
    );
  }
  function showCoupons() {
    if (!/^#\/(checkout|payment|cart)(?:\/|$)/.test(location.hash)) {
      go('coupons');
      return;
    }
    if (!readState('coupons').loaded)
      return readDialog('选择优惠券', 'coupons', () => ensureResources(['coupons']), showCoupons, 'show-coupons', '');
    const items = /^#\/(checkout|payment)/.test(location.hash) ? selectedItems() : state.cart.filter((x) => x.selected);
    const amount = ordinaryAmount(items);
    const blocked = restricted(items);
    modal(
      '选择优惠券',
      `<p class="modal-desc">每单限用一张，按普通商品优惠前金额判断门槛。${blocked ? '秒杀、拼团订单不叠加券与积分。' : `本单参与门槛金额 ¥${money(amount)}。`}</p>${coupons()
        .map(
          (c) =>
            `<div class="coupon-card"><span class="price">${esc(window.LumoCouponBenefits?.face(c.rule) || '¥' + money(c.value))}</span><div><strong>${esc(c.title)}</strong><p>${esc(window.LumoCouponBenefits?.condition(c.rule) || '满 ¥' + money(c.min) + ' 可用')} · ${esc(window.LumoCouponBenefits?.scope(c.rule) || '普通商品通用')} · ${date(c.expiresAt)} 到期</p></div><button data-member-action="select-coupon" data-id="${esc(c.id)}" ${blocked || c.status !== C.CouponViewStatus.Available || couponEstimate(c, items) <= 0 ? 'disabled' : ''}>${c.status === C.CouponViewStatus.Used ? '已使用' : c.status === C.CouponViewStatus.Expired ? '已过期' : blocked ? '不可叠加' : couponEstimate(c, items) <= 0 ? '不符合使用条件' : c.selected ? '已选择' : '使用此券'}</button></div>`,
        )
        .join(
          '',
        )}<button class="text-button" data-member-action="select-coupon" data-id="auto" style="display:block;margin:16px auto 0">${memory.couponMode === 'auto' ? '✓ 已启用自动最优券' : '自动使用最优优惠券'}</button><button class="text-button" data-member-action="select-coupon" data-id="0" style="display:block;margin:16px auto 0">本单不使用优惠券</button><a class="text-button" href="#/member/coupons" style="display:block;text-align:center;margin-top:14px">查看我的优惠券 ${icon('chevron')}</a>`,
    );
  }
  function showProfile() {
    const p = memory.profile;
    modal(
      '编辑个人资料',
      `<form id="member-profile-form"><div class="form-row"><label class="field">昵称<input name="nickname" required maxlength="30" value="${esc(p.nickname)}" placeholder="你希望大家如何称呼你"></label><label class="field">手机号码（选填）<input name="phone" type="tel" pattern="1[3-9][0-9]{9}" maxlength="11" value="${esc(p.phone)}" placeholder="联系手机"></label></div><div class="form-row"><label class="field">生日（选填）<input name="birthday" type="date" max="${dayKey()}" value="${esc(p.birthday)}"></label><label class="field">性别<select name="gender">${['保密', '女', '男'].map((g) => `<option ${p.gender === g ? 'selected' : ''}>${g}</option>`).join('')}</select></label></div><p class="modal-desc">资料保存到账户；联系信息不改变登录凭据。</p><button class="primary" type="submit">保存资料</button></form>`,
    );
  }
  function orderById(id) {
    return listOrders().find((o) => o.id === id);
  }
  function reviewOrder(id) {
    const reviewedOrder = orderById(id);
    const existing = reviewedOrder?.items?.every((item) =>
      memory.reviews.some((r) => r.orderId === id && r.sku_id === skuFor(item)),
    )
      ? memory.reviews.find((row) => row.orderId === id)
      : null;
    if (existing) {
      modal(
        '我的评价',
        `<p class="modal-desc">订单 ${esc(id)} · ${date(existing.at)}</p><p style="color:var(--red);font-size:22px;letter-spacing:5px" aria-label="${existing.rating} 星评价">${'★'.repeat(existing.rating)}${'☆'.repeat(5 - existing.rating)}</p><p class="modal-desc" style="white-space:pre-wrap;line-height:1.9">${esc(existing.text)}</p><button class="outline" data-action="close-modal">关闭</button>`,
      );
      return;
    }
    const order = orderById(id);
    if (!order || orderState(order) !== C.OrderViewStatus.Review || activeAftersale(id)) {
      toast('确认收货且售后结束后才可以评价');
      return;
    }
    modal(
      '分享你的购物体验',
      `<p class="modal-desc">${esc(order.id)} · 评价提交后由商家审核展示。</p><form id="member-review-form" data-id="${esc(id)}"><label class="field">满意度<select name="rating">${[
        [5, '5 星 · 非常满意'],
        [4, '4 星 · 满意'],
        [3, '3 星 · 一般'],
        [2, '2 星 · 有待改善'],
        [1, '1 星 · 不满意'],
      ]
        .map(([value, label]) => `<option value="${value}">${label}</option>`)
        .join(
          '',
        )}</select></label><label class="field">评价内容<textarea name="text" rows="4" maxlength="300" required placeholder="商品、包装或购物体验，有什么想分享的？"></textarea></label><button class="primary" type="submit">提交评价</button></form>`,
    );
  }
  function aftersale(id) {
    const eligible = listOrders().filter(
      (o) =>
        o.paidAt && ![C.PromotionType.Group, 'closed', 'refunded'].includes(orderState(o)) && !activeAftersale(o.id),
    );
    const order = eligible.find((o) => o.id === id);
    if (!order) {
      modal(
        '选择售后订单',
        `<p class="modal-desc">请选择需要申请售后的已支付订单。售后由服务端记录，并由商家审核处理。</p>${eligible.map((o) => `<button class="mem-address-pick" data-member-action="aftersale" data-id="${esc(o.id)}"><strong>${esc(o.items[0]?.name || findProduct(o.items[0]?.id)?.name || '商城订单')}</strong><p>${esc(o.id)} · 实付 ¥${money(o.totals.total)}</p><span>申请售后 ${icon('chevron')}</span></button>`).join('') || '<p class="modal-desc">暂无可申请的订单。完成购买后，可在这里申请售后。</p>'}`,
      );
      return;
    }
    modal(
      '申请售后',
      `<p class="modal-desc">订单 ${esc(id)} · 售后状态、退款和寄回流程以服务端处理结果为准。</p><form id="member-aftersale-form" data-id="${esc(id)}"><label class="field">服务类型<select name="type">${(orderState(order) === C.OrderViewStatus.Shipping ? ['仅退款'] : ['退货退款', '换货', '仅退款']).map((v) => `<option>${v}</option>`).join('')}</select></label><fieldset class="field"><legend>申请商品与数量</legend>${order.items.map((item, index) => `<label>${esc(item.name || findProduct(item.id)?.name || item.id)}<input type="number" name="line-${index}" min="0" max="${item.qty}" value="${item.qty}" step="1" aria-label="${esc(item.name || item.id)} 售后数量"></label>`).join('')}</fieldset><label class="field">申请原因<select name="reason"><option>不喜欢 / 不想要</option><option>商品与描述不符</option><option>质量问题</option><option>商品破损 / 少件</option><option>其他原因</option></select></label><label class="field">补充说明<textarea name="note" rows="3" maxlength="300" placeholder="选填，描述你的问题"></textarea></label><button class="primary" type="submit">提交售后申请</button></form>`,
    );
  }

  function activeAftersale(orderId) {
    const a = memory.aftersales.find(
      (a) =>
        a.orderId === orderId &&
        ![C.AftersaleViewStatus.Completed, C.AftersaleViewStatus.Rejected, C.AftersaleViewStatus.Cancelled].includes(
          a.status,
        ),
    );
    return a ? clone(a) : null;
  }
  function isRefunded(orderId) {
    const o = (window.LumoCommerce?.allOrders?.() || state.orders).find((o) => o.id === orderId);
    return (
      (o?.backendStatus || o?.status) === C.OrderStatus.Refunded ||
      (Number(o?.payable_cents || 0) > 0 && Number(o?.refunded_total_cents || 0) >= Number(o?.payable_cents || 0))
    );
  }
  function aftersaleProgress(a) {
    if (!a) return [];
    const rows = [{ label: '申请已提交', at: a.at, done: true }];
    if ([C.AftersaleViewStatus.Rejected, C.AftersaleViewStatus.Cancelled].includes(a.status)) {
      rows.push({
        label: a.status === C.AftersaleViewStatus.Rejected ? '审核未通过' : '申请已撤销',
        at: a.updatedAt,
        done: true,
      });
      return rows;
    }
    rows.push({
      label: '商家审核',
      done: ![C.AftersaleStatus.Requested, C.AftersaleStatus.Rejected, C.AftersaleStatus.Cancelled].includes(
        a.backendStatus,
      ),
    });
    if (a.type !== '仅退款')
      rows.push({
        label: a.return_tracking ? '已寄回 · ' + a.return_tracking : '寄回商品与验收',
        done: !!a.return_tracking,
      });
    rows.push({
      label: a.type === '换货' ? '换货配送完成' : '退款完成',
      at: a.completedAt,
      done: a.status === C.AftersaleViewStatus.Completed,
    });
    return rows;
  }
  const historyRow = (row) => ({
    id: viewId(row.sku_id),
    sku_id: row.sku_id,
    recordId: row.id,
    at: timestamp(row.updated_at || row.created_at),
  });
  function mergeHistory(...sources) {
    const rows = new Map();
    for (const source of sources)
      for (const row of source) {
        const key = row.recordId || row.sku_id;
        if (!rows.has(key) || row.at > rows.get(key).at) rows.set(key, row);
      }
    return [...rows.values()].sort((a, b) => b.at - a.at).slice(0, 100);
  }
  async function hydrateHistory(rows, active) {
    if (!window.LumoCommerce?.normalizeProduct) return;
    const missing = [...new Set(rows.map((row) => row.sku_id).filter((id) => id && !findProduct(id)))].slice(0, 100);
    if (!missing.length) return;
    const batch = (ids) =>
      window.LumoAPI.call(C.Service.Catalog, C.Action.BatchGet, { data: { ids } }, { auth: false });
    const result = await batch(missing);
    if (!active()) return;
    const raw = [...(result.items || [])];
    const parents = [
      ...new Set(
        raw
          .map((row) => row.product_id || row.parent_id)
          .filter((id) => id && !findProduct(id) && !raw.some((row) => row.id === id)),
      ),
    ];
    if (parents.length) {
      const parentResult = await batch(parents);
      if (!active()) return;
      raw.push(...(parentResult.items || []));
    }
    for (const row of raw) {
      if (!active()) return;
      if ((row.product_id && row.product_id !== row.id) || (row.parent_id && row.parent_id !== row.id)) continue;
      const product = window.LumoCommerce.normalizeProduct(row);
      if (product?.id && !findProduct(product.id)) products.push(product);
    }
  }
  function ensureHistoryProducts() {
    if (!loggedIn() || readStates.history.error) return;
    const owner = memberScope(),
      version = refreshVersion;
    const key = owner + '|' + JSON.stringify(memory.history.map((row) => row.sku_id));
    if (historyHydration?.key === key) return historyHydration.promise;
    if (
      !readStates.history.loaded ||
      historyProductsKey === key ||
      memory.history.every((row) => findProduct(row.sku_id))
    )
      return;
    const active = () => version === refreshVersion && owner === memberScope() && loggedIn();
    const visible = memory.history.some((row) => findProduct(row.id));
    const entry = { key, promise: null };
    historyHydration = entry;
    readStates.history = { loading: true, loaded: visible, error: '' };
    entry.promise = (async () => {
      try {
        await hydrateHistory(memory.history, active);
        if (!active()) return;
        memory.history = memory.history.map((row) => ({ ...row, id: viewId(row.sku_id) }));
        historyProductsKey = key;
        readStates.history = { loading: false, loaded: true, error: '' };
      } catch (error) {
        if (active())
          readStates.history = { loading: false, loaded: visible, error: error.message || '浏览足迹商品加载失败' };
      } finally {
        if (historyHydration === entry) historyHydration = null;
        if (active() && location.hash.startsWith('#/member/history')) redraw();
      }
    })();
    redraw();
    return entry.promise;
  }
  function track(productId) {
    if (memoryScope !== memberScope()) reset();
    const product = findProduct(productId);
    if (!loggedIn() || !product) return Promise.resolve(false);
    const owner = memberScope(),
      version = refreshVersion,
      key = owner + '|' + productId;
    if (tracking?.key === key) return tracking.promise;
    const entry = { key, promise: null };
    tracking = entry;
    const active = () => version === refreshVersion && owner === memberScope() && loggedIn();
    entry.promise = (async () => {
      const result = await call(C.Service.Engagement, C.Action.Put, {
        data: { kind: C.EngagementKind.History, sku_id: product.skuId || product.sku_id || product.id },
      });
      if (!active()) return false;
      if (!result.id || !result.sku_id || (result.user_id && result.user_id !== principal()))
        throw Error('浏览足迹未保存，请重试');
      historyRevision++;
      memory.history = mergeHistory([historyRow(result)], memory.history);
      if (location.hash === '#/product/' + productId) lastTracked = key;
      readStates.history = { loading: false, loaded: true, error: '' };
      await window.LumoSidebars?.refresh?.(true);
      return active();
    })().finally(() => {
      if (tracking === entry) tracking = null;
    });
    return entry.promise;
  }
  function afterRender() {
    if (window.LumoConnection?.ready === false || window.LumoAPI?.isBootstrapActive?.()) return;
    if (memoryScope !== memberScope()) reset();
    const route = (location.hash || '#/').slice(2).split('/'),
      id = route[0] === 'product' ? route[1] : null;
    if (route[0] === 'member' && route[1] === 'history') {
      lastTracked = null;
      void ensureHistoryProducts();
      return;
    }
    if (!id) {
      lastTracked = null;
      return;
    }
    // A direct URL may still be loading a product outside the bootstrap page.
    // Do not mark it visited before the real detail data is ready and persisted.
    if (!loggedIn() || !findProduct(id) || lastTracked === memberScope() + '|' + id) return;
    const owner = memberScope();
    void track(id).catch((error) => {
      if (owner !== memberScope() || !loggedIn()) return;
      readStates.history = { ...readStates.history, loading: false, error: error.message || '浏览足迹保存失败' };
      showError(error);
    });
  }
  function showError(error) {
    toast(error?.message || '请求未完成，请重试');
  }
  async function cancelAftersale(id) {
    await call(C.Service.Aftersale, C.Action.Cancel, { id });
    await refresh(true);
    return null;
  }
  function advanceAftersale() {
    return '请在商家管理台处理审核、验收或退款';
  }
  function rejectAftersale() {
    return '请在商家管理台处理售后审核';
  }
  async function serviceHelp(value, id) {
    if (value === 'invoice') {
      go('member/invoices');
      return;
    }
    if (value === 'support') {
      go('member/support');
      return;
    }
    if (value === 'logistics' && id) {
      await readDialog(
        '物流详情',
        'rows',
        () =>
          Promise.all([
            call(C.Service.Logistics, C.Action.Get, { id }),
            call(C.Service.Fulfillment, C.Action.Get, { id }),
          ]),
        ([row, fulfillment]) =>
          modal(
            '物流详情',
            `<p class="modal-desc">订单 ${esc(id)}<br>履约状态：${esc(fulfillment.status)}<br>物流状态：${esc(row.status)}<br>运单号：${esc(row.tracking_id || '尚未生成')}<br>配送渠道：${esc(row.provider || '')}<br>更新时间：${esc(new Date(row.updated_at).toLocaleString('zh-CN'))}</p>`,
          ),
        'service-help',
        id,
      );
      return;
    }
    modal(
      '购物帮助',
      '<div class="help-list"><p>积分按 1 分抵 ¥0.01，最多抵扣商品金额的 10%。优惠与可用金额以结算报价为准。退款、退货、换货由商家审核，状态同步到售后中心。</p><a class="outline" href="#/member/support">进入帮助中心</a></div>',
    );
  }
  async function mutate(service, action, command) {
    await call(service, action, command);
    const resources =
      {
        [C.Service.Address]: ['addresses'],
        [C.Service.Coupon]: ['coupons'],
        [C.Service.Points]: ['points'],
        [C.Service.Member]: ['profile'],
        [C.Service.Engagement]: [command?.data?.kind === C.EngagementKind.History ? 'history' : 'favorites'],
        [C.Service.Interaction]: ['reviews'],
        [C.Service.Aftersale]: ['aftersales'],
      }[service] || [];
    await refresh(true, resources);
    if (service === 'engagement') await window.LumoSidebars?.refresh?.();
    redraw();
  }
  document.addEventListener('click', async (event) => {
    const b = event.target.closest('[data-member-action]');
    if (!b || b.disabled) return;
    event.preventDefault();
    const action = b.dataset.memberAction,
      id = b.dataset.id,
      value = b.dataset.value;
    if (!window.LumoAuth?.requireLogin(location.hash)) return;
    b.disabled = true;
    try {
      switch (action) {
        case 'retry-read': {
          const request = value === 'orders' ? window.LumoCommerce?.refreshOrders?.() : refresh(true);
          redraw();
          await request;
          redraw();
          break;
        }
        case 'choose-address':
          await chooseAddress();
          break;
        case 'show-coupons':
          await showCoupons();
          break;
        case 'signin':
          await mutate(C.Service.Points, C.Action.Checkin, {});
          toast('签到结果已同步');
          break;
        case 'edit-profile':
          showProfile();
          break;
        case 'add-address':
          await openAddress(null, { checkout: b.dataset.checkout === 'true' });
          break;
        case 'edit-address':
          await openAddress(id);
          break;
        case 'delete-address':
          modal(
            '删除这个收货地址？',
            `<p class="modal-desc">删除后，历史订单仍保留原收货快照。</p><div class="modal-actions"><button class="outline" data-action="close-modal">保留地址</button><button class="primary" data-member-action="confirm-delete-address" data-id="${esc(id)}">确认删除</button></div>`,
          );
          break;
        case 'confirm-delete-address':
          await mutate(C.Service.Address, C.Action.Delete, { id });
          closeModal();
          toast('地址已删除');
          break;
        case 'default-address':
          await mutate(C.Service.Address, C.Action.Default, { id });
          toast('已设为默认地址');
          break;
        case 'select-address':
          if (memory.addresses.some((a) => a.id === id)) {
            memory.selectedAddressId = id;
            mirrorAddress();
            closeModal();
            redraw();
          }
          break;
        case 'claim-coupon': {
          const t = memory.couponTemplates.find((t) => t.id === (id || 'welcome-100'));
          if (!t) throw Error('暂无可领取的优惠券');
          await mutate(C.Service.Coupon, C.Action.Claim, { data: { template_id: t.id } });
          toast('优惠券已到账');
          break;
        }
        case 'use-coupon':
          if (selectCoupon(id)) {
            closeModal();
            go('cart');
          }
          break;
        case 'select-coupon':
          if (selectCoupon(id)) {
            closeModal();
            redraw();
          }
          break;
        case 'coupon-rules':
          await serviceHelp('faq');
          break;
        case 'remove-favorite':
          await mutate(C.Service.Engagement, C.Action.Delete, {
            data: { kind: C.EngagementKind.Favorite, sku_id: skuFor({ id }) },
          });
          break;
        case 'remove-history':
          await mutate(C.Service.Engagement, C.Action.Delete, {
            id: memory.history.find((h) => h.id === id)?.recordId,
            data: { kind: C.EngagementKind.History, sku_id: skuFor({ id }) },
          });
          break;
        case 'clear-history':
          modal(
            '清空浏览足迹？',
            '<p class="modal-desc">清空账户中的浏览记录。</p><button class="primary" data-member-action="confirm-clear-history">确认清空</button>',
          );
          break;
        case 'confirm-clear-history':
          for (const h of memory.history)
            await call(C.Service.Engagement, C.Action.Delete, {
              id: h.recordId,
              data: { kind: C.EngagementKind.History },
            });
          await refresh(true);
          await window.LumoSidebars?.refresh?.();
          closeModal();
          redraw();
          break;
        case 'cancel-pending':
          modal(
            '取消这笔订单？',
            `<p class="modal-desc">服务端会关闭支付并释放预占库存、券和积分。</p><div class="modal-actions"><button class="outline" data-action="close-modal">继续保留</button><button class="primary" data-member-action="confirm-cancel-pending" data-id="${esc(id)}">取消订单</button></div>`,
          );
          break;
        case 'confirm-cancel-pending':
          await call(C.Service.Checkout, C.Action.Cancel, { id });
          await window.LumoCommerce?.refreshOrders?.();
          await refresh(true);
          closeModal();
          redraw();
          toast('订单取消状态已同步');
          break;
        case 'resume-payment': {
          const o = orderById(id);
          if (o) {
            state.pending = o;
            go('payment');
          }
          break;
        }
        case 'receive-order':
          await call(C.Service.Order, C.Action.Complete, { id });
          await window.LumoCommerce?.refreshOrders?.();
          await refresh(true);
          redraw();
          toast('收货结果已同步');
          break;
        case 'review-order':
          reviewOrder(id);
          break;
        case 'buy-again': {
          const o = orderById(id);
          if (!o) break;
          await window.LumoCommerce?.addItems?.(o.items.map((item) => ({ ...item, promotion: undefined })));
          go('cart');
          break;
        }
        case 'aftersale':
          aftersale(id);
          break;
        case 'aftersale-details': {
          await readDialog(
            '售后详情',
            'rows',
            () => call(C.Service.Aftersale, C.Action.Get, { id }),
            (raw) => {
              const a = mapAftersale(raw),
                index = memory.aftersales.findIndex((x) => x.id === id);
              if (index >= 0) memory.aftersales[index] = a;
              modal(
                '售后详情',
                `<p class="modal-desc">售后单 ${esc(a.id)}<br>订单 ${esc(a.orderId)}<br>类型 ${esc(a.type)}<br>状态 ${esc(a.backendStatus)}<br>申请金额 ¥${money(a.refundAmount)}<br>寄回单号 ${esc(a.return_tracking || '尚未提交')}<br>换货履约单 ${esc(a.replacement_id || '尚未生成')}</p>`,
              );
            },
            'aftersale-details',
            id,
          );
          break;
        }
        case 'cancel-aftersale':
          await cancelAftersale(id);
          redraw();
          toast('售后申请已撤销');
          break;
        case 'return-aftersale':
          modal(
            '填写寄回单号',
            `<form id="member-return-form" data-id="${esc(id)}"><label class="field">快递运单号<input name="tracking" required maxlength="100"></label><button class="primary">提交寄回信息</button></form>`,
          );
          break;
        case 'refund-status': {
          const a = memory.aftersales.find((a) => a.id === id);
          if (!a?.refund_id) throw Error('退款尚未创建');
          await readDialog(
            '退款进度',
            'rows',
            () => call(C.Service.Refund, C.Action.Get, { id: a.refund_id }),
            (r) =>
              modal(
                '退款进度',
                `<p class="modal-desc">退款单 ${esc(r.id)}<br>状态：${esc(r.status)}<br>金额 ¥${money(Number(r.amount_cents || 0) / 100)}</p>`,
              ),
            'refund-status',
            id,
          );
          break;
        }
        case 'toggle-notification': {
          if (!['order', 'promotion'].includes(value)) break;
          const notifications = { ...memory.notifications, [value]: !memory.notifications[value] };
          await mutate(C.Service.Member, C.Action.Update, {
            data: {
              preferences: {
                phone: memory.profile.phone,
                gender: memory.profile.gender,
                birthday: memory.profile.birthday,
                notifications,
              },
            },
          });
          toast('偏好已保存');
          break;
        }
        case 'service-help':
          await serviceHelp(value, id);
          break;
        case 'membership':
          modal(
            '我的会员权益',
            '<p class="modal-desc">积分与成长值均由服务端记录。每日签到获得 20 积分；积分按 1 分抵 ¥0.01，订单最多抵扣 10%。订单完成后发放积分奖励，退款后按服务端规则返还或回收。</p>',
          );
          break;
      }
    } catch (error) {
      showError(error);
    } finally {
      b.disabled = false;
    }
  });
  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (
      ![
        'member-address-form',
        'member-profile-form',
        'member-review-form',
        'member-aftersale-form',
        'member-return-form',
      ].includes(form.id)
    )
      return;
    event.preventDefault();
    if (!window.LumoAuth?.requireLogin(location.hash)) return;
    const data = Object.fromEntries(new FormData(form));
    const submit = form.querySelector('[type=submit],button');
    if (submit) submit.disabled = true;
    try {
      if (form.id === 'member-address-form') {
        const region = text(data.region, 60).split(/\s+/),
          id = form.dataset.id;
        const result = await call(C.Service.Address, id ? C.Action.Update : C.Action.Create, {
          id: id || undefined,
          data: {
            recipient: text(data.name, 20),
            phone: text(data.phone, 30),
            address: text(data.detail, 1000),
            province: region[0] || '',
            city: region[1] || '',
            district: region.slice(2).join(' '),
            is_default: data.isDefault === 'on',
          },
        });
        if (form.dataset.checkout === 'true' || !memory.selectedAddressId) memory.selectedAddressId = result.id;
        await refresh(true, ['addresses']);
        closeModal();
        redraw();
        toast('收货地址已保存');
      }
      if (form.id === 'member-profile-form') {
        const p = {
          nickname: text(data.nickname, 30),
          phone: text(data.phone, 30),
          gender: text(data.gender, 10),
          birthday: text(data.birthday, 10),
        };
        if (!p.nickname) throw Error('请填写昵称');
        await mutate(C.Service.Member, C.Action.Update, {
          data: {
            display_name: p.nickname,
            preferences: {
              phone: p.phone,
              gender: p.gender,
              birthday: p.birthday,
              notifications: memory.notifications,
            },
          },
        });
        closeModal();
        redraw();
        toast('个人资料已保存');
      }
      if (form.id === 'member-review-form') {
        const order = orderById(form.dataset.id);
        if (!order) throw Error('订单不存在');
        const body = text(data.text, 300);
        if (!body) throw Error('请填写评价内容');
        for (const item of order.items)
          await call(C.Service.Interaction, C.Action.Create, {
            data: {
              kind: C.InteractionKind.Review,
              sku_id: skuFor(item),
              order_id: order.id,
              rating: Math.max(1, Math.min(5, Number(data.rating) || 5)),
              body,
            },
          });
        await refresh(true);
        await window.LumoProductExtras?.refresh?.();
        closeModal();
        redraw();
        toast('评价已提交，等待审核');
      }
      if (form.id === 'member-aftersale-form') {
        const order = orderById(form.dataset.id);
        if (!order) throw Error('订单不存在');
        const lines = order.items
          .map((item, index) => ({ sku_id: skuFor(item), quantity: Number(data['line-' + index] ?? item.qty) }))
          .filter((line) => line.quantity > 0);
        if (!lines.length) throw Error('请至少选择一件售后商品');
        form.dataset.requestKey ||= uid('aftersale');
        await call(C.Service.Aftersale, C.Action.Create, {
          idempotency_key: form.dataset.requestKey,
          data: {
            order_id: form.dataset.id,
            lines,
            type: {
              '仅退款': C.AftersaleType.Refund,
              '退货退款': C.AftersaleType.Return,
              '换货': C.PromotionType.Exchange,
            }[data.type],
            reason: [text(data.reason, 60), text(data.note, 300)].filter(Boolean).join('：'),
          },
        });
        await refresh(true);
        closeModal();
        go('member/aftersales');
        toast('售后申请已提交');
      }
      if (form.id === 'member-return-form') {
        await mutate(C.Service.Aftersale, C.Action.Return, {
          id: form.dataset.id,
          data: { tracking: text(data.tracking, 100) },
        });
        closeModal();
        redraw();
        toast('寄回信息已提交');
      }
    } catch (error) {
      showError(error);
    } finally {
      if (submit) submit.disabled = false;
    }
  });
  window.addEventListener('lumo:ready', afterRender);
  window.LumoMember = {
    model,
    readState,
    refresh,
    refreshForView,
    reset,
    addresses,
    selectedAddress,
    openAddress,
    chooseAddress,
    coupons,
    couponValue,
    selectCoupon,
    resetCheckoutCoupon,
    applyCouponQuote(q) {
      memory.quoteCouponOptions = q.coupon_options || [];
      memory.selectedCouponId = q.coupon_id || null;
      state.coupon = Number(q.coupon_discount_cents || 0) / 100;
    },
    showCoupons,
    pointBalance,
    pointsDiscount,
    checkoutMeta,
    validateCheckout,
    onPaid,
    track,
    afterRender,
    orderState,
    orderLabel,
    listOrders,
    counts,
    activeAftersale,
    advanceAftersale,
    rejectAftersale,
    cancelAftersale,
    isRefunded,
    aftersaleProgress,
    skuFor,
  };
  window.addEventListener('lumo:logout', reset);
})();
