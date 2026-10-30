# 部署配置核对

## 2026-10-28 后台交易、库存、会员与装修扩展

- 补齐20个后台入口：新增订单和用户管理；交易六个领域使用独立服务端分页/筛选/详情及状态允许操作，展示收货、商品、金额、回执、退款、物流和开票字段。状态文案按领域维护，避免相同COMPLETED/REQUESTED/SUCCEEDED协议值串用。对账明确限定最近100条内部支付及最近30份报告，演示收款、退款、签收和发票操作明确标识，不声称接入真实渠道/WMS。
- 七个领域新增AdminList/AdminSummary，IAM另增SetActive、库存另增Adjustments，维持30个独立服务和182项具名RPC；订单保持类型化protobuf。工作台只通过management/dashboard聚合7个摘要，包含8项真实指标、7/30天金额趋势、待办与分布，不携带业务列表或首页配置。全部管理读写要求真实admin。日期按UTC+8，MySQL每日键显式格式化为YYYY-MM-DD，失败服务显示不可用。
- 库存先选真实商品及颜色/规格SKU，一次批量库存；新增并发原值校验、原因、可售下限和调整审计。用户列表分页包含角色/状态/时间，会员资料按需读取；停用普通账号同时撤销会话，保护自身及管理员，原因/操作者/目标状态留痕。不提供任意角色提升或密码修改。
- 优惠券支持满减、代金、折扣、满额折扣，适用全场/类目/商品及折扣封顶。报价和预占均按整数分计算，信任后端商品归属并排除换购；领取复制权益，已领规则冻结，旧券保留兼容。商城券面、券包、弹窗和结算共用展示逻辑。
- 营销弹窗新增领券、公告、海报、新人、会员、活动6种内容模板，继续使用真实人群、时间、页面、频次和优先级。商城装修增加页头搜索、主视觉按钮、最多5张轮播、6项模块开关、公告和页脚客服，保存合并最新文档，后端校验安全图片/内链及字段类型。修复两个HTML入口共享依赖顺序，隐藏首页模块后的网格及轮播刷新布局；自动播放可关闭并尊重减少动态效果。
- 测试：全量go test、go vet、相关领域race通过；前端357项Node测试、73条API/认证断言、129项HTTP、47项会员、125项配置和258项新增运营HTTP检查通过。最后状态文案修正后8项相关Node回归通过。独立MySQL scratch库验证券规则JSON/已领快照/封顶预占、库存审计复合索引、账号状态及日期汇总，测试库与临时程序清理。全局格式与diff检查通过。写入测试全部在隔离库，实际商城只读取和登录/退出验收会话。
- 浏览器在隔离18181实测四规格库存盘点、订单详情、折扣券创建、会员弹窗草稿、装修发布与真实商城渲染；桌面和390px无横向溢出。实际8080复验工作台、已完成订单文案、301851件商品库存目录/四规格及全部装修分区，未保存实际商城业务修改。实际storefront-main尚未建立，装修页明确展示默认值，首次保存才发布。截图 `/private/tmp/lumo-admin-dashboard-live-final.png`、`/private/tmp/lumo-admin-inventory-live-final.png`、`/private/tmp/lumo-admin-decoration-modules-live-final.png`；隔离配置效果 `/private/tmp/lumo-storefront-configured-preview.png`。
- 部署：使用固定工具链交叉编译linux/arm64 server/gateway，在已运行可信镜像替换程序；标准独立Nginx镜像构建前端。先执行 `sh start.sh iam order payment fulfillment aftersale refund invoice inventory coupon pricing checkout content gateway web --no-build`，最后状态文案修正仅重建/更新web。共14个目标容器改变、26个容器ID不变（含已成功退出的存储初始化），保留卷与旧镜像；未更新catalog。未伪造标准后端源码构建指纹，后续正常构建仍可检测差异。
- 运行核验：8080/18080 healthz/readyz均200，32项Consul检查全部passing；工作台7/30天均7域完整，实际分别约11/12ms。七域管理列表200，用户精确10002条且每页20项，普通用户8项管理读取均403；5件商品9个规格库存一次批量返回，券模板总数2007。107个静态文件与工作区逐个SHA256一致，静态文件不带后端Trace；API trace_id只在x-trace-id头。仅撤销本次验收会话，未影响其它登录。
- 证据：`/private/tmp/lumo-admin-expansion-runtime-final.json`、`/private/tmp/lumo-admin-expansion-go-final.log`、`/private/tmp/lumo-admin-expansion-vet-final.log`、`/private/tmp/lumo-admin-expansion-race.log`、`/private/tmp/lumo-admin-expansion-mysql.log`、`/private/tmp/lumo-admin-expansion-frontend-final.log`、`/private/tmp/lumo-admin-expansion-last-status-tests.log`。部署日志可能含本地凭据，已设为仅当前用户可读。

## 2026-10-27 后台商品管理查询超时修复

- 在实际8080后台复现商品管理HTTP500：`catalog/list` 的 `include_disabled=true` 会去掉状态条件，原 `idx_catalog_page` 的status列阻断排序，MySQL改用parent_id单列索引并filesort，301851件商品导致约10035ms读超时。首屏修复后继续实测尾页，确认直接OFFSET读取完整JSON也会超时。
- 新增后台分页索引 `idx_catalog_admin_page(kind,parent_id,created_at,id)`，由启动迁移在线创建并验证定义，重复执行不重建；后台精确计数使用覆盖索引，页面在同一SQL中先通过覆盖索引选出本页ID，再JOIN读取本页详情。保持一次COUNT加一次分页SQL，不逐商品请求。公开在售列表保留原查询选择。未更改商品、订单、库存等业务数据。
- SQLite旧表迁移/重复执行/资料保留及分页顺序回归通过；真实MySQL使用独立scratch库验证上下架、同时间ID排序、越界页、覆盖计数、内层无filesort及外层主键读取，测试库和容器内临时程序均已清理。catalog全包race、go vet、全局format-check及diff空白检查通过。日志 `/private/tmp/lumo-product-admin-race-final.log`、`/private/tmp/lumo-product-admin-mysql-final.log`、`/private/tmp/lumo-product-admin-format-final.log`。
- 已交叉编译当前server并在已有运行镜像上替换程序，再执行 `sh start.sh catalog --no-build`，仅catalog容器ID变化，其余容器保留；旧镜像已留存。全部服务健康，8080/18080的healthz/readyz均200，Consul32项检查全部passing。实际商品首屏45ms、下一页39ms、offset20000为42ms、尾页offset301840返回11件耗时77ms、越界页71ms，均HTTP200；公开列表50ms。`x-trace-id`仅在响应头。证据 `/private/tmp/lumo-product-admin-runtime-final.json`、`/private/tmp/lumo-product-admin-health-final.json` 及 `/private/tmp/lumo-product-admin-deep-plan-final.log`。部署日志含本地访问凭据，以受限文件保存。
- 浏览器实际验证商品管理首屏、下一页、返回上一页及编辑资料加载，未提交业务修改；截图 `/private/tmp/lumo-product-admin-fixed.png`。探针登录均已退出，临时浏览器标签已关闭。

## 2026-10-27 运营配置补齐与前端规范验收

- 新增后台商品管理、优惠券管理、活动管理和商城装修，公告/FAQ改为独立内容管理模块；保留现有14入口，共18个管理菜单。新增Coupon TemplatesAdmin/TemplateUpsert、Campaign AdminList/Upsert四个独立RPC（共166），网关和领域双重验证管理员。券管理分页包含停用模板、批量领取计数；库存用stock_delta避免覆盖期间领取，已发行权益冻结、旧版本/旧种子碰撞拒绝。活动保留held/used，限制配额并冻结已参与规则，新增版本迁移和上限保护。
- 商品完整读取再合并编辑/上下架，保留规格与相对差价，不触碰库存；新商品默认下架。装修保存前合并最新JSON的变更字段，保留其它编辑器配置；公告/FAQ使用真实分页和发布接口。秒杀/拼团/换购仍有交易准入限制，只读显示；未编造会员全量列表、任意积分调整、消息群发、素材列表或推荐权重管理。
- 按用户前端规范复核现有原生模块及公共组件：统一品牌派生Token和admin语义Token，修复自定义主题后按钮/页头颜色不一致；保留语义状态及固定券面。工作台减少卡片嵌套，用数据重点与平面快捷入口改善层级。手机主要操作44px高、数量按钮44×44、后台及弹窗输入16px；统一禁用/只读样式，关闭弹窗恢复有效触发器焦点。
- 浏览器实际验证1280px首页、商品详情、后台工作台/券列表，390px首页、购物车、会员地址与地址弹窗、优惠券/商品表单，无页面横向溢出。隔离库后台创建停用券并成功读回、装修设蓝色后商城首页/购买区真实变蓝；实际运行商城配置未修改。截图 `/private/tmp/lumo-design-admin-live-final.png`、`/private/tmp/lumo-admin-config-coupons-live.png`、`/private/tmp/lumo-design-storefront-blue-desktop.png`、`/private/tmp/lumo-design-cart-mobile.png`、`/private/tmp/lumo-design-address-dialog-mobile.png`。
- 验证：全量 `go test ./...`、`go vet ./...`、coupon/campaign/httpgateway race通过；350项Node测试、73条认证断言、129项前端HTTP、47项会员链路及125项新配置HTTP检查通过。最后购物车点击宽度修正后，主题、购物车和详情相关测试及真实手机排版复验通过；常量/RPC生成、全局格式和diff检查通过。写入测试全部使用独立临时DB，未向实际商城提交配置、库存、订单或领券变更。保存进行中刷新/跨账号迟到回复另有专项回归。
- 构建环境：恢复官方Go1.26.10工具链；官方模块代理连接超时，使用镜像下载固定版本并按go.sum校验。标准Docker构建依赖层的Alpine网络不可用，本次从当前源交叉编译linux/arm64静态server/gateway，在既有运行镜像上替换两程序，旧镜像保留为 `lumo-mall-backend:before-admin-config-20261027`。使用 `sh start.sh coupon campaign gateway --no-build` 运行已构建镜像，web正常构建；增量脚本仍保留标准后端源码构建待更新标记，没有伪造源码指纹。
- 运行：仅coupon/campaign/gateway/web四个容器变化，其余36个ID不变，数据卷保留。98个静态资源与工作区逐个哈希一致，health/ready200，32个Consul检查passing；实际管理员读取2007种券和10项活动，版本均已初始化，普通会员四个新管理入口403。实际仅登录/退出自己的验收会话。
- 证据：`/private/tmp/lumo-admin-config-go-all.log`、`/private/tmp/lumo-admin-config-vet.log`、`/private/tmp/lumo-admin-config-race.log`、`/private/tmp/lumo-admin-config-frontend-final.log`、`/private/tmp/lumo-admin-config-format-check.log`、`/private/tmp/lumo-admin-config-runtime.json`、`/private/tmp/lumo-admin-config-container-diff.json`。start部署日志含凭据，均只保存于受限临时文件，不公开附加。

## 2026-10-27 后台管理界面整理

