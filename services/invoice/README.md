# invoice-service

Owns invoice requests, original invoice identity, refundable totals and linked adjustment records. It reads order facts over RPC and never writes payment/order tables.

Actions: owner `create/list/get/reversals`; administrator `issue_demo/adjust_demo`. Personal/company requests validate title and company tax identifier. There is one request per order; ownership and server-side paid amount are checked. A waiting group may store an intention, but issuing requires an eligible ready/shipped/completed order.

Issuing is a deliberately named local provider action and requires `DEMO_MODE=true`. The resulting `DEMO-INVOICE-*` number is not an actual fiscal invoice. A commercial invoice provider and authenticated result callbacks must be connected separately.

On `RefundSucceeded`, cumulative refund totals reconcile asynchronous ordering and duplicate events. An application re-reads the order after insertion to catch refunds that arrived before the request existed. Issuing computes the current net amount inside its own database transaction. Unissued requests are adjusted/cancelled; an issued original remains intact and receives a linked REVERSAL_PENDING record. The original number is never deleted. `adjust_demo` completes only explicit demonstration adjustment records.

Files/downloads are not falsely represented as real tax files. A production provider must return a genuine document object reference and media-service download authorization.

Layers: composition root → transport → usecase → repository → GORM; every boundary retains context and emits trace spans. State mutation and Outbox are committed in one local transaction.

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。

- `list` → `List(context.Context, platform.Request)`。
- `create` → `Create(context.Context, platform.Request)`。
- `get` → `Get(context.Context, platform.Request)`。
- `reversals` → `Reversals(context.Context, platform.Request)`。
- `issue_demo` → `IssueDemo(context.Context, platform.Request)`。
- `adjust_demo` → `AdjustDemo(context.Context, platform.Request)`。
