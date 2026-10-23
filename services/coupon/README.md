# coupon-service / 优惠券实例与核销账

公开 `templates`；用户 `list/claim`；内部 `reserve/confirm/cancel/refund`。

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

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/coupon/...`。