- 后台改为固定分组侧栏、顶栏面包屑与统一内容区，保留14个管理入口及独立管理会话。工作台加入快捷管理、交易流程、实际近期订单；库存查询、调整与入库拆成独立卡片，内容发布、分类和营销弹窗统一控件及卡片层级。
- 业务列表新增本地关键词/状态筛选、每页10条底部分页，明确显示已加载记录范围，不增加筛选请求。原始明细和Trace按需展开。修复延迟刷新期间跨页面行缓存混用、失败刷新回滚旧筛选；新增回归覆盖行数据隔离、筛选与分页、退出/失效会话及异步状态。
- 分类配置按内容宽度收拢，营销编辑桌面保留预览、窄屏将预览置于配置之后。390/1040/1280px 浏览器核对无页面横向溢出；手机抽屉选中后自动关闭，恢复桌面宽度释放锁定；进入营销编辑从顶部开始。截图 `/private/tmp/lumo-admin-ui-live-final.png`、`/private/tmp/lumo-admin-mobile-workbench.png`、`/private/tmp/lumo-admin-popup-editor-mobile.png`。
- 验证：309项Node测试、73条认证断言、129项隔离真实HTTP及47项会员链路通过；脚本语法、全局格式和diff检查通过。HTTP/浏览器业务写入仅在新建临时数据库进行，使用现有2026-10-25开发运行程序（本轮未改后端；本机Go模块缓存缺失，未重新编译该程序）。证据 `/private/tmp/lumo-admin-ui-node-final.log`、`/private/tmp/lumo-admin-ui-http.log`、`/private/tmp/lumo-admin-ui-member.log`、`/private/tmp/lumo-admin-ui-format-check.log`。
- 已执行 `sh start.sh web`，只更新web，其他39个容器ID保持不变。89个静态资源与工作区逐个哈希一致，健康/就绪200；实际后台登录后14个入口正常、无页面溢出。运行环境仅登录/退出自身管理员会话，没有提交测试订单、券、库存或配置变更。证据 `/private/tmp/lumo-admin-ui-runtime.json`；部署日志 `/private/tmp/lumo-admin-ui-deploy.log` 含访问凭据，仅保存在受限临时文件。

## 2026-10-26 后端全局扫描与隐患修复

本轮检查30个领域服务、HTTP网关、公共RPC/消息/发现/Trace平台及进程入口，使用源码审查、静态检查、可控并发和隔离真实HTTP回归；以下为已确认并修复的问题，不代表对全部潜在缺陷的完备性证明。

- **报价与退款计算**：重复SKU在累加前逐行校验，防止int64数量溢出绕过10件限制；退款剩余额度用减法比较，订单累计退款同样避免加法溢出；同一退款回执携带不同订单或金额时拒绝。高金额售后比例用整数大数乘除保留向下取整，避免合法订单的中间乘积溢出。
- **优惠服务端规则**：秒杀/拼团在报价及新提交两端拒绝叠券或积分；换购商品不计入券门槛及主商品10%积分上限。券预占使用主商品金额；旧快照缺字段时仅扣末尾追加的1件换购，不排除相同SKU的正常主商品，无法验证的旧快照要求重新报价。
- **幂等及数据竞争**：checkout并发创建Saga后再次核对实际落库的hash/quote，防止同key的不同请求复用订单；购物车删除仅允许本人cart_item，付款消费回执不能被公开接口删除后再重放消费。地址内容更新不回写旧默认标记；商品对比的计数与写入放入用户锁事务，两个实例也不能超过3件。新增可重入表仅 `engagement_compare_owners`，不混入业务列表或计数。
- **消息与生命周期**：生产/消费双方校验事件ID、Type的SQL字符和AMQP 255字节边界；消费要求一个完整JSON对象，尾随数据及非法元数据直接进入死信，不创建Inbox、不调用业务，避免毒消息永久重排。服务退出先取消并等待RPC/事件worker，再释放共享数据库及Caller。网关body读取、认证与业务转发共用25秒请求预算，避免前两阶段耗时后重新获得25秒而超过HTTP写超时。
- **附件及Trace配置**：已完成附件的Get/再次完成/重新签下载前校验已存ETag，拒绝相同大小/MIME但内容已变的对象。显式HTTPS OTLP端点保留TLS及SDK CA设置，裸host:port保留现有明文collector兼容；非法URL及NaN采样率拒绝。包含真实本地TLS collector的传输回归。
- **验证**：修前可复现失败、修后针对性回归通过；全量 `go test ./...`、`go vet ./...`、变更相关包及子包的race检查和新增消息/网关边界race通过；完整前端302项Node测试、73条认证断言、129项隔离HTTP及47项会员检查通过。常量/RPC生成器同步、全局格式及diff检查通过。测试写入均使用独立临时数据库，没有向运行商城提交测试订单、退款、领券或库存变更。
- **运行**：已执行 `sh start.sh`，30个领域服务及gateway共31个容器更新，7个中间件和web容器ID均保持原样，数据卷及副本保留。149项只读验收通过，Consul全部健康检查passing，新的用户锁表已在实际MySQL迁移。仅创建/退出自身探针会话，既有会员Get路径可能更新本人资料时间或懒初始化账户。
- **证据**：`/private/tmp/lumo-backend-audit-go-final.log`、`/private/tmp/lumo-backend-audit-vet-final.log`、`/private/tmp/lumo-backend-audit-boundary-race.log`、`/private/tmp/lumo-platform-checkout-audit-race.log`、`/private/tmp/lumo-money-audit-race.log`、`/private/tmp/lumo-audit-identity-race.log`、`/private/tmp/lumo-audit-cart-receipt-race.log`、`/private/tmp/lumo-backend-audit-frontend-final.log`、`/private/tmp/lumo-backend-audit-runtime.json`、`/private/tmp/lumo-backend-audit-container-diff.json`。部署日志含访问凭据，保存在受限文件 `/private/tmp/lumo-backend-audit-deploy.log`，不公开附件。
- **边界**：未向真实RabbitMQ注入故障或进行MinIO并发替换验收；ETag保护重新签发前的检查点，既有预签名URL及Stat之后的替换仍受其有效期/存储策略边界约束。对账仍是最近100条内部演示支付台账，不是全账期或外部渠道对账。官方govulncheck工具下载和官方代理IPv4访问均网络超时，依赖CVE可达性扫描未完成，不能据此声称依赖无漏洞；失败证据 `/private/tmp/lumo-backend-vuln-tool-install.log`。

## 2026-10-25 活动与领券统一商城框架

- 活动福利及领券中心移除旧独立页头/面包屑/大横幅，和首页、商品分类、搜索共用 `storefrontPageStart()` 及 `storefront-page` 样式。页头、主导航、内容宽度、紧凑标题、操作按钮、筛选和底部分页保持一致。活动商品及骨架为6/4/3/2列，券卡及骨架为3/2/1列；首页券预览保持原布局，详情/购物车/结算的内联活动区域保留自身布局。
- 浏览器1280px实测活动、领券、家电三页：主区x=148/宽1052，页头高94、导航y=126/高53、标题y=203/高34.5完全一致。390px下活动/券页标题y=180，操作按钮y=243.09、筛选y=295.09一致；390/1100/1280px均无横向溢出。搜索面板紧贴输入框底部，展开不移动主区或导航；冷进入活动/领券也使用共享框架。
- 全站302项前端Node测试、73条认证断言、129项隔离真实HTTP及47项会员检查通过，券/活动/发现独立套件通过；生成器同步、CSS/JS格式及差异检查通过。此轮只改前端和说明，业务写入回归使用临时数据库。证据：`/private/tmp/lumo-welfare-frontend-final.log`、`/private/tmp/lumo-welfare-format-check.log`、`/private/tmp/lumo-welfare-layout.json`；截图 `/private/tmp/lumo-welfare-activity-final.png`、`/private/tmp/lumo-welfare-coupons-final.png` 及对应 `*-mobile.png`。
- 已执行 `sh start.sh web`，仅web容器更新，其余容器保持原ID。全部运行容器健康，89个部署静态文件与工作区hash一致，代理healthz/readyz均200。只读验收 `/private/tmp/lumo-welfare-runtime-audit.json`；含访问凭据的部署日志保存在受限文件 `/private/tmp/lumo-welfare-deploy.log`。临时预览标签、视口覆盖和开发栈均已清理。

## 本轮：按展示场景拆分读取、统一页面与响应Trace头

- 原首页大包与通用批量入口已退出正常链路。`storefront/shell` 只返回装修和分类配置；`products` 返回有界商品与同类批量库存；`activities` 返回活动、2张券预览及相关商品/库存；`account_summary` 只返回本人资料投影与页头计数。推荐、兴趣栏、弹窗独立读取；订单、地址、售后、发票、客服与完整券包进入对应页面时再加载。`bootstrap` 仅兼容小型shell，`batch` 对游客、普通用户和管理员均403，单元测试确认零子RPC。
- 相同接口与参数的在途读取共享Promise；跨业务不再提交任意服务/动作列表。同类SKU保留每批最多500个的商品/库存批量查询。购物车按每页100行读取完整列表；目录外购物车、收藏及对比商品按引用补齐父商品、规格与库存，缺失商品明确不可用。库存和元数据按账号、令牌、接口地址隔离，旧请求不能覆盖新状态。
- 购物车件数、收藏数与通知未读数使用独立SQL聚合，分页列表仍保持有界，覆盖超过100条记录及其他用户隔离。首次支付只刷新受影响资源；成功页稳定轮询只读订单，状态变化再更新券、积分、购物车及订单SKU库存。领券后只刷新券包，取消订单只更新其受影响资源。
- HTTP成功、错误、健康检查等响应均不再添加顶层 `trace_id`，仅通过 `x-trace-id` 提供链路ID；控制台、前端错误信息与smoke脚本同步读取响应头。页头管理入口只对管理员展示；首页、分类、搜索共享页头、导航、宽度与商品列布局。左侧栏保留同一个面板和按钮平滑过渡，收起不改变主区域坐标，支持减少动态效果偏好。
- 全量Go、gateway及cart/engagement/notification专项race通过；300项前端Node测试、73条认证断言、129项隔离真实HTTP链路与47项会员检查通过，另有优惠券/发现/活动独立套件。生成器同步、全局格式和差异空白检查通过。业务写入回归仅使用独立临时数据库。日志：`/private/tmp/lumo-scoped-all-go-final.log`、`/private/tmp/lumo-scoped-race-final.log`、`/private/tmp/lumo-cart-count-race.log`、`/private/tmp/lumo-scoped-frontend-final.log`、`/private/tmp/lumo-scoped-format-check-final.log`。
- 已执行 `sh start.sh gateway web cart engagement notification`，仅5个目标服务容器变更，其余34个现存容器ID不变。39个运行容器健康，卷初始化任务已成功退出；Consul全部32项健康检查passing。149项运行验收通过，包括89个静态文件与源码hash一致、8080/18080接口边界、精确计数及Trace响应头。运行证据：`/private/tmp/lumo-scoped-runtime-audit.json`、`/private/tmp/lumo-scoped-container-diff.json`；部署日志含访问凭据，保存在受限文件 `/private/tmp/lumo-scoped-deploy-final.log`，不公开附件。
- 实际商城首页游客6次业务HTTP（289ms），会员8次（649ms），每个分区仅调用一次；没有万能bootstrap/batch或逐商品get，无加载错误。会员比游客增加会话恢复与账户摘要。下方旧记录的1次/2次大聚合数据属于此前实现，不代表当前读取边界。证据：`/private/tmp/lumo-scoped-live-requests-final.json`。验收只创建/退出自己的探针会话，既有会员Get可能更新本人资料时间或懒初始化账户；未改商品、订单、库存、券或积分余额。
- 浏览器验证首页与家用电器的页头/导航坐标、内容宽度与6列商品相同；390px下为2列且无横向溢出。普通会员没有编辑/运营管理入口，侧栏快速反向切换保持焦点和主区位置。预览：`/private/tmp/lumo-unified-home-final.png`、`/private/tmp/lumo-unified-catalog-final.png`、`/private/tmp/lumo-unified-catalog-mobile.png`。临时浏览器标签与开发栈均已清理。

## 2026-10-24 独立前端与30服务RPC：源码及隔离验证

