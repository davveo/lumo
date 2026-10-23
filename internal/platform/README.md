# 共享运行平台

各业务服务只复用基础设施，不共享其他服务的业务表。`types.go` 中的 `Module` 是组装入口，业务代码分别放在 domain / repository / usecase / transport。

## 链路结构

```text
HTTP server span（gateway 的 otelhttp）
  → rpc.client.<目标服务>.<action>
    → gRPC client / gRPC server（otelgrpc StatsHandler，W3C TraceContext）
      → transport.grpc.<服务>.<action>
        → <服务>.transport / <服务>.usecase.<动作>
          → <服务>.repository.<操作>
            → SQL SELECT / INSERT / UPDATE / DELETE <table>
```

`Start(ctx, name)` 增加代码层 span，返回的 ctx 必须继续向下传递。每次 GORM 操作使用 `WithContext(ctx)`；SQL callback 在执行前建立数据库 span，执行后恢复原 context，因此后续独立 SQL 不会错误嵌套到上一次 SQL 下。

SQL span 包含 `db.system.name`、`db.namespace`、`db.collection.name`、`db.operation.name`、`db.query.text`（模板）、返回行数和耗时。SQL 绑定值不展开；原始 SQL 的字面量和注释会脱敏。密码、令牌和完整请求正文不进入日志或 tracing。结构化日志包含活动 trace_id / span_id。

`InitTelemetry` 使用 OTLP/gRPC；Compose 中的开发采样设为 1。OTLP 内部端点使用明文网络传输；生产环境应在部署层启用受控网络和 TLS。空端点仍建立 SDK，但不会向外发送数据。测试显式使用 AlwaysSample，避免宿主采样设置影响可重复验收。

## RPC

只通过生成的 Protobuf/gRPC 协议调用，内部服务凭据放在 metadata，不放在 payload。所有领域 Execute RPC 要求凭据；标准 Health/Check 只返回健康状态，无需业务令牌。客户端继承调用上下文，并增加 15 秒上限，父调用更早的 deadline 优先。

## 事件、可靠性与恢复

`Enqueue(ctx, tx, Event)` 将业务事件和 W3C trace carrier 写入同一个数据库事务内的 `outboxes`。固定事件 ID 重复插入不会产生第二个事件。发布 worker 提取保存的上下文，建立 producer span；发送持久化消息，等待 publisher confirm，并检查 mandatory unroutable return，再标记 PUBLISHED。确认丢失时用原事件 ID 重试。

消费端恢复消息头中的 traceparent，建立 consumer span。`inboxes` 对 consumer + event_id 建唯一键和处理租约，避免同一服务并发处理重复事件。处理成功后提交 Inbox DONE 再 ACK；失败进入 2 秒 retry 队列，5 次失败后进入该服务 dead 队列。消费 handler 的业务事务和 Inbox 完成不属于同一个回调事务：进程在两者之间崩溃会重新投递，因此每个业务 handler 仍必须通过事件 ID 或业务唯一键幂等，不能宣称端到端 exactly-once。

开发版拓扑在发布前预声明全部 30 个服务的持久化队列，统一绑定事件 topic，避免启动先后造成未建队列丢事件；每个消费者依据 Module.Topics 丢弃非订阅事件。规模增长后可由集中拓扑管理收窄成精确绑定。死信人工重放前应核查业务事实，将对应 inboxes 的 DEAD 状态重置为 RETRY 并重置 attempts，再保留原事件 ID 重投；不能换一个新事件 ID 逃过业务幂等。

`RunEvents` 内部同时运行 Module.Tick（每秒检查一次，不并发叠加），启动器不应重复调度 Tick。AMQP URL 为空时只运行 Tick，不伪造 MQ 投递；Outbox 仍然保留。数据库/消息中间件故障不丢弃待发送记录，worker 重连并重试。

## 已执行的平台验证

`go test -race ./internal/platform` 覆盖真实 TCP gRPC 的认证和健康接口、跨层同 trace、SQL 的直接仓储父 span、SQL 脱敏、Outbox 事务回滚及固定 ID 去重、持久化 carrier 恢复、Inbox 租约与成功后重复投递。RabbitMQ 真实容器投递仍需 Docker 可用环境执行集成验收，不能用 SQLite 的消息重放测试替代。
