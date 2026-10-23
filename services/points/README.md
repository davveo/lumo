# points-service / 积分账户、冻结与不可变流水

用户 `get/checkin`；内部 `reserve/confirm/cancel/refund/reward`。

## 目录分层

- `service.go`：组合依赖，注册迁移、消费事件与任务。
- `internal/transport`：RPC action 白名单、分发与 transport span。
- `internal/usecase`：权限、业务流程、跨服务RPC与 usecase span。
- `internal/repository`：只访问本服务 GORM 数据库，事务与 repository span。
- `internal/domain`：数据模型与业务状态。

## 已实现规则

- demo 初始5000积分；1积分=1分。报价层限制抵扣上限，积分账本自己检查可用余额与欠账。
- reserve 将可用转冻结，confirm 冻结转消费；cancel 返还未消费冻结。所有调用沿用固定订单凭据。
- RefundSucceeded 按原实际抵扣积分乘累计现金退款比例计算应返总量，再减此前已返；全额退款吸收尾差，使用大整数乘除避免中间溢出。
- OrderCompleted 才入账奖励。退款按比例冲回奖励；奖励已花不足冲回时记 debt，后续奖励/签到先偿还 debt。
- 按订单持久化累计退款进度；退款早于完成事件时，后到的奖励按已退款比例冲回。较旧或重复退款事件不会降低累计退款进度或重复变更余额。
- 签到按UTC日期幂等，每天20积分；服务账户锁定顺序统一为账户→预占，降低与退款并发的死锁风险。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/points/...`。