- 当前30个领域服务均有明确RPC方法，共162项；29个服务使用无Action的 `platform.Request` 与专属方法注册，订单保留15个强类型方法。业务跨服务调用使用 `platform.NewClients`；HTTP动作及旧Execute只在边界兼容。方法和生成约定见 [独立RPC方法说明](rpc-methods.md)。
- `web` 独立构建和运行，默认8080提供页面/静态资源并同源代理API；Go网关默认18080提供直接API和 `/debug`。静态请求不进入后端Trace。启动脚本按独立前后端源码、配置及镜像基线更新，保留中间件、数据卷和已有领域副本数量。
- 本轮隔离回归通过45项启动测试、17项源码指纹测试，旧端口及MinIO来源引用迁移专项通过；原 `.env` 备份权限和非执行边界覆盖在测试内。使用 `.env.example` 的Compose配置校验通过。
- 官方 `public.ecr.aws/docker/library/nginx:1.30.6-alpine` 成功构建隔离前端镜像；5项真实HTTP测试验证Bearer/请求体、第一方Cookie、网关健康/就绪、静态缓存和404、非法upstream拒绝以及网关更换IP后的重解析。测试仅使用临时网络、容器及HTTP服务，完成后已清理。
- 验证日志位于 `/private/tmp/lumo-web-start-tests-final.log`、`/private/tmp/lumo-web-fingerprint-tests-final.log`、`/private/tmp/lumo-web-legacy-migration-final.log`、`/private/tmp/lumo-web-official-image-build.log` 和 `/private/tmp/lumo-web-official-proxy-tests.log`。此前默认Docker Hub拉取因token请求IPv6超时失败，改用同版本官方ECR镜像后完成构建和代理测试。
- 已执行 `sh start.sh` 更新30个领域服务、gateway与独立web，再以 `sh start.sh web` 更新最终页面。实际8080返回Nginx静态页面/脚本，200且没有X-Trace-ID；18080对页面/脚本返回404，直接及同源代理的readyz均200。web镜像仅含静态资源，gateway镜像仅含Go程序，两者镜像ID不同。
- Consul目录为32类（30领域服务、gateway及Consul自身），31个应用实例全部passing，无warning/critical。只读首页聚合产生的Jaeger Trace `23f54ed49e889e0b7d0c27d61144e9e4` 包含85个span，其中20个明确RPC span，实际调用Catalog/List、Catalog/BatchGet、Inventory/BatchGet、Recommendation/RankBootstrap等；未调用DomainService.Execute。63个span带请求/返回属性，Catalog/List的client/server两侧均已核对。
- 实际游客首页1次业务HTTP（207ms）、会员2次（588ms），均无加载错误；会员探针会话已退出，没有修改运行商城的商品、订单、库存或行为记录。真实环境验收证据 `/private/tmp/lumo-methods-runtime-audit.json` 与 `/private/tmp/lumo-methods-live-requests.json`；部署日志 `/private/tmp/lumo-final-start.log`、`/private/tmp/lumo-final-web-start.log` 含访问凭据，以受限权限保存。

## 2026-10-24 首页、详情和购物车布局与交互

- 首页采用左侧分类、中部主视觉/精选/活动、右侧会员与券的结构，下方六列推荐，继续消费后端配置和真实数据。轮播图有显式grid槽，分类尚未加载时也不会占进左侧窄列；活动明细未完成时保持骨架，已有数据在后台刷新或失败时继续展示。
- 详情改为左侧缩略图和大媒体、右侧价格/优惠/配送/规格/数量/购买操作，下方评价、详情、参数、保障、推荐、问答。仅显示实际媒体和服务端数据；手机依次为媒体、选购、详情。购物车按真实品牌分组，保留选中、数量、删除、移入收藏、优惠和换购，下方最多12件推荐，底部固定选中预览、金额和结算操作；首次加载使用骨架并隐藏未确认数量。
- 修复足迹提前标记和多规格商品缺少默认SKU的问题，等待商品与本人认证上下文就绪后仅记录一次；历史商品信息仅进入足迹页时批量补齐。真实隔离HTTP验证首屏窗口外商品可持久化并显示，1次足迹写入加1次兴趣聚合，无额外首页请求。迟到响应、切换账号及收藏失败保护均有回归。
- 全局去掉优惠券/活动列表顶部重复的翻页按钮，保留下方分页。侧栏折叠仅改变侧栏，安全空间不随之变化；浏览器实测1280px下main/hero折叠前后坐标和宽高完全相同。390/1280/1440/2560px均无横向溢出，2560px主区宽1760px；390px详情区顺序正确，购物车结算栏固定在底部。
- 全量Go测试、关键交易10包race、147项新RPC矩阵与30服务162项契约检查通过。全站前端包含真实HTTP商城107项、会员47项及认证71条断言；新增布局、延迟、足迹和账号隔离检查通过。Python69项中64通过，5项真实Nginx测试需显式镜像开关，已单独启用官方镜像验证5项全部通过。生成器同步、全局格式检查与diff空白检查通过。
- 业务写入和浏览器加购仅使用18180隔离临时数据库。预览：`/private/tmp/lumo-homepage-final.png`、`/private/tmp/lumo-product-detail-final.png`、`/private/tmp/lumo-cart-final.png`、`/private/tmp/lumo-cart-full.png`、`/private/tmp/lumo-detail-mobile.png`。测试日志 `/private/tmp/lumo-methods-go-final.log`、`/private/tmp/lumo-methods-race.log`、`/private/tmp/lumo-all-frontend-final.log`、`/private/tmp/lumo-final-python-tests.log`、`/private/tmp/lumo-footprint-http.log`、`/private/tmp/lumo-final-format-check.log`。

最近补充：2026-10-24。此记录区分静态配置、镜像元数据核对与真实容器运行。

## 2026-10-24 全局长行与缩进整理

- 检查356个普通Go文件，239个文件拆分了长结构体、调用参数、条件、链式调用或测试数据；以120列为目标，超长行从1680条降到34条，剩余为保留原文的SQL、JSON、正则、HTML字符串及生成指令。同步整理72个前端源文件、5个既有Python脚本及Shell脚本，protobuf契约也统一缩进。生成的pb.go、前端常量和演示资产保持原样。
- 以本轮修改前工作区快照核对全部Go语法树、字面量原始内容、注释及规范化token，全部一致；52个JS/CJS语法树等价，2745段模板原文保留。CSS规则、选择器和声明值等价，HTML所有文本节点与属性值相同；Python完整AST、Shell完整AST及heredoc内容均核对通过。两项依赖原排版的前端测试改为容忍换行和空白，不改变业务断言。
- 新增固定版本的 `make format-tools`、`make format` 和 `make format-check`；后者只检查源码，不自动安装依赖。开发依赖保存在node_modules及.local，排除应用镜像。实际安装与命令验证通过；格式化二次执行445个目标文件零变化，故意传入未排版的配置能返回非零，且检查不会写回源码。
- 全量Go33个有测试包、前端239项Node检查、107项真实HTTP检查、47项会员检查及23个脚本语法检查通过；最终启动脚本43项、源码指纹16项回归通过，业务写入测试使用隔离临时数据库。`make format-check`、常量生成同步检查及 `git diff --check` 通过。本轮未部署或修改运行商城数据。
- 日志：`/private/tmp/lumo-format-go.log`、`/private/tmp/lumo-format-go-semantics.log`、`/private/tmp/lumo-format-frontend-final.log`、`/private/tmp/lumo-format-frontend/report.json`、`/private/tmp/lumo-format-driver-verification.json`、`/private/tmp/lumo-format-check.log`、`/private/tmp/lumo-python-format-final-docker-test.log`、`/private/tmp/lumo-python-format-final-fingerprint-test.log`。

## 2026-10-24 全局业务枚举集中维护

- 新增 `internal/constants/`，按30个服务、公共角色/服务名/RPC动作/事件、消息投递与前端投影分文件维护117组、521个无类型字符串常量。生产服务、仓储查询和恢复任务、Saga步骤、事件订阅/发布、RPC适配、网关白名单与前端状态映射都改用语义明确的引用；数据库、JSON、RPC及事件原值保持不变，不需要迁移已有状态记录。不同状态机的同值常量仍分组，旧协议兼容值显式保留。
- `go generate ./internal/constants` 从Go声明生成深冻结的 `web/dist/constants.js`，两个HTML入口在API及业务脚本之前载入。生成器支持空默认值及gofmt注释排版，拒绝类型化/隐式/非字面量/重复分组；`-check` 只校验、不改写过期文件。前端测试脚本首先检查生成同步，再检查所有生产引用存在性和加载顺序。
- 新增Go与前端源码守卫，阻止生产代码重新内联已定义的大写持久化状态。测试fixture保留原始协议字面量验证兼容；前端额外以不同的测试值区分原本同值的领域，确认订单完成操作不依赖售后状态、支付成功不借用秒杀状态、券包和发票各自独立。字段键、提示文案、DOM操作、SQL/Redis关键字和外部库协议保持原位。
- 全量Go、生成器与源码守卫、相关go vet、交易关键路径race通过；完整前端239项Node检查、107项真实HTTP检查、47项会员检查及23个脚本语法检查通过。最后只增加源码守卫断言，专项复跑也通过。实际写入链路回归均使用独立临时数据库。
- 已执行 `sh start.sh` 更新30个业务服务及gateway，保留中间件和数据卷；运行健康/就绪200、Consul无critical服务。27个脚本和入口文件（包含生成常量）与工作区一致。实际游客首页1次业务HTTP（166ms）、会员2次（576ms），均无加载错误；仅创建/退出自己的探针会话，未修改运行商城的商品、订单、库存或行为数据。
- 维护说明 `internal/constants/README.md`；验证日志 `/private/tmp/lumo-enums-go-final.log`、`/private/tmp/lumo-enums-frontend-final.log`、`/private/tmp/lumo-enums-trading-race.log`、`/private/tmp/lumo-enums-platform-final.log`，运行证据 `/private/tmp/lumo-enums-live-verification.json`。部署日志 `/private/tmp/lumo-enums-deploy.log` 含访问凭据，保存在受限临时文件中。

## 2026-10-24 贴边侧栏、六件兴趣商品和主区扩宽

- 桌面左侧兴趣栏、右侧快捷工具及各自展开入口固定到浏览器左右边缘，不再跟随1280px内容容器定位。主区在侧栏安全空间内居中，最大宽度1760px；收起任意一栏会回收对应空间。平板/手机隐藏兴趣栏、精简快捷入口并保留安全间距。
- 新增公开、可选认证的 `POST /api/v1/storefront/recent`，空命令返回最多6个完整、唯一、可售父商品及默认SKU实际库存。本人浏览优先、收藏次之，剩余按既有搜索/对比/购买等信号的推荐排序补齐，最后使用真实可售精选；不同规格只占一个商品位置。无行为及游客显示精选来源，可售目录确实不足或依赖故障时诚实返回更少及降级标记。
- 首屏聚合直接预载recent结果，复用已有行为、订单、商品、推荐及库存；后续行为变化只刷新一次recent聚合，前端取消逐商品/库存读取。缺失SKU和父商品在后端批量解析，禁用、缺失和无货商品跳过补位；首100件不足时继续查真实目录，库存服务故障则停止无效扫描。折叠、展开和普通渲染不读取接口，账号、令牌、地址和读取版本保护旧响应。
- 全量 `go test ./...`、网关全包race、相关go vet、完整前端107项真实HTTP检查与会员47项通过。新增真实HTTP/gRPC测试验证0至5条历史各自补齐6条、变体去重和第六件收藏、本人行为优先、游客/管理员隔离及bootstrap复用；正常首屏仍只执行一次推荐排序及每种行为读取。网关单测补充隐藏/禁用/缺货、目录翻页及库存故障边界；前端侧栏与聚合相关75项回归通过。
- 隔离浏览器验证两栏贴边、滚动保持位置、双侧折叠后主区释放空间及再次展开。只有2条历史的会员得到2件历史商品加4件推荐，AX中共6个不同链接；游客显示6件精选商品。截图 `/private/tmp/lumo-rails-personalized.png`、`/private/tmp/lumo-rails-collapsed.png`、`/private/tmp/lumo-rails-scrolled.png`。浏览和弹窗配置测试只使用独立临时数据库，测试会话已退出，临时开发服务和浏览器页已关闭。
- 已执行 `sh start.sh gateway`，实际健康/就绪200，Consul无critical服务，侧栏脚本/样式、API客户端和首页与工作区一致。游客与会员recent均返回6个不同商品和6份可用库存；会员结果带 `degraded:true`，仍完整返回6件真实收藏商品。实际首页业务HTTP为游客1次（212ms）、会员2次（569ms），均无加载错误，探针会话已退出，未写入商品、库存或行为记录。
- 证据 `/private/tmp/lumo-rails-live-verification.json`、`/private/tmp/lumo-rails-live-requests.json`、`/private/tmp/lumo-rails-go-tests.log`、`/private/tmp/lumo-rails-frontend.log`、`/private/tmp/lumo-recent-gateway-race-tests.log`；部署日志 `/private/tmp/lumo-rails-deploy.log` 含访问凭据，保存在受限临时文件中。

