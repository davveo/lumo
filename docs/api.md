# HTTP 与 RPC 接口

公网仅通过 HTTP Gateway 进入；服务之间使用 protobuf/gRPC。统一路由为 `POST /api/v1/{service}/{action}`，只读动作支持 `GET`，例如 `GET /api/v1/catalog/get?id=sku-coffee`。网关设置动作白名单，不提供任意内部RPC透传。

## 领域 RPC 契约

订单使用 `api/mall/v1/order.proto` 的独立 `OrderService`：`ListOrders`、`GetOrder`、`CreateOrder`、`MarkPending`、`MarkCreateFailed`、`AcceptPayment`、`MarkReady`、`WaitForGroup`、`BeginClose`、`CompleteClose`、`MarkGroupFormed`、`MarkGroupFailed`、`MarkShipped`、`CompleteOrder`、`RecordRefund`。每个方法使用明确的protobuf请求/响应，金额为int64、时间为Timestamp；报价/地址是校验后保留的历史JSON快照，避免经过Struct丢失整数精度。

新方法与旧 `DomainService.Execute` 由同一内部凭证保护，并调用相同的独立订单用例。`list/get/complete` 沿用本人及管理员权限，其余写入仅内部角色使用；HTTP仍只开放订单 `list/get/complete`。旧调用兼容映射到新RPC；不会通过新增方法将内部订单裁决暴露给浏览器。具体分层和扩展方式见 `services/order/README.md`。

其余29个领域服务也均使用独立的protobuf服务与方法，例如 `mall.v1.CheckoutService/Submit`、
`mall.v1.InventoryService/Reserve`、`mall.v1.CatalogService/BatchGet`。请求类型 `ServiceRequest` 只有对象ID、
可信主体、幂等键和JSON业务载荷，不含action；返回 `ServiceResponse.payload_json` 保留现有业务字段及整数精度。
方法直接绑定独立Go用例，业务层不再接收 `Command` 或按action分发。订单使用上述强类型专属消息；
29个服务的业务载荷仍为JSON，字段校验在各自用例完成。当前30服务共242个领域RPC，另有2个每服务共用的事件管理RPC，完整清单在
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
| catalog | list/get/batch_get | admin_list/upsert |
| search/recommendation/content | query/list/list | 各自policy_get/policy_upsert；content.admin_list/admin_upsert/upsert/popup_list/popup_upsert/popup_configure |
| member/address | get/update；list/create/update/delete/default | 按服务业务权限 |
| cart | list/add/remove/clear/merge | 无 |
| engagement | list/put/delete（favorite/compare/history） | 无 |
| media | create/get/complete/download上传与授权下载 | 无 |
| interaction | list/create/like | moderate |
| analytics | record | 无 |
| pricing/checkout | quote/get；quote/submit/cancel | 无 |
| order/payment | list/get/complete；create/get/submit_demo（仅演示模式、本人支付单） | payment.confirm_demo |
| inventory/campaign | get/batch_get；list/evaluate | inventory.admin_list/adjust/adjustments/threshold/ledger；campaign.admin_list/upsert/rule_upsert |
| coupon/points | list/templates/templates_snapshot/claim；get/checkin/policy_get | coupon.templates_admin/template_upsert；points.admin_get/adjust/ledger/policy_upsert |
| group/flash | list/get/create/rules；list/request/get/session_get | group.admin_list/rules_admin/rule_upsert；flash.admin_list/upsert/requests_admin |
| fulfillment/logistics | get；get/quote | list/dispatch/cancel/inspect_return；deliver |
| aftersale | list/get/create/return | approve/reject/inspect/refund/exchange |
| refund/invoice | get；list/get/create | confirm_demo；issue_demo/adjust_demo |
| reconciliation | 无 | list/run/admin_list/get/note |
| notification/helpdesk | list/read/read_all；list/get/create/reply/close | notification.templates_admin/template_upsert；工单按服务业务权限 |

此表描述网关允许的业务入口。字段精确定义、状态前提及生产适配边界请结合各服务 transport/usecase 和 `DEVELOPMENT-CONTRACT.md`，不可将HTTP动作允许理解成绕过业务状态校验。

