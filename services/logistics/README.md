# logistics-service

Owns waybills and delivery status. `create` is an internal fulfillment command with one deterministic waybill per fulfillment ID; `get/list` enforce user ownership; `deliver` is an authorized carrier/admin confirmation and atomically writes `DeliveryConfirmed` to Outbox. Repeated delivery confirmation emits no duplicate business event.

`quote` currently uses an explicit free-shipping demonstration policy (`shipping_cents=0`, provider `demo-free-shipping`). `create` uses `local-demo-carrier` only when `DEMO_MODE=true`; no real carrier account, physical shipment or external tracking is implied. Replace the carrier adapter and authenticated callback route before connecting a commercial carrier.

Replacement shipments carry `kind=EXCHANGE` and a separate `fulfillment_id`, while retaining the original `order_id`. Order lifecycle consumers must ignore replacement delivery when projecting the original sale; aftersale closes the linked exchange only when the replacement is delivered.

Directory layers are `internal/transport`, `internal/usecase`, `internal/repository` and `internal/domain`. Each layer and GORM operation retains the request context and corresponding trace child spans.

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。
跨服务调用使用 `platform.NewClients(...).Service.Method` 与 `platform.Request`，业务层不传递 action。

- `quote` → `Quote(context.Context, platform.Request)`。
- `list` → `List(context.Context, platform.Request)`。
- `create` → `Create(context.Context, platform.Request)`。
- `get` → `Get(context.Context, platform.Request)`。
- `deliver` → `Deliver(context.Context, platform.Request)`。
