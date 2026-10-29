# 商城前端与 HTTP / RPC 接口覆盖对照

本表以 `internal/httpgateway/gateway.go` 当前白名单和 `web/dist`（原 Site `dist`）源代码为准。逐条阅读了通用调用包装、动态 action、后台 `specs` 与 `data-operation` 分发；不将字符串搜索命中等同于存在可操作页面。

下表记录业务入口及共享读取能力；路由全集以网关白名单为准。一次页面加载不必为每项逻辑读取发送单独 HTTP 请求：启动数据由 `storefront/bootstrap` 汇总，其他页面的并发只读请求由共享客户端合并为 `storefront/batch`。

“待补独立入口”表示当前专用页面尚未调用此 HTTP 路由；`/debug` 可以按权限手动联调全部白名单动作，但本表不会因此把业务页面标记为已接入。`pricing.quote` 已由 `checkout.quote` 间接调用，属于同一报价能力的入口别名。

## HTTP 路由矩阵

全部路由形式为 `POST /api/v1/{service}/{action}`，请求使用统一 Command 包装。只读动作还支持白名单允许的 GET。管理员页面使用独立服务器会话；浏览器不能自行指定 user_id/role 获得权限。

| 服务 | 动作 | 网关身份 | 页面模块 / 业务入口 | 覆盖状态 |
| --- | --- | --- | --- | --- |
| storefront | `bootstrap` | 公开，可选登录 | `api-bootstrap.js / api-client.js`：固定启动计划及商品、秒杀、购物车 SKU 库存快照；登录时包含本人账户数据 | 共享接入 |
| storefront | `batch` | 公开，子请求逐项鉴权 | `api-client.js`：会员、购物车、详情、活动、服务及管理页的并发只读请求合并 | 共享接入 |
| iam | `login` | 公开 | `auth.js / auth-views.js`：登录、重验会话、更新会话、退出及退出全部设备；管理员页独立会话 | 已接入口 |
| iam | `current` | 用户 | `auth.js / auth-views.js`：登录、重验会话、更新会话、退出及退出全部设备；管理员页独立会话 | 已接入口 |
| iam | `refresh` | 用户 | `auth.js / auth-views.js`：登录、重验会话、更新会话、退出及退出全部设备；管理员页独立会话 | 已接入口 |
| iam | `logout` | 用户 | `auth.js / auth-views.js`：登录、重验会话、更新会话、退出及退出全部设备；管理员页独立会话 | 已接入口 |
| iam | `logout_all` | 用户 | `auth.js / auth-views.js`：登录、重验会话、更新会话、退出及退出全部设备；管理员页独立会话 | 已接入口 |
| member | `get` | 用户 | `member.js`：个人资料、通知偏好 | 已接入口 |
| member | `update` | 用户 | `member.js`：个人资料、通知偏好 | 已接入口 |
| address | `list` | 用户 | `member.js`：地址列表、添加/编辑、删除、默认地址；update/create 为动态动作 | 已接入口 |
| address | `get` | 用户 | `member.js`：编辑地址前按 ID 读取最新地址，随后更新表单 | 已接入口 |
| address | `create` | 用户 | `member.js`：地址列表、添加/编辑、删除、默认地址；update/create 为动态动作 | 已接入口 |
| address | `update` | 用户 | `member.js`：地址列表、添加/编辑、删除、默认地址；update/create 为动态动作 | 已接入口 |
| address | `delete` | 用户 | `member.js`：地址列表、添加/编辑、删除、默认地址；update/create 为动态动作 | 已接入口 |
| address | `default` | 用户 | `member.js`：地址列表、添加/编辑、删除、默认地址；update/create 为动态动作 | 已接入口 |
| catalog | `list` | 公开 | `app.js / editor.js`：启动加载商品；管理员读取包含停用商品的目录 | 已接入口 |
| catalog | `get` | 公开 | `promotions.js`：秒杀指定 SKU 的真实价格/展示信息；启动快照优先，其他并发读取经共享批次合并 | 已接入口 |
| catalog | `batch_get` | 公开 | 网关将 SKU 单项读取合并；pricing/checkout 服务内部一次批量读取商品与价格 | 共享接入 |
| catalog | `upsert` | 管理员 | `editor.js`：编辑商品、上下架、应用备份 | 已接入口 |
| media | `create` | 用户 | `services.js / editor.js`：工单附件、商品/首页图片登记、真实上传校验及授权下载 | 已接入口 |
| media | `get` | 用户 | `services.js`：附件下载前查询元数据与上传状态 | 已接入口 |
| media | `complete` | 用户 | `services.js / editor.js`：工单附件、商品/首页图片登记、真实上传校验及授权下载 | 已接入口 |
| media | `download` | 用户 | `services.js / editor.js`：工单附件、商品/首页图片登记、真实上传校验及授权下载 | 已接入口 |
| interaction | `list` | 公开 | `product-extras.js / member.js / admin-app.js`：公开评价问答、本人待审记录、后台审核列表 | 已接入口 |
| interaction | `create` | 用户 | `product-extras.js / member.js`：提交问题、已完成订单提交评价 | 已接入口 |
| interaction | `like` | 用户 | `product-extras.js`：评价“有帮助” | 已接入口 |
| interaction | `moderate` | 管理员 | `admin-app.js`：data-operation 动态分发：审核及商家答复 | 已接入口 |
| interaction | `answer` | 管理员 | `admin-app.js`：data-operation 动态分发：审核及商家答复 | 已接入口 |
| engagement | `list` | 用户 | `app.js / member.js / discovery.js`：收藏、浏览记录、对比和搜索记录；动态 put/delete；clear 为清空搜索/对比 | 已接入口 |
| engagement | `put` | 用户 | `app.js / member.js / discovery.js`：收藏、浏览记录、对比和搜索记录；动态 put/delete；clear 为清空搜索/对比 | 已接入口 |
| engagement | `delete` | 用户 | `app.js / member.js / discovery.js`：收藏、浏览记录、对比和搜索记录；动态 put/delete；clear 为清空搜索/对比 | 已接入口 |
| engagement | `clear` | 用户 | `app.js / member.js / discovery.js`：收藏、浏览记录、对比和搜索记录；动态 put/delete；clear 为清空搜索/对比 | 已接入口 |
| search | `query` | 公开 | `categories.js / discovery.js`：完整目录按品类、关键词及品牌查询，响应带商品元数据与实时库存；普通首页空查询复用已加载商品 | 已接入口 |
| recommendation | `list` | 公开/可选登录，购后需本人订单 | `recommendations.js / product-extras.js`：首页 home、详情 similar + product_id、购后 post_purchase + order_id；实际理由与库存复用 | 已接入口 |
| content | `list` | 公开 | `services.js / editor.js`：帮助内容和商城页面配置 | 已接入口 |
| content | `get` | 公开 | `services.js`：点击帮助问答时读取该条内容的当前标题和正文 | 已接入口 |
| content | `upsert` | 管理员 | `editor.js / admin-app.js`：保存首页配置、发布帮助/公告 | 已接入口 |
| analytics | `record` | 用户 | `product-extras.js / discovery.js`：已登录用户商品浏览、搜索行为；幂等记录，不发送账号密码或支付凭据 | 已接入口 |
| analytics | `list` | 管理员 | `admin-app.js`：行为记录页面使用 specs 动态列表 | 已接入口 |
| cart | `list` | 用户 | `app.js`：购物车读取、加购、改数量、删除 | 已接入口 |
| cart | `add` | 用户 | `app.js`：购物车读取、加购、改数量、删除 | 已接入口 |
| cart | `set` | 用户 | `app.js`：购物车读取、加购、改数量、删除 | 已接入口 |
| cart | `remove` | 用户 | `app.js`：购物车读取、加购、改数量、删除 | 已接入口 |
| cart | `clear` | 用户 | `app.js`：确认删除全部购物车商品时清空 | 已接入口 |
| cart | `merge` | 用户 | `app.js / member.js`：再次购买通过 addItems 批量幂等合并当前商品与数量 | 已接入口 |
| pricing | `quote` | 用户 | `app.js → checkout.quote → pricing.quote`：结算页走 checkout/quote，服务内部 RPC 到 pricing/quote | 间接接入 |
| pricing | `get` | 用户 | `app.js`：提交前读取 quote_id 并核对服务端过期时间 | 已接入口 |
| checkout | `quote` | 用户 | `app.js / member.js`：服务端报价、稳定幂等提交、取消未支付订单 | 已接入口 |
| checkout | `submit` | 用户 | `app.js / member.js`：服务端报价、稳定幂等提交、取消未支付订单 | 已接入口 |
| checkout | `cancel` | 用户 | `app.js / member.js`：服务端报价、稳定幂等提交、取消未支付订单 | 已接入口 |
| order | `list` | 用户 | `app.js / member.js / admin-app.js`：订单列表、提交/支付后查询订单、用户确认收货 | 已接入口 |
| order | `get` | 用户 | `app.js / member.js / admin-app.js`：订单列表、提交/支付后查询订单、用户确认收货 | 已接入口 |
| order | `complete` | 用户 | `app.js / member.js / admin-app.js`：订单列表、提交/支付后查询订单、用户确认收货 | 已接入口 |
| campaign | `list` | 公开 | `promotions.js`：秒杀、拼团、换购、满减、折扣、多件优惠、直降活动配置 | 已接入口 |
| campaign | `evaluate` | 公开 | `promotions.js`：换购及普通促销选择前资格裁决；实际金额使用服务端报价 | 已接入口 |
| coupon | `list` | 用户 | `member.js`：我的优惠券及结算时选券 | 已接入口 |
| coupon | `templates` | 公开，可选登录 | `coupon-center.js / member.js`：首页第1页100条好券预览、领取时间与本人资格状态 | 已接入口 |
| coupon | `templates_snapshot` | 公开，可选登录 | `coupon-center.js`：完整领券中心按每页最多5000条读取模板元数据，全局筛选后分页展示 | 已接入口 |
| coupon | `claim` | 用户 | `coupon-center.js / member.js`：领取后刷新领券状态和本人券包 | 已接入口 |
| points | `get` | 用户 | `member.js`：余额/冻结/账本、每日签到 | 已接入口 |
| points | `checkin` | 用户 | `member.js`：余额/冻结/账本、每日签到 | 已接入口 |
| group | `list` | 公开 | `promotions.js`：服务端团列表、参团前查询、真实开团；人数只读后端 paid/occupied | 已接入口 |
| group | `get` | 公开 | `promotions.js`：服务端团列表、参团前查询、真实开团；人数只读后端 paid/occupied | 已接入口 |
| group | `create` | 用户 | `promotions.js`：服务端团列表、参团前查询、真实开团；人数只读后端 paid/occupied | 已接入口 |
| flash | `list` | 公开 | `promotions.js`：读取已开放场次，不虚构明日活动 | 已接入口 |
| flash | `request` | 用户 | `app.js`：提交异步秒杀请求、轮询成功订单或失败结果 | 已接入口 |
| flash | `get` | 用户 | `app.js`：提交异步秒杀请求、轮询成功订单或失败结果 | 已接入口 |
| inventory | `get` | 公开 | `inventory.js / promotions.js / admin-app.js`：SKU 可售快照、秒杀指定 SKU、管理页查询；启动快照或共享批次供给 | 已接入口 |
| inventory | `batch_get` | 公开 | `inventory.js`、`editor.js` 及网关按 SKU 批量读取；search/recommendation 服务内部批量查询可售库存 | 共享接入 |
| inventory | `adjust` | 管理员 | `editor.js / admin-app.js`：管理员调整实际在库量 | 已接入口 |
| fulfillment | `get` | 用户 | `member.js`：物流详情同时读取履约状态 | 已接入口 |
| fulfillment | `list` | 管理员 | `admin-app.js`：仓库任务列表、出库、拦截取消；动态 data-operation 分发 | 已接入口 |
| fulfillment | `dispatch` | 管理员 | `admin-app.js`：仓库任务列表、出库、拦截取消；动态 data-operation 分发 | 已接入口 |
| fulfillment | `cancel` | 管理员 | `admin-app.js`：仓库任务列表、出库、拦截取消；动态 data-operation 分发 | 已接入口 |
| fulfillment | `inspect_return` | 管理员 | `admin-app.js`：独立仓库验收凭证表单，品质、SKU 和数量由后端校验；常规售后优先走售后验收 | 已接入口 |
| logistics | `quote` | 公开 | `app.js`：结算并行查询运费/可配送性，与报价运费核对 | 已接入口 |
| logistics | `get` | 用户 | `member.js`：订单物流详情 | 已接入口 |
| logistics | `deliver` | 管理员 | `admin-app.js`：演示签收回执，随后由事件推动订单状态 | 已接入口 |
| aftersale | `list` | 用户 | `member.js`：售后列表、按行数量申请、寄回单号、撤销售后 | 已接入口 |
| aftersale | `get` | 用户 | `member.js / member-views.js`：售后详情按钮按 ID 读取最新服务单 | 已接入口 |
| aftersale | `create` | 用户 | `member.js`：售后列表、按行数量申请、寄回单号、撤销售后 | 已接入口 |
| aftersale | `return` | 用户 | `member.js`：售后列表、按行数量申请、寄回单号、撤销售后 | 已接入口 |
| aftersale | `cancel` | 用户 | `member.js`：售后列表、按行数量申请、寄回单号、撤销售后 | 已接入口 |
| aftersale | `approve` | 管理员 | `admin-app.js`：后台审核/驳回、验收退货、发起退款、创建换货履约 | 已接入口 |
| aftersale | `reject` | 管理员 | `admin-app.js`：后台审核/驳回、验收退货、发起退款、创建换货履约 | 已接入口 |
| aftersale | `inspect` | 管理员 | `admin-app.js`：后台审核/驳回、验收退货、发起退款、创建换货履约 | 已接入口 |
| aftersale | `refund` | 管理员 | `admin-app.js`：后台审核/驳回、验收退货、发起退款、创建换货履约 | 已接入口 |
| aftersale | `exchange` | 管理员 | `admin-app.js`：后台审核/驳回、验收退货、发起退款、创建换货履约 | 已接入口 |
| payment | `create` | 用户 | `app.js`：创建支付单、查询渠道及订单状态 | 已接入口 |
| payment | `get` | 用户 | `app.js`：创建支付单、查询渠道及订单状态 | 已接入口 |
| payment | `submit_demo` | 用户（仅演示模式、本人支付单） | `app.js`：模拟成功回执，等待后端订单接纳支付后展示结果 | 已接入口 |
| payment | `list` | 管理员 | `admin-app.js`：支付确认列表、演示回执确认；不向顾客提供管理员权限 | 已接入口 |
| payment | `confirm_demo` | 管理员 | `admin-app.js`：支付确认列表、演示回执确认；不向顾客提供管理员权限 | 已接入口 |
| refund | `get` | 用户 | `member.js`：售后退款进度 | 已接入口 |
| refund | `list` | 管理员 | `admin-app.js`：后台退款列表及演示退款回执 | 已接入口 |
| refund | `confirm_demo` | 管理员 | `admin-app.js`：后台退款列表及演示退款回执 | 已接入口 |
| invoice | `list` | 用户 | `services.js / admin-app.js`：申请、列表、详情/下载、原票和红冲记录 | 已接入口 |
| invoice | `get` | 用户 | `services.js / admin-app.js`：申请、列表、详情/下载、原票和红冲记录 | 已接入口 |
| invoice | `reversals` | 用户 | `services.js / admin-app.js`：申请、列表、详情/下载、原票和红冲记录 | 已接入口 |
| invoice | `create` | 用户 | `services.js / admin-app.js`：申请、列表、详情/下载、原票和红冲记录 | 已接入口 |
| invoice | `issue_demo` | 管理员 | `admin-app.js`：演示开票、红冲处理，保留原票 | 已接入口 |
| invoice | `adjust_demo` | 管理员 | `admin-app.js`：演示开票、红冲处理，保留原票 | 已接入口 |
| reconciliation | `list` | 管理员 | `admin-app.js`：内部账本对账列表及执行 | 已接入口 |
| reconciliation | `run` | 管理员 | `admin-app.js`：内部账本对账列表及执行 | 已接入口 |
| notification | `list` | 用户 | `services.js`：消息列表、已读/全部已读 | 已接入口 |
| notification | `read` | 用户 | `services.js`：消息列表、已读/全部已读 | 已接入口 |
| notification | `read_all` | 用户 | `services.js`：消息列表、已读/全部已读 | 已接入口 |
| helpdesk | `faq` | 公开 | `services.js`：FAQ 搜索提交时重新读取帮助台 FAQ 集合，再按输入筛选 | 已接入口 |
| helpdesk | `list` | 用户 | `services.js / admin-app.js`：工单列表、会话、提交、追问、客服回复、关闭 | 已接入口 |
| helpdesk | `get` | 用户 | `services.js / admin-app.js`：工单列表、会话、提交、追问、客服回复、关闭 | 已接入口 |
| helpdesk | `create` | 用户 | `services.js / admin-app.js`：工单列表、会话、提交、追问、客服回复、关闭 | 已接入口 |
| helpdesk | `reply` | 用户 | `services.js / admin-app.js`：工单列表、会话、提交、追问、客服回复、关闭 | 已接入口 |
| helpdesk | `close` | 用户 | `services.js / admin-app.js`：工单列表、会话、提交、追问、客服回复、关闭 | 已接入口 |

