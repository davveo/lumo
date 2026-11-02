# 独立领域RPC方法

当前30个领域服务共269项独立业务RPC，另提供受限的领域本地只读批量传输。服务名、动作与Go方法对应关系在
`api/mall/v1/service_methods.json` 维护，各服务README列出用例入口，protobuf契约位于 `api/mall/v1/`。
HTTP继续使用 `/api/v1/{service}/{action}` 白名单；内部RPC方法独立注册，不开放任意浏览器透传。

## 契约与业务入口

29个服务使用各自的protobuf服务，例如 `CheckoutService.Submit`、`InventoryService.Reserve`、
`CatalogService.BatchGet`。统一消息 `ServiceRequest` 不含action，只包含对象ID、可信主体、幂等键和
`payload_json`；`ServiceResponse` 保留JSON返回字段。业务字段仍由各自用例解码和校验，当前尚未为这29个服务的
每个操作定义逐字段protobuf消息。

对应Go用例签名为 `func(context.Context, platform.Request) (platform.Result, error)`。
每项业务拥有独立公开方法，方法内完成权限、归属、输入和状态校验；不设置Action后绕回万能函数。
仓储可复用明确的状态转换，但确认、取消等操作由明确的方法或目标状态参数确定。

订单保留 `OrderService` 的18个强类型方法、具体请求/响应及领域请求类型。订单装配通过 `RegisterRPC` 注册，
其他29个服务通过 `Module.Operations` 注册；订单映射见 [订单说明](../services/order/README.md)。

## 服务装配与客户端

29个服务的 `internal/transport/handler.go` 通过 `platform.NewMethodHandler` 声明操作到用例方法的注册表。
`Operations()` 给独立gRPC适配器使用；`Handle(ctx, Command)` 仅为现有HTTP/旧协议提供边界兼容。
字段映射、边界校验和Trace在transport完成，业务状态流转留在usecase。

跨服务业务调用使用明确客户端方法，例如：

```go
result, err := platform.NewClients(rpc).Inventory.Reserve(ctx, platform.Request{
    ID:     orderID,
    UserID: memberID,
    Role:   constants.RoleSystem,
    Data:   map[string]any{"order_id": orderID, "lines": lines},
})
```

事件消费一次处理一项恢复工作。Checkout的持久化Saga保留恢复模式和资源进度，
预占、确认和补偿以明确顺序逐项调用，不能遍历函数列表发起远程操作。

独立方法与旧Execute都校验内部RPC凭据，并沿用Consul健康实例发现、连接复用、负载均衡和Trace传播。
只有远端明确不存在专属服务/方法时才能回退Execute；鉴权、业务失败、网络错误和超时不重放。
自定义本机测试Caller仍可由平台薄适配转为旧命令，生产调用方优先使用独立gRPC。

## 循环与事务的远程调用约束

- 业务循环只处理本地计算、SQL及Outbox写入；禁止循环体直接或经辅助方法、回调、goroutine逐项调用RPC、HTTP或对象存储。固定跨域依赖使用显式调用，并按需要并行。
- 数据库事务及其回调内禁止远程调用。先在事务外读取远端快照，再在本地事务中锁定并校验状态、归属、金额、版本和截止时间。跨域修改沿用Saga、幂等键、Outbox和补偿，不依赖本地事务回滚远端。
- 批量接口必须真正一次跨服务传输。不得把逐项RPC移入批量方法，或在调用方拆块后逐块调用RPC。SQL可以在目标服务本地分块。
- 消息消费、连接处理与定时器属于基础设施调度；一项消费中的业务代码仍遵守上述约束。定时业务扫描只生成本地持久化工作，不逐条执行远程恢复。

`internal/architecture/remote_calls_test.go` 对本地方法、函数值和事务回调建立调用关系，检查循环和事务中的远程依赖；同时校验本地只读批量操作的实现没有远程依赖。新增远程客户端或事务包装器时需同步此检查。静态检查不能替代真实调用计数和数据库并发测试。