## 商品品类与结构化搜索

`POST /api/v1/search/query` 可传 `query`（最多120字）、`category`、`brand`（各最多80字）、`sort`、`in_stock`、`limit` 和 `offset`。类目、品牌与关键词是联合约束；关键词在商品名称、品牌和类目中匹配，`%`、`_` 等字符按字面处理。支持 `price_asc`、`price_desc`、`sales` 和 `newest` 排序。

搜索先在完整目录应用条件和排序，再读取最多100个可见、启用的父商品；库存仅做一次批量查询。响应的 `items` 为完整商品，`inventory_items` 为实际返回商品的实时库存，`candidate_limit`（默认100，可配置1至100）表示本次有界候选窗口，`total` 是窗口内经库存筛选后的数量，并非全站精确匹配数。`in_stock` 在权威库存读取后过滤，不按浏览器缓存判定。

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


## 后台运营配置

以下管理读写均要求真实管理员会话；`user_id`、`role` 由网关认证，伪造请求字段无效。新增独立RPC为 `CouponService/TemplatesAdmin`、`CouponService/TemplateUpsert`、`CampaignService/AdminList`、`CampaignService/Upsert`，普通顾客访问管理列表或写入均403。读请求可GET/POST，写入只接受POST。

- `coupon/templates_admin`：`data.page/page_size`（默认1/10，最多100）、`keyword`、`active`（空/字符串true/false），返回含停用模板的 `items/total/page/page_size/has_more/server_time`；条目增加 `version/claimed_count`，领取次数按本页模板批量计数。
- `coupon/template_upsert`：请求 `id`，`data` 中包含 `name/amount_cents/min_cents/valid_days/active/starts_at/expires_at/min_points/min_growth/version`。创建版本0并提交 `remaining` 初始配额；编辑必须提交实际版本及 `stock_delta` 增减量，不能回写旧的剩余配额。旧版本冲突拒绝；出现领取记录后面额、使用门槛、有效天数锁定，复制新模板才能改变权益。每人每种券固定限领1张；停用仅停止新领取，已领券快照继续生效。积分/成长值是领取资格，不在领取时扣除。
- `campaign/admin_list`：`data.page/page_size/keyword/kind/active`，active为可选布尔值，返回含停用和系统规则的分页列表及total。
- `campaign/upsert`：`id`及 `data.kind/name/description/threshold_cents/discount_cents/pay_rate_bps/min_quantity/quota/active/version`；支持 `reduction/discount/quantity_discount/direct` 四类运营规则。创建版本0，编辑携带当前版本；优惠金额以分计、支付比例用bps，quota不低于held+used。已有预占或使用后冻结优惠计算字段，允许名称、说明、额度及启停调整。held/used由交易流程维护，不能被运营请求覆盖。

秒杀场次、拼团和换购目前与指定SKU、交易准入及补偿流程关联，后台只读展示，不提供通用创建按钮。库存预占/核销、积分奖励、会员等级、MQ补偿等内部命令不作为运营配置开放。

商品后台复用 `catalog/list/get/upsert`；编辑与上下架先读取完整商品，保留规格、差价及展示元数据，不修改库存。新单规格商品默认下架，可进入库存管理单独设置库存。商城装修使用 `content/get/upsert` 的 `storefront-main` 文档，保存前读最新JSON并只合并管理员修改的字段。公告/FAQ使用 `content/list/get/upsert`；当前内容服务仅发布，不冒充提供删除或停用。

### 工作台、交易与用户管理

`management/dashboard` 是仅供后台工作台的只读聚合，`data.days` 只接受7或30。并行调用订单、支付、履约、售后、退款、发票和IAM各自的 `AdminSummary`，返回 `order/payment/fulfillment/aftersale/refund/invoice/users`，每项包括全量 `total/states` 和指定天数的 `daily`。每日键为北京时间 `YYYY-MM-DD`；金额趋势按单据创建日期汇总成功金额，不代表渠道结算日期。服务失败时用 `errors` 和 `degraded` 明确标记，前端显示不可用。该接口不返回业务列表或商城配置。

