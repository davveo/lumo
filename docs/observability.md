# 从 HTTP、RPC、业务分层一直追踪到 SQL

## 数据流

服务内 OTel SDK → OTLP gRPC → `otel-collector:4317` → `jaeger:4317` → Jaeger 查询界面16686。Jaeger固定使用2.12.0，Badger本地持久化，数据位于 `jaeger-data` 卷，仅适合本机/单节点开发验证；生产规模需评估独立持久存储、保留期限和容量。

每个服务将 `service.name` 设为自己的名称。跨 RPC 由 `otelgrpc` StatsHandler 自动提取和注入 W3C TraceContext。服务每层显式建立子 span，GORM 回调建立实际 SQL span，并记录数据库操作结果。

每个进程的资源属性 `service.instance.id` 与其Consul实例ID一致，结构化日志同时包含 `instance_id`。默认值由服务名、hostname和监听端口组成；显式设置 `CONSUL_SERVICE_ID` 时沿用该值。服务增加副本后，可按实例区分流量、错误及耗时，并对应到注册中心的具体地址；多个副本仍使用相同 `service.name`。

HTTP 入口、网关分发/转发和 RPC 边界 span 还会记录 `lumo.target_service`（本次请求的目标领域服务，如 `catalog`）与 `lumo.action`（业务动作，如 `list`）；自动 gRPC client span 记录固定的 `lumo.target_service` 和 `peer.service`。在 Jaeger 中可按这些标签检索领域服务与业务动作；标准gRPC span同时显示专属服务和方法。`service.name` 仍表示产生 span 的进程，标准 `rpc.service` 仍表示 protobuf 服务，不用目标名称覆盖它们。

HTTP 路由标签只取已开放路由，RPC 服务端以实际模块名为准，不采信浏览器或 RPC metadata 提供的服务名。入口目标与鉴权子调用分别记录：例如访问 `member/get` 时，HTTP span 的目标为 `member`，校验登录态的 RPC 目标为 `iam`。无效 RPC action 使用固定 `unknown`，不将未通过长度校验的原始值写入业务 span 名称或动作标签。请求及返回值使用下述独立脱敏属性。

30个领域服务均使用独立RPC，例如 `mall.v1.CheckoutService/Submit`、`mall.v1.InventoryService/Reserve`、`mall.v1.CatalogService/BatchGet`。订单的自动gRPC span为 `mall.v1.OrderService/GetOrder`、`mall.v1.OrderService/AcceptPayment` 等，标准 `rpc.service` 为 `mall.v1.OrderService`；`lumo.target_service=order` 和 `lumo.action=get/accept_payment` 继续保留。新旧接口共用鉴权与链路传播，旧实例明确缺少专属方法时可看到兼容Execute调用；业务失败或超时不会通过Execute重放。

独立Nginx前端直接提供HTML、JS、CSS和图片，这些请求不进入Go网关或生成后端Trace；同源 `/api/` 请求继续进入网关和领域RPC。开发运行器显式加载静态目录只供隔离测试/本机调试。

## 调用入参与返回值

在Jaeger展开HTTP、网关Dispatch/Forward、每一跳自动gRPC client/server、RPC transport、领域transport/usecase或MQ publish/consume的span，在Tags中查看：

- `lumo.request`：当前调用的结构化入参JSON预览。
- `lumo.response`：返回值JSON预览；失败时配合 `lumo.error.code`、`lumo.error.message` 查看规范错误码和具体原因。
- `lumo.request.redacted` / `lumo.response.redacted`：是否遮蔽了敏感字段。
- `lumo.request.truncated` / `lumo.response.truncated`：是否因长度、数量、深度或不支持的值而省略内容。

业务订单/SKU/支付/事件ID、金额、数量、状态及普通商品搜索条件保留。密码、令牌、内部凭据、会话、签名表单/URL、用户身份、手机号、邮箱、收货地址、收件人和用户自由正文等字段递归替换为 `[REDACTED]`；身份响应中与user_id等值的id也遮蔽，其他业务对象ID保留。JSON字节及嵌套JSON字符串先解析再脱敏，不以base64绕过；整数通过精确JSON数值处理，避免金额失真。错误消息按下述规则脱敏后写入Trace，保留具体失败原因、底层原因链和定位信息，实际接口错误响应保持原样。

