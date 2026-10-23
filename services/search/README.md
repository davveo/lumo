# search-service

支持操作：`query, suggest`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：RPC命令入口和请求追踪。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `search.transport.*` → `search.usecase.*` → `search.repository.*`，GORM回调再创建SQL Span。查询参数不展开到trace或日志。

本服务只写自己的数据库schema。跨服务通过 `platform.Caller` 访问 gRPC，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

当前 engine=catalog_rpc，真实调用 catalog 和 inventory，支持关键词/品牌/类目/可售筛选、价格排序与偏移分页；未伪称运行 Elasticsearch。