上述七个服务新增独立 `AdminList` 和 `AdminSummary` RPC。HTTP的 `admin_list` 接受 `data.page/page_size/keyword/status/from/to`（默认1/20，最多100；日期含边界，业务时区UTC+8），返回精确 `total` 与本页 `items`。订单继续采用强类型protobuf请求和响应。各领域只查询自己的schema，关联订单详情通过独立订单RPC按需读取。业务列表与工作台均要求管理员，普通账号伪造role仍403。

`iam/admin_list` 的 `data.status` 按账号角色筛选，每行返回active状态，不返回密码摘要、令牌或会话。`iam/set_active` 为POST：`id`指定普通账号，`data.active/expected_active/reason` 指定新状态、读取到的原状态和原因；并发状态冲突返回409，不能操作自身或管理员。停用账号与撤销其全部会话在同一事务完成，启用后必须重新登录；审计记录保存操作者、原因和目标状态。

`inventory/adjust` 的 `id` 是实际SKU，`data.on_hand` 是盘点后的绝对在库数。新后台提交 `expected_on_hand/reason` 防止旧页面覆盖新库存，并禁止降低到预占加已分配数量以下。`inventory/adjustments` 读取指定SKU最近20条管理员调整，含before/after、actor_id、reason和created_at。目录与规格来自商品服务，库存一次 `batch_get`，不按规格逐个请求；名称搜索覆盖上架商品，未上架商品可按完整商品或SKU编号定位。

### 多类型优惠券

`template_upsert` 在既有字段上新增 `coupon_type/discount_bps/max_discount_cents/scope_type/scope_values`：

- `reduction`：满减，`amount_cents` 和 `min_cents` 均为正数。
- `cash`：无门槛代金券，`amount_cents` 为正数、`min_cents=0`。
- `discount`：无门槛折扣券，`amount_cents/min_cents=0`，支付比例 `discount_bps=1..9999`，例如8500为8.5折。
- `threshold_discount`：满额折扣券，`min_cents>0`，同样通过bps配置比例。

折扣券可设置 `max_discount_cents` 封顶（0表示不限额）；固定金额券不能携带折扣比例或封顶。`scope_type` 为all/category/product；all的scope_values为空，category按实际一级类目、product按父商品ID匹配，最多50个去重值。门槛及优惠仅作用于适用主商品金额，排除换购。客户端提交的品类/父商品信息不会被信任，由报价服务查询商品后覆盖。

领取时将完整权益复制到券实例；已领取模板的类型、比例、封顶、适用范围与金额/门槛/有效期同样冻结。旧券空类型按原门槛推导满减或代金券、空范围视为全场。报价和预占共同使用整数分规则，预占再计算并与订单报价核对，不只改变券面文案。

### 装修与弹窗内容

`content/upsert` 的 `storefront-main` 文档支持品牌及主题、主视觉文案/图片、页头标语、搜索提示、主视觉按钮和商城内链接；`heroSlides` 最多4张附加轮播（加主视觉共5张），每张包含title/subtitle/eyebrow/image/button/link，`heroInterval` 为0或3–30秒。`showCategories/showPicks/showMember/showCoupons/showActivities/showRecommendations` 为布尔模块开关。另支持announcement/announcementLink、footerText/contactPhone/contactHours。字段类型、长度、图片和链接由后端校验，保存保留其它编辑器拥有的配置。未建立文档时显示明确默认值，保存后发布；不自动覆盖实际商城配置。

营销内容的 `content.layout` 支持coupon/announcement/image/welcome/member/campaign六类，`content.image_url/badge` 配置海报与角标。后台预设只辅助填充，可继续配置原有主题、人群、页面、优先级、时间和频控；模板类型不改变后端投放判定。图片使用安全商城素材或无凭据HTTPS地址，image布局要求图片。按钮只允许白名单商城内页面，会员类可指向会员/积分/成长页面，拒绝脚本或任意外链。


## 后台扩展管理接口

新增接口均通过原网关认证和归属服务校验。除 `flash/session_get`、`group/rules` 公开读取及登录用户 `points/policy_get`、`member/policy_get` 外，以下新能力均为管理员专用；不得将管理身份放入请求体冒充权限。

