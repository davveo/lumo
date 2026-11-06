'use strict';
// Populate a local demo installation through its public business APIs.
// Checkpoints retain completed steps so a rerun never rewinds an operator's work.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '..');
const context = { window: {} };
vm.runInNewContext(fs.readFileSync(path.join(root, 'web/dist/constants.js'), 'utf8'), context);
const C = context.window.LumoConstants;
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const label = '【体验样例】';
const password = process.env.LUMO_DEMO_PASSWORD || 'LumoDemo@2026';
const reads = [
  ['catalog', 'admin_list'],
  ['iam', 'admin_list'],
  ['iam', 'staff_list'],
  ['order', 'admin_list'],
  ['payment', 'admin_list'],
  ['fulfillment', 'admin_list'],
  ['aftersale', 'admin_list'],
  ['refund', 'admin_list'],
  ['invoice', 'admin_list'],
  ['helpdesk', 'admin_list'],
  ['inventory', 'admin_list'],
  ['inventory', 'supplier_list'],
  ['inventory', 'procurement_list'],
  ['coupon', 'templates_admin'],
  ['member', 'segment_list'],
  ['engagement', 'journey_rules'],
  ['engagement', 'journey_list'],
  ['helpdesk', 'export_list'],
  ['interaction', 'list'],
  ['content', 'admin_list'],
  ['campaign', 'admin_list'],
  ['group', 'admin_list'],
  ['group', 'rules_admin'],
  ['flash', 'admin_list'],
  ['flash', 'requests_admin'],
  ['reconciliation', 'admin_list'],
  ['points', 'offers_admin'],
  ['points', 'redemptions_admin'],
  ['iam', 'audit_list'],
  ['notification', 'templates_admin'],
  ['logistics', 'exceptions'],
  ['analytics', 'admin_list'],
  ['engagement', 'admin_list'],
  ['content', 'popup_list'],
];
function options(argv) {
  const value = (name) => {
    const i = argv.indexOf(name);
    return i < 0 ? undefined : argv[i + 1];
  };
  const base = new URL(value('--base-url') || 'http://127.0.0.1:8080');
  if (
    !['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname) ||
    !['http:', 'https:'].includes(base.protocol) ||
    base.username ||
    base.password ||
    base.search ||
    base.hash ||
    base.pathname !== '/'
  )
    throw Error('Only a local origin is supported.');
  const namespace = value('--namespace') || 'ui-demo-20261104';
  if (!/^[a-z][a-z0-9-]{5,39}$/.test(namespace))
    throw Error('Namespace must contain 6–40 lowercase letters, digits or hyphens.');
  return {
    base: base.origin,
    namespace,
    apply: argv.includes('--apply'),
    manifest: path.resolve(root, value('--manifest') || `.local/admin-demo/${namespace}.json`),
  };
}
class Seeder {
  constructor(config) {
    this.config = config;
    this.created = 0;
    this.state = fs.existsSync(config.manifest)
      ? JSON.parse(fs.readFileSync(config.manifest, 'utf8'))
      : {
          version: 1,
          namespace: config.namespace,
          origin: config.base,
          created_at: new Date().toISOString(),
          steps: {},
        };
    if (this.state.namespace !== config.namespace || this.state.origin !== config.base)
      throw Error('Manifest belongs to another local installation.');
    this.time = new Date(this.state.created_at).getTime();
    this.admin = '';
  }
  id(name) {
    return `${this.config.namespace}-${name}`;
  }
  date(days = 0) {
    return new Date(this.time + days * 86400000).toISOString();
  }
  persist() {
    fs.mkdirSync(path.dirname(this.config.manifest), { recursive: true, mode: 0o700 });
    const temporary = this.config.manifest + '.tmp';
    fs.writeFileSync(temporary, JSON.stringify(this.state, null, 2) + '\n', { mode: 0o600 });
    fs.renameSync(temporary, this.config.manifest);
  }
  async call(service, action, command = {}, token = this.admin) {
    const response = await fetch(`${this.config.base}/api/v1/${service}/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
      body: JSON.stringify(command),
      signal: AbortSignal.timeout(30000),
      redirect: 'error',
    });
    const body = await response.json();
    if (!response.ok) {
      const error = new Error(`${service}/${action} (${response.status}): ${body.error?.message || 'request failed'}`);
      error.status = response.status;
      throw error;
    }
    return body.data;
  }
  async once(key, operation) {
    if (Object.hasOwn(this.state.steps, key)) return this.state.steps[key];
    const result = await operation();
    if (JSON.stringify(result).match(/"(?:token|password|secret|authorization)"\s*:/i))
      throw Error('Credentials cannot be written to a seed manifest.');
    this.state.steps[key] = result ?? { done: true };
    this.created++;
    this.persist();
    return this.state.steps[key];
  }
  async wait(name, operation, predicate) {
    for (let i = 0; i < 80; i++) {
      let result;
      try {
        result = await operation();
      } catch (error) {
        if (error.status !== 404) throw error;
      }
      if (result && predicate(result)) return result;
      await delay(400);
    }
    throw Error(`Timed out waiting for ${name}; rerun the same command to resume.`);
  }
  async inspect() {
    const rows = [];
    for (const [service, action] of reads) {
      const result = await this.call(service, action, { data: { page: 1, page_size: 5, limit: 5 } });
      rows.push({ service, action, total: result.total ?? result.items?.length ?? result.campaigns?.length ?? 0 });
    }
    // Runtime task histories must come from valid business events, not fabricated failures.
    for (const kind of ['outbox', 'inbox']) {
      const result = await this.call('management', 'tasks', {
        data: { service: 'order', kind, page: 1, page_size: 5 },
      });
      rows.push({ service: 'management', action: kind, total: result.total ?? result.items?.length ?? 0 });
    }
    return rows;
  }
  async authenticate() {
    const session = await this.call(
      'iam',
      'login',
      {
        data: {
          username: process.env.LUMO_ADMIN_USERNAME || 'admin',
          password: process.env.LUMO_ADMIN_PASSWORD || 'Admin@2026',
        },
      },
      '',
    );
    if (session.role !== C.Role.Admin) throw Error('A local administrator account is required.');
    this.admin = session.token;
    const methods = await this.call('payment', 'methods');
    if (!methods.items?.some((item) => item.id === C.PaymentProvider.Demo))
      throw Error('Enable the local demo payment adapter before seeding.');
  }
  async accounts() {
    const people = [];
    const accountPrefix =
      this.config.namespace === 'ui-demo-20261104'
        ? 'ui_demo_1104'
        : this.config.namespace.replaceAll('-', '_').slice(0, 20);
    const usernames = [accountPrefix, accountPrefix + '_b', accountPrefix + '_c'];
    for (const [i, username] of usernames.entries()) {
      const account = await this.once(`account-${i}`, async () =>
        this.call('iam', 'register', { data: { username, password } }, ''),
      );
      const login = await this.call('iam', 'login', { data: { username, password } }, '');
      if (login.user_id !== account.user_id) throw Error('Account identity differs from the seed checkpoint.');
      const person = { id: account.user_id, username, token: login.token };
      people.push(person);
      await this.once(`profile-${i}`, () =>
        this.call(
          'member',
          'update',
          {
            data: {
              display_name: ['体验会员 · 林小满', '体验会员 · 周予安', '体验会员 · 许知夏'][i],
              preferences: { notifications: { order: true, service: true, promotion: true } },
            },
          },
          person.token,
        ),
      );
      await this.once(`customer-card-${i}`, () =>
        this.call('member', 'customer_update', {
          id: person.id,
          data: {
            tags: ['体验样例', ['品质生活', '通勤数码', '家居焕新'][i]],
            note: label + '用于演示客户资料和服务跟进，可直接修改。',
            version: 0,
          },
        }),
      );
      const address = await this.once(`address-${i}`, () =>
        this.call(
          'address',
          'create',
          {
            data: {
              recipient: ['林小满（体验）', '周予安（体验）', '许知夏（体验）'][i],
              phone: '13800000000',
              province: '浙江省',
              city: '杭州市',
              district: '西湖区',
              address: '体验路 88 号样例地址',
            },
          },
          person.token,
        ),
      );
      person.address = address.id;
      await this.once(`points-credit-${i}`, async () => {
        const balance = await this.call('points', 'admin_get', { id: person.id });
        return this.call('points', 'adjust', {
          id: person.id,
          idempotency_key: this.id(`credit-${i}`),
          data: { amount: 12000, expected_available: balance.available, reason: label + '积分兑换体验额度' },
        });
      });
    }
    for (const [i, role] of [
      'staff_operations',
      'staff_warehouse',
      'staff_support',
      'staff_finance',
      'staff_analyst',
    ].entries()) {
      const username = accountPrefix + '_' + ['ops', 'warehouse', 'support', 'finance', 'analyst'][i];
      await this.once(`staff-${i}`, () =>
        this.call('iam', 'staff_upsert', {
          data: { username, role, password, active: true, reason: label + '创建岗位权限体验账号' },
        }),
      );
    }
    return people;
  }
  async catalog() {
    const names = [
      '云感降噪耳机',
      '复古迷你冰箱',
      '轻量通勤背包',
      '精品手冲咖啡豆',
      '模块布艺沙发',
      '蓝牙便携音箱',
      '极简运动手表',
      '轻巧数码相机',
      '周末出行双肩包',
      '低库存随行包',
      '到货提醒耳机',
      '待上架生活音箱',
    ];
    const images = [
      'headphones.jpg',
      'appliance.jpg',
      'bag.jpg',
      'coffee.jpg',
      'sofa.jpg',
      'speaker.jpg',
      'watch.jpg',
      'camera.jpg',
      'bag.jpg',
      'bag.jpg',
      'headphones.jpg',
      'speaker.jpg',
    ];
    const prices = [89900, 129900, 29900, 9900, 189900, 35900, 69900, 259900, 39900, 19900, 59900, 22900];
    const products = [];
    for (let i = 0; i < names.length; i++) {
      const id = this.id(`sku-${String(i + 1).padStart(2, '0')}`);
      const product = await this.once(`product-${i}`, () =>
        this.call('catalog', 'upsert', {
          id,
          data: {
            name: label + names[i],
            price_cents: prices[i],
            old: (prices[i] + 10000) / 100,
            brand: 'LUMO体验',
            category: ['手机数码', '家用电器', '户外运动', '食品饮料', '家居生活'][i % 5],
            image: images[i],
            active: i !== 11,
            colors: ['经典款'],
            options: ['标准版'],
            defaultOption: '标准版',
            sub: '可操作的本地业务样例',
            highlights: ['样例商品', '支持真实演示业务流程'],
            service_policy: {
              configured: true,
              return_days: 7,
              return_shipping: 'case_by_case',
              warranty_months: 12,
              explanation: '本地体验样例：七天退换，一年保修。',
            },
          },
        }),
      );
      products.push({ ...product, id, price_cents: prices[i] });
      await this.once(`stock-${i}`, async () => {
        let stock;
        try {
          stock = await this.call('inventory', 'get', { id });
        } catch (error) {
          if (error.status !== 404) throw error;
          stock = { on_hand: 0 };
        }
        return this.call('inventory', 'adjust', {
          id,
          data: {
            on_hand: i === 10 ? 0 : i === 9 ? 3 : 120,
            expected_on_hand: stock.on_hand,
            reason: label + '初始化独立样例商品库存',
          },
        });
      });
      await this.once(`threshold-${i}`, async () => {
        const stock = await this.call('inventory', 'get', { id });
        return this.call('inventory', 'threshold', {
          id,
          data: { threshold: i === 9 ? 20 : 10, expected_threshold: stock.threshold, reason: label + '补货预警阈值' },
        });
      });
      if (i >= 10)
        await this.once(`draft-${i}`, () =>
          this.call('catalog', 'draft_save', {
            id,
            data: {
              product: { ...product, name: label + '待完善草稿 · ' + names[i] },
              editor: {},
              version: 0,
              base_version: product.product_version || 1,
            },
          }),
        );
    }
    return products;
  }
  async configurations(products) {
    const suppliers = [];
    for (let i = 0; i < 6; i++)
      suppliers.push(
        await this.once(`supplier-${i}`, () =>
          this.call('inventory', 'supplier_upsert', {
            id: this.id(`supplier-${i}`),
            data: {
              name:
                label +
                [
                  '杭州品质生活供应商',
                  '华东数码供货商',
                  '通勤装备合作商',
                  '精品咖啡供货商',
                  '家居生活制造商',
                  '华南音频供货商',
                ][i],
              contact: '体验联系人 · 13800000000',
              lead_days: 3 + i * 2,
              version: 0,
            },
          }),
        ),
      );
    for (let i = 0; i < 24; i++) {
      const row = await this.once(`purchase-${i}`, () =>
        this.call('inventory', 'procurement_create', {
          idempotency_key: this.id(`purchase-${i}`),
          data: {
            supplier_id: suppliers[i % 6].id,
            note: label + ['待审批备货', '已审批待到货', '部分到货待收货', '已完成验收', '已取消计划'][i % 5],
            lines: [
              {
                sku_id: products[i % 9].id,
                quantity: 20,
                unit_cost_cents: Math.round(products[i % 9].price_cents * 0.6),
              },
            ],
          },
        }),
      );
      if (i % 5 !== 0 && i % 5 !== 4)
        await this.once(`purchase-approve-${i}`, () =>
          this.call('inventory', 'procurement_approve', { id: row.id, data: { expected_version: row.version } }),
        );
      if (i % 5 === 2 || i % 5 === 3)
        await this.once(`purchase-receive-${i}`, () =>
          this.call('inventory', 'procurement_receive', {
            id: row.id,
            idempotency_key: this.id(`receipt-${i}`),
            data: {
              expected_version: row.version + 1,
              lines: [{ sku_id: products[i % 9].id, quantity: i % 5 === 2 ? 8 : 20, damaged: i % 5 === 2 ? 1 : 0 }],
            },
          }),
        );
      if (i % 5 === 4)
        await this.once(`purchase-cancel-${i}`, () =>
          this.call('inventory', 'procurement_cancel', {
            id: row.id,
            data: { expected_version: row.version, reason: label + '体验采购取消状态' },
          }),
        );
    }
    const coupons = [];
    for (let i = 0; i < 8; i++)
      coupons.push(
        await this.once(`coupon-${i}`, () =>
          this.call('coupon', 'template_upsert', {
            id: this.id(`coupon-${i}`),
            data: {
              name: label + ['新人专享满百减十', '周末满三百减三十', '品质生活九折券', '无门槛五元券'][i % 4],
              coupon_type: [C.CouponType.Reduction, C.CouponType.Reduction, C.CouponType.Discount, C.CouponType.Cash][
                i % 4
              ],
              amount_cents: i % 4 === 2 ? 0 : i % 4 === 3 ? 500 : i % 4 === 1 ? 3000 : 1000,
              min_cents: i % 4 === 0 ? 10000 : i % 4 === 1 ? 30000 : 0,
              discount_bps: i % 4 === 2 ? 9000 : 0,
              max_discount_cents: i % 4 === 2 ? 5000 : 0,
              scope_type: 'all',
              scope_values: [],
              valid_days: 30,
              remaining: 200,
              active: i !== 7,
              min_points: 0,
              min_growth: 0,
              starts_at: i === 6 ? this.date(3) : this.date(-1),
              expires_at: this.date(60),
              version: 0,
            },
          }),
        ),
      );
    for (let i = 0; i < 8; i++)
      await this.once(`segment-${i}`, () =>
        this.call('member', 'segment_upsert', {
          id: this.id(`segment-${i}`),
          data: {
            name:
              label +
              [
                '首购会员',
                '多次购买会员',
                '品质生活高客单',
                '近期活跃客户',
                '待回访老客户',
                '数码体验人群',
                '高价值会员',
                '复购潜力客户',
              ][i],
            min_orders: (i % 3) + 1,
            min_net_cents: i * 5000,
            inactive_days: i === 4 ? 30 : 0,
            version: 0,
          },
        }),
      );
    for (let i = 0; i < 12; i++)
      await this.once(`content-${i}`, () =>
        this.call('content', 'admin_upsert', {
          id: this.id(`content-${i}`),
          data: {
            kind: i % 2 ? C.ContentKind.Faq : C.ContentKind.Notice,
            title:
              label +
              ['生活好物季活动说明', '如何查询物流和申请售后', '会员积分兑换攻略', '发票申请与退款说明'][i % 4] +
              (i >= 4 ? ' · ' + (i + 1) : ''),
            body: '这是一条可编辑的本地体验内容。你可以在后台调整文案、发布状态和生效时间，继续体验前台展示。',
            status: [
              C.ContentStatus.Published,
              C.ContentStatus.Draft,
              C.ContentStatus.Scheduled,
              C.ContentStatus.Paused,
            ][i % 4],
            publish_at: i % 4 === 2 ? this.date(7) : null,
            unpublish_at: null,
            reason: label + '初始化内容展示',
            version: 0,
          },
        }),
      );
    for (let i = 0; i < 4; i++)
      await this.once(`campaign-${i}`, () =>
        this.call('campaign', 'upsert', {
          id: this.id(`campaign-${i}`),
          data: {
            name: label + ['满三百减三十', '精选好物九折', '两件享八五折', '周末直降二十'][i],
            description: '本地业务流程体验活动',
            kind: [
              C.CampaignKind.Reduction,
              C.CampaignKind.Discount,
              C.CampaignKind.QuantityDiscount,
              C.CampaignKind.Direct,
            ][i],
            threshold_cents: i === 0 ? 30000 : 0,
            discount_cents: i === 0 ? 3000 : i === 3 ? 2000 : 0,
            pay_rate_bps: i === 1 ? 9000 : i === 2 ? 8500 : 0,
            min_quantity: i === 2 ? 2 : 1,
            quota: 200,
            active: true,
            version: 0,
          },
        }),
      );
    return { suppliers, coupons };
  }
  async order(key, person, product, options = {}) {
    return this.once(`order-${key}`, async () => {
      const quote = await this.call(
        'pricing',
        'quote',
        { data: { items: [{ sku_id: product.id, quantity: 1 }], coupon_mode: 'none', points: 0, ...options.quote } },
        person.token,
      );
      return this.call(
        'checkout',
        'submit',
        {
          idempotency_key: this.id(`checkout-${key}`),
          data: {
            quote_id: quote.quote_id,
            address_id: person.address,
            note: label + (options.note || key),
            source: 'direct',
          },
        },
        person.token,
      );
    });
  }
  async pay(key, person, orderID) {
    const payment = await this.once(`payment-${key}`, () =>
      this.call('payment', 'create', { id: orderID, data: { provider: C.PaymentProvider.Demo } }, person.token),
    );
    if (payment.provider !== C.PaymentProvider.Demo) throw Error('Only the demo payment provider can be used.');
    await this.once(`paid-${key}`, () => this.call('payment', 'submit_demo', { id: payment.id }, person.token));
    await this.wait(
      'paid order',
      () => this.call('order', 'get', { id: orderID }, person.token),
      (row) =>
        [
          C.OrderStatus.ReadyToFulfill,
          C.OrderStatus.PaidWaitGroup,
          C.OrderStatus.Completed,
          C.OrderStatus.Shipped,
          C.OrderStatus.Closed,
          C.OrderStatus.Refunded,
          C.OrderStatus.PartiallyRefunded,
        ].includes(row.status),
    );
    return payment;
  }
  async transactions(people, products) {
    const names = [
      '已完成可评价',
      '已发货待收货',
      '已付款待出库',
      '仅退款待审核',
      '仅退款已批准',
      '退款处理中',
      '退款已完成',
      '退货已批准',
      '已寄回待验收',
      '已验收待退款',
      '售后已驳回',
      '已取消订单',
    ];
    const scenarios = [0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 2];
    const orders = [];
    for (let i = 0; i < scenarios.length; i++) {
      const kind = scenarios[i],
        person = people[0],
        product = products[i % 9],
        key = String(i).padStart(2, '0');
      if (this.state.steps[`scenario-${key}`]) {
        orders.push(this.state.steps[`scenario-${key}`]);
        continue;
      }
      const result = await this.order(key, person, product, { note: names[kind] });
      const id = result.order_id;
      orders.push({ id, sku: product.id, scenario: names[kind] });
      if (kind === 11) {
        await this.once(`order-cancel-${key}`, () =>
          this.call('checkout', 'cancel', { id, idempotency_key: this.id(`cancel-${key}`) }, person.token),
        );
        await this.once(`scenario-${key}`, async () => orders.at(-1));
        continue;
      }
      await this.pay(key, person, id);
      const invoice = await this.once(`invoice-${key}`, () =>
        this.call(
          'invoice',
          'create',
          { data: { order_id: id, title: label + '林小满', kind: C.InvoiceKind.Personal } },
          person.token,
        ),
      );
      if (i % 3 !== 0)
        await this.once(`invoice-issue-${key}`, () => this.call('invoice', 'issue_demo', { id: invoice.id }));
      if ([0, 1, 7, 8, 9].includes(kind)) {
        if (!this.state.steps[`dispatch-${key}`])
          await this.wait(
            'fulfillment',
            () => this.call('fulfillment', 'get', { id }),
            (r) => r.status === C.FulfillmentStatus.Ready,
          );
        await this.once(`dispatch-${key}`, () =>
          this.call('fulfillment', 'dispatch', { id, idempotency_key: this.id(`dispatch-${key}`) }),
        );
        if (!this.state.steps[`deliver-${key}`] && !this.state.steps[`complete-${key}`])
          await this.wait(
            'shipped order',
            () => this.call('order', 'get', { id }, person.token),
            (r) => r.status === C.OrderStatus.Shipped,
          );
        if (kind === 0) {
          await this.once(`deliver-${key}`, () => this.call('logistics', 'deliver', { id }));
          await this.wait(
            'completed delivered order',
            () => this.call('order', 'get', { id }, person.token),
            (r) => r.status === C.OrderStatus.Completed,
          );
          await this.once(`complete-${key}`, () => this.call('order', 'complete', { id }, person.token));
          const review = await this.once(`review-${key}`, () =>
            this.call(
              'interaction',
              'create',
              {
                idempotency_key: this.id(`review-${key}`),
                data: {
                  kind: C.InteractionKind.Review,
                  sku_id: product.id,
                  order_id: id,
                  rating: 5 - (i % 2),
                  body:
                    label +
                    [
                      '商品包装完整，使用体验很满意。',
                      '通勤携带很方便，做工也不错。',
                      '配送速度快，客服答复很清楚。',
                      '与页面描述一致，值得推荐。',
                    ][i % 4],
                },
              },
              person.token,
            ),
          );
          if (i !== 3)
            await this.once(`review-moderate-${key}`, () =>
              this.call('interaction', 'moderate', {
                id: review.id,
                data: { approved: true, version: review.version || 0 },
              }),
            );
        } else if (kind === 1) {
          const shipment = await this.call('logistics', 'get', { id });
          await this.once(`tracking-${key}`, () =>
            this.call('logistics', 'tracking_update', {
              id: this.id(`tracking-${key}`),
              data: {
                tracking_id: shipment.tracking_id,
                provider: shipment.provider || C.LogisticsProvider.Demo,
                status: i === 4 || i === 5 ? C.TrackingStatus.Exception : C.TrackingStatus.InTransit,
                location: '杭州体验转运中心',
                description:
                  label +
                  (i === 4
                    ? '收件人暂时无法联系，等待重新派送'
                    : i === 5
                      ? '包裹派送延迟，已联系渠道跟进'
                      : '包裹已进入目的地转运中心'),
                occurred_at: this.date(),
              },
            }),
          );
        }
      }
      if (kind >= 3 && kind <= 10) {
        const claim = await this.once(`aftersale-${key}`, () =>
          this.call(
            'aftersale',
            'create',
            {
              idempotency_key: this.id(`aftersale-${key}`),
              data: {
                order_id: id,
                type: kind >= 7 && kind <= 9 ? C.AftersaleType.Return : C.AftersaleType.Refund,
                reason: label + names[kind],
                lines: [{ sku_id: product.id, quantity: 1 }],
              },
            },
            person.token,
          ),
        );
        if (kind === 10)
          await this.once(`reject-${key}`, () =>
            this.call('aftersale', 'reject', {
              id: claim.id,
              data: { reason: label + '资料尚不完整，请补充照片后重新申请' },
            }),
          );
        if (kind >= 4 && kind <= 9) {
          await this.once(`approve-${key}`, () => this.call('aftersale', 'approve', { id: claim.id }));
          await this.wait(
            'approved aftersale',
            () => this.call('aftersale', 'get', { id: claim.id }),
            (r) =>
              [
                C.AftersaleStatus.Approved,
                C.AftersaleStatus.Returning,
                C.AftersaleStatus.Inspecting,
                C.AftersaleStatus.Inspected,
                C.AftersaleStatus.Refunding,
                C.AftersaleStatus.Completed,
              ].includes(r.status),
          );
          if (kind === 8 || kind === 9)
            await this.once(`return-${key}`, () =>
              this.call(
                'aftersale',
                'return',
                { id: claim.id, data: { tracking: this.id(`return-${key}`) } },
                person.token,
              ),
            );
          if (kind === 9)
            await this.once(`inspect-${key}`, () =>
              this.call('aftersale', 'inspect', { id: claim.id, data: { condition: 'sellable' } }),
            );
          if (kind === 5 || kind === 6) {
            await this.once(`refund-request-${key}`, () => this.call('aftersale', 'refund', { id: claim.id }));
            const pending = await this.wait(
              'refund reference',
              () => this.call('aftersale', 'get', { id: claim.id }),
              (r) => Boolean(r.refund_id),
            );
            if (kind === 6) {
              await this.once(`refund-confirm-${key}`, () =>
                this.call('refund', 'confirm_demo', { id: pending.refund_id }),
              );
              await this.wait(
                'completed aftersale',
                () => this.call('aftersale', 'get', { id: claim.id }),
                (r) => r.status === C.AftersaleStatus.Completed,
              );
            }
          }
        }
      }
      await this.once(`scenario-${key}`, async () => orders.at(-1));
      console.log(`order ${i + 1}/${scenarios.length}: ${names[kind]} (${id})`);
    }
    return orders;
  }
  async engagement(people, products, orders) {
    for (let i = 0; i < 24; i++) {
      const person = people[i % 3],
        product = products[i % 9];
      const question = await this.once(`question-${i}`, () =>
        this.call(
          'interaction',
          'create',
          {
            idempotency_key: this.id(`question-${i}`),
            data: {
              kind: C.InteractionKind.Question,
              sku_id: product.id,
              body:
                label +
                [
                  '这款商品适合日常通勤使用吗？',
                  '包装内有哪些配件？',
                  '是否支持七天退换服务？',
                  '这款规格近期可以发货吗？',
                ][i % 4],
            },
          },
          person.token,
        ),
      );
      if (i % 3 === 1)
        await this.once(`question-answer-${i}`, () =>
          this.call('interaction', 'answer', {
            id: question.id,
            data: {
              answer: '你好，这款商品支持日常使用。规格、配送与售后以商品详情展示为准，欢迎继续咨询。',
              version: question.version || 0,
            },
          }),
        );
      if (i % 3 === 2)
        await this.once(`question-moderate-${i}`, () =>
          this.call('interaction', 'moderate', {
            id: question.id,
            data: { approved: true, version: question.version || 0 },
          }),
        );
      const ticket = await this.once(`ticket-${i}`, () =>
        this.call(
          'helpdesk',
          'create',
          {
            idempotency_key: this.id(`ticket-${i}`),
            data: {
              title: label + ['商品规格咨询', '配送进度咨询', '售后办理咨询', '积分权益咨询'][i % 4],
              body: '这是一条可直接回复和关闭的体验工单，请协助确认处理进度。',
              order_id: i % 3 === 0 ? orders[i % orders.length].id : '',
            },
          },
          person.token,
        ),
      );
      if (i % 3 !== 0)
        await this.once(`ticket-reply-${i}`, () =>
          this.call('helpdesk', 'reply', {
            id: ticket.id,
            idempotency_key: this.id(`reply-${i}`),
            data: { body: '你好，已为你核对，可以在订单或权益页面继续操作。如有问题，欢迎继续留言。' },
          }),
        );
      if (i % 3 === 2) await this.once(`ticket-close-${i}`, () => this.call('helpdesk', 'close', { id: ticket.id }));
      await this.once(`behavior-${i}`, () =>
        this.call(
          'analytics',
          'record',
          {
            idempotency_key: this.id(`behavior-${i}`),
            data: {
              type: [C.AnalyticsBehavior.View, C.AnalyticsBehavior.Search, C.AnalyticsBehavior.Click][i % 3],
              sku_id: product.id,
              query: i % 3 === 1 ? '体验好物' : '',
              page: `#/product/${product.id}`,
              source: 'admin_demo',
            },
          },
          person.token,
        ),
      );
    }
    for (let i = 0; i < people.length; i++) {
      const person = people[i];
      await this.once(`cart-${i}`, () =>
        this.call('cart', 'add', { data: { sku_id: products[i].id, quantity: 1 } }, person.token),
      );
      for (let j = 0; j < 3; j++)
        await this.once(`alert-${i}-${j}`, () =>
          this.call(
            'engagement',
            'subscribe',
            {
              data: {
                sku_id: products[j].id,
                kind: C.SubscriptionKind.Price,
                target_cents: products[j].price_cents - 1000,
              },
            },
            person.token,
          ),
        );
      await this.once(`stock-alert-${i}`, () =>
        this.call(
          'engagement',
          'subscribe',
          { data: { sku_id: products[10].id, kind: C.SubscriptionKind.Stock } },
          person.token,
        ),
      );
    }
    for (let i = 0; i < 6; i++) {
      const rule = await this.once(`journey-rule-${i}`, () =>
        this.call('engagement', 'journey_rule_save', {
          id: this.id(`journey-${i}`),
          data: {
            name: label + ['购物车回访', '待付款提醒', '复购关怀'][i % 3] + (i >= 3 ? ' · 备用' : ''),
            kind: [C.JourneyKind.Cart, C.JourneyKind.Unpaid, C.JourneyKind.Repurchase][i % 3],
            enabled: i < 3,
            delay_minutes: 1440,
            cycle_days: 30,
            gap_hours: 24,
            sku_id: i % 3 === 2 ? products[0].id : '',
            segment_id: '',
            version: 0,
          },
        }),
      );
      if (i === 0)
        for (let j = 0; j < people.length; j++)
          await this.once(`journey-cart-${j}`, () =>
            this.call('engagement', 'journey_run', { id: rule.id, data: { customer_id: people[j].id } }),
          );
      if (i === 2)
        await this.once('journey-repurchase', () =>
          this.call('engagement', 'journey_run', {
            id: rule.id,
            data: { customer_id: people[0].id, order_id: orders[0].id },
          }),
        );
    }
  }
  async rewards(people, products, coupons) {
    for (let i = 0; i < 8; i++) {
      if (this.state.steps[`reward-complete-${i}`]) continue;
      const gift = i % 2 === 0;
      const offer = await this.once(`offer-${i}`, () =>
        this.call('points', 'offer_upsert', {
          data: {
            name:
              label +
              (gift
                ? ['降噪耳机体验礼', '精品咖啡体验礼', '通勤背包体验礼', '便携音箱体验礼'][i / 2]
                : '积分兑换优惠券 ' + (i + 1)),
            description: '可实际兑换、出库及签收的本地演示权益',
            kind: gift ? C.RewardKind.Gift : C.RewardKind.Coupon,
            reference_id: gift ? products[(i * 2) % 9].id : coupons[i % 6].id,
            cost: gift ? 500 : 200,
            total_quota: 100,
            max_per_user: 1,
            active: i !== 7,
            starts_at: this.date(-1),
            ends_at: this.date(90),
            version: 0,
          },
        }),
      );
      if (i < 6) {
        const person = people[0];
        const redemption = await this.once(`redemption-${i}`, () =>
          this.call(
            'points',
            'redeem',
            {
              id: offer.id,
              idempotency_key: this.id(`redemption-${i}`),
              data: { version: offer.version, address_id: person.address },
            },
            person.token,
          ),
        );
        await this.wait(
          'issued reward',
          () => this.call('points', 'redemption_get', { id: redemption.id }, person.token),
          (r) => [C.RedemptionState.Issued, C.RedemptionState.Shipped, C.RedemptionState.Delivered].includes(r.state),
        );
        if (gift && i >= 2) {
          if (!this.state.steps[`gift-dispatch-${i}`])
            await this.wait(
              'gift fulfillment',
              () => this.call('fulfillment', 'get', { id: redemption.id }),
              (r) => r.status === C.FulfillmentStatus.Ready,
            );
          await this.once(`gift-dispatch-${i}`, () => this.call('fulfillment', 'dispatch', { id: redemption.id }));
          if (i === 4)
            await this.once(`gift-deliver-${i}`, () => this.call('logistics', 'deliver', { id: redemption.id }));
        }
      }
      await this.once(`reward-complete-${i}`, async () => ({ done: true }));
    }
  }
  async promotions(people, products) {
    for (const kind of [C.CampaignKind.Flash, C.CampaignKind.Group]) {
      await this.once(`dedicated-quota-${kind}`, () =>
        this.call('campaign', 'rule_upsert', {
          id: this.id(`quota-${kind}`),
          data: {
            name: label + (kind === C.CampaignKind.Flash ? '限时秒杀专用名额' : '三人成团专用名额'),
            description: '仅用于本地业务流程体验',
            reason: label + '初始化专用活动配额',
            kind,
            threshold_cents: 0,
            discount_cents: 1000,
            pay_rate_bps: 0,
            min_quantity: 1,
            quota: 200,
            active: true,
            version: 0,
          },
        }),
      );
    }
    const rule = await this.once('group-rule', () =>
      this.call('group', 'rule_upsert', {
        id: this.id('group-rule'),
        data: {
          name: label + '降噪耳机三人成团',
          sku_id: products[0].id,
          activity_id: this.id(`quota-${C.CampaignKind.Group}`),
          required: 3,
          duration_minutes: 10080,
          active: true,
          version: 0,
          reason: label + '初始化拼团规则',
        },
      }),
    );
    const formed = await this.once('group-formed', () =>
      this.call(
        'group',
        'create',
        { idempotency_key: this.id('group-formed'), data: { rule_id: rule.id, sku_id: products[0].id } },
        people[0].token,
      ),
    );
    for (let i = 0; i < people.length; i++) {
      const person = people[i],
        key = `group-member-${i}`;
      if (this.state.steps[`promotion-${key}`]) continue;
      const order = await this.order(key, person, products[0], {
        quote: { group_id: formed.id },
        note: '三人成团体验订单',
      });
      await this.pay(key, person, order.order_id);
      await this.once(`promotion-${key}`, async () => ({ order_id: order.order_id }));
    }
    await this.wait(
      'formed group',
      () => this.call('group', 'get', { id: formed.id }),
      (r) => r.status === C.GroupStatus.Formed,
    );
    await this.once('group-open', () =>
      this.call(
        'group',
        'create',
        { idempotency_key: this.id('group-open'), data: { rule_id: rule.id, sku_id: products[0].id } },
        people[0].token,
      ),
    );
    const openActivity = this.id('quota-group-open');
    await this.once('actionable-group-quota', () =>
      this.call('campaign', 'rule_upsert', {
        id: openActivity,
        data: {
          name: label + '双人拼团体验配额',
          description: '可继续参团体验',
          reason: label + '创建待参团样例',
          kind: C.CampaignKind.Group,
          threshold_cents: 0,
          discount_cents: 1000,
          pay_rate_bps: 0,
          min_quantity: 1,
          quota: 200,
          active: true,
          version: 0,
        },
      }),
    );
    const openRule = await this.once('actionable-group-rule', () =>
      this.call('group', 'rule_upsert', {
        id: this.id('group-rule-open'),
        data: {
          name: label + '可继续操作的双人团',
          sku_id: products[0].id,
          activity_id: openActivity,
          required: 2,
          duration_minutes: 10080,
          active: true,
          version: 0,
          reason: label + '待参团业务体验',
        },
      }),
    );
    await this.once('actionable-group', () =>
      this.call(
        'group',
        'create',
        { idempotency_key: this.id('actionable-group'), data: { rule_id: openRule.id, sku_id: products[0].id } },
        people[0].token,
      ),
    );
    const session = await this.once('flash-session', () =>
      this.call('flash', 'upsert', {
        id: this.id('flash-session'),
        data: {
          name: label + '咖啡豆限时体验场',
          sku_id: products[3].id,
          activity_id: this.id(`quota-${C.CampaignKind.Flash}`),
          active: true,
          version: 0,
          starts_at: this.date(-1),
          ends_at: this.date(30),
          reason: label + '初始化秒杀场次',
        },
      }),
    );
    for (let i = 0; i < people.length; i++) {
      const person = people[i];
      const request = await this.once(`flash-request-${i}`, async () => {
        const quote = await this.call(
          'pricing',
          'quote',
          {
            data: {
              items: [{ sku_id: products[3].id, quantity: 1 }],
              activity_id: session.activity_id,
              points: 0,
              coupon_mode: 'none',
            },
          },
          person.token,
        );
        return this.call(
          'flash',
          'request',
          {
            idempotency_key: this.id(`flash-request-${i}`),
            data: { quote_id: quote.quote_id, address_id: person.address, note: label + '秒杀请求体验' },
          },
          person.token,
        );
      });
      await this.wait(
        'successful flash request',
        () => this.call('flash', 'get', { id: request.id }, person.token),
        (r) => r.status === C.FlashRequestStatus.Succeeded,
      );
    }
  }
  async audit(people) {
    const id = people[2].id;
    await this.once('account-pause', () =>
      this.call('iam', 'set_active', {
        id,
        data: { active: false, expected_active: true, reason: label + '演示账号停用审计' },
      }),
    );
    await this.once('account-resume', () =>
      this.call('iam', 'set_active', {
        id,
        data: { active: true, expected_active: false, reason: label + '恢复演示账号，继续业务体验' },
      }),
    );
    const exceptions = await this.call('logistics', 'exceptions', { data: { page: 1, page_size: 100 } });
    const ids = new Set(
      Object.entries(this.state.steps)
        .filter(([k]) => k.startsWith('dispatch-'))
        .map(([, v]) => v.id || v.order_id),
    );
    const row = exceptions.items?.find((item) => ids.has(item.logistics_id));
    if (row)
      await this.once('exception-resolved', () =>
        this.call('logistics', 'exception_resolve', {
          id: row.id,
          data: { resolution: label + '已联系收件人确认重新派送' },
        }),
      );
  }
  async pending(people, products) {
    for (let i = 0; i < 3; i++) {
      const result = await this.order(`pending-${i}`, people[0], products[i], {
        note: '待付款体验 · 可在前台继续支付',
      });
      await this.once(`pending-payment-${i}`, () =>
        this.call(
          'payment',
          'create',
          { id: result.order_id, data: { provider: C.PaymentProvider.Demo } },
          people[0].token,
        ),
      );
      const rule = this.state.steps['journey-rule-1'];
      await this.once(`journey-unpaid-${i}`, () =>
        this.call('engagement', 'journey_run', {
          id: rule.id,
          data: { customer_id: people[0].id, order_id: result.order_id },
        }),
      );
    }
  }
  async reports() {
    for (const source of [
      'order/admin_list',
      'payment/admin_list',
      'aftersale/admin_list',
      'invoice/admin_list',
      'inventory/ledger',
      'iam/audit_list',
    ]) {
      const result = await this.once(`export-${source}`, () =>
        this.call('helpdesk', 'export_create', {
          idempotency_key: this.id(`export-${source.replaceAll('/', '-')}`),
          data: {
            source,
            filters: source === 'inventory/ledger' ? { keyword: this.config.namespace } : {},
            reason: label + '样例报表',
          },
        }),
      );
      const id = result.job.id;
      const current = await this.call('helpdesk', 'export_get', { id });
      if (current.job.status === C.ExportStatus.Failed) await this.call('helpdesk', 'export_retry', { id });
      await this.wait(
        'downloadable export',
        async () => {
          const row = await this.call('helpdesk', 'export_get', { id });
          if (row.job.status === C.ExportStatus.Failed) throw Error('Sample export failed: ' + row.job.error);
          return row;
        },
        (row) => row.job.status === C.ExportStatus.Ready,
      );
    }
    await this.once('reconciliation', () =>
      this.call('reconciliation', 'run', {
        idempotency_key: this.id('reconciliation'),
        data: {
          async: true,
          from: new Date().toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' }),
          to: new Date().toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' }),
          reason: label + '本地演示账期核对',
        },
      }),
    );
  }
  async run() {
    await this.authenticate();
    if (!this.config.apply) {
      console.log(JSON.stringify(await this.inspect(), null, 2));
      return;
    }
    const directory = path.dirname(this.config.manifest);
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    const lock = this.config.manifest + '.lock';
    const handle = fs.openSync(lock, 'wx', 0o600);
    try {
      this.persist();
      if (!this.state.before) {
        this.state.before = await this.inspect();
        this.persist();
      }
      console.log('Creating local demo accounts and business configuration…');
      const people = await this.accounts();
      const products = await this.catalog();
      const config = await this.configurations(products);
      console.log('Creating orders through checkout, demo payment and fulfillment…');
      const orders = await this.transactions(people, products);
      console.log('Creating customer service, journeys, subscriptions and rewards…');
      await this.engagement(people, products, orders);
      await this.rewards(people, products, config.coupons);
      await this.promotions(people, products);
      await this.pending(people, products);
      await this.audit(people);
      await this.reports();
      this.state.after = await this.inspect();
      this.state.completed_at = new Date().toISOString();
      this.state.usernames = people.map((p) => p.username);
      this.persist();
      console.log(
        JSON.stringify(
          {
            completed: true,
            new_steps: this.created,
            manifest: this.config.manifest,
            usernames: this.state.usernames,
            lists: this.state.after,
          },
          null,
          2,
        ),
      );
    } finally {
      fs.closeSync(handle);
      fs.unlinkSync(lock);
    }
  }
}
if (require.main === module) {
  Promise.resolve()
    .then(() => new Seeder(options(process.argv.slice(2))).run())
    .catch((error) => {
      console.error(error.message);
      process.exitCode = 1;
    });
}
module.exports = { Seeder, options };