## 2026-10-24 全局统一加载占位

- 新增共用 `skeleton.js/css`，使用浅灰色图片、文字和价格块匹配真实商品布局，并提供优惠券、列表、详情、个人资料、表单、统计、导航和表格形状。覆盖首页/搜索/商品详情、活动/领券/推荐、购物车/结算/支付/订单、会员/客服、分类/兴趣侧栏、评价问答和后台配置；辅助技术只读取加载状态，减少动态效果设置下关闭流光。
- 按资源区分首次加载、成功为空和失败，首次读取完成前不展示虚假的“0件”或空结果操作；失败提供重试，后台更新保留已有内容。地址、券包和开票选择器复用未完成读取，关闭、换账号或切换接口后丢弃旧响应。缺少详情商品时仅补读一次实际商品；占位本身不请求数据。
- 隔离浏览器人为暂缓启动聚合和搜索/领券读取，确认商品区域显示图片、名称和价格占位，完成后替换为实际数据；领券中心显示同一风格的券卡。截图 `/private/tmp/lumo-skeleton-search-loading.png`、`/private/tmp/lumo-skeleton-coupons-loading.png`，慢请求代理和临时开发服务已停止，未修改运行商城业务数据。
- 完整前端回归包含103项真实HTTP检查、47项会员服务检查、231项Node测试及22个脚本语法检查；其中42项加载状态测试覆盖被延迟、被拒绝、成功空结果、后台刷新、请求合并、关闭弹窗和账号切换。补充修复个人搜索历史失败阻塞公开商品查询的问题，相同上下文的在途搜索复用Promise，旧账号结果不能覆盖当前搜索；真实无结果页显示默认帮助文案。验证结果保存在 `/private/tmp/lumo-skeleton-final-frontend.log`。
- 已执行 `sh start.sh gateway` 更新本地商城，健康/就绪200，Consul无critical服务，20个相关页面、脚本和样式与工作区一致。实际访客首页1次业务HTTP（启动聚合），会员首页2次（会话恢复和启动聚合），均无加载错误；探针会话已退出。证据 `/private/tmp/lumo-skeleton-live-verification.json`、`/private/tmp/lumo-skeleton-live-requests.json`；部署日志 `/private/tmp/lumo-skeleton-final-deploy.log` 含访问凭据，仅保存在受限临时文件中。

## 2026-10-24 按人群投放的营销弹窗

- 内容服务新增独立的私有活动配置、访客、投放凭证、全局及单活动频控表；后台“营销弹窗”支持草稿、发布/暂停、复制、文案/主题/券模板与按钮配置、实时预览、优先级、生效时间、投放页面及展示/点击/领取统计。版本冲突阻止覆盖其他管理员的修改，重启只补缺失演示配置。
- 资格由后端按真实IAM注册时间、首次/再次访客、会员等级、可用积分/成长值、兴趣品类及用户允许/排除名单判断。游客、新会员、老会员及成长会员四份演示活动展示不同内容；默认每人每活动最多一次，每天/同一会话最多一次，关闭后不连弹。生产模式没有自动活动种子。
- 首次决策复用首页聚合已加载的会员、积分及行为快照；无匹配时不增加浏览器请求，符合条件时实际显示前确认一次曝光。凭证有5分钟期限，绑定本人/访客、会话和视图；跨标签页并发频控通过数据库事务原子校验。后台标签页及已有表单等待展示；登录/退出、路由和接口作用域变化丢弃旧响应，领券不会在进入页面时自动执行。
- 全量Go、完整前端HTTP103项与会员47项通过；新增后台9项、营销弹窗15项、启动生命周期10项通过。内容、网关、IAM、优惠券及跨服务专项race检查通过，`go vet ./...` 和差异检查通过。隔离真实HTTP/gRPC测试覆盖新老客规则、暂停/配置更新失效、跨账号拒绝、重复领取仅一张及已领券进入真实报价；并发测试使用共享数据库上的两个服务对象确认跨实例频控。
- 隔离浏览器完成后台修改标题并保存、首次访客欢迎弹窗、新会员专享弹窗、真实领取成功及刷新不再展示。后台统计为2次曝光、1次点击、1次领取，与操作一致；测试只修改独立临时数据库。截图为 `/private/tmp/lumo-marketing-popup-guest.png`、`/private/tmp/lumo-marketing-popup-claimed.png`、`/private/tmp/lumo-popup-admin-editor.png`、`/private/tmp/lumo-popup-admin-list.png`。
- `sh start.sh` 已更新业务服务与网关，保留中间件及数据卷。实际商城健康/就绪200、Consul无critical服务；9个相关页面/脚本/样式与工作区文件一致。全新访客启动聚合113ms返回首次访客活动及日常券候选，管理员能读取四份已启用活动及全局每会话1次/间隔60秒设置。运行环境未提交测试曝光、未领取券，探针管理员会话已退出；投放记录仅为正常决策生成的短时凭证。
- 新增访客/投放/视图标识及允许/排除用户名单已纳入统一Trace脱敏。当前部署前端与API同源；以后独立部署前端需要同源反向代理 `/api` 保留第一方访客Cookie，跨域不开放凭据。证据为 `/private/tmp/lumo-popup-live-verification.json`，部署日志 `/private/tmp/lumo-popup-deploy.log`（含访问凭据，不公开附件）。

## 2026-10-24 Trace调用入参和返回值验收

- 统一 `TraceRequest` / `TraceResponse` 记录 `lumo.request`、`lumo.response`、规范错误码及redacted/truncated标记。接入HTTP、Dispatch/Forward、每一跳自动gRPC client/server、RPC transport、30个领域的旧transport与业务入口、订单15个独立用例、现有事件/恢复调用及MQ publish/consume；本地开发事件消费使用同一预览方式。没有新增替代业务span或改变实际调用参数、响应、鉴权及请求数量。
- 预览递归遮蔽凭据、用户身份、手机号、地址、签名能力和自由正文等字段，处理Go值、protobuf、JSON bytes、嵌套和双重编码JSON；与身份字段等值的普通id也遮蔽，其余业务对象ID和金额保留。HTTP及自动gRPC的错误描述只写规范码，实际业务响应保持原样；SQL继续只记录模板及行数，不展开绑定值/完整行。
- 每方向最多8KiB、列表前10项、对象64字段、字符串正文512字节，深度和节点有上限；大JSON流式截取，循环/非法/不支持的值安全省略，不执行自定义JSON marshalers。预览独立于业务对象，仅处理采样span。新增测试覆盖大整数、深层/循环、重复字段、签名URL及凭据文本、身份别名、原值不变、错误不泄露和大列表限长。
- 全量 `go test ./...`、完整前端HTTP业务103项及会员47项通过；platform、httpgateway、order、inventory完整race及业务/MQ专项race通过，相关go vet与差异检查通过。业务写入回归使用独立测试数据库。
- 已更新全部领域服务和网关，保留中间件及数据卷。实际登录Trace `37aeead459e4441f8f9694ac595a0f84`、商品Trace `c23174038cf994eac68a489a26fd2312`、会员Trace `59e45f1301044f97187f8b3832104603`、订单404 Trace `cbc9da79aa2add45ed1da37eb0e6af75`、首页聚合Trace `32c819504ab8653d0866e7ac9654a023` 共101个HTTP/RPC/transport/usecase调用span，均有有效JSON入参与返回值；每个预览不超过8KiB。未发现实际登录密码或探针会话令牌进入Trace，会员id别名也已遮蔽；首页大响应标记截断，旧Trace不回填。
- 实际接口预期状态为200/404，健康/就绪200、Consul无critical服务。验收只读业务数据并创建/退出自己的探针会话，未创建或修改商品、订单、库存、券积分等记录；MQ记录由隔离专项验证，本轮没有向运行商城注入测试事件。脱敏验收证据 `/private/tmp/lumo-trace-payload-runtime.json`，构建部署日志 `/private/tmp/lumo-trace-payload-deploy.log`。

## 2026-10-24 独立服务、Consul发现与水平扩展验收

- 核对30个领域服务各自独立进程/容器，每进程只装配一个模块，使用自身schema及数据库账号；非测试业务代码没有跨领域usecase/repository导入，也未发现跨schema SQL。IAM会话、购物车、支付及回执保存在MySQL，媒体文件存MinIO，没有依赖实例内存或容器本地文件的关键会话状态。统一镜像用于共享构建，各领域仍独立运行。
- 修复Compose将共享服务DNS作为注册地址的问题，改为解析本容器唯一hostname；默认实例ID增加监听端口。RPC原有Consul resolver继续每2秒查询passing地址，round_robin只向READY连接分流，无健康实例时返回Unavailable；显式静态调试地址覆盖除外。
- 修复增量启动脚本遇到同服务多容器就失败的问题：检查全部实例的健康、镜像及配置，更新/补启动时保留现有副本数。MySQL初始化迁移及Seed通过专用连接命名锁串行；Tick跨副本非阻塞互斥，AMQP为空时也生效。Outbox追加claim token，旧发布者不能完成或重置新租约；MQ仍是至少一次投递，业务幂等仍必要。
- Trace资源增加 `service.instance.id`，结构化日志增加 `instance_id`，均与Consul实例ID一致。全量Go测试、平台完整race、专项服务发现race重复5次、前端HTTP业务103项及会员47项通过；Python启停43项（包含4项新增副本场景）、源码指纹16项、相关go vet、Shell语法和差异检查通过。
- 已部署全部领域服务和网关。真实搜索服务从1扩到2副本，20次正常商品搜索按10/10分配；动态扩到3副本后按7/7/6分配。执行 `sh start.sh search` 后仍保留3副本，重新注册的新实例继续按7/7/6分配；缩回原单副本后20次均到存活实例。共80次正常商品查询成功，网关在上述扩缩容及搜索服务更新期间未重启；请求分配由Jaeger实例标签逐条确认。
- 最终30个领域服务及gateway共31个实例，所有注册ID及地址均与各自容器匹配；38个运行容器健康、健康/就绪200、Consul无critical服务。7个中间件容器ID保持不变、数据卷保留；30个领域schema均已有Outbox新列。运行验收未创建或修改商品、订单、库存、优惠券等业务记录，独立登录探针会话已退出。
- 证据位于 `/private/tmp/lumo-scaling-runtime.json`、`/private/tmp/lumo-scaling-final.json`、`/private/tmp/lumo-scaling-deploy.log` 及各阶段 `lumo-scaling-*-instances.json`。启动日志包含用户要求的访问凭据，保存在受限临时文件中，不作为公开验收附件。
- 两副本分流样本Trace为 `4e4d69f61539eebb1e43c4e2cb3dc99f`、`38683c7758c6d66f09fa4d9031e983cc`。最终首页访客1次业务HTTP（145ms）、登录2次（512ms），均无加载错误。
- 验收边界：默认Compose更新仍同时重建选中的副本，不是逐副本无中断滚动发布；默认gateway为固定HTTP单入口，多网关需要外部负载均衡。中间件仍是本机单实例，图中的Elasticsearch及完全异步搜索索引读模型尚未接入。
- 额外排查：不存在的长关键词命中目录JSON/前置通配LIKE全量扫描，Trace `830e50df2db2d8368f35040cc48b601b` 在仓储2秒限时处返回504；此既有长尾查询边界尚未修改，不能用正常词分流验收代替全搜索性能验收。目录COUNT的统计失真早于本轮部署，已执行 `ANALYZE TABLE`，计划恢复使用已有 `idx_catalog_page` 覆盖索引，没有修改索引或业务记录。

