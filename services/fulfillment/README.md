# fulfillment-service

Owns outbound warehouse jobs, cancellation acknowledgements and return inspection receipts. It never writes order or inventory tables.

Layers: `service.go` composes `transport → usecase → repository → GORM`; domain models and states are in `internal/domain`. All layer calls pass the tracing context. GORM SQL tracing is installed by the shared platform.

| Action | Caller | Meaning |
| --- | --- | --- |
| `list`, `get` | Owner or administrator | Paginated bounded job listing / one job |
| `dispatch` | Administrator or service | Persist DISPATCHING, call inventory.dispatch, create a logistics waybill, then persist DISPATCHED and its event |
| `cancel` | Service or administrator | Persist cancellation tombstone, warehouse cancellation request and acknowledgement, then release inventory allocation |
| `inspect_return` | Service or administrator | Fixed inspection ID, SKU, quantity and sellable/damaged/unsellable condition; independent inventory receipt |
| `create_exchange` | Service | Separate replacement fulfillment with its own reservation, linked to the original order |

`OrderReadyToFulfill` is the only normal-order creation event. `PaymentSucceeded` cannot create warehouse work. A cancellation that arrives before the ready event leaves a tombstone, so a delayed message cannot revive shipment. Once DISPATCHING or DISPATCHED, cancellation fails instead of returning stock incorrectly.

`Tick` resumes interrupted dispatch/cancellation jobs. Inventory and carrier commands use deterministic IDs, so retrying an unknown RPC result is safe.

The bundled warehouse is explicitly a deterministic local adapter and requires `DEMO_MODE=true` for dispatch/cancellation acknowledgement. With demo disabled, those actions wait/fail clearly until a real WMS adapter is supplied. Return inspection is an explicit authorized staff confirmation; it never claims to have queried a real warehouse automatically.

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。
跨服务调用使用 `platform.NewClients(...).Service.Method` 与 `platform.Request`，业务层不传递 action。

- `list` → `List(context.Context, platform.Request)`。
- `get` → `Get(context.Context, platform.Request)`。
- `create_exchange` → `CreateExchange(context.Context, platform.Request)`。
- `dispatch` → `Dispatch(context.Context, platform.Request)`。
- `cancel` → `Cancel(context.Context, platform.Request)`。
- `inspect_return` → `InspectReturn(context.Context, platform.Request)`。

定时恢复根据已持久化的业务状态直接调用本地方法，保留重试与幂等状态流转。
