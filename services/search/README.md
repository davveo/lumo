# search-service

支持操作：`query, suggest`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：注册独立 RPC 业务方法并记录请求追踪，不包含业务分发。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `search.transport.*` → `search.usecase.*` → `search.repository.*`，GORM回调再创建SQL Span。查询参数不展开到trace或日志。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 提供的明确客户端方法访问独立 gRPC 方法，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

当前 engine=catalog_rpc，真实调用 catalog 和 inventory，支持关键词/品牌/类目/可售筛选、价格/销量/新品排序与有界窗口内偏移分页；未伪称运行 Elasticsearch。

关键词、类目、品牌及排序先下推至仅系统角色可调用的 `catalog/search_products`，在整个目录筛选后取最多100项，避免仅过滤默认第一页而遗漏老商品。随后一次 `inventory/batch_get` 查询默认SKU实时库存，沿用当前用户身份，保持缺失及失败时拒绝编造库存；`in_stock` 在此候选窗口内应用，`offset`/`limit` 也只在该窗口分页，`total` 为窗口中过滤后的数量，`candidate_limit:100` 明确查询上限。

返回完整商品元数据，`items[].available` 和仅对应本次返回商品的 `inventory_items` 供前端一次水合商品与权威库存，不需要逐商品补查。目录筛选不接受外部画像或身份数据，不增加公开网关动作，不进行跨服务SQL或数据库迁移。

业务入口使用不含 `Action` 的 `platform.Request`；每项操作由独立公开方法实现权限、参数与业务校验。方法：`Query`、`Suggest`。运输层仅维护操作到方法的注册表，HTTP 兼容命令不会进入业务层。
