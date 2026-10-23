# 商城前端

`dist/` 是原 LUMO 商城的完整可编辑前端源代码，使用原生 HTML、CSS、JavaScript，无打包依赖；图片位于 `dist/assets/`。页面风格和现有电商功能保留，业务数据改由后端服务保存。

Docker 网关直接提供这些文件和 `/api/v1/`，因此本地访问 `http://localhost:8080` 即可体验完整商城。管理端为 `/admin.html`，原接口联调台移至 `/debug`。

## 数据约定

- `api-client.js` 统一 JSON 命令、Bearer、幂等键、超时及 Trace ID。默认同源，不自动连接其他主机。
- `auth.js` 使用 IAM 登录、验证、刷新和撤销会话；旧原型的浏览器订单、积分、地址不导入后端。
- `app.js` 读取服务器商品和真实 SKU，金额以整数分转换显示；通过服务器不可变报价提交订单。订单成功页只在查询到后端接纳支付后显示。
- `member.js`、`services.js`、`promotions.js` 等模块保持视图模型，异步刷新服务器状态。库存不再根据浏览器订单推算，退款不自动当成退货入库。
- 商品规格对应独立 SKU，定价和库存由服务端校验。顾客不能调用内部预占、提交库存、支付裁决或手工确认收款。
- 管理端使用独立的管理员登录会话处理演示支付、发货、售后、退款、审核和发票，顾客会话不会被自动替换成管理员。

## 单独部署

优先将本目录与 Go 网关同源部署。若单独发布前端，页面连接失败时可在“接口设置”填写 HTTPS 网关地址；网关需配置该前端来源的 `CORS_ALLOWED_ORIGINS`，对象上传则需配置 `MINIO_CORS_ORIGINS` 和实际可达的 `MINIO_PUBLIC_ENDPOINT`。更换接口地址会清除旧认证状态并要求重新登录。静态 Site 本身不运行 Go、MySQL 或消息队列。

## 测试

```sh
sh scripts/test-frontend.sh
go test ./tests -run TestHTTPStorefrontEndToEnd -count=1 -v
```

前一个命令在项目根目录执行，要求 Go、C 编译器、Node.js 22+ 和 curl。测试会创建临时数据库，启动真正的 TCP gRPC 服务及 HTTP 网关，按独立测试进程清理数据。它使用本地持久化事件派发器；完整 Docker 环境仍使用 RabbitMQ、MySQL、Redis、MinIO、Collector 和 Jaeger。

接口对应关系见 `../docs/frontend-api-coverage.md`，验收范围和基础设施限制见 `../docs/verification.md`。