## 需要补齐或明确入口别名的动作

| 动作 | 建议入口 | 说明 |
| --- | --- | --- |
| 无 | 业务动作有页面入口或已明确的业务代理；聚合和批量读取由共享客户端/网关接入 | pricing.quote 通过 checkout.quote 调用，不创建重复报价；共享读取能力不额外创建操作按钮 |

完整链路验证应继续区分“已接页面”、“HTTP 参数/权限验证通过”和“外部基础设施真实联调通过”。本表是源代码入口覆盖，不代替 MySQL/RabbitMQ/MinIO/Jaeger 及第三方支付渠道验证。

## 仅服务内部 RPC / 事件 / 恢复任务入口

下列资源和状态命令必须保留在内部。它们通过浏览器触发的合法业务操作间接发生，但不能为了满足前端覆盖率而加入 HTTP 白名单。

| 服务 | 内部动作 | 调用方与目的 |
| --- | --- | --- |
| iam | `validate` | 网关每个受保护 HTTP 请求先校验 IAM；聚合请求验证外层会话后派生子请求身份，不接受嵌套身份；current 是受保护的 validate 别名 |
| inventory | `reserve`, `confirm`, `cancel`, `dispatch`, `restock` | checkout 预占/确认/释放；fulfillment 出库与验收后回库 |
| coupon | `reserve`, `confirm`, `cancel`, `refund` | checkout 资产预占/提交/取消；RefundSucceeded 后按规则返还 |
| points | `reserve`, `confirm`, `cancel`, `refund`, `reward` | checkout 冻结与扣减；订单完成奖励；退款比例返还/奖励追回 |
| campaign | `reserve`, `confirm`, `cancel` | checkout 协调活动名额，浏览器不能自行冻结或提交名额 |
| group | `reserve`, `paid`, `cancel`, `expire` | checkout 占席、支付接受事件计入人数、取消和到期任务 |
| checkout | `get`, `paid` | 持久化 Saga 查询和支付后资源提交，由事件/恢复任务触发 |
| order | `create`, `pending`, `create_failed`, `accept_payment`, `ready`, `wait_group`, `close_begin`, `close_complete`, `group_formed`, `group_failed`, `shipped`, `refunded` | 订单创建、支付裁决、资源/拼团门禁及事件状态流转；complete 是用户可调用的独立动作 |
| payment | `close` | 先冻结全部支付尝试，再允许订单释放资源 |
| refund | `create` | 售后/拒绝支付补偿创建；金额上限由退款服务裁决 |
| fulfillment | `create_exchange` | 售后验收后创建换货履约单 |
| logistics | `create`, `list` | 出库时创建配送记录；内部查询汇总 |

