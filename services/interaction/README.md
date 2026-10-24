# interaction-service

支持操作：`list, create, like, moderate (admin), answer (admin)`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：注册独立 RPC 业务方法并记录请求追踪，不包含业务分发。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `interaction.transport.*` → `interaction.usecase.*` → `interaction.repository.*`，GORM回调再创建SQL Span。SQL绑定参数不展开到trace或日志；结构化业务入参和返回值按共享规则递归脱敏并限长。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 提供的明确客户端方法访问独立 gRPC 方法，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

评价必须验证本人已完成订单并包含SKU；待审核内容不进入公开列表，点赞关系唯一。问答和商家回答也有公开状态。

业务入口使用不含 `Action` 的 `platform.Request`；每项操作由独立公开方法实现权限、参数与业务校验。方法：`List`、`Create`、`Moderate`、`Answer`、`Like`。运输层仅维护操作到方法的注册表，HTTP 兼容命令不会进入业务层。
