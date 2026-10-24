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
- 通用优惠：`reduction-demo` 为商品满300元减30元，`discount-demo` 为9折，`quantity-discount-demo` 为满2件8.5折，`direct-demo` 为每单立减20元。四种活动各有1000单配额，每用户每活动限享一次；显式选择 `activity_id` 才参与，普通订单不会自动套用。
- 通用活动公开 `name`、`description`、`pay_rate_bps` 与 `min_quantity`。`evaluate` 接收 `subtotal_cents` 和 `quantity`；正式报价与提交阶段的金额、件数由服务端从SKU及不可变报价计算，不采信浏览器自报优惠或总件数。
- 百分比活动的优惠额以整数分向下取整，固定减额须小于商品金额，不能将应付抹零；门槛、最低件数、活动状态和剩余额度均需满足。`evaluate` 返回规则与优惠明细、`eligible` 和不满足原因，通用活动使用 `v2` 政策快照，原活动继续使用 `v1`。
- 每单使用一种活动，不与秒杀、拼团或换购同时参与；通用优惠可继续叠加满足条件的券及积分，最终应付仍须大于零。价格服务保留活动名称、类型、折扣比例与最低件数快照，订单和退款沿用该报价。
- 活动额度与真实 SKU 库存是两份不同资源，不能以活动准入替代 inventory 预占。
- 同一活动每用户只允许一笔 RESERVED/CONFIRMED 请求；取消未付款预占后可重试，已确认资格默认不因退款恢复。
- evaluate 只计算资格与优惠，reserve 才裁决最终配额。持久化预占与取消墓碑防重复/悬挂。
- 当前提供示例规则，运营可配置规则编辑/版本发布尚需后续管理接口扩展。
- 演示种子重复启动只补充缺失记录，不覆盖已有活动配置及已使用/预占额度。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/campaign/...`。
