# payment

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。

- `get` → `Get(context.Context, platform.Request)`。
- `list` → `List(context.Context, platform.Request)`。
- `create` → `Create(context.Context, platform.Request)`。
- `submit_demo` → `SubmitDemo(context.Context, platform.Request)`。
- `confirm_demo` → `ConfirmDemo(context.Context, platform.Request)`。
- `close` → `Close(context.Context, platform.Request)`。

顾客模拟提交与管理员确认分别校验权限；支付锁顺序、订单状态校验、幂等回执与 Outbox 事件保持不变。