所有服务的 Migrate、Seed、Tick、OnEvent 是生命周期/消息入口，不是应直接接入前端的 HTTP 操作。其他服务没有额外业务动作时，前端通过上述 HTTP 路由访问。

## 全局读取、活动和库存异步审查

- `api-bootstrap.js` 先恢复会话，再用 `storefront/bootstrap` 获取公开数据及本人数据。公开结果和本人结果按 `auth` 作用域保存，只供本轮模块初始化消费；启动结束清除快照。不同接口地址、认证模式和会话不共享账户结果，账号切换及写入使旧读取作用域失效。
- `api-client.js` 在所有启用共享客户端的页面合并同一轮并发只读请求，每批最多64项，同键请求共享 Promise。网关在执行前校验全部动作、参数和权限；拒绝写操作、嵌套聚合、IAM 和子请求身份字段。`pricing/quote`、`checkout/quote` 会保存报价，保持独立请求；支付、订单等实时状态查询也直接访问服务端。
- 聚合 RPC 最多8个并发，保持外层 Trace、超时及取消信号；按输入顺序返回，单项业务错误不吞掉其他成功结果。`catalog/get` 和 `inventory/get` 去重后使用每组最多500个 SKU 的 SQL `IN` 查询，缺失项分别报错；search/recommendation 以及 pricing/checkout 的内部商品/库存读取也使用批量动作，避免服务内部逐 SKU 查询。
- `inventory.refresh` 优先消费启动计划的商品默认/规格、秒杀和本人购物车 SKU 快照，后续并发读取走共享批次；不按浏览器订单扣减库存，不因退款自行回库。规格切换由 `commerceDetailSKU` 去重，只有所选 SKU 变化才异步刷新并重绘。
- `discovery.refreshSearch` 仅普通首页空查询复用已加载商品；选择品类、细分词或搜索时，通过一次公开 `search/query` 在完整目录按类目、品牌、词及排序筛选，商品与库存随响应合并。20个大类的586个细分入口仅本地展开，不为每个入口调用列表；搜索结果池最多100件，旧结果淘汰并保护购物车及当前详情。聚焦搜索框展示连续红框的历史/发现面板，历史复用本人启动快照，发现词从实际商品提取并在本地轮换；不因聚焦或换一换读取接口。删除历史调用本人 `engagement/clear`，保留当前输入；会话、用户或接口变化后晚到的历史及建议均不能覆盖新状态。首页领券只读取 `templates` 第1页100条预览；完整中心才用 `templates_snapshot` 每页最多5000条读取全部筛选元数据，普通 `templates` 仍限制100条。
- `promotions.refresh` 合并进行中的刷新；场次和团人数来自后端。活动倒计时只更新文本，没有每秒触发 render 或伪造成团。换购裁决返回时再次比较结算上下文，避免把旧请求结果应用到变更后的购物车。
- 首页推荐通过启动聚合复用已验证的行为、订单、商品及库存；默认综合排序按推荐顺序展示实际理由，用户指定的分类、关键词、品牌和排序优先。详情采用相似商品，购后带本人订单并排除已购商品。推荐状态按用户、令牌、网关和请求版本隔离，目录外推荐商品池有界；响应库存直接注入且不能覆盖后来发起的库存读取。当前为内容与行为混合排序，未声称已训练机器学习模型。
- 固定兴趣栏复用本人足迹及收藏，启动聚合按前六个原始 SKU 预载目录外商品、父商品与默认库存，并复用既有商品/推荐快照；不重复读取行为列表，也不增加首页物理 HTTP。新增浏览及显式移除、清空足迹/收藏后刷新元数据；渲染与收起只投影已加载状态。右侧工具导航至既有业务入口，反馈使用现有 helpdesk 工单表单。
- 待优化：单独停留拼团页面至到期时，倒计时归零但业务状态仍等待服务器刷新；进入页面时现已由 route hook 刷新；还可针对每个到期团只刷新一次。必须继续使用服务端状态，不在客户端推导退款或成团结果。
- 邀请链接的真实组 ID 含冒号，编码后含 `%HH`。登录 returnTo 原先限制过严，已由认证模块维护者修正为允许安全站内 hash 编码路径。

