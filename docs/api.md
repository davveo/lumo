# HTTP 与 RPC 接口

公网仅通过 HTTP Gateway 进入；服务之间使用 protobuf/gRPC。统一路由为 `POST /api/v1/{service}/{action}`，只读动作支持 `GET`，例如 `GET /api/v1/catalog/get?id=sku-coffee`。网关设置动作白名单，不提供任意内部RPC透传。

## 领域 RPC 契约

订单使用 `api/mall/v1/order.proto` 的独立 `OrderService`：`ListOrders`、`GetOrder`、`CreateOrder`、`MarkPending`、`MarkCreateFailed`、`AcceptPayment`、`MarkReady`、`WaitForGroup`、`BeginClose`、`CompleteClose`、`MarkGroupFormed`、`MarkGroupFailed`、`MarkShipped`、`CompleteOrder`、`RecordRefund`。每个方法使用明确的protobuf请求/响应，金额为int64、时间为Timestamp；报价/地址是校验后保留的历史JSON快照，避免经过Struct丢失整数精度。

新方法与旧 `DomainService.Execute` 由同一内部凭证保护，并调用相同的独立订单用例。`list/get/complete` 沿用本人及管理员权限，其余写入仅内部角色使用；HTTP仍只开放订单 `list/get/complete`。旧调用兼容映射到新RPC；不会通过新增方法将内部订单裁决暴露给浏览器。具体分层和扩展方式见 `services/order/README.md`。

其余29个领域服务也均使用独立的protobuf服务与方法，例如 `mall.v1.CheckoutService/Submit`、
`mall.v1.InventoryService/Reserve`、`mall.v1.CatalogService/BatchGet`。请求类型 `ServiceRequest` 只有对象ID、
可信主体、幂等键和JSON业务载荷，不含action；返回 `ServiceResponse.payload_json` 保留现有业务字段及整数精度。
方法直接绑定独立Go用例，业务层不再接收 `Command` 或按action分发。订单使用上述强类型专属消息；
29个服务的业务载荷仍为JSON，字段校验在各自用例完成。当前30服务共162个RPC，完整清单在
`api/mall/v1/service_methods.json` 维护，新增与扩展步骤见 [独立RPC方法说明](rpc-methods.md)。

生产调用优先独立方法，只有服务端明确缺少专属服务/方法时才允许回退旧Execute；业务错误、鉴权失败、
网络错误或超时不会通过兼容入口重放。HTTP动作与白名单保留，公开HTTP路由不等于全部内部RPC。

## 请求、响应和认证

```json
{"id":"业务对象ID","idempotency_key":"本次创建请求唯一键","data":{"field":"value"}}
```

```json
{"data":{"业务响应字段":"值"}}
```

所有响应通过 `x-trace-id` 标头提供32位十六进制链路编号，JSON返回体不重复该字段。浏览器跨域读取时由 `Access-Control-Expose-Headers` 暴露该标头。

普通用户请求携带 `Authorization: Bearer <token>`。不要传入 user_id/role；即使传入，网关也会清除，并用IAM验证结果重建身份。管理员演示操作需要真正的管理员会话，不能在JSON里声称自己是admin。内部资源预占、确认、订单裁决和资金退款创建不向HTTP客户端开放。

`Idempotency-Key` 请求头优先于请求体。建议登录后为每次订单提交、售后、秒杀请求使用稳定键；超时查询/重试保持相同键。

## 页面聚合读取

首页按展示依赖拆成固定的 BFF 接口，均使用 POST 和空命令 `{"data":{}}`，不接受浏览器提交服务、动作或任意读取计划：

