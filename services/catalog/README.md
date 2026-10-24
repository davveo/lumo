# catalog-service

支持操作：`list, get, batch_get, upsert (admin)`；内部 `recommendation_candidates`、`search_products` 仅允许 `system` 角色，不对 HTTP 开放。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：注册独立 RPC 业务方法并记录请求追踪，不包含业务分发。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `catalog.transport.*` → `catalog.usecase.*` → `catalog.repository.*`，GORM回调再创建SQL Span。查询参数不展开到trace或日志。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 提供的明确客户端方法访问独立 gRPC 方法，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

推荐召回接受 `categories`（最多4项）、`brands`（最多4项）、`queries`（最多3项）和 `limit`（默认100、最多200），类别及品牌最多64字，搜索词最多80字，输入trim去重并拒绝控制字符。空兴趣返回空列表，不扫描商品表。返回完整公开商品元数据 `items` 和 `degraded` 状态，推荐服务负责最终排序。

类别、品牌、关键词使用最多3条并发、参数化的JSON过滤SQL，各分配固定候选名额，防止常见类别完全挤掉稀有品牌；每通道独立400ms截止，保留成功通道并标记降级。仅召回启用、无父级、可公开展示的商品，包含默认第一页之外的老商品；无COUNT、逐条SKU查询、全量加载或启动时回填。MySQL/SQLite使用各自JSON提取语法，搜索词中的 `%`、`_` 按字面处理。现有JSON文档过滤在稀疏兴趣下仍可能扫描较多行，超时安全降级，不依赖运行库历史遗留投影列。

目录搜索 `search_products` 供搜索服务执行，输入 `query`（最多120字）、`category`、`brand`（各最多80字）、`sort` 和 `limit`（默认100、最多100）。关键词按名称/品牌/类目包含匹配，结合精确类目和品牌条件后，在整个可见商品目录中筛选、排序，再截取有界候选；空关键词可按类目浏览。支持价格升降序、`sales`/`sales_desc` 销量降序、`newest` 创建时间降序。LIKE通配符按字面处理；调用及SQL仍受2秒截止限制，沿用调用方更早的取消信号。

MySQL使用两版自动生成投影：`search_category_v1` 与原类目索引兼容；v2增加公开可见标记、类目/品牌、搜索文本、数字文本及超长标记。搜索文本以NUL分隔名称/品牌/类目，明确转换UTF-8后LOWER，使用utf8mb4_bin保留大小写转换后的字面匹配；请求不允许控制字符，因此不会跨字段连接处误命中。`idx_catalog_search_document_v2` 覆盖正常商品的过滤、排序值和ID，只在选出最多limit个ID后按主键加载完整payload，避免全局词搜索逐行解析30万条大JSON。

v2搜索文本最多300字符、品牌/类目各80字符、数字文本各32字符；超过任一边界的商品进入独立长文档分支，使用原JSON字段精确过滤与排序，避免截断丢结果。正常分支和长文档分支各取同序topK，单SQL UNION后再按同序取全局topK并JOIN商品表，保证混合排序和limit正确；不存在预先截断商品目录或仅搜索样例ID的逻辑。数字仅在SELECT时CAST，保留历史数值字符串/无效数值的查询行为，不因生成列严格类型转换使旧记录无法迁移。SQLite保留原单SQL JSON查询。

生成列和索引通过MySQL `ALGORITHM=INPLACE, LOCK=NONE` 在线DDL创建，从既有payload自动派生，无独立同步任务或陈旧搜索镜像；商品新增、编辑、下架及种子写入都会原子更新索引。迁移不改业务payload，重复执行校验字段类型、长度、字符集、生成表达式和完整索引列顺序，不信任同名普通列或错误索引。生成表达式只接受安全语法包装差异，保留字符串、函数范围及AND/OR优先级。首次在既有大目录创建覆盖索引会占用额外磁盘与构建时间；不支持在线DDL时明确失败。

显式启用本机MySQL探针后，测试仅在容器中新建并删除独立scratch库，不打开实际商城schema。`LUMO_MYSQL_PROJECTION_TEST=1` 验证旧类目投影在线DDL；`LUMO_MYSQL_SEARCH_TEST=1` 验证v2迁移、查询与旧SQL语义对照、动态更新及错误结构拒绝；再加 `LUMO_MYSQL_SEARCH_SCALE=1` 执行30万背景商品覆盖索引探针，记录在线迁移时间、全局命中/无结果查询耗时与EXPLAIN。默认测试跳过本机专用探针。

演示模式 `DEMO_MODE=true/1` 在原12件展示商品之外加载 `internal/democatalog` 的分类样例商品，每个商品及其独立规格由本服务写入。每100个父商品分批事务初始化；已有父商品整组跳过，不覆盖管理员文案/价格/上下架状态，也不重建已移除的旧规格。新扩展样例的创建时间取原12件展示商品最早创建时间减1小时，既保留原有活动展示商品在前，也让新样例领先较早的压力测试数据；重复初始化不修改已有商品时间；搜索仍覆盖完整目录。关闭演示模式不导入商品。

业务入口使用不含 `Action` 的 `platform.Request`；每项操作由独立公开方法实现权限、参数与业务校验。方法：`SearchProducts`、`RecommendationCandidates`、`BatchGet`、`List`、`Get`、`Upsert`。运输层仅维护操作到方法的注册表，HTTP 兼容命令不会进入业务层。
