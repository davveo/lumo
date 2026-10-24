# group-service / 团、付费席位与成团裁决

公开 `list/get`；用户 `create`；内部 `reserve/paid/cancel`；管理员 `expire`，后台 Tick 自动到期。

## 目录分层

- `service.go`：组合依赖，注册迁移、消费事件与任务。
- `internal/transport`：RPC action 白名单、分发与 transport span。
- `internal/usecase`：权限、业务流程、跨服务RPC与 usecase span。
- `internal/repository`：只访问本服务 GORM 数据库，事务与 repository span。
- `internal/domain`：数据模型与业务状态。

## 已实现规则

- 开团不自动计付费人数，参团预占占席位但不能算已付款成员。
- 只有 OrderPaymentAccepted 确认的席位进入 PAID。成员上限、最后付款与截止裁决在同一团事务中串行化。
- 成团/失败按每个已付款订单分别发 GroupFormed/GroupFailed，AggregateID=order_id、UserID=该成员，避免跨用户身份混淆。
- `paid` 返回团级 `status` 与 `formed`，重复消息不会重复计数。团失败后迟到收款发本订单 GroupFailed，不复活团。
- 服务只决定团事实；券积分、库存提交、退款、发货仍由交易编排与各权威服务处理。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/group/...`。

## 独立业务方法

- `list` → `List`
- `get` → `Get`
- `create` → `Create`
- `reserve` → `Reserve`
- `paid` → `Paid`
- `cancel` → `Cancel`
- `expire` → `Expire`

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。支付确认与取消使用各自用例和仓储方法，事务与幂等规则保持不变。

跨服务调用使用 `platform.NewClients(...).Service.Method(ctx, platform.Request{...})`，不传递业务action。
