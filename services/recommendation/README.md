# recommendation-service

支持公开操作：`list`。内部 `rank_bootstrap` 仅允许服务端的 `system` 调用，不向HTTP暴露。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：注册独立 RPC 业务方法并记录请求追踪，不包含业务分发。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `recommendation.transport.*` → `recommendation.usecase.*` → `recommendation.repository.*`，GORM回调再创建SQL Span。SQL绑定参数不展开到trace或日志；结构化业务入参和返回值按共享规则递归脱敏并限长。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 提供的明确客户端方法访问独立 gRPC 方法，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

当前策略 `hybrid_content_v2` 是可解释的内容与用户偏好混合排序，不宣称已训练模型（`personalized_model:false`）。

公开请求支持 `scene:home|similar|post_purchase`、`limit:1..20`，相似场景必须提供 `product_id`，购后场景必须提供本人 `order_id`。兼容旧的仅带 `order_id` 请求。返回完整商品、中文 `recommendation_reason`、原因代码数组 `recommendation_reasons` 与分数 `recommendation_score`；顶层包含 `personalized`、`degraded` 和 `signal_count`。`inventory_items` 仅包含实际推荐商品的实时默认SKU库存，方便页面复用。

登录用户的收藏、浏览、对比、搜索和已付款订单由对应服务真实读取。并发读取最多5类信号，共用700ms辅助读取超时；即使调用者是管理员，画像读取也强制使用普通用户身份与当前用户ID，不能读取全站订单生成个人画像。外部JSON中的 `user_id`、画像、目录或库存字段不能注入公开推荐。内部启动排序复用网关已读的数据，不重复请求行为列表。

收藏/对比/浏览/购买基础权重分别为8/5/3/12，按30天半衰期衰减；搜索使用14天半衰期。相同商品与行为类型按最新有效权重去重，最多保留200项商品兴趣和20条搜索，防止重复历史无限放大。权重总量同时控制画像置信度，避免只有很久以前的行为时归一化抵消时间衰减。

商品相似度由类目、品牌、对数价格距离及名称二元字符相似度组成；相似场景和购后场景增加真实上下文商品的关联权重。基础销量、新鲜度作为较小探索因子。最终按类目及品牌重复惩罚重排，兼顾偏好与多样性；排序确定，不使用随机共享状态。已付款订单会排除购买SKU及其父商品，未付款/已全退订单不作为购买偏好；显式订单上下文也会排除本单商品。

候选先读公开目录100项，再按最多4个偏好类目、4个品牌和3条搜索通过目录内部召回最多200项，去重后不超过300个候选。历史SKU元数据与父商品分最多两次 `catalog/batch_get` 读取，库存使用一次 `inventory/batch_get`，不逐SKU请求。启动排序复用传入的库存，仅补查新增候选，并内部返回 `stock_snapshot` 给网关生成启动快照；该字段不出现在公开推荐响应。

辅助行为或召回部分失败会标记 `degraded:true`，以仍可靠的公开目录、画像或库存继续推荐；目录与整个权威库存查询失败不会凭空编造可售状态。缺失库存的候选会跳过，下架商品、相似场景自身商品和已购商品不会返回。目录召回可返回部分通道结果及 `degraded`，推荐服务沿用该标记。

业务入口使用不含 `Action` 的 `platform.Request`；每项操作由独立公开方法实现权限、参数与业务校验。方法：`List`、`RankBootstrap`。运输层仅维护操作到方法的注册表，HTTP 兼容命令不会进入业务层。
`List` 读取当前用户的权威行为与公开目录，`RankBootstrap` 验证系统身份后使用聚合快照；两者共享纯排序与候选整理算法，不通过 action 或操作模式切换业务入口。
