# 独立领域RPC方法

当前30个领域服务共162项独立RPC。服务名、动作与Go方法对应关系在
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

订单保留 `OrderService` 的15个强类型方法、具体请求/响应及领域请求类型。订单装配通过 `RegisterRPC` 注册，
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

事件和定时恢复直接调用对应方法。Checkout的持久化Saga可以保留内部恢复模式和资源步骤，
预占、确认与补偿列表保存明确的 `platform.Operation` 函数值，不通过跨服务action字符串选择调用。

独立方法与旧Execute都校验内部RPC凭据，并沿用Consul健康实例发现、连接复用、负载均衡和Trace传播。
只有远端明确不存在专属服务/方法时才能回退Execute；鉴权、业务失败、网络错误和超时不重放。
自定义本机测试Caller仍可由平台薄适配转为旧命令，生产调用方优先使用独立gRPC。

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
