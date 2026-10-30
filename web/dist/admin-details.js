(function () {
  'use strict';
  const U = window.LumoAdminUI;
  const labels = {
    id: '记录编号',
    user_id: '会员编号',
    order_id: '订单编号',
    payment_id: '支付编号',
    refund_id: '退款编号',
    ticket_id: '工单编号',
    business_id: '业务编号',
    sku_id: '商品编号',
    product_id: '商品编号',
    name: '名称',
    title: '标题',
    body: '内容',
    answer: '回复',
    reason: '原因',
    status: '状态',
    type: '类型',
    kind: '类别',
    created_at: '创建时间',
    updated_at: '更新时间',
    paid_at: '付款时间',
    expires_at: '有效期至',
    completed_at: '完成时间',
    closed_at: '关闭时间',
    amount_cents: '金额',
    payable_cents: '应付金额',
    subtotal_cents: '商品金额',
    discount_cents: '优惠金额',
    refund_amount_cents: '退款金额',
    quantity: '数量',
    role: '发送方',
    messages: '会话消息',
    items: '记录',
    total: '总数',
    message_total: '消息总数',
    tracking_id: '运单编号',
    provider: '渠道',
    number: '发票号码',
    invoice_id: '发票编号',
    reversals: '红冲记录',
    accepted: '是否已受理',
    removed: '是否已移除',
    cleared: '是否已清空',
    active: '是否启用',
    version: '版本',
    event_type: '事件类型',
    event_id: '事件编号',
    aggregate_id: '关联业务编号',
    source: '来源',
    query: '搜索词',
    page: '页面',
    record_category: '记录类别',
    summary: '说明',
    product_name: '商品名称',
    action_label: '行为',
    condition: '商品品质',
    lines: '商品明细',
    address: '收货信息',
    recipient: '收货人',
    phone: '联系电话',
    region: '地区',
    detail: '详细地址',
    quote: '订单价格快照',
    parent_id: '关联记录',
    details: '明细',
    before: '修改前',
    after: '修改后',
    actor_id: '操作人',
    attempts: '尝试次数',
    next_attempt_at: '下次处理时间',
    error: '处理说明',
    code: '结果',
    result: '结果',
    success: '是否成功',
    duplicate: '是否重复请求',
    limit: '本次条数',
    offset: '起始位置',
    page_size: '每页条数',
    has_more: '还有下一页',
    state: '处理状态',
    processed: '已处理',
    sent: '已发送',
    read: '是否已读',
    recorded: '是否已记录',
    message: '说明',
    reservation_id: '预占编号',
    shipment_id: '履约编号',
    inspection_id: '验收编号',
    refunds: '退款记录',
    shipping_cents: '运费',
    coupon_id: '优惠券编号',
    points: '使用积分',
    refund_total_cents: '累计退款',
    original_number: '原发票号码',
  };
  const values = {
    ACCEPTED: '已受理',
    PENDING: '待处理',
    OPEN: '待回复',
    REPLIED: '已回复',
    CLOSED: '已关闭',
    SENT: '已发送',
    SUCCEEDED: '已成功',
    FAILED: '失败',
    READ: '已读',
    UNREAD: '未读',
    PUBLISHED: '已公开',
    REQUESTED: '待审核',
    APPROVED: '已批准',
    REJECTED: '已驳回',
    PROCESSING: '处理中',
    CANCELLED: '已取消',
    COMPLETED: '已完成',
    ISSUED: '已开票',
    PENDING_PAYMENT: '待付款',
    READY_TO_FULFILL: '待发货',
    SHIPPED: '已发货',
    DELIVERED: '已签收',
    REFUNDED: '已退款',
    REVERSAL_PENDING: '退款调整待处理',
    ADJUSTED_REQUEST: '申请已调整',
    ADJUSTED: '已调整',
    VOID: '已作废',
    CANCELLED_UNISSUED: '未开票申请已调整',
    PENDING_EXTERNAL_ADJUSTMENT: '待开票渠道调整',
    DEMO_ADJUSTED: '演示调整已完成',
    alipay: '支付宝',
    wechat: '微信支付',
    card: '银行卡收单',
    user: '顾客',
    admin: '客服',
    system: '系统',
    demo: '演示渠道',
    review: '商品评价',
    question: '商品问答',
    behavior: '访问互动',
    purchase_event: '交易结果',
    view: '浏览商品',
    search: '搜索商品',
    click: '点击商品',
    impression: '商品曝光',
    OrderCompleted: '订单完成',
    RefundSucceeded: '退款成功',
    sellable: '可售',
    damaged: '破损',
    unsellable: '不可售',
  };
  function render(value, options = {}) {
    let nodes = 0;
    const block = (v, key = '', depth = 0) => {
      if (++nodes > 200 || depth > 5) return '<span class="admin-muted">更多内容请查看对应业务详情</span>';
      if (/secret|password|token|private.?key|api.?v3.?key|signing.?key|credential/i.test(key))
        return '<span>已隐藏敏感信息</span>';
      if (v == null || v === '') return '<span class="admin-muted">—</span>';
      if (typeof v === 'string' && /^[\[{]/.test(v.trim())) {
        try {
          v = JSON.parse(v);
        } catch (_) {}
      }
      if (Array.isArray(v))
        return v.length
          ? `<ol class="admin-readable-list">${v
              .slice(0, 100)
              .map((row) => `<li>${block(row, '', depth + 1)}</li>`)
              .join('')}</ol>`
          : '<span class="admin-muted">暂无记录</span>';
      if (typeof v === 'object')
        return `<dl class="admin-readable-fields">${Object.entries(v)
          .filter(([k]) => !['message_limit', 'message_offset'].includes(k))
          .map(
            ([k, row]) =>
              `<div><dt>${U.esc(labels[k] || k.replace(/_/g, ' '))}</dt><dd>${block(row, k, depth + 1)}</dd></div>`,
          )
          .join('')}</dl>`;
      if (typeof v === 'boolean') return `<span>${v ? '是' : '否'}</span>`;
      if (/_cents$/.test(key)) return `<span>${U.money(v)}</span>`;
      if (/_at$/.test(key)) return `<time>${U.esc(U.date(v))}</time>`;
      return `<span class="admin-readable-text">${U.esc(values[v] || v)}</span>`;
    };
    return `<section class="admin-readable-result">${options.title ? `<h2>${U.esc(options.title)}</h2>` : ''}${block(value)}</section>`;
  }
  window.LumoAdminDetails = { render };
})();
