# coupon-service / 优惠券实例与核销账

公开 `templates/templates_snapshot`；用户 `list/claim`；内部 `reserve/confirm/cancel/refund`。

## 目录分层

- `service.go`：组合依赖，注册迁移、消费事件与任务。
- `internal/transport`：RPC action 白名单、分发与 transport span。
- `internal/usecase`：权限、业务流程、跨服务RPC与 usecase span。
- `internal/repository`：只访问本服务 GORM 数据库，事务与 repository span。
- `internal/domain`：数据模型与业务状态。

## 已实现规则

- 演示券 `coupon-demo-100` 属于 demo，金额10000分、门槛100000分。
- reserve 核验归属、门槛、过期、状态；确认收款后核销；未付关单取消释放。
- 当前明确政策：部分退款不退整张券，累计全额退款时返还原券；已过期原券保持过期，不随机新发券。
- 直接消费 RefundSucceeded，依赖其中 `order_id/payable_cents/refunded_total_cents`；重复事件不二次返还。
- 已实现持久化券模板与领取池：`templates` 返回额度、门槛、有效天数、剩余配额和本人领取状态；`claim` 以用户+模板唯一，事务原子扣配额并生成券实例。重复点击返回原券，不再扣配额。
- 演示模板 `welcome-100` 配额1000，满100000分减10000分，自领取起30天有效。模板过期/停用后停止新领取，历史已领取实例继续按自己的到期时间使用。
- 领券中心公开展示全部启用模板，包括尚未开始、领取结束和已领完的券。`templates` 支持 `data.page/page_size`，默认第1页、每页100条，单页最多100条；返回 `items/total/page/page_size/has_more/server_time`。传入登录身份时包含本人领取状态；关闭的模板不公开展示。
- `templates_snapshot` 使用相同分页参数、结果字段和领取状态，单页最多5000条；默认未指定大小时仍为100条。每次读取只计算一次本人积分、成长值等资格上下文，不逐模板请求资格服务。首页通过 `templates` 只读第1页100条预览，完整领券中心才用 `templates_snapshot` 获取全量筛选元数据；超过单页上限时继续按 `has_more` 分页。普通 `templates` 的100条上限保持不变。
- 首页 `templates` 由网关 `storefront/bootstrap` 读取，两种模板读取也可通过只读 `storefront/batch` 合并，匿名与本人会话分别处理；快照不会预占或扣减领取配额。领取后刷新模板状态及本人券包，`claim` 始终重新校验资格和名额，不依赖浏览器保存的读取结果。
- 模板 `starts_at` 为空表示立即开放，`expires_at` 是领取截止时间，`valid_days` 是领券后可使用天数。到点和剩余配额均在领取事务内重新核验，不采信前端倒计时。
- `min_points` 为可用积分门槛，领取不扣积分；`min_growth` 为会员成长值门槛，两项同时配置时需同时满足。coupon 通过 `points/get`、`member/get` 查询当前登录用户的真实值，不接受请求里的积分、成长值或门槛覆盖。资格服务不可用时拒绝条件券领取，普通券仍可正常浏览和领取。
- 每项提供 `claim_status/claimable/claim_reason/claimed`，已领取时另有 `coupon_id`。状态为 `available/login_required/upcoming/ended/exhausted/claimed/conditions_unmet/unavailable`；已领取状态优先，重复领取即使当前已停用、过期、领完或不满足新门槛，也返回原券，不额外扣库存。
- 新增日常满100减10、限时满200减25、明日满500减50、5000积分专享满300减30、1000成长值专享满600减80等演示模板。所有种子只补缺失项，重启不会覆盖运营配置、已消耗额度和领取记录。
- 用户 `list` 返回每张券的 `template_name`，通过一次本地关联查询解析本人已领券名称，不依赖公开模板分页，也保留已停用模板的名称；无模板的历史券返回“品质生活优惠券”。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/coupon/...`。
