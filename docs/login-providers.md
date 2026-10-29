# 登录渠道配置与联调

后台“系统设置 → 登录方式”保存开关、版本、修改原因；服务端在每次新登录时执行相同规则。前台只调用公开 `iam/login_methods` 读取可用方法，不读取密钥。管理员后台密码入口独立保留，避免关闭会员密码登录后失去管理入口。

## 手机验证码

配置 `IAM_OTP_HASH_KEY` 为独立随机服务端密钥，并配置 HTTPS 的 `SMS_WEBHOOK_URL` 与 `SMS_WEBHOOK_TOKEN`。Compose 已将这些变量仅传入 IAM。

IAM 向 webhook 发送 POST JSON：`{phone,code,ttl_seconds,request_id}`，并使用 `Authorization: Bearer <SMS_WEBHOOK_TOKEN>`。服务端适配器应向实际短信供应商发送短信；只有在确认供应商接受请求后返回 2xx。IAM 不跟随重定向，8 秒超时，错误时不能登录；日志不应记录验证码、手机号或授权头。`request_id` 可作为供应商侧幂等键。

`otp_send` 返回 `challenge_id`、有效期与重试间隔；`otp_login` 传入 `phone`、`challenge_id`、`code`，成功后得到正常 IAM 会话。验证码只存 HMAC 摘要，5 次错误后失效；每个手机号每日最多 10 次、每个网关连接来源每日最多 50 次，并受后台间隔限制。网关使用连接来源地址，忽略浏览器伪造的 `_client_ip`，不会直接信任外部 `X-Forwarded-For`。反向代理部署应规划可信客户端 IP 解析或供应商风控，当前同一代理出口共享 IP 额度。

`DEMO_MODE=true` 且未配置 webhook 时响应包含 `demo_code`，页面明确提示未发送短信；正式模式不会返回验证码。请勿将演示模式用于正式会员登录。

## 微信与 QQ

微信配置 `WECHAT_LOGIN_APP_ID`、`WECHAT_LOGIN_APP_SECRET`、`WECHAT_LOGIN_REDIRECT_URI`；QQ 配置对应的 `QQ_LOGIN_*` 三个变量。回调地址是前台 `index.html` 的 HTTPS 地址，域名与路径必须在对应平台登记，不能含 hash。开发演示允许 HTTP localhost 回调。

前台生成浏览器 proof、调用 `oauth_start`，保存返回的一次性 state 并跳转官方授权地址。授权返回 index.html 后，前台把 `code`、`oauth_state`、`proof` 发给 `oauth_callback`；IAM 消费数据库挑战，向固定官方 API 换取账号身份，再签发本商城会话。成功或失败后授权查询参数从地址栏移除。state 有效期 5 分钟、单次消费；`oauth_start` 同来源每日最多 200 次。

微信使用网站扫码登录 `snsapi_login`；QQ 使用授权码流程，并通过 `/oauth2.0/me` 绑定 OpenID 和本应用编号。不接收浏览器提供的 OpenID/角色，也不会仅凭同手机号、邮箱或昵称合并已有账号。已禁用会员无法通过另一种登录方式绕过停用。

缺少凭据或回调不合法时，后台开关不可用，服务端也拒绝启用。外部平台账号与权限仍需实际申请；当前测试用替代 HTTP 传输核验回调、应用归属和单次消费，没有进行真实账号授权联调。
