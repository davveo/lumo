# content-service

支持操作：`list, get, upsert (admin)`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：注册独立 RPC 业务方法并记录请求追踪，不包含业务分发。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `content.transport.*` → `content.usecase.*` → `content.repository.*`，GORM回调再创建SQL Span。调用入参与返回值使用统一脱敏、限长预览，SQL只记录模板及行数。

本服务只写自己的数据库schema。跨服务通过 `platform.NewClients` 提供的明确客户端方法访问独立 gRPC 方法，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

## 首页分类导航

分类导航是持久化运营内容，固定ID为 `storefront-categories`、kind为 `storefront`。公开 `content/get` 返回现有content文档，其body为配置JSON；管理员通过 `content/upsert` 发布修改，沿用既有登录与管理员校验。分类名、展示文案、分组、搜索词、推荐标记、相关品牌、排序及开关都由该文档提供，数组顺序即展示顺序。

```json
{
  "version": 1,
  "groups": [{
    "id": "configured", "category": "配置品类", "label": "配置品类", "title": "配置品类", "icon": "bag", "enabled": true,
    "shortcuts": [{"label": "全部配置品类", "query": ""}],
    "rows": [{"title": "精选分组", "query": "精选", "items": [{"label": "精选商品", "query": "精选", "featured": true}]}],
    "brands": [{"label": "品牌展示名", "brand": "实际商品品牌"}]
  }]
}
```

`Migrate`在非演示环境也以InsertOnce创建初始配置，初始数据位于 `internal/usecase/category_navigation.json`，包含原20个大类、93个分组和586个细分词；默认品牌来源于已有种子商品定义，不从首屏商品临时推断。已有管理员文档不会因重启或演示初始化被覆盖。内容payload使用MySQL MEDIUMTEXT容纳合法配置，不将配置上限误设为TEXT列无法保存的容量。

配置body最大256KiB；最多40个大类、每类20个分组/8个快捷入口/16个品牌，每组40个词，全局最多400个分组及2000个词。大类ID仅允许64字符内的小写标识符，类目名称及ID不能重复；展示文字最多80字、搜索词最多120字，拒绝控制字符、未知字段、不支持的图标和错误字段类型。enabled/featured可省略但显式null无效；省略enabled表示启用。row.query及item.query为空时分别默认取title及label，快捷入口query允许为空。

groups必须存在但允许空数组以关闭目录；group.rows及row.items必须存在，允许空数组。shortcuts/brands可省略并归一为空数组。隐藏分类仍保留在配置中，管理员可以重新启用；所有配置（包括隐藏内容）均执行同样校验。

## 人群营销弹窗

管理员通过 `popup_list`、`popup_upsert` 和 `popup_configure` 管理弹窗。弹窗配置、访客首次会话、投放凭证和频控记录使用独立的 `content_popup_*` 表，公开 `content/get` / `content/list` 无法读取这些配置或指定用户名单。更新已有弹窗必须提交列表返回的 `version`；版本冲突返回 Aborted，避免后台多人编辑互相覆盖。

配置包括草稿/发布/暂停、优先级、开始结束时间、允许页面、展示内容和操作。人群预设支持所有人、游客、首次/回访游客、会员、新/老会员和成长会员；预设之间取或，成长值/积分范围、允许用户、排除用户和兴趣品类条件取且，排除名单优先。新会员天数按 IAM 账号实际注册时间计算，不能用首次创建的会员资料时间替代；可信资料读取失败时不能满足依赖该资料的范围条件。购物、支付等未允许页面不会参与投放。

网关使用私有的 `popup_decide` / `popup_event` / `popup_claim` RPC，内容服务要求 system 身份；浏览器不能提供可信用户画像。决策最多返回一个优先级最高的可用弹窗，券模板通过 `coupon/templates_by_ids` 每批最多 50 个读取，结束、未开始、领完、不符合条件的券会跳过。仅返回展示内容、券信息和短时投放凭证，不向顾客返回完整人群规则。

决策不占用曝光次数，实际展示前确认 impression。凭证绑定用户（游客绑定匿名访客）和浏览器会话，5 分钟内有效，并由首次成功确认的 view_id 独占；同视图重试幂等，关闭后不能重新打开。数据库事务先锁用户频控行，再锁活动和凭证，随后原子更新全局会话、活动会话、用户累计/北京时间自然日上限、展示冷却、关闭冷却及统计，支持多服务实例同时投放。暂停、更新配置、全局关闭、时间窗结束和凭证超时都会阻止待展示的弹窗。

领券必须来自已曝光且属于当前登录用户的凭证，券模板由服务端配置确定。`coupon/claim` 负责最终校验时间、剩余额度、积分/成长值与重复领取；调用失败不会记录已领取，重试不会重复发券或累计领取统计。点击和关闭事件也按凭证幂等统计。默认全局每会话最多 1 个弹窗，最小展示间隔 60 秒；单活动可配置累计/每日次数、同会话一次、展示/关闭冷却和领取后停止。

非演示环境不会自动创建营销活动。演示模式使用 InsertOnce 提供首次访客、新会员、老会员及成长会员四个不同的首页活动，每个用户每活动最多一次，每天/每会话最多一次，冷却 24 小时；重启不会覆盖管理员修改，不会自动发券。

业务入口使用不含 `Action` 的 `platform.Request`；每项操作由独立公开方法实现权限、参数与业务校验。方法：`List`、`Get`、`Upsert`、`PopupList`、`PopupUpsert`、`PopupConfigure`、`PopupDecide`、`PopupEvent`、`PopupClaim`。运输层仅维护操作到方法的注册表，HTTP 兼容命令不会进入业务层。


## 内容模板与商城装修

营销内容layout支持coupon、announcement、image、welcome、member、campaign六种，配合image_url、badge及既有主题。后端只接收安全商城图片或无凭据HTTPS地址，海报必须有图片；按钮保持商城内白名单。选模板不绕过真实人群和频控条件，新增配置默认保存为草稿。

`storefront-main`文档补齐页头标语与搜索提示、最多5张轮播（主视觉加4张附加图）、0或3–30秒轮播间隔、6项首页模块开关、公告及页脚客服信息。新字段执行明确类型/长度/图片/链接校验，其它编辑器字段保持兼容；前端保存前合并最新文档的已修改键。未配置时使用公开默认值，首次后台保存才发布，不在启动时覆盖运营内容。
