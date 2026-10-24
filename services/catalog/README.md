# catalog-service

支持操作：`list, get, batch_get, upsert (admin)`；内部 `recommendation_candidates`、`search_products` 仅允许 `system` 角色，不对 HTTP 开放。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：RPC命令入口和请求追踪。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `catalog.transport.*` → `catalog.usecase.*` → `catalog.repository.*`，GORM回调再创建SQL Span。查询参数不展开到trace或日志。

本服务只写自己的数据库schema。跨服务通过 `platform.Caller` 访问 gRPC，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

推荐召回接受 `categories`（最多4项）、`brands`（最多4项）、`queries`（最多3项）和 `limit`（默认100、最多200），类别及品牌最多64字，搜索词最多80字，输入trim去重并拒绝控制字符。空兴趣返回空列表，不扫描商品表。返回完整公开商品元数据 `items` 和 `degraded` 状态，推荐服务负责最终排序。

类别、品牌、关键词使用最多3条并发、参数化的JSON过滤SQL，各分配固定候选名额，防止常见类别完全挤掉稀有品牌；每通道独立400ms截止，保留成功通道并标记降级。仅召回启用、无父级、可公开展示的商品，包含默认第一页之外的老商品；无COUNT、逐条SKU查询、全量加载或启动时回填。MySQL/SQLite使用各自JSON提取语法，搜索词中的 `%`、`_` 按字面处理。现有JSON文档过滤在稀疏兴趣下仍可能扫描较多行，超时安全降级，不依赖运行库历史遗留投影列。

目录搜索 `search_products` 供搜索服务执行，输入 `query`（最多80字）、`category`、`brand`（各最多64字）、`sort` 和 `limit`（默认100、最多100）。关键词匹配名称/品牌/类目，结合精确类目和品牌条件后，在整个可见商品目录中筛选、排序，再截取有界候选；空关键词可按类目浏览。支持价格升降序、`sales`/`sales_desc` 销量降序、`newest` 创建时间降序。单条参数化SQL，LIKE通配符按字面处理，不COUNT或进行offset遍历；调用及SQL受2秒截止限制，沿用调用方更早的取消信号。该操作不修改现有商品、类目或数据库结构。

MySQL迁移新增只读虚拟生成列 `search_category_v1`，从带JSON有效性保护的payload.category派生并截取80字；新索引 `idx_catalog_search_category_v1(kind,parent_id,status,search_category_v1,created_at,id)` 使不存在的类目直接返回空结果。生成列随payload变更自动更新，不依赖或覆盖历史普通category列，不回填业务payload。两条DDL均使用 `ALGORITHM=INPLACE, LOCK=NONE`，不支持在线DDL时显式失败；重复迁移校验生成表达式、类型和索引列顺序，不静默信任同名错误结构。SQLite继续使用JSON筛选。仅创建该最小类目索引，未扩展推荐召回或添加无关投影。

`TestMySQLCategoryProjectionOnlineMigration` 显式启用后在MySQL容器中建立并删除独立scratch库，验证真实在线DDL、重复迁移、旧普通列隔离、正常保存自动更新投影，以及错误索引/普通同名列被拒绝。默认测试跳过此本机专用探针。
