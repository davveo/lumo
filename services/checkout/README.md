# checkout-service

## 独立业务方法

`quote` → `Quote`；`submit` → `Submit`；`get` → `Get`；`paid` → `Paid`；`cancel` → `Cancel`。

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。确认与取消分别调用明确的仓储方法，事务和幂等规则保持不变。

`Submit`、`Cancel` 和 `Paid` 分别承载提交、取消和支付确认。持久化 Saga 的创建补偿、资源释放与重试
继续由私有 `resumeWorkflow` 状态机恢复，避免任务恢复再次执行外部提交或取消检查；支付事件及支付恢复调用 `Paid`。