- `flash/admin_list|upsert|requests_admin`；`group/admin_list|rules_admin|rule_upsert`；`campaign/rule_upsert`：场次、成团规则、专用配额及请求/团历史。场次与成团规则写入携带当前`version`（新建0）和`reason`；时间为RFC3339，配额计数由资源流程管理。
- `catalog/admin_list`：`keyword/category/status/page/page_size`，全目录精确分页并可读停用商品。`inventory/admin_list`：`keyword`匹配SKU，`status=low|out|healthy`；`threshold`需`threshold/expected_threshold/reason`；`ledger`可用ID指定SKU，`adjustments`传`page/page_size`读取完整历史，不传页码兼容近期20条。
- `points/admin_get|ledger`：ID为普通会员账号；`points/adjust`需稳定`idempotency_key`及`amount/expected_available/reason`，允许每次正负100万以内的非零积分，不得扣减冻结积分或使可用余额负数。`member/update`以ID指定其他会员时要求管理员及原因，支持受控昵称/头像编辑。
- `points|member|logistics|recommendation|search` 的 `policy_get|policy_upsert`：读返回`{config,version}`；写`data={config,version,reason}`。积分字段为`checkin_points/deduction_bps/reward_per_yuan`；会员为`gold_growth/growth_spend_cents`。版本冲突返回409。
- 运费配置：`name/enabled/mode=flat|per_item/base_fee_cents/additional_fee_cents/free_threshold_cents/allowed_regions/blocked_regions/regions`；地区费用项为`{region,base_fee_cents,additional_fee_cents,free_threshold_cents}`，先匹配生效。`pricing/quote`及`checkout/quote`接受`address_id`或地区地址，响应含`shipping_cents/shipping_policy/shipping_region`；提交沿用报价版本并验证目的地。
- `notification/templates_admin|template_upsert`：ID为支持的业务事件，写`title/body/enabled/version/reason`，仅支持`{{business_id}}/{{event_type}}/{{amount}}`。`content/admin_list|admin_upsert`：公告与FAQ，`status=DRAFT|PUBLISHED|SCHEDULED|PAUSED`，支持`publish_at/unpublish_at`，版本及原因必填；`storefront-main`与`storefront-categories`仍走专门配置验证。
- `reconciliation/run`新异步方式：稳定幂等键，`data={async:true,from:"YYYY-MM-DD",to:"YYYY-MM-DD",reason:"任务说明"}`，范围最长366天。`admin_list`分页查任务，`get`以ID查进度及分页`differences`，`note`需稳定幂等键及`data.body`；失败任务以原ID、稳定新键和`data={retry:true,reason}`继续检查点。按UTC+8日期核对内部支付与订单。
- `iam/audit_list`：分页查询账号启停审计，ID可限定目标账号。
- `management/tasks`：`data={service,kind:"outbox|inbox|recovery|audit",page,page_size,keyword,status}`；service只能为注册的30个服务。`management/retry`：原事件ID、稳定幂等键和`data={service,kind:"outbox|inbox",reason}`。共用`PlatformAdminService.Tasks/Retry`在归属服务自己的数据库查询和排队，不跨库直接修改业务状态；完成或有效租约中的消息不可重放，历史无信封Inbox只可查看。

