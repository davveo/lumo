# campaign-service / 活动规则与配额

公开 `list/evaluate`；内部 `reserve/confirm/cancel`。

## 目录分层

- `service.go`：组合依赖，注册迁移、消费事件与任务。
- `internal/transport`：RPC action 白名单、分发与 transport span。
- `internal/usecase`：权限、业务流程、跨服务RPC与 usecase span。
- `internal/repository`：只访问本服务 GORM 数据库，事务与 repository span。
- `internal/domain`：数据模型与业务状态。

## 已实现规则

- 初始规则：`addon-coffee` 门槛100000分、优惠6000分；`flash-demo`配额20；`group-demo`配额30。
- 活动额度与真实 SKU 库存是两份不同资源，不能以活动准入替代 inventory 预占。
- 同一活动每用户只允许一笔 RESERVED/CONFIRMED 请求；取消未付款预占后可重试，已确认资格默认不因退款恢复。
- evaluate 只计算资格与优惠，reserve 才裁决最终配额。持久化预占与取消墓碑防重复/悬挂。
- 当前提供示例规则，运营可配置规则编辑/版本发布尚需后续管理接口扩展。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/campaign/...`。
