# inventory-service / 库存与实物账

公开查询 `get/batch_get`；管理员 `adjust`；内部 `reserve/confirm/cancel/dispatch/restock`。

## 目录分层

- `service.go`：组合依赖，注册迁移、消费事件与任务。
- `internal/transport`：独立 RPC 方法注册、入参校验与 transport span；兼容 action 仅在边界解析。
- `internal/usecase`：权限、业务流程、跨服务RPC与 usecase span。
- `internal/repository`：只访问本服务 GORM 数据库，事务与 repository span。
- `internal/domain`：数据模型与业务状态。

## 已实现规则

- 当前落地默认仓 `default`，库存按 SKU 建账，多仓是后续模型扩展点。
- 可承诺量 `on_hand - reserved - allocated`。预占不减实物量，confirm 只把 reserved 转 allocated；dispatch 在仓库确认出库时同时减少 on_hand/allocated。
- 固定 reservation_id 幂等，取消先到时保留墓碑；重复 Confirm/Dispatch 不重复扣减。
- ALLOCATED 的取消必须传 `warehouse_confirmed: true`。已 DISPATCHED 不允许取消释放，走独立退回验收。
- restock 仅 `condition=sellable` 增加可售；`damaged/unsellable` 仅写验收记录，不虚增可售。
- 预占/确认/取消/实际出库及验收均在本地事务中持久化，并保留不可变流水。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/inventory/...`。

扩展分类商品库存仅在 `DEMO_MODE=true/1` 时初始化，SKU来源与商品服务共用 `internal/democatalog` 定义，各自只写本服务数据库。按100个SKU批量插入，仅为不存在的SKU建立50件初始库存；重复启动不增加已有库存，不改变预占、分配、缺货状态和库存流水。原基础SKU种子行为保留。

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。
跨服务调用使用 `platform.NewClients(...).Service.Method` 与 `platform.Request`，业务层不传递 action。

- `get` → `Get(context.Context, platform.Request)`。
- `batch_get` → `BatchGet(context.Context, platform.Request)`。
- `reserve` → `Reserve(context.Context, platform.Request)`。
- `confirm` → `Confirm(context.Context, platform.Request)`。
- `cancel` → `Cancel(context.Context, platform.Request)`。
- `dispatch` → `Dispatch(context.Context, platform.Request)`。
- `restock` → `Restock(context.Context, platform.Request)`。
- `adjust` → `Adjust(context.Context, platform.Request)`。

库存确认、取消、发货分别调用仓储方法；共享事务只接收明确的目标状态，保持原行锁与库存流水规则。


## 后台按商品盘点

前端从商品服务取得父商品和实际颜色/规格SKU，一次 `BatchGet` 读取本页所有库存。`Adjust`支持 `expected_on_hand` 乐观校验和reason；在本地行锁事务中禁止盘点量低于reserved+allocated，写入Adjustment的before/after、actor_id、reason及时间。不通过盘点接口直接修改预占或分配。

`Adjustments`为独立管理员读取，按SKU返回最近20条调整，使用(sku_id,created_at,id)复合索引。内部订单库存流水继续保存在既有账本，两种记录不混淆。