- `/api/v1/storefront/shell`：匿名读取 `content/get` 的 `storefront-main` 装修文档与 `storefront-categories` 分类文档。尚未装修时，主文档的404片段标记 `optional:true`，前端使用默认装修且本轮不重复查询；分类配置读取失败仍显示错误。
- `/api/v1/storefront/products`：匿名读取前100件公开商品，随后汇总默认和规格SKU，以每组最多500个SKU读取真实库存。商品列表与库存作为同一展示分区返回，避免逐商品读取。
- `/api/v1/storefront/activities`：读取普通活动、秒杀、拼团及第1页2张券预览，并按每组最多500个SKU补齐秒杀、拼团的商品和库存。可选 Bearer 仅用于本人的券领取资格，公开活动使用匿名上下文。
- `/api/v1/storefront/account_summary`：要求 Bearer，只返回本人首页与页头需要的资料投影、积分、可用券数、收藏数/收藏SKU标记、购物车数量及通知未读数。响应为 `{summary,errors}`，不包含完整券包、订单、地址、售后、发票或工单；某个依赖失败时，对应字段为 `null` 并单独记录错误，其他字段继续返回。

`POST /api/v1/storefront/bootstrap` 仅保留为 `shell` 的兼容别名，不再返回全部业务数据。`/api/v1/storefront/batch` 已退出HTTP白名单，任何身份请求均返回403且不执行子RPC。商品和库存的同类批量查询继续保留。

前三个分区返回固定命令结果，示例：

```json
{"data":{"results":[
  {"key":"products","service":"catalog","action":"list","command":{"data":{"limit":100}},"auth":false,"data":{"items":[]}},
  {"key":"product-stock-0","service":"inventory","action":"batch_get","command":{"data":{"ids":["sku-coffee"]}},"auth":false,"data":{"items":[{"sku_id":"sku-coffee","available":50}],"missing_ids":[]}}
]}}
```

每个片段包含 `data` 或 `error:{code,message,status}`。固定计划中的某项读取失败不阻止其他项返回；子调用沿用外层Trace、超时和取消信号。客户端只复用同一轮启动的分区快照，之后恢复对应业务接口的正常读取；相同作用域下完全相同的在途请求共享Promise，无关业务保持各自的接口地址。

公共装修和商品分区与会话恢复并行加载，不等待会员摘要。推荐通过 `recommendation/list` 独立加载；商品详情、会员各标签、购物车、结算及支付只读取当前视图需要的资源。购物车件数、收藏数与通知未读数使用列表接口的独立 `quantity_total`/`total`/`unread_total` 精确计数，SKU标记和通知明细仍保持有界；翻页或筛选结果不会覆盖全账户计数。完整私有列表不能因收到摘要就标为已加载。摘要在当前账号和接口作用域内短时复用，相关业务写入后更新对应计数字段；浏览、分析记录不会重新拉取全部账户数据。

左侧“最近感兴趣”通过 `POST /api/v1/storefront/recent` 独立读取，请求固定为 `{"data":{}}`，可选 Bearer；不接受商品、用户、画像或数量参数。响应 `data` 为 `{items,inventory_items,personalized,degraded,limit:6,count}`：`items` 是最多6个不同的、可展示且有实际可用库存的父商品，保留完整公开商品字段，并增加 `recent_source=history|favorite|recommendation|featured`；推荐商品可包含推荐理由。`inventory_items` 与商品一一对应，提供其默认SKU真实库存。优先展示本人的浏览和收藏，缺额按包含浏览、收藏、搜索、对比、购买偏好的推荐排序补齐；无行为或游客使用真实精选商品。禁用、隐藏、缺失、无名称、无有效价格或无可用库存的商品跳过，多个规格只计一个父商品。首批目录不足时才继续读取目录页；库存依赖故障停止无效扫描，保留已知真实结果并标记 `degraded:true`，不伪造库存或商品补数。仅在真实可展示可售商品不足时正常少于6条；关键目录失败返回错误，不伪装为空列表。浏览或收藏变化后只刷新此接口，收起、展开及普通渲染不读取接口。

