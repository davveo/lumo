# flash-service / 秒杀准入与异步排队

公开 `list`；用户 `request/get`。

## 目录分层

- `service.go`：组合依赖，注册迁移、消费事件与任务。
- `internal/transport`：RPC action 白名单、分发与 transport span。
- `internal/usecase`：权限、业务流程、跨服务RPC与 usecase span。
- `internal/repository`：只访问本服务 GORM 数据库，事务与 repository span。
- `internal/domain`：数据模型与业务状态。

## 已实现规则

- 演示场次 `flash-demo` 仅允许 sku-headphones 一件；报价必须属于该活动，不能任意SKU伪造活动标记。
- Redis SetNX 只限流准入，数据库 Request + Outbox 同事务才是排队凭据。MQ 事件触发固定幂等键的 checkout.submit。
- `QUEUED/PROCESSING` 不是下单成功。只有交易返回有效订单状态才记 SUCCEEDED；用户按 request_id 查询原结果。
- 重复MQ投递不会新建第二个订单；交易处理中、网络超时等保留重试，永久业务失败落 FAILED。
- 首期将活动库存和普通库存交由同一个权威 SKU 账裁决，活动配额另由 campaign 控制；没有宣称实现独立仓库活动库存池。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/flash/...`。

## 独立业务方法

- `list` → `List`
- `get` → `Get`
- `request` → `Request`

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。确认与取消分别调用明确的仓储方法，事务和幂等规则保持不变。
