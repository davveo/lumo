// UI event handlers -> real HTTP gateway -> gRPC -> independent databases.
// Run against cmd/devstack in an isolated DEVSTACK_DATA_DIR.
const path = require('node:path'),
  fs = require('node:fs'),
  vm = require('node:vm'),
  assert = require('node:assert/strict'),
  crypto = require('node:crypto').webcrypto;
const base = process.env.API_BASE || 'http://127.0.0.1:8080',
  source = process.env.FRONTEND_DIR || path.join(__dirname, '../web/dist');
const listeners = {},
  events = {},
  toasts = [];
let lastModal = '',
  authUser = null;
const on = (table, key, handler) => (table[key] ||= []).push(handler);
const doc = {
  addEventListener: (n, f) => on(listeners, n, f),
  querySelectorAll: () => [],
  querySelector: () => null,
  createElement: () => ({ click() {}, remove() {} }),
};
const win = {
  LUMO_API_BASE: base,
  crypto,
  addEventListener: (n, f) => on(events, n, f),
  dispatchEvent: (e) => {
    for (const f of events[e.type] || []) f(e);
  },
};
const context = vm.createContext({
  window: win,
  document: doc,
  location: { origin: base, hash: '#/member/overview' },
  console,
  crypto,
  URL,
  Blob,
  AbortController,
  Uint8Array,
  setTimeout,
  clearTimeout,
  CustomEvent: class {
    constructor(type, options) {
      this.type = type;
      this.detail = options?.detail;
    }
  },
  FormData: class {
    constructor(form) {
      this.data = form?.values || {};
    }
    *[Symbol.iterator]() {
      yield* Object.entries(this.data);
    }
    append() {}
  },
  fetch,
  products: [
    {
      id: 'coffee',
      skuId: 'sku-coffee',
      name: 'Coffee',
      colors: ['标准'],
      options: ['标准版'],
      defaultOption: '标准版',
      price: 399,
    },
  ],
  state: { orders: [], cart: [], favorites: [], address: null, points: true, coupon: 0 },
  itemPrice: (i) => i.unitPrice || 399,
  renderKeepScroll() {},
  save() {
    throw Error('business state must not write browser persistence');
  },
  go(path) {
    context.location.hash = '#/' + path;
  },
  toast: (m) => toasts.push(m),
  esc: (s) => String(s ?? ''),
  icon: () => '',
  money: (n) => String(n),
  price: (n) => '¥' + n,
  modal: (title, html) => {
    lastModal = title + ' ' + html;
  },
  closeModal() {
    lastModal = '';
  },
  selectedItems: () => [],
  header: () => '',
  footer: () => '',
});
win.LumoAuth = {
  isLoggedIn: () => !!authUser,
  user: () => authUser,
  requireLogin: () => !!authUser,
  ensureSession: () => !!authUser,
};
for (const file of ['constants.js', 'api-client.js', 'member.js', 'member-views.js', 'services.js'])
  vm.runInContext(fs.readFileSync(source + '/' + file, 'utf8'), context, { filename: file });
