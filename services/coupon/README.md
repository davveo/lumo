# coupon-service / 优惠券实例与核销账

公开 `templates/templates_snapshot`；用户 `list/claim`；管理员 `templates_admin/template_upsert`；内部 `templates_by_ids/reserve/confirm/cancel/refund`。

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
- 内部 `templates_by_ids` 仅接受 `system`，`data.ids` 必须为1～50个有效模板ID。一次按ID集合读取模板、一次查询本人领取记录，复用公开模板的状态投影和积分/成长值资格校验；不扫描领券中心分页。可返回已停用模板的 `unavailable` 状态，未知ID不返回。网关禁止直接访问和只读批处理访问这个内部动作。
- 用户 `list` 返回每张券的 `template_name`，通过一次本地关联查询解析本人已领券名称，不依赖公开模板分页，也保留已停用模板的名称；无模板的历史券返回“品质生活优惠券”。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/coupon/...`。

## 独立业务方法

- `templates` → `Templates`
- `templates_snapshot` → `TemplatesSnapshot`
- `templates_by_ids` → `TemplatesByIds`
- `list` → `List`
- `claim` → `Claim`
- `reserve` → `Reserve`
- `confirm` → `Confirm`
- `cancel` → `Cancel`
- `refund` → `Refund`

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。确认与取消分别调用明确的仓储方法，事务和幂等规则保持不变。

跨服务调用使用 `platform.NewClients(...).Service.Method(ctx, platform.Request{...})`，不传递业务action。


## 后台优惠券配置

- 管理员独立方法 `TemplatesAdmin` / `TemplateUpsert` 对应 `templates_admin` / `template_upsert`；均要求具备真实登录身份的 admin，system 与普通会员也不能调用。管理列表包含停用模板，按名称/编号、启停状态和每页最多100项分页，返回精确总数及本页模板的已领取数；不混入当前管理员的领券资格，也不调用积分/会员服务。
- 可创建、编辑、复制、启用/停用，配置面额、使用门槛、领取后有效天数、领取窗口、积分/成长值门槛。当前每个用户每种券固定限领1张，由既有唯一约束执行；后台不提供虚假的可配置限领次数。
- 新建提交 `remaining` 初始可领数量。编辑必须提交读取到的 `version`，库存只允许提交相对 `stock_delta`，不能回写旧的剩余数量。模板行锁下累计增减并检查非负/精确整数边界，与领取使用同一行锁；版本冲突要求刷新。迁移将旧模板版本归一化为1，新建的version0不能覆盖既有编号。
- 一旦存在领取记录，优惠金额、使用门槛、领券后有效天数冻结；修改权益请复制为新模板。名称、未来领取时间、领取资格和启停只影响后续领取。既有券实例继续保留原来的金额、门槛和到期时间；停用不作废已领券，也不重置已领取配额。
- 后台保存后重新读取分页权威数据。管理写接口不开放给匿名或通用批处理；所有管理动作仍记录独立RPC/用例/仓储trace。


## 券类型与适用范围

共享 `internal/couponrules.Rule` 维护满减reduction、代金cash、折扣discount和满额折扣threshold_discount。折扣以整数bps表示应付比例，支持抵扣封顶；范围为全场、实际一级类目或父商品ID。门槛只看范围内主商品金额，不包含换购；报价服务覆写商品归属，预占重新计算并与报价对账。

模板和已领券均持久化完整规则。首次领取后冻结规则、适用范围和有效天数；领取时复制快照，修改模板不影响既有券。旧记录缺少type/scope字段时按原金额门槛归一为固定金额券/全场，不要求强制覆写存量记录。MySQL JSON scope序列化、迁移重复执行及快照预占另有独立scratch库回归。