## 核对方法

1. 从网关 public/user/admin actionSet 解析出 HTTP 动作全集，统计并逐条核对。
2. 人工阅读 API 包装和动态分发：`auth.js` logout/logout_all、`member.js` mutate/create/update、`app.js` 收藏 put/delete、`admin-app.js` specs 与 data-operation。
3. 对照各服务 usecase/transport，分辨 HTTP 操作、内部资源命令和 Tick/事件入口。
4. 活动模块 VM 回归脚本位于 `web/tests/promotions-api.test.cjs`（`FRONTEND_DIR` 可指向其他前端目录），已验证准确的 SKU/金额、服务端库存、换购元数据、开团请求及 order_id 推荐请求。

## 后台管理扩展复查（2026-10-28）

已复查30个服务的RPC、HTTP权限和后台入口，并按确认的七类扩展完成接入。管理页现有31个分组菜单，使用真实服务接口、独立管理会话、服务端分页及有原因的受控写入。

### 页面长度与库存检索

- 库存目录每页8件紧凑行，列表独立滚动；名称、品牌、分类与编号搜索通过 `catalog/admin_list` 覆盖全部上下架商品，服务端返回精确总数，不受前台搜索屏蔽或100件候选窗口影响。
- 规格详情下拉选择一个SKU，只展示对应数量、表单和记录。完整规格定位与调整后保留对应SKU；历史兼容SKU的独立库存不会被误切到关联父商品。库存按最多500个SKU批量读取。
- 库存详情保留近期20条调整记录，每页5条；“库存预警与流水”提供完整盘点记录与出入库流水分页和CSV导出。
- 后台长表格统一限高、固定表头和可聚焦滚动区域，分页位于滚动区外。长正文先摘要、按需展开；对账差异每页8条，其他新管理列表每页10条。

