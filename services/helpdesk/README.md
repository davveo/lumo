# helpdesk-service

支持操作：`faq, list, get, create, reply, close`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：RPC命令入口和请求追踪。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `helpdesk.transport.*` → `helpdesk.usecase.*` → `helpdesk.repository.*`，GORM回调再创建SQL Span。查询参数不展开到trace或日志。

本服务只写自己的数据库schema。跨服务通过 `platform.Caller` 访问 gRPC，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

关联订单在创建时通过RPC验证归属；工单会话与回复Outbox在同一本地事务提交，关闭后拒绝回复。

## 独立业务方法

每个操作对应 usecase 的独立公开方法，接收不含 `Action` 的 `platform.Request`。
transport 通过 `MethodHandler` 注册方法并执行边界校验与链路追踪；兼容入口仅负责路由。
服务装配同时导出 `Module.Operations`，供独立 gRPC 方法直接调用。

- `faq` → `Faq(context.Context, platform.Request)`。
- `list` → `List(context.Context, platform.Request)`。
- `get` → `Get(context.Context, platform.Request)`。
- `create` → `Create(context.Context, platform.Request)`。
- `reply` → `Reply(context.Context, platform.Request)`。
- `close` → `Close(context.Context, platform.Request)`。