## 2026-10-24 订单强类型 RPC 与用例拆分

- 原订单把15个动作和事务状态流转放进同一个Handle；已移除该业务分发函数，按查询、创建、支付、关闭、拼团、履约、退款及事件分文件，用例使用独立Go请求及返回类型。共用事务设施仍保持行锁、权限、版本、退款回执与Outbox原子性，事件直接调用相应方法。
- 新增 `api/mall/v1/order.proto` 和真实生成代码，提供15个独立OrderService方法；核心金额使用int64、时间使用Timestamp。报价/地址是校验后保留的不可变JSON文档，不通过浮点Struct转换；旧Execute及JSON HTTP仅在transport兼容边界适配，不进入万能业务Handle。其他领域服务尚未迁移。
- 模块可注册专属RPC，统一内部凭证、错误归一化、Trace及健康检查。旧Caller的订单调用优先走生成客户端，仅在实例明确缺少新服务/方法时回退Execute，权限、业务、网络及超时错误不重放。订单客户端/服务端Trace保留目标服务和业务动作，标准rpc.service使用真实OrderService。
- 全量Go回归及完整前端HTTP业务103项、会员服务47项检查通过；新增订单8项行为/事务和5项真实bufconn场景、平台9项主测试及子场景通过。订单和平台专项race通过，验证15方法权限、新旧接口互通、大整数快照、业务错误不重放、回退、deadline、取消及链路标签。所有业务写入测试使用独立临时数据库。
- 已定向部署 `order gateway`，健康/就绪200、Consul无critical服务，其余服务及中间件保持运行。独立会话读取不存在订单返回404；Trace `78a9aeb5d1902c99713105535924b95b` 包含gateway、iam、order，客户端和服务端均为 `mall.v1.OrderService/GetOrder`，目标服务为order。验收后已退出测试会话，未修改运行商城的订单等业务记录。
- 运行首页继续为访客1次、登录2次业务HTTP，均无加载错误。部署及实际链路证据位于 `/private/tmp/lumo-order-typed-deploy.log`、`/private/tmp/lumo-order-typed-runtime.json`、`/private/tmp/lumo-order-typed-bootstrap.json`。

## 2026-10-24 领券中心单页数量调整

- 将领券中心每页展示从24张减少为6张，桌面为3列2排；总数、当前范围及上下页仍按完整筛选结果计算，不截断优惠券。首页3张好券预览保持原样。
- 页面切片大小与接口读取大小独立：完整中心继续使用每批最多5000条快照读取全量筛选元数据，首页仅预读100条；翻页复用已加载快照，不增加请求。
- 现有2007张券回归验证335页、首/下一页各6张、末页3张，前后翻页的券ID和范围正确；状态展示、条件筛选、领取校验、跨会话隔离及领取失败重试回归通过。
- 已通过完整前端HTTP业务103项及会员服务47项回归，并定向部署 `gateway`；健康/就绪200，前端资源与工作区一致。运行页面实际2007种券分335页，首/下一页分别显示1–6和7–12；390px手机视口无横向溢出、仍只渲染6张。浏览器验收未提交领券，截图为 `/private/tmp/lumo-coupon-six-runtime.png`、`/private/tmp/lumo-coupon-six-mobile.png`。

## 2026-10-24 活动福利分页验收

- 原秒杀、拼团、换购活动页和“我的拼团”直接渲染全部数组，四类普通活动又固定截断前12件。改为统一每页6项，展示总数及当前范围，顶部快捷上一/下一页，底部数字页码；不足一页隐藏分页按钮，空态保留零条计数。
- 活动类型、秒杀场次、拼团邀请、账号、令牌及网关变化重置页码；数据刷新后夹紧页码，避免末页失效导致空列表；旧页面的分页按钮不能改变新活动。卡片保留原活动、商品、SKU和团ID，购物车/结算的内联换购区保持原有展示。
- 翻页仅切片当前已加载候选，不新增HTTP；普通活动去掉前12件截断，但不声称遍历完整30万商品目录。现有候选边界保持：启动基础目录最多100件及有界补充，公开团列表最近100条。
- 新增18项分页专项全部通过，前端完整真实HTTP业务103项和会员服务47项回归通过。隔离浏览器验证拼团与普通活动12件分为2页，切换活动回到第1页；390px手机视口仍宽390px、每页只渲染6张卡片，翻页后列表定位正常。
- 已定向部署 `gateway`，资源与工作区一致，健康/就绪200、Consul无critical服务。实际拼团与满额立减候选各100件，分17页，第2页显示7–12件，末页仅渲染97–100四件；未提交购买、拼团或换购。首页仍为访客1次业务HTTP（123ms）、登录2次（508ms），均无加载错误。
- 截图：`/private/tmp/lumo-promo-pagination-runtime.png`、`/private/tmp/lumo-promo-pagination-desktop.png`、`/private/tmp/lumo-promo-pagination-mobile.png`。

## 2026-10-24 兴趣商品与快捷工具侧栏验收

- 桌面左侧新增“最近感兴趣”固定商品栏，优先本人足迹，再补收藏；按六个原始 SKU 限制加载量，规格归并到真实父商品，展示图片、两行名称及红色价格。右侧接通首页、购物车数量、个人中心、客服、领券、足迹、意见反馈和回到顶部；反馈打开已有工单表单，未提交测试工单。
- 两侧均支持收起与展开，刷新保留显示偏好；浏览记录与收藏仍只由服务端保存。桌面1280、1440、1472及1560px视口下，右栏与正文均保留8px间距，左栏展开或收起均不覆盖正文。手机端隐藏兴趣栏，右侧保留购物车、客服、回顶部及收起；390px视口页面宽度仍为390px。登录、编辑、结算及支付页隐藏侧栏，替换旧的悬浮客服按钮。
- 启动聚合复用已校验归属的本人行为、目录、推荐和库存，批量补齐前六个 SKU 的目录外信息、父商品及默认库存，不额外读取行为列表。前端按用户、令牌、网关及请求版本隔离，并保护较新的库存读取。新增浏览、移除/清空足迹及收藏变化后显式刷新侧栏；渲染与展开/收起不发接口请求。
- 全量Go回归通过，网关新增6项聚合预载测试；前端18项侧栏测试、完整真实HTTP业务103项和会员服务47项检查通过。专项覆盖规格归并、目录外补位、账号/令牌/网关切换、旧响应丢弃、初始快照复用和工具真实入口。修正测试中等待异步履约记录时提前404的轮询处理，仍要求最终状态为READY；所有业务写入测试使用隔离数据库。
- 已定向部署 `gateway`。实际首页访客1次业务HTTP（143ms）、登录2次（529ms），均无加载错误；健康/就绪200、Consul无critical服务，七项前端资源与工作区字节一致。运行账号展示真实兴趣商品及购物车数量；本次运行环境验收只读取业务数据，未修改商品、订单、库存、足迹或收藏。
- 截图：`/private/tmp/lumo-sidebars-runtime.png`、`/private/tmp/lumo-sidebars-desktop.png`、`/private/tmp/lumo-sidebars-collapsed.png`、`/private/tmp/lumo-sidebars-mobile.png`。

## 2026-10-24 完整品类演示商品与搜索性能验收

- 原始演示目录仅12件，导航20类/586细分类多数没有对应商品。现按后端默认导航派生演示商品计划，新增1,838件商品、3,676个独立SKU；每个细分类至少3款，另有80个分组主题组合。每类新增75–251件，所有分组、快捷入口和配置品牌均有实际搜索结果；价格、规格、演示来源标记和图片由后端保存，库存由inventory独立初始化为50。
- 商品按父聚合幂等批量初始化，已存在父商品整组跳过；库存只插入缺失SKU，不重置预占、分配或账本。新增样例创建时间取原12件最早时间减1小时，使展示商品继续优先，新样例也能出现在较旧压力数据之前。真实商城发布前后比对原12件的名称、价格、规格、状态及库存完全一致。
- 排查同时确认全局“养生壶/猫粮/冰箱”原本均触发2秒504。MySQL新增自动随商品行变化的只读生成投影和覆盖索引，短字段先索引过滤排序再按主键加载命中商品；超长字段使用互斥的精确JSON分支，单SQL合并后统一排序和限量，不截断漏检。保留大小写处理、独立字段匹配、字面通配符、原排序及2秒截止。
- 独立30万行MySQL中在线DDL约19.9秒，搜索63–126ms，无结果搜索67ms；EXPLAIN确认常规分支使用覆盖索引，仅最终结果按主键回表。165组新旧查询对照、生成列表达式验证及17项独立边界审查通过；完整Catalog race、数据和购买链路race通过，全量Go、完整前端/103项真实HTTP/47项会员检查及16项指纹检查通过。
- 已执行 `sh start.sh catalog inventory gateway`。实际库逐批读回全部1,838商品及3,676 SKU，全部可用库存50；20类浏览及20类叶词查询均成功，新样例进入各品类前列。全局养生壶3件/87ms、猫粮3件/87ms、冰箱43个候选/90ms、篮球6件/86ms、打印机6件/86ms；小写apple返回18个候选/81ms，无结果78ms。仍保持浏览器访客1次、会员2次首屏请求；健康/就绪200、Consul critical为0。
- 证据：`/private/tmp/lumo-assortment-live-verification.json`、`/private/tmp/lumo-assortment-live-home-requests.json`、`/private/tmp/lumo-search-document-mysql-scale.log`；实际商城浏览器确认“养生壶”3款商品，截图 `/private/tmp/lumo-assortment-search-live.png`。完整支付验证仅在隔离测试商城执行，未向实际商城创建测试订单。
- 上线复核发现MySQL计数选择parent_id单列索引回表，刷新统计后仍导致catalog/list约911ms。仅为MySQL COUNT固定既有 `idx_catalog_page` 覆盖索引，分页SELECT与SQLite不变；真实MySQL验证ACTIVE/DISABLED/include_disabled/空页计数和顺序均正确、COUNT使用覆盖索引。定向再次更新catalog后，实际catalog/list返回301851件总数/82ms，访客首页180ms、会员564ms且仍为1/2次请求；并发复测搜索89–225ms，无超时。最终性能证据 `/private/tmp/lumo-assortment-final-performance.json`。

## 2026-10-24 分类导航后端配置化验收