### 已接入的七类管理能力

1. **秒杀与拼团**：维护场次启停、时间窗、SKU和配额关联；维护成团人数、有效分钟数、适用SKU与规则版本；创建秒杀/拼团专用优惠配额，查看预占/已用计数、秒杀请求结果和拼团记录。报价与秒杀申请按实际场次校验；开团由服务端规则决定人数/期限，已有团保留规则快照，规则变更后重试原请求仍返回原团。已占用的优惠参数不能改价，名额不能低于预占加已用。
2. **库存预警与流水**：每SKU维护阈值，按全部库存筛选缺货、低库存和正常库存；查看完整盘点历史和资源出入库流水，导出全部匹配记录。阈值和库存数量各自携带旧值校验、操作人及原因，数量仍不允许侵占预占/分配资源。
3. **账期对账**：按UTC+8日期创建最长366天的异步任务，固定创建时点的支付记录范围，每批100条经支付/订单RPC核对，展示进度、历史任务、分页差异和处理备注；失败任务保留检查点，可带原因继续。任务、备注与恢复均支持幂等；CSV导出完整差异。旧同步核对入口兼容保留，管理页使用异步入口。
4. **会员与积分**：查看会员资料、可用/冻结积分、欠账与完整流水；编辑昵称/头像；有旧余额校验、幂等键和审计原因的人工补偿/扣减，只影响可用积分，补偿优先偿还欠账。维护签到分值、抵扣比例、消费奖励、黄金会员门槛和成长换算。定价保存积分/成长规则与奖励快照，提交沿用快照，订单完成按原快照记账；已产生权益不受后续改规则影响。
5. **配送与运费**：配置启停、固定/首件续件费用、免邮门槛、可配送/禁运地区与地区特殊费用。定价从归属服务读取模板，根据权威商品金额、数量和地址计算运费；提交校验目的地和报价快照。浏览器不能以预览金额覆盖订单运费，变更收货地区需要重新报价。
6. **通知、内容与推荐运营**：11类业务事件的站内信标题/正文模板、变量、启停和安全示例预览；公告/FAQ草稿、发布、定时上线和下线；收藏/对比/浏览/购买/搜索、销量/新品/个性化/相似权重，首页精选与屏蔽商品；搜索默认排序、候选上限、商品/品牌屏蔽。配置真实参与新通知投影、公开内容可见性、推荐排序及搜索，历史通知保留原文。
7. **运行管理与审计**：查询IAM账号启停操作历史；按30个归属服务查询Outbox、Inbox、重放任务和重试审计。未完成且租约已过期的事件支持带原因的重试，Inbox重放使用原事件、消费租约、订阅校验与幂等。完成、仍持有租约或没有原始信封的历史消息受保护。页面只返回任务元数据和Trace编号，不返回消息载荷或原始传播头。