const api = win.LumoAPI;
api.enableReadBatching();
async function click(kind, action, id = '', value = '') {
  const b = { dataset: { [kind === 'member' ? 'memberAction' : 'serviceAction']: action, id, value }, disabled: false };
  const target = { closest: (s) => (s === `[data-${kind}-action]` ? b : null) };
  for (const f of listeners.click || []) await f({ target, preventDefault() {} });
}
async function submit(id, values, dataset = {}) {
  const target = { id, values, dataset, querySelector: () => ({ disabled: false }) };
  for (const f of listeners.submit || []) await f({ target, preventDefault() {} });
}
let count = 0;
function check(v, m) {
  assert.ok(v, m);
  count++;
}
(async () => {
  const login = await api.call('iam', 'login', { data: { username: 'demo', password: 'Lumo@2026' } }, { auth: false });
  authUser = { id: login.user_id, user_id: login.user_id, role: login.role };
  api.setToken(login.token);
  await win.LumoMember.refresh();
  await win.LumoServices.refresh();
  check(win.LumoMember.model().points === 5000, 'server points');
  check(win.LumoMember.model().addresses.length === 0, 'fresh server addresses');
  await submit('member-profile-form', {
    nickname: '服务端会员',
    phone: '13800138000',
    gender: '保密',
    birthday: '2000-01-01',
  });
  check(win.LumoMember.model().profile.nickname === '服务端会员', 'profile update');
  check((await api.call('member', 'get')).preferences.phone === '13800138000', 'contact preferences');
  await submit(
    'member-address-form',
    { name: '收货测试', phone: '13800138000', region: '广东省 深圳市 南山区', detail: '测试街道 101', isDefault: 'on' },
    { checkout: 'true' },
  );
  const address = win.LumoMember.selectedAddress();
  check(!!address?.id, 'create address');
  check(context.state.address.detail === '测试街道 101', 'checkout address mirror');
  await submit(
    'member-address-form',
    { name: '收货测试', phone: '13800138000', region: '广东省 深圳市 南山区', detail: '测试街道 102', isDefault: 'on' },
    { id: address.id },
  );
  check((await api.call('address', 'list')).items[0].address === '测试街道 102', 'update address');
  await click('member', 'edit-address', address.id);
  check(lastModal.includes('测试街道 102'), 'address editor fetches fresh owned address');
  await click('member', 'claim-coupon');
  check(
    win.LumoMember.model().coupons.some((c) => c.templateId === 'welcome-100'),
    'claim real template',
  );
  const before = win.LumoMember.pointBalance();
  await click('member', 'signin');
  check(win.LumoMember.pointBalance() === before + 20, 'server check-in');
  await click('member', 'signin');
  check(win.LumoMember.pointBalance() === before + 20, 'check-in idempotence');
  check(
    win.LumoMember.pointsDiscount([{ id: 'coffee', qty: 1, unitPrice: 399 }]) === 39.9,
    'points cents conversion and cap',
  );
  await win.LumoMember.track('coffee');
  check(win.LumoMember.model().history[0]?.id === 'coffee', 'history SKU mapping');
  await click('member', 'remove-history', 'coffee');
  check(win.LumoMember.model().history.length === 0, 'history delete');
  await api.call('engagement', 'put', { data: { kind: 'favorite', sku_id: 'sku-coffee' } });
  await win.LumoMember.refresh();
  check(context.state.favorites.includes('coffee'), 'favorite projection');
  await click('member', 'remove-favorite', 'coffee');
  check(!context.state.favorites.includes('coffee'), 'favorite delete');
  await submit('service-ticket-form', {
    type: '其他问题',
    orderId: '',
    title: '接口联调工单',
    detail: '验证真实客服服务',
  });
  const ticket = win.LumoServices.model().tickets[0];
  check(ticket?.title === '接口联调工单', 'ticket create');
  await submit('service-ticket-reply-form', { message: '补充测试说明' }, { id: ticket.id });
  check(
    (await api.call('helpdesk', 'get', { id: ticket.id })).messages.some((m) => m.body === '补充测试说明'),
    'reply persisted',
  );
  await click('service', 'view-ticket', ticket.id);
  check(lastModal.includes('补充测试说明'), 'ticket details server fetch');
  await click('service', 'close-ticket', ticket.id);
  check((await api.call('helpdesk', 'get', { id: ticket.id })).status === 'CLOSED', 'close ticket');
  check(!win.LumoServices.content('support', 'tickets').includes('模拟客服回复'), 'no customer staff simulation');
  check(!win.LumoMemberView.page('orders').includes('模拟发货'), 'no customer shipment simulation');
  check(!win.LumoMemberView.page('aftersales').includes('模拟审核'), 'no customer approval simulation');

  // Paid order, immutable invoice, customer aftersale -> staff refund -> reversal.
  const admin = await api.call(
    'iam',
    'login',
    { data: { username: 'admin', password: 'Admin@2026' } },
    { auth: false },
  );
  const staff = (service, action, command = {}) => api.call(service, action, command, { token: admin.token });
  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  async function waitFor(read, accept) {
    for (let n = 0; n < 80; n++) {
      try {
        const result = await read();
        if (accept(result)) return result;
      } catch (error) {
        if (error.status !== 404) throw error;
      }
      await sleep(100);
    }
    throw Error('event convergence timed out');
  }
  const canonical = [];
  function mapOrder(o) {
    return {
      ...o,
      backendStatus: o.status,
      createdAt: Date.parse(o.created_at),
      paidAt: o.payment_id ? Date.parse(o.updated_at) : null,
      items: (o.items || []).map((i) => ({
        ...i,
        id: 'coffee',
        sku_id: i.sku_id,
        qty: i.quantity,
        name: i.name || 'Coffee',
        unitPrice: Number(i.unit_price_cents || i.price_cents || 39900) / 100,
        color: '标准',
        option: '标准版',
      })),
      address: { name: '收货测试', phone: '13800138000', region: '广东省 深圳市 南山区', detail: '测试街道' },
      totals: { total: Number(o.payable_cents || 0) / 100, coupon: 0, points: 0 },
    };
  }
  async function refreshOrders() {
    const list = await api.call('order', 'list');
    canonical.splice(0, canonical.length, ...(list.items || []).map(mapOrder));
    context.state.orders = canonical;
    return canonical;
  }
  win.LumoCommerce = { allOrders: () => canonical, refreshOrders, skuForItem: (item) => item.sku_id || 'sku-coffee' };
  async function paidOrder() {
    const quote = await api.call('pricing', 'quote', {
      data: { items: [{ sku_id: 'sku-coffee', quantity: 2 }], points: 0 },
    });
    const submitted = await api.call('checkout', 'submit', {
      idempotency_key: api.key('member-order'),
      data: { quote_id: quote.quote_id, address_id: address.id },
    });
    const payment = await api.call('payment', 'create', { id: submitted.order_id });
    await staff('payment', 'confirm_demo', { id: payment.id });
    const order = await waitFor(
      () => api.call('order', 'get', { id: submitted.order_id }),
      (r) => r.status === 'READY_TO_FULFILL',
    );
    await waitFor(
      () => api.call('fulfillment', 'get', { id: order.id }),
      (r) => r.status === 'READY',
    );
    await refreshOrders();
    return order;
  }
  const paid = await paidOrder();
  await win.LumoMember.refresh();
  check(
    win.LumoMember.orderState(canonical.find((o) => o.id === paid.id)) === 'shipping',
    'server-ready status maps to pending shipment',
  );
  await submit('service-invoice-form', { kind: 'personal', title: '个人' }, { order: paid.id });
  let invoice = win.LumoServices.model().invoices.find((i) => i.orderId === paid.id);
  check(!!invoice?.id, 'invoice application handler persisted');
  await staff('invoice', 'issue_demo', { id: invoice.id });
  await win.LumoServices.refresh();
  check(
    win.LumoServices.model().invoices.find((i) => i.id === invoice.id).status === 'issued',
    'issued server invoice projection',
  );
  await submit(
    'member-aftersale-form',
    { type: '仅退款', reason: '不喜欢 / 不想要', note: '联调退款', 'line-0': 2 },
    { id: paid.id },
  );
  let after = win.LumoMember.model().aftersales.find((a) => a.orderId === paid.id);
  check(after?.backendStatus === 'REQUESTED', 'aftersale handler persisted selected quantities');
  await click('member', 'aftersale-details', after.id);
  check(lastModal.includes('REQUESTED'), 'aftersale detail reads fresh record');
  await click('member', 'cancel-aftersale', after.id);
  check(
    win.LumoMember.model().aftersales.find((a) => a.id === after.id).backendStatus === 'CANCELLED',
    'customer cancels pending aftersale',
  );
  await submit('member-aftersale-form', { type: '仅退款', reason: '质量问题', 'line-0': 2 }, { id: paid.id });
  after = win.LumoMember.model().aftersales.find((a) => a.orderId === paid.id && a.backendStatus === 'REQUESTED');
  check(!!after, 'new aftersale can follow cancellation');
  await staff('aftersale', 'approve', { id: after.id });
  const refunded = await staff('aftersale', 'refund', { id: after.id });
  await staff('refund', 'confirm_demo', { id: refunded.refund_id });
  await waitFor(
    () => api.call('aftersale', 'get', { id: after.id }),
    (r) => r.status === 'COMPLETED',
  );
  await waitFor(
    () => api.call('order', 'get', { id: paid.id }),
    (r) => r.refunded_total_cents === r.payable_cents,
  );
  await refreshOrders();
  await win.LumoMember.refresh();
  await win.LumoServices.refresh();
  check(win.LumoMember.isRefunded(paid.id), 'full refund uses server cumulative cash');
  check(win.LumoMember.orderState(canonical.find((o) => o.id === paid.id)) === 'refunded', 'refund order category');
  const original = await api.call('invoice', 'get', { id: invoice.id }),
    reversals = await api.call('invoice', 'reversals', { id: invoice.id });
  check(!!original.number && reversals.items.length === 1, 'original invoice retained and reversal exists');
  check(
    win.LumoServices.model().invoices.find((i) => i.id === invoice.id).status === 'issued',
    'refund does not erase original invoice',
  );
  await click('service', 'view-invoice', invoice.id);
  check(lastModal.includes(original.number) && lastModal.includes('红冲'), 'invoice detail renders actual reversal');
  // Shipment and receipt -> review is submitted for moderation, not auto-published.
  const reviewOrder = await paidOrder();
  await staff('fulfillment', 'dispatch', { id: reviewOrder.id });
  await waitFor(
    () => api.call('order', 'get', { id: reviewOrder.id }),
    (r) => r.status === 'SHIPPED',
  );
  await refreshOrders();
  await click('member', 'receive-order', reviewOrder.id);
  check(
    (await api.call('order', 'get', { id: reviewOrder.id })).status === 'COMPLETED',
    'receipt handler calls server complete',
  );
  await submit('member-review-form', { rating: '5', text: '真实接口评价' }, { id: reviewOrder.id });
  check(
    win.LumoMember.model().reviews.some((r) => r.orderId === reviewOrder.id && r.status === 'PENDING'),
    'review stored pending moderation',
  );

  // Physical return is an independent warehouse action, and partial money refund
  // must not mark the full order refunded or fabricate a stock restoration.
  const inventoryBefore = await api.call('inventory', 'get', { id: 'sku-coffee' }, { auth: false });
  await submit(
    'member-aftersale-form',
    { type: '退货退款', reason: '商品破损 / 少件', 'line-0': 1 },
    { id: reviewOrder.id },
  );
  let returnCase = win.LumoMember.model().aftersales.find(
    (a) => a.orderId === reviewOrder.id && a.backendStatus === 'REQUESTED',
  );
  check(!!returnCase, 'partial return request persisted');
  await staff('aftersale', 'approve', { id: returnCase.id });
  await win.LumoMember.refresh();
  await click('member', 'return-aftersale', returnCase.id);
  check(lastModal.includes('member-return-form'), 'approved return exposes real tracking form');
  await submit('member-return-form', { tracking: 'RETURN-HTTP-001' }, { id: returnCase.id });
  check(
    (await api.call('aftersale', 'get', { id: returnCase.id })).return_tracking === 'RETURN-HTTP-001',
    'return tracking handler persisted',
  );
  check(
    (await api.call('inventory', 'get', { id: 'sku-coffee' }, { auth: false })).on_hand === inventoryBefore.on_hand,
    'request does not fabricate restock',
  );
  await staff('aftersale', 'inspect', { id: returnCase.id, data: { condition: 'sellable' } });
  check(
    (await api.call('inventory', 'get', { id: 'sku-coffee' }, { auth: false })).on_hand === inventoryBefore.on_hand + 1,
    'warehouse inspection restocks inspected quantity',
  );
  const partial = await staff('aftersale', 'refund', { id: returnCase.id });
  await staff('refund', 'confirm_demo', { id: partial.refund_id });
  await waitFor(
    () => api.call('aftersale', 'get', { id: returnCase.id }),
    (r) => r.status === 'COMPLETED',
  );
  await waitFor(
    () => api.call('order', 'get', { id: reviewOrder.id }),
    (r) => r.refunded_total_cents > 0,
  );
  await refreshOrders();
  await win.LumoMember.refresh();
  check(!win.LumoMember.isRefunded(reviewOrder.id), 'partial refund preserves remaining order value');
  await click('member', 'confirm-delete-address', address.id);
  check((await api.call('address', 'list')).items.length === 0, 'delete address');
  const n = win.LumoServices.model().tickets.length;
  await submit('service-ticket-form', { title: '', detail: '非法参数' });
  check(win.LumoServices.model().tickets.length === n, 'failed request creates no fake record');
  authUser = null;
  win.dispatchEvent({ type: 'lumo:logout' });
  check(
    win.LumoMember.model().points === 0 && win.LumoMember.model().addresses.length === 0,
    'logout clears member cache',
  );
  check(win.LumoServices.model().tickets.length === 0, 'logout clears service cache');
  console.log(
    JSON.stringify({
      passed: count,
      apiRequests: api.requests().length,
      domains: [...new Set(api.requests().map((r) => r.service))],
    }),
  );
})().catch((e) => {
  console.error(e.stack);
  console.error('toasts:', toasts);
  process.exitCode = 1;
});
