# iam-service

支持操作：`login, validate, refresh, logout, logout_all, admin_list, admin_summary, set_active`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：注册独立 RPC 业务方法并记录请求追踪，不包含业务分发。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `iam.transport.*` → `iam.usecase.*` → `iam.repository.*`，GORM回调再创建SQL Span。SQL绑定参数不展开到trace或日志；结构化业务入参和返回值按共享规则递归脱敏并限长。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 提供的明确客户端方法访问独立 gRPC 方法，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

演示账号只在 DEMO_MODE 开启时写入。密码为 bcrypt 摘要；随机令牌只保存 SHA-256 摘要；validate 同时检查会话撤销、期限和账号状态。refresh 在本地事务中轮换令牌。

`login/validate/refresh` 返回账号表原始 `CreatedAt` 对应的 `registered_at`（UTC RFC3339）。网关使用它判断营销弹窗的新老会员，而非会话签发时间或首次加载会员档案的时间；浏览器不能覆盖这个事实。

业务入口使用不含 `Action` 的 `platform.Request`；每项操作由独立公开方法实现权限、参数与业务校验。方法：`Login`、`Validate`、`Refresh`、`Logout`、`LogoutAll`。运输层仅维护操作到方法的注册表，HTTP 兼容命令不会进入业务层。


## 后台用户管理

`AdminList`按账号/ID、角色及注册日期服务端分页，默认20项，最多100，返回精确总数；凭据和会话不序列化。`AdminSummary`提供工作台账号角色总量及7/30天注册分布。两个读取均要求真实管理员。

`SetActive`要求普通目标账号、active、expected_active及原因，拒绝自身和管理员账号。事务内锁定账号、校验旧状态、更新可用状态并记录AccountChange；停用同时撤销全部现有会话，启用不恢复旧会话。后台不提供创建管理员、修改角色或密码功能。

登录方式新增 `login_methods, otp_send, otp_login, oauth_start, oauth_callback`；管理员 `policy_get, policy_upsert` 使用版本化开关。验证码、OAuth持久化一次性挑战与身份映射，普通新账号为 USR 雪花编号；管理员独立密码入口保留。渠道配置及短信契约见 `docs/login-providers.md`。
