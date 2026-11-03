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

`OrderReadyToFulfill` creates the normal-order root; an authenticated line operation can also materialize that same root from the ready order snapshot if its event is delayed. `PaymentSucceeded` cannot create warehouse work. A cancellation that arrives before the ready event leaves a tombstone, so a delayed message cannot revive shipment. Once DISPATCHING or DISPATCHED, cancellation fails instead of returning stock incorrectly.

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

## 按行取消与拆包

`line_state` 返回当前账号可见的商品数量、售后拦截、装包计划及各包裹状态。后台在仓储履约详情选择商品和数量，通过 `parcel_create` 创建独立计划，再对各包裹执行 `dispatch`；未出库计划可 `parcel_cancel`。有效计划与售后拦截共用订单锁，不能占用同一剩余数量。新售后拦截会令涉及商品的旧计划失效，需重新装包。

`hold_lines`、`release_hold` 和 `cancel_lines` 只接受售后服务内部命令。取消回执、库存行操作、包裹和运单使用固定编号；事务内只修改本服务数据，远程库存与物流调用在提交后执行。中断的出库与取消可由恢复队列继续。订单收到带版本的完整数量进度，忽略旧版本；首个包裹签收不会结束还有待发商品的订单。所有未取消商品签收后只产生一次完成事件。

启用按行履约后，旧整单出库/取消入口拒绝执行，防止再次扣减原始总数量。非演示环境仍需真实仓库适配器提供确认，系统不会伪造取消或出库成功。