- 原分类树与一级导航写在前端脚本，品牌从首屏商品临时推导。现由内容服务数据库保存 `storefront-categories` 文档，管理端及商城编辑工作台新增“分类导航”，可编辑展示文字、实际类目、图标、快捷入口、分组、细分类搜索词、推荐标记及品牌，并排序、增删和启停。前端仅消费配置，加载失败不回退静态业务数据。
- 首次迁移初始化20个大类、93个分组、586个细分词，非演示模式也创建；重复迁移及重启不覆盖管理员内容。固定文档执行管理员权限、结构与容量校验；隐藏导航不会改商品所属类目，已有商品仍可编辑。配置先作为草稿，保存并发布后更新；其他已打开页面刷新后读取新配置。
- 首页复用启动聚合中的装修内容；固定文档未进入前100条时，网关内部只补一次ID查询。实际商城验证访客仅1次bootstrap，会员仅会话恢复及1次bootstrap；无额外浏览器分类请求，悬停和键盘展开不请求接口。
- 全量Go测试、内容服务及聚合race测试通过；完整前端回归、103项真实HTTP检查和47项会员服务检查通过。新增分类生命周期、持久化、权限、编辑发布、会话晚返回隔离及动态文本转义测试通过。隔离商城浏览器实测修改展示名/细分词及排序，保存后新开首页显示准确内容；未修改实际商城运营配置。
- 已执行 `sh start.sh content gateway` 定向更新，实际分类接口返回20/93/586，健康及就绪均200，Consul critical为0，发布资源与工作区一致。运行证据：`/private/tmp/lumo-category-live-verification.json`、`/private/tmp/lumo-category-live-requests.json`；截图：`/private/tmp/lumo-category-editor-admin.png`、`/private/tmp/lumo-category-dynamic-home.png`。

## 2026-10-24 多品类目录与完整分类查询验收

- 首页改为左侧全品类目录和右侧详细面板，最终包含20个大类、93个分组、586个细分入口。面板提供分组标题、快捷分类、细分词及实际商品品牌；悬停、点击和键盘均可切换，展开不查询接口。家电细类补齐冰洗、空调、厨卫、影音、商用、配件及冷柜，编辑器的一级类目同步扩展为20类。
- 手机端采用横向类目入口，详细面板按两列词展示并独立滚动。390px视口面板左右14px/376px、页面宽度390px；修正领券预览的覆盖层级，品类面板覆盖正文，页头搜索保持更高层级。桌面左栏宽232px，与首页横幅同高520px，关闭后横幅位置不变。
- 分类查询由前端过滤首100件改为完整目录先按类目、关键词、品牌及排序查询，再返回最多100个候选及一次批量库存。目录外商品可回显，搜索商品池最多100件并清理旧批次、保留购物车与当前详情；同查询后发请求和不同品类/品牌/接口的晚响应均不能覆盖新商品及库存。现有商品分类不改写，尚无商品的新类目返回空态。
- MySQL新增 `search_category_v1` 虚拟派生列及 `idx_catalog_search_category_v1` 复合索引，在线 `INPLACE/LOCK=NONE` 建立；不信任运行库历史普通类目列，不改商品载荷。迁移重复执行和错误结构拒绝已在独立真实MySQL库验证；SQLite保持等价JSON查询，查询保留2s截止。
- 全量Go回归26个有测试包、目录/搜索race及完整前端回归通过；前端新增13项品类回归，完整HTTP业务103项和会员服务47项检查通过。新增HTTP集成验证类目/品牌/词的联合约束、完整库存、缺货即时变化和内部搜索动作403；数据库回归验证首100件之外的老商品及字面LIKE/注入边界。最终补充细类后，13项品类回归及语法复核仍通过。
- 已部署 `catalog search gateway`。实际30万商品库20个类目合批查询86ms，无单项错误，8个既有类目各返回100个商品/库存，12个新空类目返回空数组。独立空类目查询4ms；家电“冰箱”返回1件真实商品及库存，1228ms；手机数码/Apple品牌返回1件，803ms。首页仍为访客1次业务HTTP（170ms）、登录2次（553ms），均无加载错误；健康/就绪200、Consul无critical，前端资源一致。运行商城验收未修改既有商品、订单或库存。
- 截图：`/private/tmp/lumo-categories-runtime.png`、`/private/tmp/lumo-categories-desktop.png`、`/private/tmp/lumo-categories-mobile.png`、`/private/tmp/lumo-categories-digital.png`。

## 2026-10-24 参考图搜索面板验收

- 搜索框、本人搜索历史及两列搜索发现组成连续红框面板；改为文字搜索按钮，去掉分离卡片和阴影。聚焦即可展开，输入后真实匹配商品在同一面板显示。发现词取自已加载商品并去重，换一换在本地轮换；未接入图片搜索，因此没有添加无效摄像头按钮。
- 历史复用本人已加载的数据，删除调用 `engagement/clear` 且保留输入内容。用户、令牌或接口切换后拒绝旧响应；外部点击、Escape、滚动和路由切换关闭，方向键仅选择实际匹配商品。聚焦、换一换不增加HTTP。
- 桌面展开前后页头均141px、分类栏top124px，表单与面板均宽540px、边框重叠1px连续衔接。390px手机视口中表单/面板左右均14px/376px，页头111px、分类栏top68px不变，页面无横向溢出。手机键盘选择咖啡后真实结果为1件；隔离账户删除历史后当前搜索词保留。
- 新增11项搜索交互回归通过；完整前端真实HTTP业务103项和会员服务47项检查通过。已执行 `sh start.sh gateway`，实际资源与工作区一致；健康/就绪200、Consul无critical服务。实际首页访客仍1次HTTP、登录仍2次，无加载错误。运行商城只读取既有历史，删除及搜索写入验收使用隔离数据库。
- 截图：`/private/tmp/lumo-search-reference-runtime.png`、`/private/tmp/lumo-search-reference-desktop.png`、`/private/tmp/lumo-search-reference-mobile.png`。

## 2026-10-24 个性化与相似商品推荐验收

- 将 `catalog_available_v1` 升级为 `hybrid_content_v2`，基于本人收藏、浏览、对比、搜索和已付款订单构建偏好，结合行为时间衰减、类目/品牌/价格/名称相似度、销量、新品与多样性重排。首页、详情相似商品及购后推荐均接入，返回真实中文理由；当前为内容与行为混合排序，未训练机器学习模型。
- 基础目录100项与偏好召回最多200项合并，最多300个候选；召回最多3条并发SQL、每条400ms截止，部分失败保留成功结果并标记降级。历史元数据、父商品与库存批量读取，排除下架、缺失库存、相似场景自身和已购父商品。历史规格在元数据上限之外也按候选规格排除已购父商品。
- 首页启动复用已读取的本人行为、订单、目录和库存，不重复拉取行为列表。内部 `rank_bootstrap`、`recommendation_candidates` 不对HTTP开放；管理员画像也仅取本人订单。公开接口不接受客户端伪造的画像、用户ID、目录或库存。前端按用户、令牌、网关及请求版本隔离，推荐库存不能覆盖更新的直接库存读取。
- 全量Go回归26个有测试包通过，推荐后端13项和前端11项专项测试通过；推荐、目录、网关和行为服务race检查通过。前端真实HTTP业务103项、会员服务47项检查通过；跨服务测试验证两用户偏好排序不同、画像防伪造、相似商品、实时缺货排除、bootstrap复用和内部动作403。所有业务写入测试使用隔离数据库。
- 隔离浏览器确认收藏耳机并搜索后耳机排首，展示搜索/收藏理由，切换价格排序后咖啡先于高价商品；详情显示相似好物且不含自身。390px手机视口页面宽度仍390px。截图：`/private/tmp/lumo-personal-reco-home.png`、`/private/tmp/lumo-personal-reco-mobile.png`、`/private/tmp/lumo-personal-reco-similar.png`。
- 已定向部署 `catalog recommendation engagement gateway`，实际商城访客首页1次HTTP、134ms；登录首页2次HTTP（current + bootstrap）、526ms，均无加载错误。健康/就绪200、Consul无critical服务、推荐服务1个passing实例；页面引用的28个脚本/样式/HTML与工作区一致。实际商城展示“为你推荐”及真实购买/收藏理由，截图：`/private/tmp/lumo-personal-reco-runtime.png`。运行商城验收只读业务数据，测试登录会话用毕撤销。
- 实际30万商品数据下，独立推荐读取516ms、返回20条商品及20条实时库存，每条有理由；部分稀疏召回触及400ms截止，响应如实返回 `degraded:true`，保留其余成功候选。该限制保证大目录辅助召回不会无限阻塞页面。

## 2026-10-24 搜索建议附着输入框验收

- 原定位使用分类导航下沿，输入框与建议卡片之间隔着整条分类栏。现在建议面板挂在搜索表单内，始终距输入框下沿6px；桌面与输入框等宽，手机端按可用宽度限制边界，不占布局空间。
- 删除“相关好物 / 搜索关键词 / 查看全部结果”的重复占位内容，只在实际匹配商品时显示最多5条紧凑建议。空输入、无匹配或建议请求失败不弹空卡片，搜索按钮与回车仍可直接提交关键词。
- 保留方向键选择、回车提交及Escape关闭；外部点击、离开搜索区域、切换页面和滚动会关闭面板。异步响应检查输入焦点及本轮请求版本，已关闭或过时的查询不会重新打开浮层；建议查询使用公开读取。
- 隔离浏览器验证桌面展开前后页头高度141px、分类栏top124px不变，输入框与浮层均宽540px、间距6px。390px手机视口下浮层左右边界98px/378px，页面宽度仍390px；截图中的无匹配词 `fdsa f` 不再弹框。键盘选中咖啡建议并回车后，真实结果列表显示1件匹配商品；Escape与分类切换均可关闭。
- JavaScript语法、发现/编辑器已有业务回归及7项导航回归通过。`sh start.sh gateway` 定向部署完成，实际脚本和样式与工作区一致，健康/就绪200、Consul无critical服务；实际页面确认浮层与搜索框等宽且间隔6px，无匹配词不弹框。截图：`/private/tmp/lumo-search-attached-fixed.png`、`/private/tmp/lumo-search-no-empty-panel.png`、`/private/tmp/lumo-search-attached-mobile.png`。

## 2026-10-24 全局读取聚合验收

- 原首页由各模块单独读取列表、按 SKU 逐个查询库存，并自动读取全部优惠券分页。运行商城原始业务 API 请求为访客53次、登录用户71次，其中库存22次、券模板21次；统计不包含脚本、样式、图片下载。
- 新增 `storefront/bootstrap` 一次返回启动数据，`storefront/batch` 合并其他页面同一轮的只读请求并去重。最多64个子请求、8个并发 RPC；商品与库存使用每批最多500个 SKU 的 SQL `IN` 查询。搜索、推荐、报价和下单内部的逐 SKU 商品/库存读取也已改为批量查询。
- 首页仅预览100条券模板，进入领券中心时通过 `templates_snapshot` 每页最多5000条批量读取全部元数据。券页重复渲染及60ms路由回调不会重复加载；重新进入、手动刷新或领取后重新核验资格。空搜索复用已有商品；浏览记录更新仅刷新历史列表，FAQ不随每次账户操作重复读取；详情、活动、发票与管理台的独立读取统一并发合并。
- 库存模块直接按500个 SKU 批量读取，100个商品只需一次 HTTP；缺失、漏行和失败均清除该项旧库存，不回退逐个查询。每个 SKU 记录读取版本，晚到的旧成功或旧失败均不能覆盖新库存及错误，新增交错响应与合批回归共17项。
- 聚合入口在执行前检查全部动作和权限，不接受子请求身份、写操作、IAM或嵌套聚合。外层会话只验证一次，公开与个人数据分开派生；会话、接口地址变化使启动快照失效。报价、支付和订单实时状态查询保持独立，批量读取不替代下单时的权威价格与资源校验。
- 已定向更新 `catalog inventory search recommendation pricing checkout coupon gateway`，随后将前端收尾修改更新至网关，保留运行数据及中间件。实际商城复测：访客1次 `storefront/bootstrap`，登录用户2次（`iam/current` 和 `storefront/bootstrap`），均无加载错误；会员中心、消息中心与完整领券中心各一次聚合请求。活动页按列表和依赖列表的商品/库存分两轮读取，共2次；券中心仍显示全部2007种券、每页24张。
- 全量 Go 回归25个有测试包通过，领域测试校验单次SQL批量查询；新增真实 HTTP/gRPC 集成测试覆盖 RPC批量计数、权限预检、账户隔离、库存更新后即时读取及缺失 SKU 独立错误。前端共享聚合、库存批量、编辑器、路由与完整 HTTP 业务102项、会员服务47项检查通过，交易测试使用临时数据库。
- 网关健康与就绪200，Consul无critical服务，实际提供的31个脚本、样式及页面文件与工作区字节一致。实际 Trace `5d477e0924f90283b74594fdcefe34b3` 包含8个服务，库存仅出现 `repository.inventory.GetMany`，各子调用保留真实目标服务标签。浏览器确认首页正常、券预览3张且无错误提示，截图：`/private/tmp/lumo-aggregation-home.png`。

