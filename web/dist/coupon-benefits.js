(function () {
  'use strict';
  const C = window.LumoConstants;
  const discount = (row) => [C.CouponType.Discount, C.CouponType.ThresholdDiscount].includes(row?.coupon_type);
  const type = (row) => row?.coupon_type || (Number(row?.min_cents || 0) ? C.CouponType.Reduction : C.CouponType.Cash);
  const names = {
    [C.CouponType.Reduction]: '满减券',
    [C.CouponType.Cash]: '无门槛券',
    [C.CouponType.Discount]: '折扣券',
    [C.CouponType.ThresholdDiscount]: '满额折扣券',
  };
  const face = (row) =>
    discount(row)
      ? `${Number(row.discount_bps || 0) / 1000}折`
      : `¥${(Number(row?.amount_cents || 0) / 100).toLocaleString('zh-CN', { maximumFractionDigits: 2 })}`;
  const scope = (row) =>
    row?.scope_type === C.CouponScope.Category
      ? `指定品类：${(row.scope_values || []).join('、')}`
      : row?.scope_type === C.CouponScope.Product
        ? '指定商品可用'
        : '普通商品通用';
  const condition = (row) =>
    `${Number(row?.min_cents || 0) ? '满 ¥' + (Number(row.min_cents) / 100).toFixed(2) + ' 可用' : '无最低消费门槛'}${discount(row) && Number(row.max_discount_cents) > 0 ? ' · 最多优惠 ¥' + (Number(row.max_discount_cents) / 100).toFixed(2) : ''}`;
  function estimate(row, subtotal, items = []) {
    let eligible = subtotal;
    if (row.scope_type && row.scope_type !== C.CouponScope.All) {
      const allowed = row.scope_values || [];
      eligible = items.reduce(
        (sum, line) =>
          sum +
          (allowed.includes(row.scope_type === C.CouponScope.Category ? line.category : line.product_id || line.sku_id)
            ? Number(line.amount_cents || 0)
            : 0),
        0,
      );
    }
    if (eligible <= 0 || eligible < Number(row.min_cents || 0)) return 0;
    let value = Number(row.amount_cents || 0);
    if (discount(row)) {
      const rate = 10000 - Number(row.discount_bps);
      if (!Number.isSafeInteger(eligible) || !Number.isInteger(rate) || rate < 1 || rate >= 10000) return 0;
      value = Math.floor(eligible / 10000) * rate + Math.floor(((eligible % 10000) * rate) / 10000);
      if (Number(row.max_discount_cents) > 0) value = Math.min(value, Number(row.max_discount_cents));
    }
    return Math.max(0, Math.min(value, eligible));
  }
  window.LumoCouponBenefits = { discount, type, names, face, scope, condition, estimate };
})();
