# HTTP 与 RPC 接口

公网仅通过 HTTP Gateway 进入；服务之间使用 protobuf/gRPC。统一路由为 `POST /api/v1/{service}/{action}`，只读动作支持 `GET`，例如 `GET /api/v1/catalog/get?id=sku-coffee`。网关设置动作白名单，不提供任意内部RPC透传。

## 请求、响应和认证

```json
{"id":"业务对象ID","idempotency_key":"本次创建请求唯一键","data":{"field":"value"}}
```

```json
{"data":{"业务响应字段":"值"},"trace_id":"32位hex"}
```

普通用户请求携带 `Authorization: Bearer <token>`。不要传入 user_id/role；即使传入，网关也会清除，并用IAM验证结果重建身份。管理员演示操作需要真正的管理员会话，不能在JSON里声称自己是admin。内部资源预占、确认、订单裁决和资金退款创建不向HTTP客户端开放。

`Idempotency-Key` 请求头优先于请求体。建议登录后为每次订单提交、售后、秒杀请求使用稳定键；超时查询/重试保持相同键。

## 登录与退出

```sh
curl -s http://localhost:8080/api/v1/iam/login \
  -H 'Content-Type: application/json' \
  --data '{"data":{"username":"demo","password":"Lumo@2026"}}'

curl -s http://localhost:8080/api/v1/iam/logout \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' --data '{}'
```

登录返回 token,user_id,role。退出会撤销服务器会话，不能仅靠浏览器删除token。

## 主要动作表

| 服务 | 用户/公开动作 | 管理员动作 |
| --- | --- | --- |
| catalog | list/get | upsert |
| search/recommendation/content | query/list/list | content.upsert |
| member/address | get/update；list/create/update/delete/default | 按服务业务权限 |
| cart | list/add/remove/clear/merge | 无 |
| engagement | list/put/delete（favorite/compare/history） | 无 |
| media | create/get/complete/download上传与授权下载 | 无 |
| interaction | list/create/like | moderate |
| analytics | record | 无 |
| pricing/checkout | quote/get；quote/submit/cancel | 无 |
| order/payment | list/get/complete；create/get | payment.confirm_demo |
| inventory/campaign | get；list/evaluate | inventory.adjust |
| coupon/points | list/templates/claim；get/checkin | 无 |
| group/flash | list/get/create；list/request/get | 按活动业务政策 |
| fulfillment/logistics | get；get/quote | list/dispatch/cancel/inspect_return；deliver |
| aftersale | list/get/create/return | approve/reject/inspect/refund/exchange |
| refund/invoice | get；list/get/create | confirm_demo；issue_demo/adjust_demo |
| reconciliation | 无 | list/run |
| notification/helpdesk | list/read/read_all；list/get/create/reply/close | 受工单服务权限约束 |

此表描述网关允许的业务入口。字段精确定义、状态前提及生产适配边界请结合各服务 transport/usecase 和 `DEVELOPMENT-CONTRACT.md`，不可将HTTP动作允许理解成绕过业务状态校验。

## 普通交易示例

报价：

```json
{"data":{"items":[{"sku_id":"sku-coffee","quantity":1}],"points":0}}
```

发送到 `/api/v1/pricing/quote`，取响应 `data.quote_id`。金额为整数分。提交订单：

```json
{"idempotency_key":"order-request-001","data":{"quote_id":"<quote_id>","address":{"recipient":"Demo","phone":"00000000000","address":"Demo street","city":"Shenzhen"}}}
```

发送到 `/api/v1/checkout/submit`，响应提供 order_id。然后发送 `{"id":"<order_id>","data":{}}` 到 `/api/v1/payment/create`。实际支付渠道未接入；仅管理员在 DEMO_MODE=true 下通过 payment/confirm_demo 使用返回的支付单ID模拟渠道确认。不能把浏览器支付成功页作为收款依据。

库存、券和积分只由各自服务提交/释放。浏览器无法调用 inventory.reserve、coupon.confirm、order.accept_payment、order.ready、refund.create 等内部动作。

## 错误

HTTP 400 参数错误，401 未登录/会话失效，403 权限或未公开动作，404 对象不存在，409 状态或幂等冲突，429 资源/准入限制，503 依赖不可用，504 RPC超时。响应 `error.code` 为gRPC状态码，`trace_id` 用于定位调用树。

## 领券与文件上传

领券：登录后向 `/api/v1/coupon/templates` 查询模板，再向 `/api/v1/coupon/claim` 发送 `{"data":{"template_id":"welcome-100"}}`。同一用户同一模板只能领取一次，额度由优惠券服务原子裁决。

文件上传：登录后向 `/api/v1/media/create` 发送 `{"data":{"file_name":"receipt.txt","content_type":"text/plain","size":5}}`。响应提供私有对象登记和短时上传授权；按返回的 `upload_method`、`upload_url`、`upload_fields` 上传真实文件（POST policy采用multipart表单）。完成后用文件ID调用 `/api/v1/media/complete`，服务端StatObject核验大小/类型。再调用 `/api/v1/media/download` 获取短时授权下载地址，其他账号不能访问。