## 2026-10-24 主导航交互统一验收

- 原“活动福利”始终标红并采用独立分隔样式，点击后活动页隐藏主导航；商品分类则保留导航并使用选中下划线。现在商品分类、活动福利及领券中心使用同一主导航，仅当前栏目高亮，悬停、下划线和键盘焦点样式一致。
- 活动各子页与领券中心保留主导航，可直接切回商品分类；活动及领券页不会同时高亮上一次商品分类。保留既有分类筛选行为，手机端仅横向调整导航位置以显示当前项，不滚动正文。
- 新增3项导航回归覆盖全部活动子页、领券中心、分类往返、唯一选中态和横向定位。前端完整回归通过，真实HTTP业务96项、会员服务47项检查通过。
- 隔离浏览器连续切换“家用电器→活动福利→拼团→领券中心→家用电器”，页头底部均为173px，导航高48px；390px手机视口导航高42px，食品生鲜当前项保持可见，无页面横向溢出。搜索建议仍位于导航下方8px。
- 已完成 `sh start.sh gateway` 定向部署，实际商城复核分类与活动的唯一高亮、导航保留及173px页头位置。网关健康/就绪200、Consul无critical服务，发布资源与工作区一致。截图：`/private/tmp/lumo-navigation-fixed.png`、`/private/tmp/lumo-navigation-category.png`、`/private/tmp/lumo-navigation-mobile.png`。

## 2026-10-24 首页领券中心验收

- 首页增加领券预览和导航入口，`#/coupons` 可匿名浏览全部已启用券，支持全部、可领取、限时、条件、已领取筛选。按领取状态和创建时间排序，每页最多渲染24张卡片；筛选使用全部模板元数据，不受当前展示页限制。
- 优惠券新增领取开始时间、可用积分与成长值门槛，分别通过积分及会员服务核验本人真实值，领取不扣积分。服务端事务重验时间和名额，依赖异常时条件券拒绝领取，普通券继续可用；重复请求返回原券且不重复扣名额。种子仅新增5种缺失模板，保留已有配置及领取记录。
- 券包通过本地关联查询返回 `template_name`，停用或公开列表后续页的模板也能正确显示名称；领取成功同步领券状态及本人券包。前端处理重复点击、并发领取/刷新、会话和网关变更，倒计时按服务端时间校准，只在状态边界重新查询。
- 全量Go回归23个有测试包、优惠券race及前端完整回归通过；前端真实HTTP链路96项、会员服务47项检查通过。新增跨服务测试覆盖匿名/分页/资格防伪造，以及领取→报价→下单预占→模拟支付核销→履约→整单退款返券；所有交易测试使用隔离数据库。
- 隔离商城浏览器确认访客领取跳转登录并返回中心、领取后剩余数量减1且券包到账。实际商城已有2007种启用券，界面仅渲染24张，共84页；第二页显示25–48种，限时分类跨全部模板找出2种券，分别可领及尚未开放。390px视口无横向溢出，翻页按钮44px高。
- 已完成 `sh start.sh coupon gateway` 定向部署，两服务健康，网关健康/就绪均200，Consul无critical服务，前端发布资源与工作区一致。实际商城只作浏览验收，未领取现有券或修改已有订单。截图：`/private/tmp/lumo-coupon-center.png`、`/private/tmp/lumo-coupon-home.png`、`/private/tmp/lumo-coupon-center-mobile.png`。

## 2026-10-24 新增四类商品活动验收

- 在原秒杀、拼团、换购基础上增加满额立减（满300减30）、折扣特惠（9折）、多件优惠（满2件8.5折）和限量直降（每单减20元），合计7种类型。新增规则仅插入缺失ID，不覆盖原活动配置及使用量。
- 首页、活动分类、商品详情、购物车和结算页均已接入；普通活动与换购互斥，选择绑定账号、会话、网关、商品SKU及数量，过期或乱序响应不能覆盖后续选择。下单后清除临时选择，订单保留不可变活动快照。
- Pricing从真实SKU累计金额与件数，Checkout提交时再次校验件数、优惠与活动版本，活动配额沿用预占/确认/取消链路。折扣按整数分计算，预览不写入名额，不能由客户端指定优惠额。
- 全量Go回归、Campaign/Pricing相关race及前端完整回归通过。新跨服务测试覆盖四类报价、下单、模拟支付、待发货、幂等名额确认、取消释放、券积分叠加和实付退款；前端真实HTTP业务87项及会员/服务47项检查通过。
- 独立临时商城浏览器验收：两件99元咖啡选择多件优惠，服务端确认原价198元、优惠29.70元、实付168.30元；切换9折后实付更新为178.20元。390px手机视口宽度保持390px，活动分类可横向滚动且当前选项可见，规则卡片和按钮正常换行。
- 已完成 `sh start.sh campaign pricing checkout gateway` 定向部署，四服务健康；运行库保留原活动并新增四类，600元/2件公开预览分别得到3000、6000、9000、2000分优惠。网关就绪200，Consul无critical服务，实际前端资源与工作区一致。真实商城页面与手机布局已复核，未创建运行商城订单；截图位于 `/private/tmp/lumo-campaign-types-fixed.png`。

## 2026-10-23 搜索面板布局与交互验收

- 原建议面板占据页头第二行，桌面展开时页头由141px增高至249px，导航和正文均下移108px。改为绝对定位浮层：有分类导航时位于导航下方8px，无导航时贴近搜索框下沿8px；不占文档流。
- 空搜索框聚焦不再自动弹出热门搜索，输入文字后才显示相关建议；清空、失焦、外部点击或Escape关闭。保留方向键选择、回车提交，窗口及可视区域尺寸变化时重新定位，页面滚动时关闭。
- 独立临时商城浏览器验收：桌面打开建议前后页头141px、导航top124px、正文top199px完全一致，建议与搜索框均宽540px。390px手机视口下页头111px、导航top68px、正文top127px保持不变，浮层左右各12px，页面宽度390px，无横向溢出。
- 建议展开时分类一次点击即可切换；键盘选择并回车后显示真实搜索结果，Escape可关闭；商品详情无分类导航时浮层与输入框间距8px。搜索/发现已有回归、JavaScript语法及差异检查均通过。
- `sh start.sh gateway` 定向部署完成，网关就绪200，实际提供的搜索脚本和样式与工作区一致；运行商城刷新后实测空搜索框已聚焦、建议面板仍隐藏，页头及导航没有下移。最终截图位于 `/private/tmp/lumo-search-focus-fixed.png`。

## 2026-10-23 买家模拟支付完整链路验收

- 原支付页只创建 `PENDING` 支付单，之后需要管理员执行 `confirm_demo`，买家只能查询等待。新增 `POST /api/v1/payment/submit_demo`，支付页点击“模拟支付”自动创建支付单并提交模拟成功回执；已有支付单可直接继续提交。
- 网关与服务均限制演示模式，服务校验支付单归属、demo渠道、订单状态、截止时间及金额；金额、时间和回执由服务端生成。沿用订单Gate→支付行的锁顺序、冻结门禁及唯一支付成功事件，重复提交返回原回执；管理员原入口及权限保留。
- 独立临时商城的真实浏览器验收：待付款订单→模拟支付回执 `SUCCEEDED`→查询服务端订单→“支付已确认/待发货”。购物车原2件、本次购买1件，付款后剩1件；没有修改运行商城已有业务记录。截图位于 `/private/tmp/lumo-demo-payment-success.png`。
- 新跨服务集成测试验证 `PaymentSucceeded`→订单接纳→库存、券、积分提交→待发货及仓库任务、购物车精确消费、支付通知，支付前后重复提交均无重复资源扣减或事件。新增9项前端支付回归覆盖失败重试、重复点击、晚响应会话隔离及跨订单支付单隔离。
- 全量 `go test ./...`、支付/网关相关race测试及前端完整回归通过；前端HTTP链路84项检查、会员/服务47项检查通过。上述交易测试使用隔离数据库，模拟支付不产生真实扣款。
- 已完成 `sh start.sh payment gateway` 定向部署，两服务健康；网关健康/就绪均200、Consul无critical服务、`app.js`与工作区字节一致。使用不存在的支付单验证新接口返回支付服务404，没有对运行商城已有订单提交模拟成功回执。

## 2026-10-23 商城初始加载提示与慢查询排查

- 顶部“正在连接商城服务…”原本从初始化开始一直显示到商品、搜索、活动、账户及推荐数据全部加载完成，并非连接失败专用提示。改为核心数据等待超过500ms才显示中性色“正在加载商城数据…”，商品、库存及搜索数据完成后即隐藏并渲染；后续模块继续加载，真实错误仍显示错误提示与重试入口。
- 排查时网关健康检查正常，Consul无critical服务，前端32个访客请求、50个登录请求均成功；原始整套数据加载耗时约3.0–3.4秒。
- Trace定位到 `catalog.repository.ListProducts` 的COUNT查询。运行库约30万条记录，旧统计信息令优化器选中 `parent_id` 单列索引并回表过滤，实测约850ms；既有 `idx_catalog_page(kind,parent_id,status,created_at,id)` 覆盖索引约60ms。执行 `ANALYZE TABLE catalog_records` 更新统计后，默认查询自动选中覆盖索引，实测约48ms；没有修改业务记录。
- 将同名同列顺序的复合索引声明补入模型，供新部署及已有表自动迁移使用，避免依赖手工建索引；保留运行库已有索引，无重复索引或强制索引提示。
- 完成 `sh start.sh catalog gateway` 定向部署后，针对真实服务重测：访客核心数据就绪163ms、整套233ms；演示账号核心就绪127ms、整套202ms，均无请求失败且提示最终隐藏。此为本机API数据加载耗时，不包含图片下载和浏览器绘制。
- 前端完整回归通过，涵盖71项API/认证断言、6项加载生命周期场景、81项HTTP业务检查及47项会员/服务检查；HTTP测试使用独立临时开发集群。
- Catalog仓储及服务测试通过，新增用已有数据验证索引迁移可重复执行、单列索引保留、COUNT覆盖索引、分页排序和过滤结果不变的回归。部署后确认运行库复用原索引，默认COUNT约49ms；网关健康/就绪均200、Consul无critical服务，前端资源与工作区字节一致，浏览器刷新确认提示隐藏。

## 2026-10-23 请求目标服务标签验收

- HTTP、网关分发/转发、RPC业务span记录 `lumo.target_service`、`lumo.action`；自动gRPC客户端记录目标服务和 `peer.service`，服务端以实际模块名标注。资源级 `service.name` 和标准 `rpc.service` 保持原义。
- 公共商品查询Trace `8ac92476edf11391161295f343589369` 包含gateway、catalog共12个span；HTTP入口目标为catalog、动作为list，自动gRPC和transport目标一致。
- 登录后的会员查询Trace `3ee79e6f65ec8e88790fb5a6e404289a` 包含gateway、iam、member共25个span；入口及转发目标为member/get，鉴权子调用独立标为iam/validate。Jaeger页面已实际展开HTTP Tags确认可见。
- 全量 `go test ./...`、相关HTTP/RPC的race测试和 `go vet` 通过。`sh start.sh` 已完成全部领域服务和网关增量更新，中间件保持运行，数据卷保留。标签从更新后新生成的Trace开始生效。

