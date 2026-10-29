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
- 管理员通过独立 `admin_list/upsert` 维护满减、折扣、多件优惠和直降规则。公开 `list` 仍只返回启用规则；管理员列表包含停用和系统规则，支持关键字、类型、状态与分页。
- 演示种子重复启动只补充缺失记录，不覆盖已有活动配置及已使用/预占额度。

所有内部写动作要求 system/admin 角色，并核对原资源用户归属。网关不公开内部动作。服务只写自己的 schema，所有 GORM 操作使用请求 context，SQL 模板追踪由共享平台统一记录。

测试：`go test ./services/campaign/...`。

## 独立业务方法

- `list` → `List`
- `evaluate` → `Evaluate`
- `reserve` → `Reserve`
- `confirm` → `Confirm`
- `cancel` → `Cancel`

业务方法统一接收不含动作字段的 `platform.Request`，权限与资源归属校验在方法内部执行。
`internal/transport/handler.go` 只维护边界路由注册表；模块通过 `Operations` 暴露独立操作，
`Handle` 仅兼容现有命令调用入口。确认与取消分别调用明确的仓储方法，事务和幂等规则保持不变。

跨服务调用使用 `platform.NewClients(...).Service.Method(ctx, platform.Request{...})`，不传递业务action。


## 后台规则配置

`AdminList` 与 `Upsert` 只允许管理员；内部预占/确认/取消权限及路径保持不变。新建活动用版本0，保存返回版本1，已有规则至少为版本1；迁移归一化旧规则版本。编辑必须带当前版本，迟到保存或新建编号撞已有规则返回Aborted。`held/used` 始终由资源账本维护，客户端传入值不采信；额度不得低于预占与已使用之和。已有参与记录后不能更改类型、金额门槛、最低件数及优惠算法，只能修改展示文案、总配额与启停；变更优惠应复制新活动。停用阻止新预占，已预占订单仍可确认或取消。

换购、秒杀和拼团现有规则依赖专用交易准入，为系统规则，在此只读，不允许通过通用编辑器重设。当前优惠规则没有独立时间段或可配置每人限额字段，管理表单不提供这类伪配置；每用户每活动一次沿用后端规则。
