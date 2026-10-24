# 业务枚举

此目录是后端与前端业务字符串的单一维护位置。按领域拆分文件，角色、服务名、RPC动作及事件使用公共分组；不同状态机即使字符串相同，也使用各自的领域分组。新增值应保留已有数据库、JSON、RPC和事件协议的大小写，不顺便重命名状态。

```go
// enum:OrderStatus
const (
    OrderStatusCreating = "CREATING"
)
```

Go代码引用 `constants.OrderStatusCreating`。保持无类型字符串常量，避免动态结果字典的类型比较发生变化。字段名、提示文案、SQL关键字、HTML/DOM操作以及外部库定义的协议不属于业务状态枚举。

`// enum:分组名` 标记供生成器读取，每个成员必须使用分组名作为前缀并明确写出字符串字面量。`go generate ./internal/constants` 从这些定义生成 `web/dist/constants.js`，前端通过 `window.LumoConstants.OrderStatus.Creating` 引用；分组及整个对象均被冻结，两个HTML入口先加载该文件。请修改Go源定义后重新生成，不手动修改生成文件。

纯前端状态投影位于 `frontend.go`，例如将多个订单协议状态映射到同一个“待付款”视图；它们与服务状态保持独立。旧协议兼容值显式保留在各领域分组中，不因当前后端不再产生该值而删除。

在项目根目录校验生成结果：

```sh
go run ./internal/constants/generate -source internal/constants -output web/dist/constants.js -check
sh scripts/test-frontend.sh
```

前端测试脚本会先检查生成文件是否过期。Go及前端源码检查还会阻止生产代码重新内联已定义的持久化状态；测试和既有业务fixture保留原始协议字面量，用于验证本次提取没有改变线上值。
