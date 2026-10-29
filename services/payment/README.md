# payment

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。
跨服务调用使用 `platform.NewClients(...).Service.Method` 与 `platform.Request`，业务层不传递 action。

- `get` → `Get(context.Context, platform.Request)`。
- `list` → `List(context.Context, platform.Request)`。
- `create` → `Create(context.Context, platform.Request)`。
- `submit_demo` → `SubmitDemo(context.Context, platform.Request)`。
- `confirm_demo` → `ConfirmDemo(context.Context, platform.Request)`。
- `close` → `Close(context.Context, platform.Request)`。

顾客模拟提交与管理员确认分别校验权限；支付锁顺序、订单状态校验、幂等回执与 Outbox 事件保持不变。

新增 `methods` 返回已配置启用的可用渠道，`policy_get/policy_upsert` 仅管理员可读写。创建支付和用户 submit_demo 均重新检查开关；已产生渠道回执仍可处理。当前真实可执行适配器只有演示支付，其他渠道不能启用。新支付号为 PAY 雪花编号。
