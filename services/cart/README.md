# cart-service

支持操作：`list, add, remove, clear, merge`。

`List` 返回本人 `cart_item` 的精确 `total`（商品行数）和 `quantity_total`（商品件数），不受 `limit` / `offset` 分页影响；付款消费回执不计入。`items` 仍最多 100 行，`limit` 表示本页实际返回行数。统计与列表共用用户、类型及可选状态过滤条件，通过兼容 MySQL / SQLite 的 SQL 聚合完成，不为计数加载全部记录；无效 JSON、缺失数量或负数数量按 0 件计。

公开 `Remove` 只删除本人的 `cart_item`，兼容真实行ID和SKU形式；同表的付款消费回执不能通过该入口删除，保证合法支付事件重投不会再次消耗后来加入的商品。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：RPC命令入口和请求追踪。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `cart.transport.*` → `cart.usecase.*` → `cart.repository.*`，GORM回调再创建SQL Span。SQL绑定参数不展开到trace或日志；结构化业务入参和返回值按共享规则递归脱敏并限长。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 的明确方法访问独立 gRPC，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

加入购物车查询可售商品与库存，累计每SKU不得超过10。merge 使用 max 合并而非重复累加，可在部分失败后安全重试。购物车不预占库存。

## 独立业务方法

- `list` → `List`
- `add` → `Add`
- `set` → `Set`
- `remove` → `Remove`
- `clear` → `Clear`
- `merge` → `Merge`

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。`Add` 累加数量，`Set` 设置绝对数量；两者共享明确职责的数量校验与持久化逻辑。