本次批量路径：

- 游客购物车合并：最多100行，先校验全部输入，再一次 `Catalog.BatchGet` 验证商品；缺失商品或远端失败不开始写购物车，数量按最大值合并以支持重试。
- 历史统计和对账：先收集订单ID，再一次强类型 `OrderService.BatchGetOrders`（1..500个ID，仅内部/管理员主体）。缺失订单省略；网络失败终止本轮，不伪造成业务差异。
- 退货：`Fulfillment.InspectReturns`（1..100张收货单）一次调用 `Inventory.RestockBatch`。库存批次在本地事务中全部成功或回滚，并按收货单幂等；已完成验收的重试不再次入库。收货单ID最多93字节，库存收货凭据ID最多120字节，SKU最多80字节。
- 弹窗优惠券：一次 `Coupon.TemplatesByIds` 读取最多5000个模板，超过上限明确失败。资格所需积分、成长值在进入行循环和领券事务前各读取至多一次。

网关的 `DomainService.ReadSnapshot` 将一个服务的1..64个本地只读操作合并为一次RPC，保留每项主体、权限检查、独立错误及取消信号。Catalog/Inventory单SKU读取先合并SQL批次；例如1200个SKU分成3个本地SQL读取，但仍只有一次RPC/服务。只读清单位于 `internal/platform/read_snapshot.go`，禁止添加有跨域依赖的操作。

Search、Recommendation、券资格、FAQ等跨域派生读取不进入本地快照循环；同类派生读取一次聚合中最多一个不同命令，多参数需求使用对应独立入口。前端自动批量器已将这些操作改走独立入口。此传输仅供可信网关/内部调用，不开放任意HTTP动作列表。

搜索每次请求最多读取一个策略限定的候选页和一次批量库存。库存/策略过滤后可能出现空的中间页：当 `has_more=true` 时保留 `next_cursor`，`total_exact=false`，客户端应继续分页；前台显示“当前批次暂无匹配商品，可继续加载”。扫描完候选目录后才返回准确总数。

订单到期、结算、履约和售后恢复扫描将工作写入本地Outbox；消费时按聚合ID处理一项，重查归属、状态/版本，过期工作直接忽略。拼团结果先持久化每张订单的独立工作。原有消费重试、租约、死信及业务幂等继续生效，未完成状态也会生成后续恢复工作。

## 增加或变更操作

1. 在usecase增加独立公开方法，完成权限、状态、事务、幂等与Outbox规则；仓储不接收Action。
2. 在transport注册该方法，并同步 `api/mall/v1/service_methods.json` 的动作、方法名和所属服务。
   新动作常量在 `internal/constants/actions.go` 维护。当前生成器读取该清单，注册表与清单需要同步。
3. 运行 `make proto` 更新专属proto、Go消息、gRPC客户端/服务器和平台适配器；不直接编辑生成文件。
   订单的强类型契约还需同步 `order.proto`、专属transport和 `internal/orderwire` 映射。
4. 补充直接方法鉴权、状态前提、幂等及失败补偿回归；仅当需公开HTTP时更新网关白名单及HTTP测试。

`python3 scripts/generate-service-rpc.py --check` 校验生成proto及平台适配器是否与清单同步；
该命令本身不会替代注册表一致性和实际gRPC测试。
`tests/service_methods_test.go` 的 `TestAllServicesExposeTheirCompleteNamedRPCContract` 核对全部服务装配与清单，
平台测试再验证独立方法确实不走Execute。相关检查包括：

```sh
GOWORK=off go test ./internal/platform ./services/... ./tests
sh scripts/test-frontend.sh
```

当前源码契约与隔离测试记录不等同于实际环境部署完成。部署后的32类应用服务及其实际副本、31类业务/API注册服务、
网关独立方法Trace与真实前端同源链路结果，需由主任务补录到 [部署验收记录](deployment-verification.md)。