## 2026-10-23 Jaeger实际运行验收

- Jaeger 2.5.0在加载已有Badger数据时长时间停留在查询扩展初始化阶段，16686和4317未就绪。升级到包含[服务名去重修复](https://github.com/jaegertracing/jaeger/pull/7551)的2.12.0，继续使用原 `lumo-mall_jaeger-data` 卷。
- 升级前停止Jaeger并完整备份3.5 GiB历史数据，压缩归档位于 `.local/backups/jaeger-before-2.12-20261023.zuzNDc/data.tar.gz`，已通过 `gzip -t` 校验。
- Jaeger和Collector探针镜像构建成功；两容器均为 `healthy`。浏览器实际打开16686查询页，并显示完整Trace时间线。
- 旧Trace `7828334e560afb917c36787a508817cb` 仍可查询。新Trace `1d859b781edb9fabe3fbc2a2dc1bac3a` 可查到gateway、catalog及SQL共12个span，确认接收、存储和查询链路恢复。
- 仅替换Jaeger和Collector容器；其余容器ID、数据库/消息队列/对象存储/Consul的数据卷保持不变，业务Consul健康检查仍全部通过。
- 新增健康探针 `go test -race ./cmd/healthprobe` 通过；完整Python启动/源码指纹回归50项通过，Compose配置、Shell语法和差异检查通过。

以下为2026-10-22阶段的静态核对及当时的验证边界。

## 已完成

- `docker-compose -f compose.yaml config -q` 通过，包含30个领域服务、gateway、MySQL、Redis、RabbitMQ、MinIO、Collector、Jaeger和一个卷权限初始化任务，共38项。
- `sh -n` 检查 up/down/smoke 和MySQL初始化脚本通过。
- Gateway 权限测试通过：HTTP身份伪造被清除、内部动作不可透传、普通账号不可调用管理员动作、无会话拒绝、DEMO关闭后演示资金动作被拒、非法JSON拒绝。
- 所有宿主机发布端口绑定127.0.0.1，30个领域RPC端口没有发布到宿主机。
- Jaeger2.5 Badger配置参考官方固定版本样例，显式设置OTLP和查询监听地址，使用独立持久卷；共享初始化任务为Jaeger/MinIO设置10001用户权限。
- Collector0.124.1官方构建清单包含本项目使用的otlpreceiver、otlpexporter、memorylimiterprocessor、batchprocessor和healthcheckextension。
- MinIO固定官方源码commit已在本地用Go1.26.10、CGO_ENABLED=0、`-buildvcs=false -trimpath`成功编译；此项验证源码构建命令，不能替代Docker镜像和容器运行验收。

## 固定镜像与架构

| 镜像 | 核对来源 | amd64 / arm64 |
| --- | --- | --- |
| golang:1.26.10-alpine | DockerHub tag元数据 | 均存在 |
| alpine:3.22.1 | DockerHub tag元数据 | 均存在 |
| mysql:8.4.4 | DockerHub tag元数据 | 均存在 |
| redis:7.4.2-alpine | DockerHub tag元数据 | 均存在 |
| rabbitmq:4.0.7-management-alpine | DockerHub tag元数据 | 均存在 |
| jaegertracing/jaeger:2.5.0 | DockerHub tag元数据 | 均存在 |
| ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.124.1 | 官方GHCR匿名manifest | 均存在 |
| lumo-minio:release-20250907 | 本地构建官方固定源码commit | 随构建平台编译；真实镜像运行待验证 |

Collector的DockerHub对应旧tag未能获取，已改用验证可取的官方GHCR镜像。MinIO DockerHub仓库返回404，Quay匿名拉取仍返回401，因此默认使用 `deploy/minio.Dockerfile` 从官方源码构建，不依赖不可取的旧镜像。官方release `RELEASE.2025-09-07T16-13-09Z` 经GitHub API核对commit为 `07c3a429bfed433e49018cb0f78a52145d4bedeb`，构建时再次校验HEAD。

`start.sh`（兼容入口 `scripts/up.sh`）按服务源码、Compose配置和实际容器镜像记录识别更新范围。需要新应用镜像时执行一次 `docker compose build gateway`，随后只对明确选中的业务服务执行 `up --no-deps --no-build --force-recreate --wait`。运行中的中间件不参与默认更新；MinIO只在需要启动且缺少本地镜像，或显式 `--update-infra` 时构建。

## 早期验证限制（2026-10-22）

该阶段交付环境没有可使用的Docker daemon，未执行完整 `docker compose up`。该阶段记录不能证明所有容器真实启动、镜像构建、MySQL初始化、RabbitMQ路由、MinIO运行和Jaeger写入。后续实际运行验收见上方增补记录。Go单元/真实RPC集成测试的结果见 verification.md；这些不能替代容器启动验收。

首次在有Docker的开发机上运行 `sh start.sh` 后，应检查 `docker compose ps`、运行 `sh scripts/smoke.sh`，并在Jaeger按 `x-trace-id` 响应头查看到数据库SQL子span。文件服务还应完成一次真实预签名上传、complete校验和download下载。

## 本次一键脚本更新

- 根目录 start.sh 和 macOS start.command 提供统一入口；Docker不可用时在创建.env或启动容器前失败。
- 已有.env不会覆盖，也不会作为shell执行；Compose负责解析配置。
- 默认增量更新业务服务；无变化时跳过构建及容器更新。已停止容器使用no-recreate补启动；首次没有基线时更新业务容器一次，中间件保留。
- 每个业务容器保存自己的源码/配置/镜像部署记录；共享镜像标签变化不会触发无关服务更新。健康等待失败不推进业务部署记录，no-build也不会把未构建源码记为成功。
- 中间件运行但不健康会明确报错；配置变化提示显式update-infra，不会自动重建它们。成功退出的卷初始化任务不重复执行。
- 成功地址通过docker compose port获取，支持自定义网关、Jaeger、MQ及MinIO端口。
- MinIO增加HTTP readiness检查，媒体服务显式等待MinIO健康；生成器与提交的Compose保持一致。
- Compose CLI已校验默认端口和8181/9101/9102自定义组合，MinIO签名端口和浏览器上传来源随配置同步。
- 启动脚本回归使用fake Docker验证进程调用与失败行为，不能单独代表真实容器运行结果；后续真实验收另列于本文件开头。

## 2026-10-28 商城能力完善验收

- 完整 `go test ./...` 通过；最终补测支付渠道在非演示模式的限制、购物提醒8个并发检查任务去重与跨用户归属、验证码和OAuth单次消费。
- `scripts/test-frontend.sh` 通过：前端语法、RPC/枚举生成一致性、模块回归；真实HTTP基础业务129项、会员服务47项、管理配置125项、后台操作258项。新增5个前端回归覆盖报价保留/乱序、登录方法错误重试、后台开关版本、订阅会话隔离及新订单恢复自动择券。
- 隔离开发服务浏览器实测后台登录/支付开关、手机验证码登录、商品页订阅及个人中心/后台查询；桌面及390px手机布局没有页面横向溢出，后台表格在容器内滚动。积分切换前后金额面板 top/height 一致，未重新出现骨架。
- `docker compose config --quiet` 通过；生成器与已提交Compose的40个服务环境配置逐项一致。新增凭据仅传入IAM。此次没有重启已有Compose业务，也没有执行真实短信、OAuth账号或支付渠道联调。

## 2026-10-29 管理可读性与渠道参数

- 完整Go回归通过；另验证凭据AES-GCM加密、空密钥保留/显式清除、版本冲突、数据库参数覆盖环境变量、OAuth换码与单次消费、行为查询和工单会话分页/回复关闭。
- 前端完整HTTP回归通过，后台业务操作261项；新增可读行为列表、工单对话/回复、渠道独立参数版本与密钥不回显、通用中文结果4项专用回归。
- 桌面浏览器验证右侧客服对话窗的原始问题、顾客/客服消息和直接回复；390px会话窗与页面均为390px，无横向溢出。
- 已更新本机实际Docker环境，32个业务容器健康；8080/admin.html实际加载新service-admin、admin-details和渠道参数表单，微信登录回调及微信支付商户/证书/通知字段已可见。测试会话回复及示例参数仅写入隔离验收数据库。
- 真实收单适配器仍未接入，配置保存不代表已可真实收款；现有开关与用户配置保留。

## 2026-10-29：注册、商品优惠和具体错误追踪

- 实际商城已显示免费注册入口，公开登录配置确认允许注册且保留原有账号密码方式；隔离环境实测注册成功后可登录，普通用户角色、重名校验和关闭注册的服务器限制通过。
- 实际商城推荐列表桌面1280px显示12件、两排、100件共9页；手机390px显示4件、两排、共25页，无横向溢出。装饰性副标题、标语与推荐解释条已移除。
- 商品详情使用计价服务的最佳已领优惠券，隔离浏览器沙发一件显示可减100元、券后1799元，两件重新计算为3698元；旧响应与跨账号结果会丢弃。
- 异常任务以业务名称、关联单据、中文失败分类和处理进度展示，详情独立排版；历史记录缺失原因有明确提示，新错误分类与失败时间开始保存。
- 完整Go回归、前端全部模块及4组隔离HTTP场景通过；网关修正另完成HTTP、平台及全业务集成回归。真实SQL失败、RPC转换前原始原因、消费失败但死信回执成功、无效消息和长敏感值截断均有测试。
- 在现有Docker环境更新后，发送不改变业务数据的不存在商品查询，实际Jaeger生成Trace `9cd85f7228569e99a5357bbe6aed1a7a`：11个span，10个错误span均包含 `record not found` 和 `exception` 事件，覆盖HTTP、网关、RPC、业务和仓储；记录点含文件、函数和行号。此前网关仅留错误码或被遮蔽的问题已在实际链路确认修复。旧Trace不会回填。

## 2026-10-30：磁盘耗尽、追踪积压与启动恢复

- 实际故障为Docker虚拟磁盘59GiB、100%占用，Jaeger约21.19GB的Badger数据不能写入LOCK文件并反复重启；Collector积压后触发内存拒收。仅清理一天前的闲置构建缓存，释放约7.974GB；保留业务卷与Jaeger历史数据。恢复期间Badger按原有策略整理数据，后续磁盘检查剩余约9GiB。
- Collector配置通过项目固定0.124.1版本的`validate`，Jaeger配置通过2.12.0版本校验；Compose与生成器40个服务逐项一致。应用追踪组件的内存、字节队列和批次上限，沿用72小时保留期。
- 后台轮询过滤覆盖真实SQLite空扫描、查到业务行时的完整父子链、被业务捕获的SQL错误、外部RPC、缓冲上限和并发隔离；`go test -race ./internal/platform -run TestBackground`通过。完整`go test ./...`通过，启动脚本49项测试通过，磁盘满与内存拒收的独立诊断用例通过。
- 实际`sh start.sh`成功构建和增量更新，40个容器中39个常驻容器健康、1个数据卷初始化容器正常完成；商城8080返回200。再次执行普通启动成功，提示代码配置未变、无需重建。
- 同为20秒空闲抽样，order/checkout/engagement三个服务合计从377个span降到12个，剩余均为健康检查，没有空Outbox、Recovery或业务扫描span。新Collector发送队列为0，拒收span和发送失败span均为0；观察时Collector约140MiB/512MiB、Jaeger约1015MiB/2GiB。
- 更新后的真实404商品查询生成Trace `9c496d5046c9cdc4eaf47ea638c83da8`，共11个span、10个错误span，全部错误原因仍为`record not found`，并保留12处exception代码定位。仅使用只读错误请求验证，未新增订单或支付数据。
