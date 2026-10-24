# aftersale-service

Owns customer applications, reviewed eligibility, claimed order-line quantity, physical return state and refund/exchange orchestration. It delegates money movement to refund and inventory movement to fulfillment/inventory.

Actions: `create`, `list`, `get`, staff `approve/reject`, owner `cancel/return`, staff `inspect`, and authorized workflow `refund/exchange`. Application creation requires an idempotency key and checks the order owner, paid/fulfillment state and immutable purchase snapshot. `lines` contains `{sku_id,quantity}`; omitted lines mean the whole order. Active/completed claims cannot exceed purchased quantities. A lock row serializes claims per order, including concurrent different application IDs.

The cash amount is allocated from original order cash payment; the last cash-return claim absorbs integer rounding. An application under review holds its quantities. Rejection/cancellation releases the quantity claim. Refunds stay REFUND_REQUESTED/REFUNDING until an actual `RefundSucceeded` event arrives; an RPC acceptance is not completion.

Return/refund: REQUESTED → APPROVED → RETURNING → INSPECTING → INSPECTED → REFUND_REQUESTED → REFUNDING → COMPLETED. Refund-only skips physical return but first requires confirmed fulfillment cancellation. Damaged or unsellable returns never increase sellable inventory.

Before shipment, refund-only applications must cover the whole order because warehouse cancellation releases the entire order allocation. Partial refund-only applications are rejected before creating a claim or cancelling fulfillment. Physical returns after shipment can still cover selected purchased lines.

Exchange: receive/inspect old goods → reserve and confirm replacement inventory → create a separate replacement fulfillment → EXCHANGING → replacement delivery → COMPLETED. The implementation accepts same-SKU or equal-price replacement; a price-changing replacement is explicitly rejected until a differential-payment workflow is configured. It does not grant a free upgrade or pretend a supplementary payment exists.

`Tick` resumes interrupted inspections, refund requests and exchange preparation. Deterministic receipt/refund/reservation IDs prevent duplicated effects. These workflows have explicit usecase/repository/transport spans and propagate the same context into RPC and GORM operations.

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。
跨服务调用使用 `platform.NewClients(...).Service.Method` 与 `platform.Request`，业务层不传递 action。

- `create` → `Create(context.Context, platform.Request)`。
- `get` → `Get(context.Context, platform.Request)`。
- `list` → `List(context.Context, platform.Request)`。
- `approve` → `Approve(context.Context, platform.Request)`。
- `reject` → `Reject(context.Context, platform.Request)`。
- `cancel` → `Cancel(context.Context, platform.Request)`。
- `return` → `Return(context.Context, platform.Request)`。
- `inspect` → `Inspect(context.Context, platform.Request)`。
- `refund` → `Refund(context.Context, platform.Request)`。
- `exchange` → `Exchange(context.Context, platform.Request)`。

定时恢复根据已持久化的业务状态直接调用本地方法，保留重试与幂等状态流转。
