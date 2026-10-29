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

SQL span 包含 `db.system.name`、`db.namespace`、`db.collection.name`、`db.operation.name`、`db.query.text`（模板）、返回行数和耗时。SQL 绑定值不展开；原始 SQL 的字面量和注释会脱敏。结构化日志包含活动 trace_id / span_id，不输出请求及返回正文。

调用span通过 `TraceRequest(ctx, input)` 和 `TraceResponse(ctx, result, err)` 记录 `lumo.request` / `lumo.response` 脱敏JSON预览、规范 `lumo.error.code` 及redacted/truncated标志。HTTP、每跳自动gRPC、transport/usecase及MQ边界均接入；gRPC错误描述也只记录规范码。密码、令牌、签名能力、身份及地址等递归遮蔽，JSON bytes和编码JSON不会按base64或字符串原样输出。金额/数量/普通业务ID保持可读；每方向最多8KiB、数组前10项、字符串512字节，且有对象、节点和深度上限。只清理采样span的独立预览，不修改业务值；不执行自定义JSON marshalers。详见 [observability.md](../../docs/observability.md)。

`InitTelemetry` 使用 OTLP/gRPC；Compose 中的开发采样设为 1。OTLP 内部端点使用明文网络传输；生产环境应在部署层启用受控网络和 TLS。空端点仍建立 SDK，但不会向外发送数据。测试显式使用 AlwaysSample，避免宿主采样设置影响可重复验收。

## RPC

只通过生成的 Protobuf/gRPC 协议调用，内部服务凭据放在 metadata，不放在 payload。所有领域 RPC 均要求凭据，包括独立的 `OrderService` 方法和兼容 `DomainService.Execute`；标准 Health/Check 只返回健康状态，无需业务令牌。客户端继承调用上下文，并增加 15 秒上限，父调用更早的 deadline 优先。

`Module.RegisterRPC` 可注册领域专属服务，`RPCActions` 将实际方法关联到业务动作标签；没有注册专属服务的现有模块继续使用 Execute。订单已迁移为15个显式方法，请求/响应在 `api/mall/v1/order.proto`，用例层使用独立Go请求类型，不依赖protobuf或万能Command。原 `Caller` 的订单调用会映射到生成的 `OrderServiceClient`，返回值只在JSON兼容边界转换；仅在实例明确缺少新服务/方法时回退Execute，业务错误、超时、凭证错误不会重放。平台统一保持凭证、Trace、错误归一化和健康检查。

## 多副本运行

部署环境每个进程只运行一个领域模块，各副本通过Consul独立注册。RPC调用方持续刷新passing地址，gRPC round_robin只向READY连接分流；无健康实例时返回不可用，显式静态目标才绕过Consul。停止或新增实例无需重启调用方。

同一领域的副本共享该服务的MySQL schema和RabbitMQ持久化队列，消费采用竞争模式。平台Outbox发布和Inbox消费均通过数据库claim及租约协调，Outbox完成/失败回写还校验claim token，避免旧worker覆盖新租约。MQ仍是至少一次交付，领域处理必须继续使用业务/事件ID保证幂等。

MySQL环境下，`WithServiceStartupLock` 通过专用数据库连接的命名锁串行化同schema、同领域的迁移及种子初始化；DDL不能用普通事务代替此锁。定时Tick使用单独的非阻塞锁，已有副本执行时其他副本跳过本次扫描，仍依赖领域行锁、状态前提及幂等防止重复业务副作用。连接退出释放锁，SQLite仅用于隔离开发和测试，不提供该跨进程协调保证。

## 事件、可靠性与恢复

`Enqueue(ctx, tx, Event)` 将业务事件和 W3C trace carrier 写入同一个数据库事务内的 `outboxes`。固定事件 ID 重复插入不会产生第二个事件。发布 worker 提取保存的上下文，建立 producer span；发送持久化消息，等待 publisher confirm，并检查 mandatory unroutable return，再标记 PUBLISHED。确认丢失时用原事件 ID 重试。

消费端恢复消息头中的 traceparent，建立 consumer span。`inboxes` 对 consumer + event_id 建唯一键和处理租约，避免同一服务并发处理重复事件。处理成功后提交 Inbox DONE 再 ACK；失败进入 2 秒 retry 队列，5 次失败后进入该服务 dead 队列。消费 handler 的业务事务和 Inbox 完成不属于同一个回调事务：进程在两者之间崩溃会重新投递，因此每个业务 handler 仍必须通过事件 ID 或业务唯一键幂等，不能宣称端到端 exactly-once。

开发版拓扑在发布前预声明全部 30 个服务的持久化队列，统一绑定事件 topic，避免启动先后造成未建队列丢事件；每个消费者依据 Module.Topics 丢弃非订阅事件。规模增长后可由集中拓扑管理收窄成精确绑定。死信人工重放前应核查业务事实，将对应 inboxes 的 DEAD 状态重置为 RETRY 并重置 attempts，再保留原事件 ID 重投；不能换一个新事件 ID 逃过业务幂等。

`RunEvents` 内部同时运行 Module.Tick（每秒检查一次，不并发叠加），启动器不应重复调度 Tick。AMQP URL 为空时只运行 Tick，不伪造 MQ 投递；Outbox 仍然保留。数据库/消息中间件故障不丢弃待发送记录，worker 重连并重试。

事件必须是一个完整JSON对象，尾部仅允许空白；ID为1至80字符，Type为1至100字符，两者还需满足AMQP shortstr最多255字节。生产端落库前拒绝无效元数据，消费端对格式或元数据无效的消息直接死信，不创建Inbox或调用业务，避免永久重排毒消息。

## 已执行的平台验证

`go test -race ./internal/platform` 覆盖真实 TCP gRPC 的认证和健康接口、跨层同 trace、SQL 的直接仓储父 span、SQL 脱敏、Outbox 事务回滚及固定 ID 去重、持久化 carrier 恢复、Inbox 租约与成功后重复投递。RabbitMQ 真实容器投递仍需 Docker 可用环境执行集成验收，不能用 SQLite 的消息重放测试替代。
