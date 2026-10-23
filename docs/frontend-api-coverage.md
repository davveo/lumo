# 商城前端与 HTTP / RPC 接口覆盖对照

本表以 `internal/httpgateway/gateway.go` 当前白名单和 `web/dist`（原 Site `dist`）源代码为准。逐条阅读了通用调用包装、动态 action、后台 `specs` 与 `data-operation` 分发；不将字符串搜索命中等同于存在可操作页面。

当前网关共有 **107 个 HTTP 动作**：公开 16、用户 67、管理员 24。本次快照：直接页面入口 106、通过业务代理间接接入 1、待补独立入口 0。

“待补独立入口”表示当前专用页面尚未调用此 HTTP 路由；`/debug` 可以按权限手动联调全部白名单动作，但本表不会因此把业务页面标记为已接入。`pricing.quote` 已由 `checkout.quote` 间接调用，属于同一报价能力的入口别名。

## HTTP 路由矩阵

全部路由形式为 `POST /api/v1/{service}/{action}`，请求使用统一 Command 包装。只读动作还支持白名单允许的 GET。管理员页面使用独立服务器会话；浏览器不能自行指定 user_id/role 获得权限。

| 服务 | 动作 | 网关身份 | 页面模块 / 业务入口 | 覆盖状态 |
| --- | --- | --- | --- | --- |
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
| catalog | `get` | 公开 | `promotions.js`：秒杀指定 SKU 的真实价格/展示信息 | 已接入口 |
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
| search | `query` | 公开 | `discovery.js`：搜索、联想、分类/品牌/库存筛选 | 已接入口 |
| recommendation | `list` | 公开 | `recommendations.js / product-extras.js`：支付结果推荐带 order_id；详情推荐读取后端目录策略 | 已接入口 |
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
| campaign | `list` | 公开 | `promotions.js`：活动配置、选择换购前资格裁决 | 已接入口 |
| campaign | `evaluate` | 公开 | `promotions.js`：活动配置、选择换购前资格裁决 | 已接入口 |
| coupon | `list` | 用户 | `member.js`：我的券、可领模板、实际领券 | 已接入口 |
| coupon | `templates` | 用户 | `member.js`：我的券、可领模板、实际领券 | 已接入口 |
| coupon | `claim` | 用户 | `member.js`：我的券、可领模板、实际领券 | 已接入口 |
| points | `get` | 用户 | `member.js`：余额/冻结/账本、每日签到 | 已接入口 |
| points | `checkin` | 用户 | `member.js`：余额/冻结/账本、每日签到 | 已接入口 |
| group | `list` | 公开 | `promotions.js`：服务端团列表、参团前查询、真实开团；人数只读后端 paid/occupied | 已接入口 |
| group | `get` | 公开 | `promotions.js`：服务端团列表、参团前查询、真实开团；人数只读后端 paid/occupied | 已接入口 |
| group | `create` | 用户 | `promotions.js`：服务端团列表、参团前查询、真实开团；人数只读后端 paid/occupied | 已接入口 |
| flash | `list` | 公开 | `promotions.js`：读取已开放场次，不虚构明日活动 | 已接入口 |
| flash | `request` | 用户 | `app.js`：提交异步秒杀请求、轮询成功订单或失败结果 | 已接入口 |
| flash | `get` | 用户 | `app.js`：提交异步秒杀请求、轮询成功订单或失败结果 | 已接入口 |
| inventory | `get` | 公开 | `inventory.js / promotions.js / editor.js / admin-app.js`：SKU 可售快照、秒杀指定 SKU、管理页查询 | 已接入口 |
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
| 无 | 所有白名单动作均有页面入口或已明确的业务代理 | pricing.quote 通过 checkout.quote 调用，不创建重复报价 |

完整链路验证应继续区分“已接页面”、“HTTP 参数/权限验证通过”和“外部基础设施真实联调通过”。本表是源代码入口覆盖，不代替 MySQL/RabbitMQ/MinIO/Jaeger 及第三方支付渠道验证。

## 仅服务内部 RPC / 事件 / 恢复任务入口

下列资源和状态命令必须保留在内部。它们通过浏览器触发的合法业务操作间接发生，但不能为了满足前端覆盖率而加入 HTTP 白名单。

| 服务 | 内部动作 | 调用方与目的 |
| --- | --- | --- |
| iam | `validate` | 网关每个受保护 HTTP 请求先校验 IAM；current 是受保护的 validate 别名 |
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

## 活动、库存和推荐异步审查

- `inventory.refresh` 批量按 SKU 拉取快照；不按浏览器订单扣减库存，不因退款自行回库。规格切换由 `commerceDetailSKU` 去重，只有所选 SKU 变化才异步刷新并重绘。
- `promotions.refresh` 合并进行中的刷新；场次和团人数来自后端。活动倒计时只更新文本，没有每秒触发 render 或伪造成团。换购裁决返回时再次比较结算上下文，避免把旧请求结果应用到变更后的购物车。
- `recommendations.render` 按账号和订单缓存，并复用同键进行中的 Promise；请求完成只重绘一次。失败显示重试，不在 render 中无限请求。当前后端是目录可售规则策略，文案没有声称机器学习个性化。
- 待优化：单独停留拼团页面至到期时，倒计时归零但业务状态仍等待服务器刷新；进入页面时现已由 route hook 刷新；还可针对每个到期团只刷新一次。必须继续使用服务端状态，不在客户端推导退款或成团结果。
- 邀请链接的真实组 ID 含冒号，编码后含 `%HH`。登录 returnTo 原先限制过严，已由认证模块维护者修正为允许安全站内 hash 编码路径。

## 核对方法

1. 从网关 public/user/admin actionSet 解析出 HTTP 动作全集，统计并逐条核对。
2. 人工阅读 API 包装和动态分发：`auth.js` logout/logout_all、`member.js` mutate/create/update、`app.js` 收藏 put/delete、`admin-app.js` specs 与 data-operation。
3. 对照各服务 usecase/transport，分辨 HTTP 操作、内部资源命令和 Tick/事件入口。
4. 活动模块 VM 回归脚本位于 `web/tests/promotions-api.test.cjs`（`FRONTEND_DIR` 可指向其他前端目录），已验证准确的 SKU/金额、服务端库存、换购元数据、开团请求及 order_id 推荐请求。
