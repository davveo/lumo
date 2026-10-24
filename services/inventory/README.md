# inventory-service / 库存与实物账

公开查询 `get`；管理员 `adjust`；内部 `reserve/confirm/cancel/dispatch/restock`。

## 目录分层

- `service.go`：组合依赖，注册迁移、消费事件与任务。
- `internal/transport`：RPC action 白名单、分发与 transport span。
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