每个方向最多8KiB，每个列表预览前10项、对象最多64字段、字符串最多512字节，递归深度和访问节点均有上限；超大、非法或循环内容使用省略标记。预览过长时 `lumo.request/response` 是带 `_truncated` 和 `preview` 的有效JSON对象。只处理采样中的span，所有清理在独立预览上进行，不修改请求、返回或持久化数据。

记录来自已经解析的API命令、protobuf消息和业务返回值，不读取Authorization/Cookie等请求头，不缓存静态资源、媒体二进制或整个ResponseWriter。HTTP解析失败记录具体的JSON解析错误，但不记录未成功解析的原始请求体。SQL继续展示参数化模板和返回行数，不展开绑定值或整行数据；底层HTTP/gRPC连接属性保持原有观测方式。更新后的新Trace才包含这些属性，旧Trace不会回填。

| 层级 | 观察内容 | span 的父上下文 |
| --- | --- | --- |
| HTTP | 路由、状态、总耗时 | 入站追踪上下文 / 新根 |
| Gateway | 鉴权、路由、RPC转发 | HTTP span |
| gRPC client / server | 专属服务与方法、状态、网络耗时 | 调用层 / RPC传播 |
| transport | 请求边界校验和方法绑定 | gRPC server |
| usecase | 报价、预占、订单状态与补偿流程 | transport |
| repository | 事务、查询、条件更新等仓储操作 | usecase |
| GORM SQL | 参数化SQL模板、表、执行耗时、行数、错误 | 当前 repository / transaction |
| MQ producer / consumer | 事件类型、事件ID、重试、后续业务 | 事件创建时保存的TraceContext |

`context.Context` 必须从 HTTP/RPC 入口一路传到 repository，再调用 `db.WithContext(ctx)`。新开 goroutine / worker 不能随意使用 `context.Background()` 代替业务上下文；生命周期独立的worker从持久化事件的上下文恢复调用关系。

## 错误原因与定位

新增错误信息只对更新后产生的Trace生效。点击报错span，在Tags中查看 `lumo.error.message`（也映射为标准 `error.message`）、`lumo.error.code` 和 `error.type`；在Logs/Events中展开 `exception`，查看 `exception.message` 及 `code.file.path`、`code.line.number`、`code.function.name`。定位信息表示记录错误的业务边界或处理点，并非Go原生error自带的创建栈。

`lumo.error.causes` 保存最多8层底层原因，例如「预占库存失败 → 库存不足」或「插入订单失败 → UNIQUE constraint failed: orders.id」。顶层具体错误消息最多2048字节，原因链每条最多512字节；超出时设置 `lumo.error.truncated`。错误中的本次请求敏感值、密码/token赋值、手机号、邮箱、数据库连接凭据、带鉴权参数的URL、私钥及重复键中的实际数据会被遮蔽，`lumo.error.redacted` 标识发生脱敏。请求间的脱敏记忆隔离，不持久化、不导出原值。

HTTP、业务RPC和自动gRPC span、已有业务层、349处补齐的仓储/业务返回点、SQL、MQ发布/消费及人工重试均记录错误。26处把底层错误转换为业务提示的分支在转换前保留原始原因；响应仍沿用现有业务错误码，避免把SQL等内部详情直接返回浏览器。JSON解析、响应编码和解码失败同样保留具体原因。SQL只记录参数模板及数据库实际错误，不展开绑定值；正常的“未找到记录”查询可由业务层决定是否为失败。

异步消息业务处理失败后，即使重试/死信回执发送成功，原消息span仍保留失败原因；无效消息在进入业务处理前被拒收时也会产生带原因的追踪。排查时找到最早失败的下游业务或SQL span，结合父子调用关系查看原始原因，传输层的通用 `operation failed` 不再覆盖该处理点已记录的具体异常。

## 启动健康检查

Jaeger镜像基于固定官方版本2.12.0，Collector沿用0.124.1；两个镜像只额外加入静态HTTP探针，不依赖镜像内的shell或curl。Jaeger检查13133端口的 `/status`，Collector检查13133端口的 `/`；应用启动依赖Collector健康，Collector启动依赖Jaeger健康。容器处于running但没有健康检查或检查未通过时，启动脚本返回失败，不输出成功。

Jaeger沿用 `jaeger-data` 中已有Badger数据，升级不会删除卷。首次应用新版配置时只需更新Jaeger和Collector，业务容器无需因为探针源码变化而重建；探针及观测镜像配方使用单独的 `--observability` 源码指纹。

## 实际查一条链路

