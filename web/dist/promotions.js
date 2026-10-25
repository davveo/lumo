(function () {
  'use strict';
  const C = window.LumoConstants;
  // Business state comes from campaign/group/flash. Only checkout selection is local.
  let activities = [],
    sessions = [],
    groupRows = [],
    selectedExchange = null,
    selectedCampaign = null,
    selectionEpoch = 0,
    selectionRevision = 0;
  const campaignTypes = [
    C.CampaignKind.Reduction,
    C.CampaignKind.Discount,
    C.CampaignKind.QuantityDiscount,
    C.CampaignKind.Direct,
  ];
  const campaignLabels = {
    [C.CampaignKind.Reduction]: '满额立减',
    [C.CampaignKind.Discount]: '折扣特惠',
    [C.CampaignKind.QuantityDiscount]: '多件优惠',
    [C.CampaignKind.Direct]: '限量直降',
  };
  const sessionProducts = new Map();
  let loaded = false,
    loadError = '',
    refreshPromise,
    timer,
    busy = false,
    readRevision = 0;
  const readScope = () => window.LumoAPI.getBase?.() || location.origin;
  let readOwner = readScope(),
    resourceLoaded = { campaign: false, flash: false, group: false };
  function ensureReadScope() {
    if (readOwner === readScope()) return;
    readOwner = readScope();
    readRevision++;
    activities = [];
    sessions = [];
    groupRows = [];
    sessionProducts.clear();
    loaded = false;
    loadError = '';
    refreshPromise = null;
    resourceLoaded = { campaign: false, flash: false, group: false };
  }
  const api = (service, action, command = {}, options = {}) => window.LumoAPI.call(service, action, command, options);
  const sku = (product) => String(product?.skuId || product?.sku_id || product?.id || '');
  const legacyProductIDs = {
    'sku-phone': 'iphone',
    'sku-headphones': 'headphones',
    'sku-watch': 'watch',
    'sku-coffee': 'coffee',
  };
  const findProduct = (id) =>
    products.find((p) => p.id === id || sku(p) === id) || products.find((p) => p.id === legacyProductIDs[id]);
  const nowUser = () => window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '';
  const selectionOwner = () =>
    JSON.stringify([nowUser(), window.LumoAPI.getBase?.() || location.origin, selectionEpoch]);
  const count = (n) => Math.max(0, Math.floor(Number(n) || 0));
  const activityFor = (id) => activities.find((a) => a.id === id);
  const catalogPrice = (item) => {
    const p = findProduct(item?.id || item?.sku_id);
    return Math.max(0, Number(p?.price) || 0);
  };
  const regularPrice = (item) => (Number.isFinite(item?.regularUnitPrice) ? item.regularUnitPrice : catalogPrice(item));
  const redraw = () => typeof renderKeepScroll === 'function' && renderKeepScroll();
  const report = (e) => {
    toast((e?.message || '活动服务暂不可用') + (e?.traceID ? ' · ' + e.traceID.slice(0, 8) : ''));
  };

  async function refresh() {
    ensureReadScope();
    if (refreshPromise) return refreshPromise;
    const context = readOwner,
      revision = ++readRevision;
    const active = () => context === readScope() && revision === readRevision;
    loadError = '';
    const request = (async () => {
      const results = await Promise.allSettled([
        api(C.Service.Campaign, C.Action.List, {}, { auth: false }),
        api(C.Service.Flash, C.Action.List, {}, { auth: false }),
        api(C.Service.Group, C.Action.List, {}, { auth: false }),
      ]);
      if (!active()) return;
      const fields = [
        (v) => {
          activities = v.items || [];
        },
        (v) => {
          sessions = v.items || [];
        },
        (v) => {
          groupRows = v.items || [];
        },
      ];
      let failure;
      results.forEach((result, i) => {
        if (result.status === 'fulfilled') {
          fields[i](result.value);
          resourceLoaded[['campaign', 'flash', 'group'][i]] = true;
        } else failure = result.reason;
      });
      if (results[1].status === 'fulfilled') {
        const ids = [...new Set(sessions.map((session) => session.sku_id).filter(Boolean))];
        const detailResults = ids.length
          ? await Promise.allSettled([
              Promise.all(
                Array.from({ length: Math.ceil(ids.length / 500) }, (_, index) =>
                  api(
                    C.Service.Catalog,
                    C.Action.BatchGet,
                    { data: { ids: ids.slice(index * 500, (index + 1) * 500) } },
                    { auth: false },
                  ),
                ),
              ).then((results) => {
                if (active())
                  for (const result of results)
                    for (const detail of result.items || []) sessionProducts.set(detail.id, detail);
              }),
              window.LumoInventory?.refresh?.(ids),
            ])
          : [];
        if (!active()) return;
        for (const detailResult of detailResults) if (detailResult.status === 'rejected') failure = detailResult.reason;
      }
      loaded = results.every((result) => result.status === 'fulfilled') && !failure;
      loadError = failure?.message || '';
      if (failure) throw failure;
      return { activities, sessions, groups: groupRows };
    })().finally(() => {
      if (refreshPromise === request) refreshPromise = null;
    });
    refreshPromise = request;
    return request;
  }
  function offer(type, activity, p, extra = {}) {
    if (!activity || !p) return null;
    const remainingQuota = Math.max(0, count(activity.quota) - count(activity.held) - count(activity.used));
    const available = window.LumoInventory ? window.LumoInventory.stockFor(p) : 0;
    const regular = Number(p.price) || 0,
      discount = Math.min(regular, Number(activity.discount_cents || 0) / 100);
    return {
      id: activity.id + ':' + sku(p),
      activityId: activity.id,
      type,
      productId: p.id,
      skuId: sku(p),
      product: p,
      regularPrice: regular,
      price: Math.max(0, regular - discount),
      discount,
      stock: count(activity.quota),
      remaining: Math.min(remainingQuota, available),
      used: count(activity.used),
      threshold: Number(activity.threshold_cents || 0) / 100,
      limit: 1,
      limitReached: false,
      status:
        activity.active && p.visible !== false && remainingQuota > 0 && available > 0
          ? C.PromotionOfferStatus.Active
          : C.PromotionOfferStatus.Ended,
      startAt: 0,
      endAt: 0,
      teamSize: 2,
      ...extra,
    };
  }
  function flashOffers(session = C.PromotionSession.Today) {
    ensureReadScope();
    if (session === C.PromotionSession.Tomorrow) return []; // No forecast exists in the backend contract.
    return sessions
      .filter((s) => s.active)
      .map((s) => {
        const raw = sessionProducts.get(s.sku_id),
          p = findProduct(raw?.product_id || raw?.attributes?.product_id || s.sku_id);
        if (!raw || !p) return null;
        const unit = Number(raw.price_cents || 0) / 100,
          activity = activityFor(s.activity_id);
        const row = offer(C.PromotionType.Flash, activity, p, {
          sessionId: s.id,
          skuId: s.sku_id,
          regularPrice: unit,
          price: Math.max(0, unit - Number(activity?.discount_cents || 0) / 100),
        });
        if (row && window.LumoInventory) {
          row.remaining = Math.min(
            Math.max(0, count(activity?.quota) - count(activity?.held) - count(activity?.used)),
            window.LumoInventory.stockFor(s.sku_id),
          );
          row.status =
            activity?.active && row.remaining > 0 ? C.PromotionOfferStatus.Active : C.PromotionOfferStatus.Ended;
        }
        return row;
      })
      .filter(Boolean);
  }
  function groupOffers() {
    ensureReadScope();
    const activity = activities.find((a) => a.kind === C.CampaignKind.Group && a.active);
    if (!activity) return [];
    // The current policy allows any active catalogue SKU. Existing teams come first.
    const teamSKUs = new Set(groupRows.map((g) => g.sku_id));
    return products
      .filter((p) => p.visible !== false)
      .slice()
      .sort((a, b) => Number(teamSKUs.has(sku(b))) - Number(teamSKUs.has(sku(a))))
      .map((p) => offer(C.PromotionType.Group, activity, p))
      .filter(Boolean);
  }
  function exchangeOffers(items = baseItems()) {
    ensureReadScope();
    const eligibility = exchangeState(items);
    return activities
      .filter((a) => a.kind === C.CampaignKind.Addon && a.active)
      .map((a) => {
        // This SKU is part of the current versioned campaign contract.
        const p = findProduct(
          a.addon_sku_id || (a.id === 'addon-coffee' ? (findProduct('coffee') ? 'coffee' : 'sku-coffee') : ''),
        );
        const result = offer(C.PromotionType.Exchange, a, p);
        return result
          ? { ...result, eligible: eligibility.eligible, selected: eligibility.selectedId === result.id }
          : null;
      })
      .filter(Boolean);
  }
  function allOffers() {
    return [...flashOffers(), ...groupOffers(), ...exchangeOffers([])];
  }
  function offerFor(type, productId) {
    return (
      (type === C.PromotionType.Flash
        ? flashOffers()
        : type === C.PromotionType.Group
          ? groupOffers()
          : exchangeOffers()
      ).find((row) => row.productId === productId) || null
    );
  }
  function offerByID(id) {
    return allOffers().find((row) => row.id === id);
  }
  function getPrice(item, basePrice) {
    if (!item?.promotion) return basePrice;
    const row = offerByID(item.promotion.offerId);
    return row && row.productId === item.id ? row.price : basePrice;
  }
  function baseItems() {
    return /^#\/(checkout|payment)(?:\/|$)/.test(location.hash) && checkoutMode === C.OrderSource.Direct && directItem
      ? [directItem]
      : (state.cart || []).filter((i) => i.selected);
  }
  const contextKey = (items) =>
    JSON.stringify(
      (items || [])
        .filter((i) => i.promotion?.type !== C.PromotionType.Exchange)
        .map((i) => [i.id, i.sku_id || '', i.color, i.option, count(i.qty), i.promotion?.type || ''])
        .sort(),
    );
  const ordinaryAmount = (items) =>
    (items || []).reduce(
      (sum, item) =>
        sum +
        Math.round((typeof itemPrice === 'function' ? itemPrice(item) : regularPrice(item)) * 100) * count(item.qty),
      0,
    );
  function campaigns(type) {
    ensureReadScope();
    return activities.filter((a) => campaignTypes.includes(a.kind) && a.active && (!type || a.kind === type));
  }
  function campaignRule(activity) {
    if (!activity) return '';
    if ([C.CampaignKind.Discount, C.CampaignKind.QuantityDiscount].includes(activity.kind)) {
      const rate = Number(activity.pay_rate_bps || 10000) / 1000;
      return `${activity.kind === C.CampaignKind.QuantityDiscount ? `满 ${count(activity.min_quantity) || 2} 件，` : ''}${money(rate)} 折`;
    }
    const reduction = Number(activity.discount_cents || 0) / 100;
    return activity.kind === C.CampaignKind.Reduction
      ? `满 ¥${money(Number(activity.threshold_cents || 0) / 100)} 减 ¥${money(reduction)}`
      : `每单直减 ¥${money(reduction)}`;
  }
  function campaignEligibility(activity, items) {
    const amount = ordinaryAmount(items),
      quantity = (items || []).reduce((n, item) => n + count(item.qty), 0);
    const restricted = (items || []).some((item) =>
      [C.PromotionType.Flash, C.PromotionType.Group, C.PromotionType.Exchange].includes(item.promotion?.type),
    );
    const remaining = count(activity.quota) - count(activity.held) - count(activity.used);
    let reason = '';
    if (!activity.active || remaining <= 0) reason = '活动名额已用完';
    else if (restricted) reason = '本单已参加秒杀、拼团或换购';
    else if (!quantity) reason = '请先选择商品';
    else if (quantity < count(activity.min_quantity)) reason = `再选 ${count(activity.min_quantity) - quantity} 件可用`;
    else if (amount < Number(activity.threshold_cents || 0))
      reason = `还差 ¥${money((Number(activity.threshold_cents) - amount) / 100)} 可用`;
    else if (
      [C.CampaignKind.Reduction, C.CampaignKind.Direct].includes(activity.kind) &&
      amount <= Number(activity.discount_cents || 0)
    )
      reason = '商品金额需高于优惠金额';
    return { eligible: !reason, reason, amount, quantity, remaining: Math.max(0, remaining) };
  }
  function campaignState(items = baseItems()) {
    const matches =
      selectedCampaign && selectedCampaign.owner === selectionOwner() && selectedCampaign.context === contextKey(items);
    const activity = matches && campaigns().find((a) => a.id === selectedCampaign.id);
    const eligible = activity && campaignEligibility(activity, items).eligible;
    return {
      selectedId: eligible ? activity.id : null,
      activity: eligible ? activity : null,
      hasRestricted: (items || []).some((item) =>
        [C.PromotionType.Flash, C.PromotionType.Group, C.PromotionType.Exchange].includes(item.promotion?.type),
      ),
    };
  }
  function clearCampaign() {
    selectionRevision++;
    selectedCampaign = null;
  }
  function checkoutChanged() {
    window.dispatchEvent(new CustomEvent('lumo:checkout-changed'));
    redraw();
  }
  async function selectCampaign(id, items = baseItems(), direct = false) {
    if (!window.LumoAuth?.requireLogin(location.hash)) return false;
    const activity = campaigns().find((a) => a.id === id),
      owner = selectionOwner(),
      context = contextKey(items);
    if (!activity) {
      toast('活动已结束，请刷新后重试');
      return false;
    }
    const eligibility = campaignEligibility(activity, items);
    if (!eligibility.eligible) {
      toast(eligibility.reason);
      return false;
    }
    const revision = ++selectionRevision;
    try {
      const result = await api(C.Service.Campaign, C.Action.Evaluate, {
        data: { activity_id: id, subtotal_cents: eligibility.amount, quantity: eligibility.quantity },
      });
      if (owner !== selectionOwner() || revision !== selectionRevision) return false;
      const current = direct && checkoutMode === C.OrderSource.Direct && directItem ? [directItem] : baseItems();
      if (contextKey(current) !== context) {
        toast('结算商品已变化，请重新选择活动');
        return false;
      }
      if (!result.eligible) {
        toast(result.reason || '当前商品不符合活动规则');
        return false;
      }
      clearExchange();
      selectedCampaign = { id, owner, context };
      checkoutChanged();
      return true;
    } catch (error) {
      if (owner === selectionOwner() && revision === selectionRevision) report(error);
      return false;
    }
  }
  async function startCampaignPurchase(id, productId, fromDetail = false) {
    if (busy || !window.LumoAuth?.requireLogin(location.hash)) return;
    const activity = campaigns().find((a) => a.id === id),
      product = findProduct(productId);
    if (!activity || !product) return;
    busy = true;
    try {
      const quantity = Math.max(
        1,
        count(activity.min_quantity),
        Math.ceil(Number(activity.threshold_cents || 0) / Math.max(1, Math.round(Number(product.price) * 100))),
      );
      const item =
        fromDetail && typeof detail !== 'undefined' && detail.id === product.id
          ? { ...detail, selected: true }
          : {
              id: product.id,
              color: product.colors[0],
              option: product.defaultOption,
              qty: Math.min(10, quantity),
              selected: true,
            };
      clearExchange();
      clearCampaign();
      setCheckout(C.OrderSource.Direct, item);
      if (await selectCampaign(id, [item], true)) go('checkout');
    } finally {
      busy = false;
    }
  }
  function eligibleAmount(items) {
    return (items || []).filter((i) => !i.promotion).reduce((sum, i) => sum + regularPrice(i) * count(i.qty), 0);
  }
  function exchangeState(items = baseItems()) {
    const activity = activities.find((a) => a.kind === C.CampaignKind.Addon && a.active);
    const threshold = Number(activity?.threshold_cents || 0) / 100;
    const baseAmount = eligibleAmount(items),
      hasRestricted = items.some((i) => [C.PromotionType.Flash, C.PromotionType.Group].includes(i.promotion?.type));
    const eligible = !!activity && !hasRestricted && baseAmount >= threshold;
    const row =
      selectedExchange &&
      selectedExchange.owner === selectionOwner() &&
      selectedExchange.context === contextKey(items) &&
      eligible
        ? offerByRawID(selectedExchange.offerId)
        : null;
    const selectedId = row && row.status === C.PromotionOfferStatus.Active ? row.id : null;
    return {
      threshold,
      baseAmount,
      eligible,
      remaining: Math.max(0, threshold - baseAmount),
      hasRestricted,
      selectedId,
      selectedProductId: selectedId ? row.productId : null,
    };
  }
  // Avoid a cycle between exchangeOffers() and exchangeState().
  function offerByRawID(id) {
    for (const a of activities.filter((a) => a.kind === C.CampaignKind.Addon)) {
      const p = findProduct(
        a.addon_sku_id || (a.id === 'addon-coffee' ? (findProduct('coffee') ? 'coffee' : 'sku-coffee') : ''),
      );
      const result = offer(C.PromotionType.Exchange, a, p);
      if (result?.id === id) return result;
    }
    return null;
  }
  function augmentItems(items) {
    const base = (items || []).filter((i) => i.promotion?.type !== C.PromotionType.Exchange);
    const chosen = exchangeState(base);
    const row = chosen.selectedId && offerByRawID(chosen.selectedId);
    return !row
      ? base
      : [
          ...base,
          {
            id: row.productId,
            color: row.product.colors[0],
            option: row.product.defaultOption,
            qty: 1,
            selected: true,
            promotion: { type: C.PromotionType.Exchange, offerId: row.id, activityId: row.activityId },
          },
        ];
  }
  function clearExchange() {
    selectionRevision++;
    selectedExchange = null;
  }
  function groupView(raw) {
    if (!raw) return null;
    const product = findProduct(raw.sku_id),
      user = nowUser();
    const orders = state.orders || [];
    const ownOrder = orders.find((o) => (o.groupId || o.group_id || o.raw?.group_id) === raw.id);
    return {
      id: raw.id,
      product,
      productId: product?.id || raw.sku_id,
      offerId: 'group-demo:' + raw.sku_id,
      leader: raw.creator_id === user && user ? '我的拼团' : '商城用户',
      joined: count(raw.paid),
      occupied: count(raw.occupied),
      required: count(raw.required),
      endAt: Date.parse(raw.expires_at) || 0,
      createdAt: Date.parse(raw.created_at) || 0,
      status:
        raw.status === C.GroupStatus.Formed
          ? C.GroupViewStatus.Formed
          : [C.GroupStatus.Expired, C.GroupStatus.Failed, C.GroupStatus.Cancelled].includes(raw.status)
            ? C.GroupViewStatus.Expired
            : C.GroupViewStatus.Waiting,
      mine: !!user && (raw.creator_id === user || !!ownOrder),
      orderId: ownOrder?.id || null,
      serverStatus: raw.status,
    };
  }
  const teams = (offerId) => groupRows.map(groupView).filter((row) => row.offerId === offerId);
  const myGroups = () => groupRows.map(groupView).filter((row) => row.mine);
  function groupForOrder(order) {
    const id =
      order?.groupId ||
      order?.group_id ||
      order?.raw?.group_id ||
      order?.items?.find((i) => i.promotion?.type === C.PromotionType.Group)?.promotion?.teamId;
    return groupView(groupRows.find((g) => g.id === id));
  }
  function orderStatus(order) {
    const group = groupForOrder(order);
    return group
      ? group.status === C.GroupViewStatus.Formed
        ? '已成团'
        : group.status === C.GroupViewStatus.Expired
          ? '拼团已结束'
          : '等待成团'
      : '支付状态以订单为准';
  }
  function quoteData(items) {
    const event = (items || []).find((i) => [C.PromotionType.Flash, C.PromotionType.Group].includes(i.promotion?.type));
    if (event) {
      const row = offerByID(event.promotion.offerId);
      return {
        activity_id:
          row?.activityId ||
          event.promotion.activityId ||
          (event.promotion.type === C.PromotionType.Flash ? 'flash-demo' : 'group-demo'),
        ...(event.promotion.type === C.PromotionType.Group ? { group_id: event.promotion.teamId } : {}),
      };
    }
    const addon = (items || []).find((i) => i.promotion?.type === C.PromotionType.Exchange);
    if (addon) return { addon_sku_id: addon.id === 'coffee' ? 'coffee' : sku(findProduct(addon.id)) };
    const chosen = campaignState(items);
    return chosen.selectedId ? { activity_id: chosen.selectedId } : {};
  }
  function validate(items) {
    if (!Array.isArray(items) || !items.length) return '请先选择商品';
    const restricted = items.filter((i) => [C.PromotionType.Flash, C.PromotionType.Group].includes(i.promotion?.type));
    if (restricted.length && (restricted.length !== 1 || items.length !== 1 || count(restricted[0].qty) !== 1))
      return '秒杀和拼团每单限一件商品';
    const addons = items.filter((i) => i.promotion?.type === C.PromotionType.Exchange);
    if (addons.length > 1) return '每单最多选择一件换购商品';
    for (const item of items.filter((i) => i.promotion)) {
      const row = offerByID(item.promotion.offerId);
      if (!row || row.productId !== item.id || row.status !== C.PromotionOfferStatus.Active)
        return '活动信息已变化，请刷新活动后重试';
      if (item.promotion.type === C.PromotionType.Group) {
        const group = groupRows.find((g) => g.id === item.promotion.teamId);
        if (!group || group.status !== C.GroupStatus.Open || (Date.parse(group.expires_at) || 0) <= Date.now())
          return '拼团已结束，请选择其他拼团';
      }
    }
    if (addons.length && !exchangeState(items).eligible) return '尚未满足服务端换购门槛';
    return null;
  }
  function savings(items) {
    return (items || []).reduce(
      (sum, i) =>
        sum +
        Math.max(0, regularPrice(i) - (Number.isFinite(i.unitPrice) ? i.unitPrice : getPrice(i, catalogPrice(i)))) *
          count(i.qty),
      0,
    );
  }
  async function startPurchase(type, offerId, mode, teamId, session) {
    if (busy) return;
    if (session === C.PromotionSession.Tomorrow) {
      toast('后端尚未发布预告场次');
      return;
    }
    if (!window.LumoAuth?.requireLogin(location.hash)) return;
    const row = offerByID(offerId);
    if (!row || row.type !== type || row.status !== C.PromotionOfferStatus.Active) {
      toast('该活动暂不可购买，请刷新活动');
      return;
    }
    busy = true;
    try {
      const p = row.product;
      let groupId = teamId;
      if (type === C.PromotionType.Group) {
        if (mode === C.GroupPurchaseMode.Create) {
          const key = window.LumoAPI.key ? window.LumoAPI.key(C.PromotionType.Group) : 'g-' + crypto.randomUUID();
          const result = await api(C.Service.Group, C.Action.Create, {
            idempotency_key: key,
            data: { sku_id: sku(p), required: 2 },
          });
          groupId = result.id;
          groupRows = [result, ...groupRows.filter((g) => g.id !== result.id)];
        } else {
          const team = await api(C.Service.Group, C.Action.Get, { id: groupId }, { auth: false });
          groupRows = [team, ...groupRows.filter((g) => g.id !== team.id)];
          if (team.status !== C.GroupStatus.Open || count(team.occupied) >= count(team.required))
            throw new Error('该团已满员或结束');
        }
      }
      const item = {
        id: p.id,
        sku_id: row.skuId,
        unitPrice: row.price,
        regularUnitPrice: row.regularPrice,
        color: p.colors[0],
        option: p.defaultOption,
        qty: 1,
        selected: true,
        promotion: {
          type,
          offerId: row.id,
          activityId: row.activityId,
          ...(type === C.PromotionType.Group ? { mode, teamId: groupId } : { sessionId: row.sessionId }),
        },
      };
      clearExchange();
      clearCampaign();
      setCheckout(C.OrderSource.Direct, item);
      go('checkout');
    } catch (e) {
      report(e);
    } finally {
      busy = false;
    }
  }
  async function selectExchange(offerId) {
    const owner = selectionOwner();
    const items = baseItems(),
      eligibility = exchangeState(items),
      row = offerByRawID(offerId);
    if (!row || row.status !== C.PromotionOfferStatus.Active || !eligibility.eligible) {
      toast('尚未满足换购门槛，或活动库存不足');
      return;
    }
    if (eligibility.selectedId === offerId) {
      clearExchange();
      redraw();
      window.dispatchEvent(new CustomEvent('lumo:checkout-changed'));
      return;
    }
    const revision = ++selectionRevision;
    try {
      const decision = await api(
        C.Service.Campaign,
        C.Action.Evaluate,
        {
          data: {
            subtotal_cents: Math.round(eligibility.baseAmount * 100),
            addon_sku_id: row.productId === 'coffee' ? 'coffee' : row.skuId,
          },
        },
        { auth: false },
      );
      if (owner !== selectionOwner() || revision !== selectionRevision) return;
      if (!decision.eligible) {
        toast('后端确认本单暂不符合换购资格');
        return;
      }
      // A cart edit during the request invalidates this selection instead of
      // applying a promotion to a different set of goods.
      if (contextKey(baseItems()) !== contextKey(items)) {
        toast('结算商品已变化，请重新选择换购');
        return;
      }
      clearCampaign();
      selectedExchange = { offerId, context: contextKey(items), owner };
      redraw();
      window.dispatchEvent(new CustomEvent('lumo:checkout-changed'));
    } catch (e) {
      if (owner === selectionOwner() && revision === selectionRevision) report(e);
    }
  }
  function rules(type) {
    if (campaignTypes.includes(type)) {
      const activity = campaigns(type)[0];
      modal(
        `${campaignLabels[type]}规则`,
        `<p class="modal-desc" style="line-height:2">${esc(activity ? campaignRule(activity) + '。' : '')}活动以订单中的普通商品总金额和总件数计算，每单可选一种活动，不能同时参加秒杀、拼团或换购。符合条件时可继续使用优惠券与积分，每人每活动限参与一次，最终资格、优惠和名额由服务端报价及下单确认。</p><button class="primary" data-action="close-modal">知道了</button>`,
      );
      return;
    }
    const titles = {
      [C.PromotionType.Flash]: '秒杀规则',
      [C.PromotionType.Group]: '拼团规则',
      exchange: '加价换购规则',
    };
    const copy = {
      [C.PromotionType.Flash]:
        '活动商品、价格、库存与限购资格由服务端裁决。提交请求后进入队列，请等待服务端返回订单结果；重复提交使用同一请求键。尚未配置的未来场次不会开放购买。',
      [C.PromotionType.Group]:
        '两位不同用户分别下单并经服务端确认支付后成团。新开团有效期为30分钟，具体到期时间以拼团返回为准。未成团订单按后端关闭与退款流程处理，成团后仍需库存及订单资源确认才进入待发货。',
      exchange:
        '换购门槛、商品与优惠金额以服务端活动配置及报价为准。每单最多一件换购品，换购品金额不计入主商品门槛。秒杀、拼团订单不能同时换购。',
    };
    modal(
      titles[type] || '活动规则',
      '<p class="modal-desc" style="line-height:2">' +
        esc(copy[type] || copy.flash) +
        '</p><button class="primary" data-action="close-modal">知道了</button>',
    );
  }
  async function copyInvite(teamId) {
    const team = groupRows.find((g) => g.id === teamId);
    if (!team || team.status !== C.GroupStatus.Open) {
      toast('该拼团当前不可邀请');
      return;
    }
    const url = location.origin + location.pathname + '#/promotions/group/' + encodeURIComponent(teamId);
    try {
      await navigator.clipboard.writeText(url);
      toast('邀请链接已复制，请好友登录自己的账号参团');
    } catch (_) {
      modal(
        '邀请好友参团',
        '<label class="field">邀请链接<input readonly value="' +
          esc(url) +
          '"></label><button class="primary" data-action="close-modal">完成</button>',
      );
    }
  }
  function positionActivityTab() {
    const tabs = document.querySelector('.promo-tabs'),
      active = tabs?.querySelector('a.active');
    if (active && tabs.scrollWidth > tabs.clientWidth) {
      const item = active.getBoundingClientRect(),
        bounds = tabs.getBoundingClientRect();
      if (item.left < bounds.left || item.right > bounds.right)
        tabs.scrollLeft += item.left - bounds.left - (tabs.clientWidth - item.width) / 2;
    }
  }
  window.addEventListener('resize', positionActivityTab);
  function afterRender() {
    clearInterval(timer);
    positionActivityTab();
    const update = () =>
      document.querySelectorAll('[data-promo-countdown]').forEach((node) => {
        const end = Number(node.dataset.promoCountdown);
        if (!(end > 0)) {
          node.textContent = '以活动配置为准';
          return;
        }
        const seconds = Math.max(0, Math.floor((end - Date.now()) / 1000));
        node.textContent = [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60]
          .map((n) => String(n).padStart(2, '0'))
          .join(':');
      });
    if (document.querySelector('[data-promo-countdown]')) {
      update();
      timer = setInterval(update, 1000);
    }
  }
  document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-promo-action]');
    if (!button || button.disabled) return;
    const { promoAction: action, offer: offerId, team, type, session, campaign, product } = button.dataset;
    if (action === 'select-campaign') selectCampaign(campaign);
    if (action === 'remove-campaign') {
      clearCampaign();
      checkoutChanged();
    }
    if (action === 'buy-campaign') startCampaignPurchase(campaign, product, button.dataset.detail === 'true');
    if (action === 'buy-flash') startPurchase(C.PromotionType.Flash, offerId, null, null, session);
    if (action === 'create-group') startPurchase(C.PromotionType.Group, offerId, C.GroupPurchaseMode.Create);
    if (action === 'join-group') startPurchase(C.PromotionType.Group, offerId, C.GroupPurchaseMode.Join, team);
    if (action === 'select-exchange') selectExchange(offerId);
    if (action === 'remove-exchange') {
      clearExchange();
      redraw();
      window.dispatchEvent(new CustomEvent('lumo:checkout-changed'));
    }
    if (action === 'copy-invite') copyInvite(team);
    if (action === 'show-rules') rules(type);
    if (action === 'refresh-promotions') {
      button.disabled = true;
      refresh()
        .then(redraw)
        .catch(report)
        .finally(() => {
          button.disabled = false;
        });
    }
  });
  const resetSelections = () => {
    selectionEpoch++;
    busy = false;
    clearExchange();
    clearCampaign();
  };
  window.addEventListener('lumo:login', resetSelections);
  window.addEventListener('lumo:logout', resetSelections);
  window.LumoPromotions = {
    flashOffers,
    groupOffers,
    exchangeOffers,
    offerFor,
    teams,
    myGroups,
    baseItems,
    getPrice,
    regularPrice,
    eligibleAmount,
    exchangeState,
    augmentItems,
    validate,
    campaigns,
    campaignRule,
    campaignEligibility,
    campaignState,
    selectCampaign,
    clearCampaign,
    startCampaignPurchase,
    onPaid: () => null,
    savings,
    orderStatus,
    groupForOrder,
    afterRender,
    clearExchange,
    quoteData,
    refresh,
    status: () => {
      ensureReadScope();
      return {
        loaded,
        loading: !!refreshPromise,
        error: loadError,
        hasData: activities.length > 0 || sessions.length > 0 || groupRows.length > 0,
        ready: {
          flash: resourceLoaded.campaign && resourceLoaded.flash,
          group: resourceLoaded.campaign && resourceLoaded.group,
          exchange: resourceLoaded.campaign,
          campaign: resourceLoaded.campaign,
          mygroups: resourceLoaded.group,
        },
      };
    },
  };
})();
