# 从 HTTP、RPC、业务分层一直追踪到 SQL

## 数据流

服务内 OTel SDK → OTLP gRPC → `otel-collector:4317` → `jaeger:4317` → Jaeger 查询界面16686。Jaeger2.5使用Badger本地持久化，数据位于 `jaeger-data` 卷，仅适合本机/单节点开发验证；生产规模需评估独立持久存储、保留期限和容量。

每个服务将 `service.name` 设为自己的名称。跨 RPC 由 `otelgrpc` StatsHandler 自动提取和注入 W3C TraceContext。服务每层显式建立子 span，GORM 回调建立实际 SQL span，并记录数据库操作结果。

| 层级 | 观察内容 | span 的父上下文 |
| --- | --- | --- |
| HTTP | 路由、状态、总耗时 | 入站追踪上下文 / 新根 |
| Gateway | 鉴权、路由、RPC转发 | HTTP span |
| gRPC client / server | 被调用服务、Execute、状态、网络耗时 | 调用层 / RPC传播 |
| transport | 动作校验和分发 | gRPC server |
| usecase | 报价、预占、订单状态与补偿流程 | transport |
| repository | 事务、查询、条件更新等仓储操作 | usecase |
| GORM SQL | 参数化SQL模板、表、执行耗时、行数、错误 | 当前 repository / transaction |
| MQ producer / consumer | 事件类型、事件ID、重试、后续业务 | 事件创建时保存的TraceContext |

`context.Context` 必须从 HTTP/RPC 入口一路传到 repository，再调用 `db.WithContext(ctx)`。新开 goroutine / worker 不能随意使用 `context.Background()` 代替业务上下文；生命周期独立的worker从持久化事件的上下文恢复调用关系。

## 实际查一条链路

1. 运行 `sh scripts/smoke.sh` 或在联调台发送下单请求。
2. 复制响应 JSON 的 `trace_id` 或响应头 `X-Trace-ID`。
3. 打开 `http://localhost:16686/trace/<trace_id>`。
4. 展开 Gateway、checkout、pricing、catalog、inventory、coupon 等服务的子树，查看 transport/usecase/repository/SQL 层。
5. 点开 SQL span，核对模板、耗时和错误，不应看到实参值。
6. 使用管理员演示支付后，异步链路会通过 PaymentSucceeded → OrderPaymentAccepted → OrderReadyToFulfill 延续到后续消费；等批量导出间隔1秒后刷新。

HTTP 返回 trace_id 不等于 trace 已成功持久化。Collector 或 Jaeger 短暂不可用时，业务不应因为追踪导出失败而扣款失败；应从 SDK/Collector 日志检查丢弃或重试，并按生产观测要求配置可靠队列和监控。

## 默认开发配置

项目附带 `docs/examples/trace-example.json`，来自一次通过的真实gRPC集成测试（551个同trace span）。可用Jaeger页面的Import功能导入查看；测试内存Exporter样例与Docker运行时写入Jaeger是两种不同验证，详情见 `docs/verification.md`。

`.env` 中 `OTEL_TRACES_SAMPLER_ARG=1` 默认全采样，便于联调查看每次请求。生产环境应根据业务目标制定采样规则，尤其关注错误、慢SQL、资金未知态和长时间未完成的Saga。敏感值不进入 trace attributes；SQL记录模板占位符，不插值打印用户输入。

结构化日志中的 trace_id/span_id 可关联同一请求。建议用 `order_id`、`event_id` 和 `reservation_id` 作为排错业务键，不把手机号、地址、密码或token作为查询字段。异步请求重试可能出现在原trace上下文下，业务幂等与trace去重是不同概念。

## 参考

- OpenTelemetry Go instrumentation: https://opentelemetry.io/docs/languages/go/instrumentation/
- otelgrpc StatsHandler: https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc
- Jaeger Badger: https://www.jaegertracing.io/docs/2.5/storage/badger/
- Jaeger2.5 Badger sample: https://github.com/jaegertracing/jaeger/blob/v2.5.0/cmd/jaeger/config-badger.yaml
