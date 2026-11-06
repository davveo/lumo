(function () {
  'use strict';
  const C = LumoConstants,
    { base: B, at } = LumoAdminDemoData,
    clone = (v) => JSON.parse(JSON.stringify(v));
  let enabled = false,
    onChange = () => {};
  try {
    enabled = sessionStorage.getItem('lumo-admin-demo-v1') === 'true';
  } catch (_) {}
  const requests = [];
  const registry = {
    'catalog/admin_list': B.products,
    'catalog/list': B.products,
    'iam/admin_list': B.users,
    'iam/staff_list': B.staff,
    'iam/audit_list': B.audit,
    'order/admin_list': B.orders,
    'order/list': B.orders,
    'payment/admin_list': B.payments,
    'payment/list': B.payments,
    'fulfillment/admin_list': B.fulfillments,
    'fulfillment/list': B.fulfillments,
    'aftersale/admin_list': B.aftersales,
    'aftersale/list': B.aftersales,
    'refund/admin_list': B.refunds,
    'refund/list': B.refunds,
    'invoice/admin_list': B.invoices,
    'invoice/list': B.invoices,
    'helpdesk/admin_list': B.tickets,
    'helpdesk/list': B.tickets,
    'inventory/admin_list': B.stocks,
    'inventory/ledger': B.ledger,
    'inventory/adjustments': B.ledger,
    'inventory/supplier_list': B.suppliers,
    'inventory/procurement_list': B.purchases,
    'coupon/templates_admin': B.coupons,
    'coupon/templates': B.coupons,
    'coupon/templates_snapshot': B.coupons,
    'member/segment_list': B.segments,
    'engagement/journey_rules': B.rules,
    'engagement/journey_list': B.journeys,
    'helpdesk/export_list': B.exports,
    'interaction/list': B.interactions,
    'content/admin_list': B.content,
    'campaign/admin_list': B.campaigns,
    'campaign/list': B.campaigns,
    'group/admin_list': B.groups,
    'group/list': B.groups,
    'reconciliation/admin_list': B.reports,
    'reconciliation/list': B.reports,
    'points/offers_admin': B.offers,
    'points/redemptions_admin': B.redemptions,
    'points/ledger': B.ledger,
  };
  const policies = {
    iam: {
      password_enabled: true,
      phone_enabled: true,
      wechat_enabled: true,
      qq_enabled: true,
      allow_registration: true,
      otp_ttl_seconds: 300,
      otp_cooldown_seconds: 60,
    },
    payment: { demo_enabled: true, wechat_enabled: true, alipay_enabled: true, card_enabled: true },
    points: { checkin_points: 20, deduction_bps: 1000, reward_per_yuan: 1 },
    member: { gold_growth: 1000, growth_spend_cents: 10000 },
    engagement: { price_enabled: true, stock_enabled: true, ttl_days: 30, max_per_user: 20 },
    logistics: {
      name: '标准包邮 · 华东仓',
      enabled: true,
      promise_enabled: true,
      warehouse: '杭州示例中心仓',
      handling_days: 1,
      cutoff_hour: 16,
      transit_min_days: 1,
      transit_max_days: 3,
      mode: 'flat',
      base_cents: 600,
      free_threshold_cents: 9900,
      extra_cents: 200,
      remote_regions: ['西藏', '新疆'],
    },
    aftersale: {
      configured: true,
      review_hours: 24,
      refund_submit_hours: 48,
      return_recipient: '售后服务组',
      return_phone: '13800000000',
      return_address: '示例售后中心 88 号',
      instructions: '保持商品及包装完整，审核通过后按说明寄回。',
    },
    recommendation: { enabled: true, personalized: true, limit: 8, weights: { view: 1, favorite: 3, paid: 5 } },
    search: { enabled: true, synonyms: { 耳麦: ['耳机'], 背包: ['双肩包'] }, boosts: [], zero_result_recovery: true },
  };
  function page(rows, c = {}) {
    const d = c.data || {},
      size = Math.min(100, Math.max(1, Number(d.page_size || d.limit) || 20)),
      p = Math.max(1, Number(d.page) || 1),
      offset = Number(d.offset) || (p - 1) * size;
    const keyword = String(d.keyword || '').toLowerCase();
    let selected = rows.filter(
      (r) =>
        !keyword ||
        [r.id, r.name, r.username, r.title, r.order_id, r.reason, r.sku_id].some((x) =>
          String(x || '')
            .toLowerCase()
            .includes(keyword),
        ),
    );
    if (d.status)
      selected = selected.filter((r) => r.status === d.status || r.state === d.status || r.role === d.status);
    if (d.kind) selected = selected.filter((r) => r.kind === d.kind);
    if (d.ids) selected = selected.filter((r) => d.ids.includes(r.id || r.sku_id));
    return {
      items: selected.slice(offset, offset + size),
      total: selected.length,
      page: p,
      page_size: size,
      limit: size,
      offset,
      has_more: offset + size < selected.length,
      next_cursor: '',
    };
  }
  function summary(rows) {
    const states = Object.entries(
      Object.groupBy
        ? Object.groupBy(rows, (r) => r.status || r.state || r.role || 'active')
        : rows.reduce((a, r) => {
            (a[r.status || r.state || r.role || 'active'] ||= []).push(r);
            return a;
          }, {}),
    ).map(([status, rs]) => ({
      status,
      count: rs.length,
      amount_cents: rs.reduce((s, r) => s + Number(r.amount_cents || r.payable_cents || 0), 0),
    }));
    return {
      total: rows.length,
      states,
      daily: Array.from({ length: 7 }, (_, i) => ({
        day: at(6 - i).slice(0, 10),
        status: C.PaymentStatus.Succeeded,
        count: 10 + i * 3,
        amount_cents: 180000 + i * 23400,
      })),
    };
  }
  function providers(service) {
    const ids = service === 'iam' ? ['phone', 'wechat', 'qq'] : ['wechat', 'alipay', 'card'];
    return {
      items: ids.map((id) => ({
        id,
        name: { phone: '手机验证码', wechat: '微信', qq: 'QQ', alipay: '支付宝', card: '银行卡' }[id],
        configured: true,
        parameters_complete: true,
        secure_storage_ready: true,
        source: 'preview',
        version: 2,
        reason: '样例参数，仅用于界面预览',
        values: {
          app_id: 'preview-app-' + id,
          merchant_id: 'PREVIEW-MERCHANT',
          client_id: 'preview-client',
          redirect_uri: 'https://example.invalid/callback',
          environment: 'sandbox',
          webhook_url: 'https://example.invalid/sms',
        },
        secret_configured: { app_secret: true, private_key: true, api_key: true, client_secret: true },
        fields: [
          { key: 'app_id', label: '应用编号', type: 'text', required: true, help: '示例应用编号' },
          { key: 'redirect_uri', label: '回调地址', type: 'url', help: '仅展示示例回调格式' },
          { key: 'environment', label: '运行环境', type: 'select', options: ['sandbox', 'production'] },
          { key: 'app_secret', label: '应用密钥', type: 'text', secret: true, help: '演示占位，不含真实密钥' },
        ],
      })),
    };
  }
  function policy(service) {
    const config = policies[service];
    return {
      config,
      version: 2,
      methods: [],
      capabilities: Object.entries(config)
        .filter(([k]) => k.endsWith('_enabled'))
        .map(([key]) => ({ id: key.replace('_enabled', ''), configured: true, reason: '示例渠道' })),
    };
  }
  function profile(id) {
    const u = B.users.find((u) => u.id === id) || B.users[0];
    return {
      ...u,
      profile: { display_name: u.display_name, level: C.MemberLevel.Gold, growth: 1260 },
      card: {
        tags: ['品质生活', '活跃老客'],
        note: '示例服务备注：优先关注到货和配送进展。',
        version: 1,
        updated_by: B.staff[0].username,
      },
      level: C.MemberLevel.Gold,
      growth: 1260,
      available: 2680,
      frozen: 200,
    };
  }
  function plan(id) {
    const o = B.orders.find((o) => o.id === id) || B.orders[0];
    return {
      order_id: o.id,
      parcelized: true,
      version: 2,
      lines: o.items.map((l) => ({
        sku_id: l.sku_id,
        quantity: l.quantity,
        cancelled: 0,
        shipped: 0,
        delivered: 0,
        held: 0,
        planned: 0,
        dispatching: 0,
        remaining: l.quantity,
      })),
      parcels: [],
    };
  }
  function dashboard() {
    const r = {};
    for (const [key, rows] of Object.entries({
      order: B.orders,
      payment: B.payments,
      fulfillment: B.fulfillments,
      aftersale: B.aftersales,
      refund: B.refunds,
      invoice: B.invoices,
      users: B.users,
    }))
      r[key] = summary(rows);
    r.generated_at = at(0);
    r.errors = {};
    r.insights = {
      paid_cents: 3698800,
      refund_cents: 129900,
      net_cents: 3568900,
      average_order_cents: 16223,
      buyers: 156,
      paid_orders: 228,
      repeat_purchase_bps: 2460,
      views: 1860,
      carts: 760,
      checkouts: 390,
      paid: 228,
      top_products: B.products.slice(0, 5).map((p, i) => ({ ...p, quantity: 156 - i * 18 })),
      funnel: [
        ['view', 1860],
        ['add_cart', 760],
        ['checkout', 390],
        ['paid', 228],
      ].map(([stage, users]) => ({ stage, users, cohort_users: users })),
      scope: '样例经营指标 · 仅用于展示',
      historical_backfill: true,
    };
    return r;
  }
  function drilldown(c) {
    const items = B.products.map((p, i) => ({
      key: p.id,
      name: p.name,
      views: 680 - i * 9,
      carts: 260 - i * 3,
      searches: 120,
      zero_results: 3,
      paid_orders: 42 - i,
      paid_cents: p.price_cents * 10,
      refund_cents: 9900,
      cost_known_orders: 30,
      campaign_cost: 30000,
      coupon_cost: 12000,
      points_cost: 3000,
    }));
    return {
      ...page(items, c),
      anonymous_sessions: 1240,
      logged_sessions: 1860,
      searches: 920,
      zero_results: 18,
      paid_orders: 228,
      missing_attribution_orders: 8,
      abandoned_cart_sessions: 125,
      immature_cart_sessions: 18,
      cost_known_orders: 216,
      campaign_cost: 56800,
      coupon_cost: 42800,
      points_cost: 12900,
      coverage_since: at(30),
      funnel: { view: 1860, add_cart: 760, checkout: 390, paid: 228 },
      scope: '示例：同一会话内依次发生的行为，仅用于演示。',
    };
  }
  function extra(service, action, c) {
    const key = service + '/' + action,
      d = c.data || {},
      id = c.id || '';
    if (action === C.Action.PolicyGet && policies[service]) return policy(service);
    if (action === C.Action.ProviderGet && ['iam', 'payment'].includes(service)) return providers(service);
    if (action === C.Action.AdminSummary) {
      const map = {
        order: B.orders,
        payment: B.payments,
        fulfillment: B.fulfillments,
        aftersale: B.aftersales,
        refund: B.refunds,
        invoice: B.invoices,
      };
      if (map[service]) return summary(map[service]);
    }
    if (key === 'catalog/batch_get') return page(B.products, { data: { ids: d.ids, limit: 100 } });
    if (key === 'inventory/batch_get') return { items: B.stocks.filter((s) => d.ids?.includes(s.sku_id)) };
    if (key === 'catalog/get') return B.products.find((p) => p.id === id) || B.products[0];
    if (key === 'inventory/get') return B.stocks.find((p) => p.sku_id === id) || B.stocks[0];
    if (key === 'member/get' || key === 'member/customer_get' || key === 'points/admin_get') return profile(id);
    if (key === 'management/customer_profile')
      return {
        account: B.users.find((u) => u.id === id) || B.users[0],
        can_edit: true,
        can_segment: true,
        generated_at: at(0),
        errors: {},
        sections: {
          member: profile(id),
          stats: {
            stats: {
              paid_orders: 12,
              paid_cents: 1288800,
              refund_cents: 19900,
              net_cents: 1268900,
              last_paid_at: at(2),
            },
            scope: '示例客户交易记录',
            generated_at: at(0),
          },
          orders: page(B.orders, { data: { limit: 5 } }),
          aftersales: page(B.aftersales, { data: { limit: 3 } }),
          tickets: page(B.tickets, { data: { limit: 3 } }),
          points: { available: 2680, frozen: 200 },
          coupons: { available: 8, used: 12 },
        },
      };
    if (key === 'member/segment_preview') return page(B.users, c);
    if (key === 'management/dashboard') return dashboard();
    if (key === 'analytics/drilldown') return drilldown(c);
    if (key === 'management/global_search')
      return {
        groups: [
          {
            label: '商品',
            items: B.products.slice(0, 4).map((p) => ({ id: p.id, title: p.name, subtitle: p.brand, tab: 'products' })),
          },
        ],
      };
    if (key === 'management/tasks')
      return page(
        Array.from({ length: 24 }, (_, i) => ({
          id: 'preview_task_' + i,
          service: ['payment', 'order', 'notification'][i % 3],
          state: [C.OutboxState.Pending, C.InboxState.Retry, C.InboxState.Dead][i % 3],
          status: [C.OutboxState.Pending, C.InboxState.Retry, C.InboxState.Dead][i % 3],
          event_type: [C.Event.OrderCompleted, C.Event.RefundSucceeded][i % 2],
          event_id: 'preview_event_' + i,
          attempts: i % 4,
          last_error: '示例：渠道临时不可用，等待重试',
          next_attempt_at: at(-0.01),
          created_at: at(1),
          updated_at: at(0.1),
        })),
        c,
      );
    if (key === 'helpdesk/work_queue')
      return {
        items: B.fulfillments.slice(0, 6).map((r, i) => ({
          ...r,
          service: 'fulfillment',
          work_id: 'preview_work_' + i,
          resource_id: r.id,
          title: B.products[i].name,
          waiting_hours: 12 + i * 9,
          overdue: i % 2 === 0,
          due_at: at(i % 2 ? -0.2 : 0.5),
          target_label: '24小时内确认出库',
          assignment: i % 2 ? { assignee_id: B.staff[2].id, assignee_name: B.staff[2].username } : null,
        })),
        errors: {},
        has_more_windows: false,
        actor_id: 'admin',
        actor_name: '管理员',
        can_assign_others: true,
        notice: '示例待办队列，可查看不同负责人和跟进状态。',
      };
    if (key === 'helpdesk/work_views')
      return {
        items: [
          { id: 'preview_view', name: '待发货 · 超时优先', filters: { service: 'fulfillment', overdue_only: true } },
        ],
      };
    if (key === 'engagement/journey_list')
      return {
        ...page(B.journeys, c),
        counts: { queued: 16, delivered: 128, clicked: 42, recovered: 18 },
        scope: '示例触达数据，付款匹配不代表因果提升。',
      };
    if (key === 'engagement/admin_list')
      return page(
        B.users.map((u, i) => ({
          id: 'preview_subscription_' + i,
          user_id: u.id,
          sku_id: B.products[i % 24].id,
          product_name: B.products[i % 24].name,
          kind: i % 2 ? C.SubscriptionKind.Price : C.SubscriptionKind.Stock,
          status: [C.SubscriptionStatus.Active, C.SubscriptionStatus.Triggered, C.SubscriptionStatus.Expired][i % 3],
          target_cents: 19900,
          current_cents: 24900,
          expires_at: at(-10),
          created_at: at(i / 2),
        })),
        c,
      );
    if (key === 'logistics/exceptions')
      return page(
        B.fulfillments.slice(0, 23).map((r, i) => ({
          id: 'preview_exception_' + i,
          logistics_id: r.id,
          tracking_id: 'PREVIEW_TRACK_' + i,
          kind: ['late_pickup', 'stalled', 'carrier_exception'][i % 3],
          status: i % 3 === 0 ? C.DeliveryExceptionStatus.Resolved : C.DeliveryExceptionStatus.Open,
          details: ['揽收等待超过目标时长', '运输轨迹暂未更新', '收件人暂时无法联系'][i % 3],
          resolution: i % 3 === 0 ? '已联系渠道安排重派' : '',
          version: 1,
          created_at: at(2),
        })),
        c,
      );
    if (key === 'logistics/tracking')
      return {
        shipments: [
          {
            id: 'preview_shipment_1',
            tracking_id: 'PREVIEW_TRACK_1001',
            kind: C.FulfillmentKind.Parcel,
            status: C.TrackingStatus.InTransit,
            provider: '示例物流',
            lines_json: JSON.stringify([{ sku_id: B.products[0].id, quantity: 1 }]),
            events: [
              {
                status: C.TrackingStatus.InTransit,
                occurred_at: at(0.3),
                location: '杭州转运中心',
                description: '包裹已离开分拨中心，正在前往目的地。',
                source: 'staff',
              },
              {
                status: C.TrackingStatus.PickedUp,
                occurred_at: at(1),
                location: '杭州示例中心仓',
                description: '快递已揽收',
                source: 'staff',
              },
            ],
            events_total: 2,
            has_more_events: false,
          },
        ],
      };
    if (key === 'fulfillment/line_state') return plan(id);
    if (key === 'fulfillment/get') {
      const r = B.fulfillments.find((r) => r.id === id) || B.fulfillments[0];
      return { ...r, plan: plan(id) };
    }
    if (key === 'inventory/procurement_get') {
      const r = B.purchases.find((r) => r.id === id) || B.purchases[0];
      return {
        order: r,
        lines: JSON.parse(r.lines_json),
        receipts: [
          {
            id: 'preview_receipt',
            lines: [{ sku_id: B.products[0].id, quantity: 20, damaged: 1 }],
            note: '示例到货验收记录',
            created_at: at(1),
          },
        ],
      };
    }
    if (key === 'inventory/replenishment_get')
      return {
        items: B.products.slice(0, 8).map((p) => ({
          sku_id: p.id,
          name: p.name,
          on_hand: 8,
          reserved: 2,
          allocated: 3,
          available: 3,
          inbound: 5,
          threshold: 10,
          target: 30,
          suggested: 22,
          suggested_quantity: 22,
          sales_quantity: 38,
          forecast_ready: true,
        })),
        scope: '示例补货建议，基于近30天销量和采购交期。',
      };
    if (key === 'group/rules_admin')
      return page(
        B.products.slice(0, 8).map((p) => ({
          id: 'preview_rule_' + p.id,
          name: p.name + ' · 三人成团',
          sku_id: p.id,
          required: 3,
          size: 3,
          active: true,
          discount_cents: 2000,
          ttl_seconds: 86400,
          version: 1,
        })),
        c,
      );
    if (key === 'flash/admin_list' || key === 'flash/list')
      return page(
        B.products.slice(0, 8).map((p, i) => ({
          id: 'preview_flash_' + i,
          name: p.name + ' · 限时抢购',
          sku_id: p.id,
          active: true,
          stock: 100,
          quota: 100,
          used: 36,
          discount_cents: 2000,
          starts_at: at(1),
          ends_at: at(-1),
        })),
        c,
      );
    if (key === 'flash/requests_admin')
      return page(
        B.orders.slice(0, 22).map((o, i) => ({
          id: 'preview_flash_request_' + i,
          order_id: o.id,
          user_id: o.user_id,
          sku_id: B.products[i % 24].id,
          status: i % 3 ? C.FlashRequestStatus.Succeeded : C.FlashRequestStatus.Queued,
          created_at: o.created_at,
        })),
        c,
      );
    if (key === 'notification/templates_admin')
      return {
        ...page(B.templates, c),
        events: Object.fromEntries(B.templates.map((t) => [t.event_type, t.title])),
        bindings: {},
      };
    if (key === 'catalog/draft_list')
      return page(
        B.products.slice(0, 6).map((p) => ({
          product_id: p.id,
          name: p.name + ' · 草稿',
          status: C.ProductDraftStatus.Draft,
          version: 2,
          base_version: 3,
          updated_at: at(0.5),
        })),
        c,
      );
    if (key === 'catalog/workspace_get')
      return {
        product: B.products.find((p) => p.id === id) || B.products[0],
        draft: null,
        versions: [
          { product_id: id, version: 3, actor_id: B.staff[0].username, created_at: at(1) },
          { product_id: id, version: 2, actor_id: B.staff[0].username, created_at: at(4) },
        ],
        publications: [],
      };
    if (key === 'catalog/import_get')
      return {
        id: 'preview_import_1',
        status: C.CatalogImportStatus.Completed,
        total: 24,
        succeeded: 24,
        failed: 0,
        rows: B.products.slice(0, 5).map((p, i) => ({
          row: i + 1,
          id: p.id,
          name: p.name,
          status: C.CatalogImportRowStatus.Succeeded,
          errors: [],
        })),
        created_at: at(1),
      };
    if (key === 'interaction/review_get') return B.interactions.find((r) => r.id === id) || B.interactions[1];
    if (key === 'helpdesk/get') {
      const r = B.tickets.find((r) => r.id === id) || B.tickets[0];
      return {
        ...r,
        messages: [
          { id: 'preview_message_1', body: r.body, role: C.Role.User, user_id: r.user_id, created_at: r.created_at },
          {
            id: 'preview_message_2',
            body: '你好，已经为你确认，预计今天安排发货。',
            role: C.Role.Admin,
            user_id: B.staff[1].id,
            created_at: at(0.2),
          },
        ],
        messages_total: 2,
        has_more_messages: false,
      };
    }
    if (key === 'invoice/reversals')
      return page(
        B.refunds.slice(0, 3).map((r) => ({ ...r, status: C.InvoiceReversalStatus.DemoAdjusted })),
        c,
      );
    if (key === 'reconciliation/get')
      return {
        report: B.reports.find((r) => r.id === id) || B.reports[0],
        items: [
          {
            id: 'preview_difference',
            order_id: B.orders[0].id,
            kind: 'amount',
            expected_cents: 89900,
            actual_cents: 88900,
            status: 'unresolved',
            note: '示例：渠道费用差异，待核对',
          },
        ],
        total: 1,
      };
    if (key === 'content/get')
      return {
        id,
        body: JSON.stringify(
          id === 'storefront-categories'
            ? {
                version: 1,
                groups: Array.from({ length: 4 }, (_, i) => ({
                  id: 'preview_category_' + (i + 1),
                  name: ['数码影音', '家用电器', '户外出行', '日用生活'][i],
                  title: ['数码影音', '家用电器', '户外出行', '日用生活'][i],
                  rows: [
                    {
                      label: '精选推荐',
                      items: [
                        {
                          name: B.products[i].name,
                          label: B.products[i].name,
                          target: '#/product/' + B.products[i].id,
                        },
                      ],
                    },
                  ],
                })),
              }
            : {
                brand: 'LUMO',
                brandSuffix: 'Mall',
                tagline: '品质日常，灵感生活',
                accent: '#4f46e5',
                notice: '好物有礼，欢迎探索品质生活',
                phone: '400-000-0000',
              },
        ),
      };
    if (key === 'content/popup_list')
      return {
        items: B.coupons.slice(0, 6).map((r, i) => ({
          id: 'preview_popup_' + i,
          name: '会员礼遇 · ' + r.name,
          title: '你的生活灵感，从一份心意开始',
          subtitle: r.name,
          body: '精选好物礼券，陪你开启品质日常。',
          coupon_id: r.id,
          enabled: i % 3 !== 0,
          active: i % 3 !== 0,
          status: C.PopupStatus.Published,
          version: 2,
          priority: 10 - i,
          trigger: 'welcome',
          pages: ['home'],
          audience: 'all',
          starts_at: at(3),
          ends_at: at(-30),
          image: 'assets/coffee.jpg',
          created_at: at(4),
        })),
        settings: { enabled: true, max_per_session: 1, min_gap_seconds: 60 },
      };
    if (key === 'analytics/admin_list' || key === 'analytics/list')
      return page(
        B.orders.map((o, i) => ({
          id: 'preview_behavior_' + i,
          user_id: o.user_id,
          event_type: [
            C.AnalyticsBehavior.View,
            C.AnalyticsBehavior.Search,
            C.AnalyticsBehavior.Click,
            C.Event.OrderCompleted,
          ][i % 4],
          action: [C.AnalyticsBehavior.View, C.AnalyticsBehavior.Search][i % 2],
          sku_id: B.products[i % 24].id,
          product_id: B.products[i % 24].id,
          source: 'homepage',
          body: '示例行为记录',
          created_at: o.created_at,
          details: { scene: 'homepage', query: '耳机', result_count: 12 },
        })),
        c,
      );
    const getMap = {
      order: B.orders,
      payment: B.payments,
      refund: B.refunds,
      aftersale: B.aftersales,
      invoice: B.invoices,
      iam: B.users,
    };
    if (action === C.Action.Get && getMap[service])
      return getMap[service].find((r) => r.id === id) || getMap[service][0];
    return undefined;
  }
  async function call(service, action, command = {}) {
    requests.push({ service, action, id: command.id || '', at: Date.now() });
    if (requests.length > 500) requests.shift();
    const key = service + '/' + action;
    let result = extra(service, action, command);
    if (result === undefined && registry[key]) result = page(registry[key], command);
    if (result === undefined) {
      const error = new Error('当前为示例预览。请切回“业务数据”后执行此操作。');
      error.code = 'preview_read_only';
      throw error;
    }
    return clone(result);
  }
  function banner() {
    return enabled
      ? `<div class="admin-demo-banner"><span class="admin-demo-dot"></span><div><strong>正在浏览示例数据</strong><p>用于体验列表、状态与页面样式，保存业务操作请切回业务数据。</p></div><button class="outline" type="button" data-demo-action="disable">切回业务数据</button></div>`
      : '';
  }
  function renderToggle() {
    const b = document.getElementById('admin-demo-toggle');
    if (b) {
      b.setAttribute('aria-pressed', String(enabled));
      b.textContent = enabled ? '示例预览中' : '示例预览';
    }
    document.body.dataset.adminDemo = String(enabled);
  }
  async function toggle(next) {
    if (next === enabled) return;
    if (
      !(await LumoAdminFeedback.confirm(
        next ? '切换到示例预览，查看不同业务状态的列表和表单效果。' : '切回当前账号的业务数据。',
        { title: next ? '打开示例预览' : '返回业务数据', confirmLabel: '确认切换' },
      ))
    )
      return;
    enabled = next;
    try {
      sessionStorage.setItem('lumo-admin-demo-v1', String(enabled));
    } catch (_) {}
    renderToggle();
    onChange();
  }
  document.addEventListener('click', (e) => {
    if (e.target.closest('#admin-demo-toggle')) void toggle(!enabled);
    if (e.target.closest('[data-demo-action="disable"]')) void toggle(false);
  });
  window.LumoAdminDemo = {
    active: () => enabled,
    call,
    banner,
    requests,
    configure: (options) => {
      onChange = options.onChange;
      renderToggle();
    },
    sampleSKU: () => B.products[0].id,
  };
})();
