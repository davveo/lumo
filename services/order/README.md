# 订单服务

订单使用独立的 `OrderService` protobuf/gRPC 契约，不再在业务层通过 `Command.Action` 分发。此次迁移覆盖订单的15个操作，其余领域服务仍保留原协议。

## 分层

- `api/mall/v1/order.proto` 定义独立RPC及明确请求/响应。订单金额、状态、版本、团标记及时间有具体字段；报价和地址是不可变历史文档，以JSON字节保存，避免大整数被通用Struct转为浮点数。
- `internal/transport/rpc.go` 实现生成的服务器接口，只做字段转换、格式校验和调用。HTTP及旧Execute由 `handler.go`、`legacy.go` 的薄适配层兼容，不包含业务状态流转。
- `internal/usecase/requests.go` 定义Go请求类型；查询、创建、支付、关闭、拼团、履约、退款及事件处理按业务分文件。每个方法只负责一个用例，不接收protobuf请求或万能Command。
- `internal/usecase/service.go` 仅保留依赖、权限和共同事务流程；具体方法提供自己的状态前提与事件。repository继续负责行锁、持久化和同事务Outbox。

`OnEvent` 直接调用相应的用例，不通过字符串action绕回RPC入口。过期订单Tick继续委托checkout取消，沿用支付关闭及库存、券、积分释放流程。

## 权限与兼容

所有RPC必须携带内部服务凭证；Actor由可信调用方传入，HTTP网关先校验登录并重建身份。查询列表、详情及完成订单沿用本人权限，其他变更要求system/admin；原有行锁、幂等、事件ID、退款收据及事务回滚保持不变。

现有JSON HTTP路径及响应字段保留。平台旧Caller的订单调用优先使用生成客户端，只有服务端明确缺少新服务/方法时回退Execute；业务失败、凭证错误、网络错误及超时均不重放。新代码可直接使用 `mallv1.OrderServiceClient`，无需构造action。

## 增加操作

先定义请求/响应与独立RPC，再增加对应的Go用例方法及transport映射；需要旧HTTP兼容时只在边界增加一条适配路由。鉴权、Trace、行锁与Outbox继续复用共同设施，避免把操作内容重新堆进dispatcher。在已有状态回归基础上验证新操作的权限、幂等、事件及回滚边界。

执行 `make proto` 生成代码；验证使用 `go test ./services/order ./internal/platform ./tests` 和订单/平台的race测试，前端兼容链路由 `sh scripts/test-frontend.sh` 覆盖。测试使用独立数据库，不修改运行订单。