可直接调用 `/api/v1/catalog/batch_get` 或 `/api/v1/inventory/batch_get`，请求为 `{"data":{"ids":["sku-coffee","sku-watch"]}}`，一次接受1至500个 SKU，响应为 `{"items":[...],"missing_ids":[...]}`。返回顺序保留首次出现的 SKU 顺序；普通目录读取将停用商品视为缺失，管理员沿用单项读取权限。写操作、保存报价、支付及订单实时状态查询均使用独立业务接口；批量读取不替代提交时的权威校验。

## 登录与退出

```sh
curl -s http://localhost:8080/api/v1/iam/login \
  -H 'Content-Type: application/json' \
  --data '{"data":{"username":"demo","password":"Lumo@2026"}}'

curl -s http://localhost:8080/api/v1/iam/logout \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' --data '{}'
```

登录返回 token,user_id,role。退出会撤销服务器会话，不能仅靠浏览器删除token。

## 首页分类导航配置

匿名调用 `POST /api/v1/content/get`，请求 `{"id":"storefront-categories"}`，返回的 `data.body` 为版本1的分类配置JSON。首页通过 `storefront/shell` 读取并复用该文档；展开面板不读取接口。

管理员调用 `POST /api/v1/content/upsert`，固定 `id=storefront-categories`、`data.kind=storefront`，提交标题和JSON正文。配置包含大类 `id/category/label/title/icon/enabled`，快捷入口 `shortcuts[{label,query}]`、分组 `rows[{title,query,items:[{label,query,featured}]}]`、品牌 `brands[{label,brand}]`；数组顺序即展示顺序。完整结构及边界见 [内容服务说明](../services/content/README.md)。管理端“分类导航”提供可视化编辑。首次迁移初始化，重复启动不会覆盖已保存配置。

## 营销弹窗

营销弹窗由内容服务独立保存配置及展示记录。管理员使用 `content/popup_list` 读取活动及全局设置，`content/popup_upsert` 保存活动（更新必须携带当前 `version`，并发编辑冲突返回409），`content/popup_configure` 保存全局 `enabled/max_per_session/min_gap_seconds`。管理页面为 `/admin.html` 的“营销弹窗”。完整规则字段及持久化语义见 [内容服务说明](../services/content/README.md)。

首页公共内容就绪后，独立调用 `POST /api/v1/storefront/popup`，例如 `{"data":{"placement":"home"}}`。进入后续页面按 `home/category/product/coupons/none` 重新匹配；不匹配时返回 `popup:null`。用户身份、人群画像及注册时间来自服务端验证的会话与业务数据，不接收浏览器提供的画像。登录、结算、支付和管理页使用 `none`，不展示营销内容；可选营销服务异常不会阻断公共商城内容渲染。弹窗不再附带在任何首页分区响应中。

返回的弹窗包含 `delivery_id/campaign_id/content/coupon/expires_at`；内容不含用户名单或完整人群规则。前端在页面可见、其他对话框关闭、配置延迟结束后，以 `storefront/popup_event` 提交 `delivery_id/view_id/event=impression`，仅在 `allowed:true` 后显示；`click/dismiss/auto_close` 沿用同一凭证且统计幂等。匹配本身不扣减展示次数，不会领取优惠券。

登录用户通过 `POST storefront/popup_claim`，提交 `{"data":{"delivery_id":"返回的投放编号","view_id":"已确认展示的视图编号"}}` 领取已配置的优惠券。服务端校验本人投放凭证并调用权威 `coupon/claim`，返回 `coupon/already_claimed`，不能通过请求覆盖券模板。内部 `content/popup_decide|popup_event|popup_claim`、`coupon/templates_by_ids` 不向浏览器或通用只读聚合开放。公开决策、展示事件及领取入口全部仅接受POST；领取要求Bearer，匿名Cookie不构成登录。

访客频控需要同源 `HttpOnly/SameSite=Lax` 访客与会话Cookie；HTTPS下附加Secure。API客户端仅发送同源Cookie，跨域不开放凭据。独立部署前端时请用同源反向代理转发 `/api`；仅配置跨域Bearer访问不足以保留访客弹窗身份，无法确认曝光时前端不会弹出。

