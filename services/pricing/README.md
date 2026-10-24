# pricing-service

## 独立业务方法

- `quote` → `Quote`
- `get` → `Get`

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。`Quote` 保存不可变报价快照，`Get` 校验报价归属后读取。

跨服务调用使用 `platform.NewClients(...).Service.Method(ctx, platform.Request{...})`，不传递业务action。
