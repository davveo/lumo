# analytics-service

支持操作：`record, list (admin)`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：RPC命令入口和请求追踪。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `analytics.transport.*` → `analytics.usecase.*` → `analytics.repository.*`，GORM回调再创建SQL Span。SQL绑定参数不展开到trace或日志；结构化业务入参和返回值按共享规则递归脱敏并限长。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 的明确方法访问独立 gRPC，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

## 独立业务方法

- `record` → `Record`
- `list` → `List`

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。行为记录继续使用唯一事件键和事务Outbox保证幂等。