## 主要动作表

| 服务 | 用户/公开动作 | 管理员动作 |
| --- | --- | --- |
| storefront | shell/products/activities；bootstrap（shell兼容别名）；account_summary（登录）；recent（可选登录，最多6个真实不同父商品）；popup/popup_event；popup_claim（登录） | 无通用batch入口 |
| catalog | list/get/batch_get | upsert |
| search/recommendation/content | query/list/list | content.upsert/popup_list/popup_upsert/popup_configure |
| member/address | get/update；list/create/update/delete/default | 按服务业务权限 |
| cart | list/add/remove/clear/merge | 无 |
| engagement | list/put/delete（favorite/compare/history） | 无 |
| media | create/get/complete/download上传与授权下载 | 无 |
| interaction | list/create/like | moderate |
| analytics | record | 无 |
| pricing/checkout | quote/get；quote/submit/cancel | 无 |
| order/payment | list/get/complete；create/get/submit_demo（仅演示模式、本人支付单） | payment.confirm_demo |
| inventory/campaign | get/batch_get；list/evaluate | inventory.adjust |
| coupon/points | list/templates/templates_snapshot/claim；get/checkin | 无 |
| group/flash | list/get/create；list/request/get | 按活动业务政策 |
| fulfillment/logistics | get；get/quote | list/dispatch/cancel/inspect_return；deliver |
| aftersale | list/get/create/return | approve/reject/inspect/refund/exchange |
| refund/invoice | get；list/get/create | confirm_demo；issue_demo/adjust_demo |
| reconciliation | 无 | list/run |
| notification/helpdesk | list/read/read_all；list/get/create/reply/close | 受工单服务权限约束 |

此表描述网关允许的业务入口。字段精确定义、状态前提及生产适配边界请结合各服务 transport/usecase 和 `DEVELOPMENT-CONTRACT.md`，不可将HTTP动作允许理解成绕过业务状态校验。

## 商品品类与结构化搜索

`POST /api/v1/search/query` 可传 `query`（最多120字）、`category`、`brand`（各最多80字）、`sort`、`in_stock`、`limit` 和 `offset`。类目、品牌与关键词是联合约束；关键词在商品名称、品牌和类目中匹配，`%`、`_` 等字符按字面处理。支持 `price_asc`、`price_desc`、`sales` 和 `newest` 排序。

搜索先在完整目录应用条件和排序，再读取最多100个可见、启用的父商品；库存仅做一次批量查询。响应的 `items` 为完整商品，`inventory_items` 为实际返回商品的实时库存，`candidate_limit:100` 表示本次有界候选窗口，`total` 是窗口内经库存筛选后的数量，并非全站精确匹配数。`in_stock` 在权威库存读取后过滤，不按浏览器缓存判定。

首页细分入口复用该接口，按一级类目与关键词或品牌查询；悬停展开类目面板不请求接口。内部 `catalog/search_products` 仅允许系统调用，不提供HTTP入口；MySQL按派生自JSON的只读类目列建立索引，以支持已有大目录和无商品的新类目，现有商品分类与载荷不变。

MySQL关键词搜索使用随商品行自动更新的只读覆盖索引，先筛选有界商品ID，再读取完整载荷，避免在大目录中逐行解析JSON。超长字段仍按原JSON值精确筛选，不因索引长度而遗漏；价格、销量及时间排序保持原有规则。迁移使用在线DDL，搜索保留2秒截止；SQLite保持等价的JSON查询行为。演示模式提供覆盖默认20品类、全部586细分类的实际商品和SKU，搜索结果继续包含权威库存。

## 推荐场景与个性化排序

`POST /api/v1/recommendation/list` 支持三种场景，`limit` 默认为4、范围1至20：

```json
{"data":{"scene":"home","limit":20}}
{"data":{"scene":"similar","product_id":"sku-coffee","limit":8}}
{"data":{"scene":"post_purchase","order_id":"本人订单ID","limit":20}}
```