版本化配置及变更审计均保存在归属服务自己的数据库，通过新增独立RPC访问。更新使用期望版本，冲突返回409；服务间调用继续通过具名客户端。CSV为当前筛选的全页读取，最多10万条，超限要求缩小范围，不悄悄截断，并防止文本被电子表格解释为公式。

### 仍需真实外部集成的边界

账期对账核对内部支付/订单台账，未读取渠道账单；承运商、支付、退款、开票和短信/邮件仍需真实适配器与部署凭据。通知模板当前用于站内信，推荐为可解释规则排序。后台配置不会将演示渠道转成真实渠道。库存预占/确认/释放、订单支付裁决和退款金额裁决继续由既有业务流程协调。

### 验证范围

- 完整 `go test ./...`；新增 `tests/admin_management_integration_test.go` 在30个隔离SQLite数据库和真实TCP gRPC/HTTP环境验证权限、规则快照、积分记账、地区运费、内容可见性、通知模板、推荐/搜索策略、105条差异跨页对账、恢复幂等及事件租约/重放审计。
- `scripts/test-frontend.sh` 全量模块回归和四组独立临时库HTTP业务回归；新增 `web/tests/management-admin.test.cjs` 覆盖表单金额/版本、失败后稳定重试键、并行会员读取、分页、会话切换和归属服务重放命令。
- 真实浏览器验证桌面及390px窄屏，实际保存运费模板、积分补偿、公告草稿与对账任务，查看保存后的版本、余额、流水和发布状态。测试数据仅写入临时开发库。
- 以上不替代MySQL、RabbitMQ、MinIO和真实渠道的部署联调。

2026-10-28 增补：前台已接 Coupon.Calculate、Payment.Methods、IAM 登录方法/验证码/OAuth，以及 Engagement 商品提醒。管理页新增登录开关、支付开关和购物提醒规则及订阅分页。详见 `docs/commerce-improvements.md` 和 `docs/login-providers.md`。
