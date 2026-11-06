# reconciliation

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。
跨服务调用使用 `platform.NewClients(...).Service.Method` 与 `platform.Request`，业务层不传递 action。

- `list` → `List(context.Context, platform.Request)`。
- `run` → `Run(context.Context, platform.Request)`。

对账任务尚未领取时，`lease_until` 为 SQL NULL，不写入 Go 零日期。领取后保存实际租约截止时间，兼容 MySQL 严格日期校验。