不传场景默认为首页；旧请求只传 `order_id` 时使用购后推荐。相似场景必须提供商品，购后场景必须登录且校验订单归属。

策略 `hybrid_content_v2` 使用本人收藏、浏览、对比、搜索及已付款订单。基础权重分别为8、3、5、4、12；商品行为按30天半衰期衰减，搜索按14天衰减。商品相似度结合类目、品牌、价格接近度及名称双字相似度，并加入适量销量、新品分和类目/品牌多样性。匿名或低行为用户使用多样化精选；当前为可解释的内容与行为混合排序，`personalized_model:false`，未训练机器学习模型。

候选为公开目录100项及按偏好类目、品牌、搜索词召回的最多200项。内部召回最多3条并发SQL，每条400ms截止，保留成功通道；商品元数据、父商品及库存使用批量读取。下架、无库存、相似场景自身及已购商品排除。行为或部分召回失败时保留可靠结果并标记 `degraded:true`；权威库存失败不会编造可售状态。

响应包含完整 `items`，每项附 `recommendation_reason`、`recommendation_reasons` 及 `recommendation_score`；顶层提供 `scene`、`strategy`、`personalized`、`signal_count`、`degraded` 和实际推荐商品的实时 `inventory_items`。客户端传入的用户ID、画像、目录或库存不参与公开推荐；画像由已验证身份读取，管理员也仅使用本人订单。内部 `rank_bootstrap` 和 `catalog/recommendation_candidates` 仅允许服务间系统调用，不在HTTP白名单中。

## 普通交易示例

报价：

```json
{"data":{"items":[{"sku_id":"sku-coffee","quantity":1}],"points":0}}
```

发送到 `/api/v1/pricing/quote`，取响应 `data.quote_id`。金额为整数分。提交订单：

```json
{"idempotency_key":"order-request-001","data":{"quote_id":"<quote_id>","address":{"recipient":"Demo","phone":"00000000000","address":"Demo street","city":"Shenzhen"}}}
```

发送到 `/api/v1/checkout/submit`，响应提供 order_id。然后发送 `{"id":"<order_id>","data":{}}` 到 `/api/v1/payment/create`，返回 `PENDING` 支付单。实际支付渠道未接入；在演示模式下，支付单所属用户可向 `/api/v1/payment/submit_demo` POST `{"id":"<payment_id>"}` 提交模拟成功回执，无须管理员代操作。

`submit_demo` 由网关及支付服务共同校验演示模式、登录用户、支付单归属和demo渠道，并验证订单仍待支付、未过期、金额匹配且支付门禁未冻结。金额、回执及支付时间由服务端生成；成功返回 `SUCCEEDED` 支付单并写入唯一 `PaymentSucceeded` 事件。重复提交返回同一回执，不重复扣减资源。随后通过订单接纳、资源确认、购物车消费和履约事件更新状态；前端查询到订单接纳支付后才展示成功。管理员原有 `payment/confirm_demo` 入口保留，生产渠道尚未接入，模拟流程不会产生真实扣款。

库存、券和积分只由各自服务提交/释放。浏览器无法调用 inventory.reserve、coupon.confirm、order.accept_payment、order.ready、refund.create 等内部动作。

## 商品活动类型

商城支持秒杀、拼团、换购，以及四类普通促销。演示配置新增：

- `reduction-demo` / `reduction`：满额立减，商品满300元减30元。
- `discount-demo` / `discount`：折扣特惠，商品享9折，`pay_rate_bps=9000`。
- `quantity-discount-demo` / `quantity_discount`：多件优惠，商品满2件享8.5折，`min_quantity=2`、`pay_rate_bps=8500`。
- `direct-demo` / `direct`：限量直降，每单立减20元，商品金额须大于20元。