1. 运行 `sh scripts/smoke.sh` 或在联调台发送下单请求。
2. 复制响应头 `x-trace-id`；成功与失败的JSON返回体都不包含附加链路字段。
3. 打开 `http://localhost:16686/trace/<trace_id>`。
4. 展开 Gateway、checkout、pricing、catalog、inventory、coupon 等服务的子树，查看 transport/usecase/repository/SQL 层。
5. 点开 SQL span，核对模板、耗时和错误，不应看到实参值。
6. 使用管理员演示支付后，异步链路会通过 PaymentSucceeded → OrderPaymentAccepted → OrderReadyToFulfill 延续到后续消费；等批量导出间隔1秒后刷新。

HTTP 响应头返回 `x-trace-id` 不等于 trace 已成功持久化。Collector 或 Jaeger 短暂不可用时，业务不应因为追踪导出失败而扣款失败；应从 SDK/Collector 日志检查丢弃或重试，并按生产观测要求配置可靠队列和监控。

## 默认开发配置

项目附带 `docs/examples/trace-example.json`，来自一次通过的真实gRPC集成测试（551个同trace span）。可用Jaeger页面的Import功能导入查看；测试内存Exporter样例与Docker运行时写入Jaeger是两种不同验证，详情见 `docs/verification.md`。

`.env` 中 `OTEL_TRACES_SAMPLER_ARG=1` 默认全采样，便于联调查看每次请求。生产环境应根据业务目标制定采样规则，尤其关注错误、慢SQL、资金未知态和长时间未完成的Saga。敏感值不进入 trace attributes；SQL记录模板占位符，不插值打印用户输入。

结构化日志中的 trace_id/span_id 可关联同一请求。建议用 `order_id`、`event_id` 和 `reservation_id` 作为排错业务键，不把手机号、地址、密码或token作为查询字段。异步请求重试可能出现在原trace上下文下，业务幂等与trace去重是不同概念。

## 本机追踪容量与启动排错

本机全采样保留真实HTTP/RPC、消息处理和后台业务操作。Outbox扫描和定时任务只在没有查询到数据、没有外部调用、没有事件或错误时省略整组空闲span；查到待处理数据或发生错误时，包含此前扫描在内的本机链路继续导出。扫描缓冲最多64个span，超出后直接保留链路；普通请求不受此规则影响。

Collector的内存队列按字节计量，最多8MiB、2个发送协程；Collector和Jaeger均使用128个span的批次触发量、256个span的批次上限。Collector容器内存上限512MiB、`GOMEMLIMIT=256MiB`，memory_limiter硬上限384MiB、突增预留64MiB。Jaeger容器上限2GiB、`GOMEMLIMIT=1024MiB`，memory_limiter硬上限1280MiB、突增预留256MiB。内存限制器在批处理之前执行，避免下游不可用时积压失控。

Jaeger Badger显式配置72小时保留期和5分钟维护间隔，与此前默认保留策略一致。数据继续保存在`jaeger-data`卷中；启动和更新不会清空历史链路。过期数据按Badger原有策略回收，数据卷仍需足够空间进行写入和整理。

`traces export: ... data refused due to high memory usage`表示收集器拒收，必须同时检查下游Jaeger及磁盘，而不是只提高内存。若Jaeger日志出现`no space left on device`或不能写入`/var/jaeger/keys/LOCK`，Docker虚拟磁盘已满，可能同时影响数据库和新镜像构建。启动脚本会在发现该原因时于构建前终止，并给出对应诊断。

```sh
docker compose ps --all
docker compose logs --since=5m --tail=40 jaeger otel-collector
docker system df -v
# 确认占用后，交互确认清理一天前的闲置构建缓存；不删除镜像或数据卷。
docker builder prune --filter until=24h
sh start.sh
```

不使用删除`mysql-data`、`jaeger-data`或`docker compose down -v`的方式修复空间问题。启动脚本不自动执行任何缓存或数据清理。健康的中间件仍遵循增量启动规则；修改追踪配置后需要显式应用配置，再执行普通启动。

## 参考

- OpenTelemetry Go instrumentation: https://opentelemetry.io/docs/languages/go/instrumentation/
- otelgrpc StatsHandler: https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc
- Jaeger Badger: https://www.jaegertracing.io/docs/2.12/storage/badger/
- Jaeger2.12 Badger sample: https://github.com/jaegertracing/jaeger/blob/v2.12.0/cmd/jaeger/config-badger.yaml