新管理列表返回`items/total/page/page_size`（通知模板为11项有界列表）。配置审计和原订单快照持久保存。推荐`config`接受9类`*_weight`、`pinned_ids`和`excluded_ids`，搜索接受`default_sort/max_results/excluded_ids/hidden_brands`，配置真实参与后续查询。详见 [后台能力与验证范围](frontend-api-coverage.md#后台管理扩展复查2026-10-28)。

## 本轮新增接口

- `coupon/calculate`（会员）：`data.items`、`coupon_mode: auto|manual|none`、手动模式 `coupon_id`、`points_mode: max`、配送地址与已有活动字段。返回不可变价格快照、`coupon_options`、所选券号、优惠明细和 `quote_id`；后续 checkout/submit 只提交报价号。
- `payment/methods`（公开）返回可执行的启用渠道；`payment/policy_get`、`policy_upsert`（管理员）读取与保存开关。保存载荷为 `data:{config,version,reason}`。
- `iam/login_methods`、`otp_send`、`otp_login`、`oauth_start`、`oauth_callback`（公开）；`iam/policy_get`、`policy_upsert`（管理员）。手机与授权载荷、凭据部署见 [登录渠道](login-providers.md)。
- `engagement/policy_get`（公开）仅返回提醒规则；`policy_upsert`（管理员）载荷同上，配置含 `price_enabled,stock_enabled,ttl_days,max_per_user`。
- `engagement/subscribe`（会员）：`data:{sku_id,kind:price|stock,target_cents}`；stock 不使用目标金额。重复有效订阅返回同一记录，改变目标需先取消。
- `engagement/subscriptions`（会员）和 `admin_list`（管理员）：`data:{limit,offset,kind,status,sku_id}`；管理员可另筛选 `user_id`，会员始终只读本人。每页 1..100，返回 items/total/limit/offset。
- `engagement/unsubscribe`（会员）传 `id`；`admin_cancel`（管理员）另传 `data.reason`，写入停用审计。

新业务号见 [雪花编号规范](business-identifiers.md)。所有新增方法均在服务自身校验权限和归属，HTTP白名单不会开放内部资金或状态命令。

## 管理可读性与渠道参数（2026-10-29）

`analytics/admin_list` 和 `helpdesk/admin_list` 仅管理员可用，均支持 page/page_size、keyword、from/to；返回精确 total。行为记录另支持 event_type（view/click/impression/search/OrderCompleted/RefundSucceeded）及 status=behavior|purchase_event，返回 action_label、product_name、summary、record_category；商品名批量补齐，缺失时保留原编号与已有快照。工单 status=OPEN|REPLIED|CLOSED。

`helpdesk/get` 保留原工单字段，增加 message_total/message_limit/message_offset；用 data.message_limit（1..100）和 message_offset 分页读取按时间顺序排列的回复，发起问题在 body 中。

IAM和Payment各新增管理员 `provider_get`、`provider_upsert`。Get 的 id 可省略返回所有可配置渠道；返回 fields、非敏感 values、secret_configured、独立 version、配置完整性与可用状态。Upsert 的 id 为渠道名，载荷为 `data:{values,clear_secrets,version,reason}`。密钥字段留空保留原值，清除必须明确列入 clear_secrets；启用中的登录方式不能清除必需参数。版本冲突返回409。

IAM渠道为 phone/wechat/qq，数据库参数优先于原环境变量，并用于实际验证码发送和OAuth授权/回调。Payment渠道为 wechat/alipay/card；可保存参数，真实支付适配器尚未接入时 adapter_ready=false，不会因参数保存成功而伪造已可收款。

## 账号注册与商品优惠预览（2026-10-29）

- `POST /api/v1/iam/register` 为公开的账号密码注册接口，参数 `data.username`、`data.password`。账号规范化为小写，4–32位、字母开头，密码8–72字节且含字母数字。服务端仅创建普通会员，忽略客户端身份；返回 `USR_` 雪花编号及注册时间，不返回密码或自动登录凭据。
- 注册同时受 `allow_registration` 和账号密码登录开关控制。按网关真实连接IP限20次/UTC日，IP由网关覆盖，数据库唯一索引防止并发重复账号。已有会员登录不受关闭注册影响。
- 商品详情使用已有 `coupon.calculate`，其调用计价服务的权威报价，按当前SKU、数量及账号已领券选择实际抵扣最高的一张；不会领券、锁券或预占商品。规格、数量和登录身份变化会丢弃旧请求，缓存限定账号并短时复用，结算仍重新报价。
- 异常任务支持 `status=attention` 筛选需处理记录，以及关联业务编号搜索；返回中文任务名称、业务编号、执行状态、错误分类、处理建议和重试条件，不返回消息全文、电话、凭据或连接信息。新失败记录保存错误分类与时间，历史缺失原因明确标注。处理中与已完成任务仍受后台重试保护。