公开的 `campaign/list` 提供名称、说明、金额门槛、最低件数、支付比例、活动名额及占用情况。`campaign/evaluate` 接受 `activity_id`、`subtotal_cents` 和 `quantity` 预览资格，返回 `eligible`、`reason` 及整数分优惠金额；预览不会预占名额。

实际报价仍发送到 `pricing/quote`，例如：

```json
{"data":{"items":[{"sku_id":"sku-watch","quantity":2}],"activity_id":"quantity-discount-demo","points":0}}
```

服务端重新读取SKU价格、累计真实件数并计算优惠，折扣优惠额按整数分向下取整；固定优惠不能令应付金额归零。报价保存 `activity_kind`、`activity_name`、`pay_rate_bps`、`min_quantity` 等快照，订单提交时再次校验活动与商品。浏览器只传活动ID，不能指定折扣或优惠金额。

每笔订单只能选择一种活动；四类普通促销不能与秒杀、拼团或换购叠加，正常优惠券及积分仍受各自门槛和余额限制。新示例活动各1000个名额，沿用每人每活动一次的规则；未付款取消释放名额，支付成功确认使用，退款不自动恢复已使用活动资格。旧活动配置和已有订单保持原有规则。

## 错误

HTTP 400 参数错误，401 未登录/会话失效，403 权限或未公开动作，404 对象不存在，409 状态或幂等冲突，429 资源/准入限制，503 依赖不可用，504 RPC超时。响应 `error.code` 为gRPC状态码，响应头 `x-trace-id` 用于定位调用树；成功和失败的JSON返回体都不附加 `trace_id`。

## 领券与文件上传

首页提供领券预览和 `#/coupons` 领券中心入口。`/api/v1/coupon/templates` 支持匿名浏览；携带登录会话时返回本人的领取状态。请求 `data.page`、`data.page_size` 分页（每页最多100张），响应包含 `items`、`total`、`page`、`page_size`、`has_more` 和 `server_time`。首页只读取第1页2张预览，不遍历整个领取池。

完整领券中心使用只读 `/api/v1/coupon/templates_snapshot`，同样传入 `data.page/page_size`，每页最多5000条，响应字段和资格状态与 `templates` 一致；超过5000条时按 `has_more` 继续取下一页。该接口一次计算当前用户的资格上下文，供页面全局筛选所有已启用的券，包括未开始、已结束、领完和条件未满足的券；普通 `templates` 的100条上限保持不变。快照不预占名额，实际领取仍由服务端重新裁决。

每张券的 `claim_status` 为 `available`、`login_required`、`upcoming`、`ended`、`exhausted`、`claimed`、`conditions_unmet` 或 `unavailable`，配合 `claimable` 与 `claim_reason` 显示按钮和原因。`starts_at` / `expires_at` 是领取窗口；`valid_days` 是领取后的使用有效天数；`min_cents` 是订单使用门槛，区别于 `min_points`（可用积分门槛，不扣积分）和 `min_growth`（会员成长值门槛）。

登录后向 `/api/v1/coupon/claim` 发送 `{"data":{"template_id":"welcome-100"}}`。服务端查询积分及会员服务验证本人资格，在事务内校验时间并扣减名额；浏览器提交的积分、成长值或身份不能绕过校验。同一用户同一模板只能领取一次，重复请求返回原优惠券且不再扣名额。领取后通过 `coupon/list` 查询，在原有报价、下单、支付与退款链路使用。券包条目包含服务端关联查询得到的 `template_name`，不依赖公开模板的当前分页或启用状态。

文件上传：登录后向 `/api/v1/media/create` 发送 `{"data":{"file_name":"receipt.txt","content_type":"text/plain","size":5}}`。响应提供私有对象登记和短时上传授权；按返回的 `upload_method`、`upload_url`、`upload_fields` 上传真实文件（POST policy采用multipart表单）。完成后用文件ID调用 `/api/v1/media/complete`，服务端StatObject核验大小/类型。再调用 `/api/v1/media/download` 获取短时授权下载地址，其他账号不能访问。
