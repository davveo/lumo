# content-service

支持操作：`list, get, upsert (admin)`。

入口 `service.go` 只组装依赖；各层依次为：

1. `internal/transport`：RPC命令入口和请求追踪。
2. `internal/usecase`：权限、归属、输入校验、业务规则与跨服务RPC编排。
3. `internal/repository`：唯一GORM访问层，本地事务、条件查询及Outbox持久化。
4. `internal/domain`：本服务拥有的数据模型与索引键。

所有调用逐层传递 `context.Context`，层Span为 `content.transport.*` → `content.usecase.*` → `content.repository.*`，GORM回调再创建SQL Span。调用入参与返回值使用统一脱敏、限长预览，SQL只记录模板及行数。

本服务只写自己的数据库schema。跨服务通过 `platform.Caller` 访问 gRPC，禁止直接读写对方业务表。网关验证会话后填入调用主体，服务再次检查用户资源归属与管理员角色。

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
