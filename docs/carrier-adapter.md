# 物流渠道适配协议

本项目提供通用 HTTP 适配接口。实际供应商的字段、签名和账号由商家侧适配服务转换为以下协议；未配置渠道时只在演示模式允许演示运单。此接口不代替仓库 WMS 出库适配。

## 创建运单

配置物流服务的环境变量 `LOGISTICS_CARRIER_PROVIDER`（最多 40 字节）、`LOGISTICS_CARRIER_CREATE_URL`（HTTPS）和 `LOGISTICS_CARRIER_TOKEN`。仅演示模式允许 HTTP localhost/127.0.0.1 联调。令牌只保存在服务端；请求不跟随跳转，超时 8 秒。

物流服务发送 `POST <LOGISTICS_CARRIER_CREATE_URL>`，请求头 `Authorization: Bearer <token>`、`Idempotency-Key: <fulfillment_id>`，JSON：

```json
{
  "fulfillment_id": "履约编号",
  "order_id": "订单编号",
  "kind": "ORDER",
  "address": { "recipient": "收件人", "phone": "手机号", "address": "地址" },
  "items": [{ "sku_id": "商品规格编号", "quantity": 1 }]
}
```

地址和商品来自服务端订单快照。适配服务须按履约编号幂等，只有供应商确认受理后返回 2xx 与 `{"tracking_id":"真实运单号"}`（1–100 字节）。同一履约的网络超时重试必须返回同一运单；否则可能在供应商端重复下单。响应体最多读取 16 KB。

## 轨迹回调

网关配置独立的 `LOGISTICS_CARRIER_CALLBACK_KEY`，至少 32 字符。渠道适配服务 `POST /api/v1/logistics/carrier_callback`，JSON 原始字节不超过 64 KB：

```json
{
  "event_id": "渠道内唯一事件编号",
  "tracking_id": "真实运单号",
  "provider": "与创建配置相同的渠道编号",
  "status": "IN_TRANSIT",
  "occurred_at": "2026-11-02T10:30:00+08:00",
  "location": "深圳分拨中心",
  "description": "包裹已离开分拨中心"
}
```

请求头：

- `X-Lumo-Timestamp`：发送时的 Unix 秒，允许与服务器相差 5 分钟。
- `X-Lumo-Signature`：`hex(HMAC-SHA256(key, timestamp + "." + 原始请求体))`，十六进制小写，不含前缀。

`occurred_at` 支持 RFC3339 纳秒及带时区时间。状态允许 `CREATED`、`PICKED_UP`、`IN_TRANSIT`、`OUT_FOR_DELIVERY`、`DELIVERED`、`EXCEPTION`。事件编号最多 128 字节，地点 160 字节，说明 500 字节。

成功返回 200；错误签名返回 401，渠道不匹配返回 403，尚未登记的运单返回 404，可稍后重试。相同渠道/事件编号/内容重复回调返回成功且只写一条；同编号不同内容返回 409。重试时可更新请求头时间戳和签名，保持事件内容不变。乱序事件仍进入历史，但不会倒退当前投影；签收后不再回退状态。

`PICKED_UP` 和 `OUT_FOR_DELIVERY` 的详细状态保存在轨迹中，现有运单主状态统一为运输中，以兼容原订单履约状态机。`DELIVERED` 通过原 `DeliveryConfirmed` 事件推进订单，不能仅以浏览器显示或前台请求代替渠道确认。
