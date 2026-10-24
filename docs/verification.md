# 集成验收记录

日期：2026-10-22。交付版本：0.2.2（Docker增量启动版，沿用0.2.0应用层及0.2.1部署配置验收结果）。

本文件保留当时的验收事实和限制，包含旧网关静态部署、端口及通用RPC的历史说明。当前独立前端和30服务专属RPC以 [部署说明](docker-start.md)、[独立RPC方法](rpc-methods.md) 为准；后续实际部署结果见 [部署验收记录](deployment-verification.md)。

## 已实际执行并通过

| 检查 | 结果与范围 |
| --- | --- |
| `go test ./...` | 全包通过；当前源码有63个Test函数，其中1个真实MinIO测试默认按需跳过。最后新增CORS测试定向回归后，交付前全量测试再次通过 |
| `go test -race ./...` | 全包通过，未发现数据竞争 |
| 最后一次仓储时间字段修正后的 `go test -race ./services/catalog ./services/media/...` | 通过；覆盖会员/商品等跨服务流程与MinIO适配单测，新增CreatedAt保留回归通过 |
| `go vet ./...` | 全包通过 |
| `go mod verify` | 所有下载模块校验通过；依赖锁定于go.mod/go.sum |
| Linux amd64、arm64，`CGO_ENABLED=0 go build -buildvcs=false -trimpath` | server和gateway均编译通过；与容器应用构建方式一致，arm64为交叉编译验证 |
| Compose `config -q` | 38个配置项通过，包含30个领域服务、网关、6类基础组件和卷初始化 |
| shell语法与目录边界 | 启停/冒烟/桌面安装脚本通过sh -n；每个服务有domain/repository/usecase/transport目录 |
| MinIO官方固定commit源码构建 | 本机Go1.26.10、CGO关闭编译通过；未修改官方源码 |
| `sh scripts/test-frontend.sh` | 前端语法、36项认证/接口断言、活动与发现/编辑VM回归、81项真实HTTP页面检查、47项会员/售后真实HTTP检查均通过 |
| 最终网关修改定向检查 | CORS严格来源名单8种场景、HTTP网关和devstack恢复测试race通过；gateway/devstack的vet通过，Compose配置再次通过 |
| 原生开发运行器 | CGO_ENABLED=1编译并运行SQLite devstack，不能将CGO关闭的devstack视为可运行验证；Docker server/gateway使用MySQL路径，不受此限制 |

本机编译/测试使用Go1.26.10。测试使用独立SQLite数据库，**不表示完成了真实MySQL容器验收**。

## 前后端接口与页面链路

原商城完整前端已位于 `web/dist`，由 Go 网关同源提供；另有独立登录的 `/admin.html` 管理页面，原接口联调台位于 `/debug`。107个HTTP动作对应的全部业务能力有页面入口：106个直接接入，pricing.quote通过checkout.quote代理使用同一报价服务；内部RPC资源操作保持不可从浏览器直接调用。逐项来源见 `frontend-api-coverage.md`。该对照表示接口接入，不代表每个动作都完成了真实外部渠道验收。

`tests/http_storefront_integration_test.go::TestHTTPStorefrontEndToEnd` 启动真实HTTP网关、30个TCP gRPC服务和各自独立数据库，记录30个服务域均有成功调用。主流程覆盖登录续期/退出、资料地址、真实SKU及购物车合并、领券签到、服务器报价和稳定幂等提交、支付确认和履约、签收评价、退货验收入库/退款、原票和红冲、对账消息工单、秒杀异步请求、两个不同账号分别支付后成团。

前端测试运行实际HTML声明的16个脚本，使用轻量DOM/事件环境加真实HTTP后端，包含完整启动、19类页面渲染、顾客和独立管理会话、真实按钮/表单处理与支付结果同步。会员售后测试验证部分退货不会误标整单已退、验收前不会补库存，退款后保留原票。认证测试还验证账号切换时旧异步响应不能覆盖新账号数据、401不会误撤销另一管理会话、失败操作不会伪造本地成功记录。

公开页面还在受管浏览器中检查了12款商品、分页、真实规格价格/库存、搜索、活动及登录表单，未发现商城脚本错误。受保护业务页面的上述验证采用VM事件/HTML与真实HTTP；未将其表述成浏览器内登录后的全流程点击验证。

支付后购物车清理由Cart服务消费OrderPaymentAccepted完成；相同事件重复投递不会再次扣减。重启恢复测试验证支付事务提交、事件尚未投递时重启，持久化Outbox仍可推动订单READY且库存只分配一次。

## 真实gRPC集成链路

`tests/commerce_integration_test.go` 启动30个实际TCP gRPC服务，每个服务连接自己的临时数据库；生产服务间仍使用同一gRPC调用代码。

1. 账号密码登录、服务端令牌验证、退出后令牌失效；跨用户订单读取被拒。
2. 商品查询、加入购物车、后端报价、库存/券/积分预占、提交订单。
3. 相同幂等键与相同请求返回原订单；换地址重用同键被拒。
4. 显式演示支付确认后，订单接纳收款、资源提交、订单进入可履约状态。
5. 申请并演示开具发票，保存原票号。
6. 显式演示退款成功后，订单累计实退金额更新、抵扣积分返还，原发票保留并新增一条调整记录。
7. 未付款取消后释放预占，后续为关闭订单创建支付被拒。
8. 两位用户分别付款：首位保持待成团；第二位付款成团后，两笔订单各自放行，库存分配总量为2。

事件集成测试从实际持久化Outbox读取事件、恢复W3C上下文，并确定性调用消费者。**它验证业务和追踪衔接，不代替真实RabbitMQ网络、确认与重投的运行验收**。

## 追踪证据

已验证真实gRPC客户端/服务端、transport、usecase、repository与SQL的父子span关系。SQL记录模板、表、操作、行数与耗时，不记录绑定参数；平台测试验证仓储span确为SQL父span。

`docs/examples/trace-example.json` 导出自当前支付/退款/开票集成运行，包含551个同trace span、19个参与服务、1个根节点、0个缺失父节点，可在Jaeger界面使用Import功能导入。30是集成集群与HTTP测试覆盖的服务总数，并非每一笔订单都调用30个服务。仅含生成的演示上下文与脱敏SQL模板；此样例使用测试内存Exporter，不能证明本环境已成功写入Jaeger容器。

重新导出样例：

```sh
UPDATE_TRACE_FIXTURE=1 go test ./tests -run TestCommerceRPCAndTrace -count=1 -v
```

## 当前环境未能执行的验收

- Docker daemon的socket访问被执行环境拒绝，未运行完整 `docker compose up`；真实MySQL初始化、RabbitMQ网络消费、Collector到Jaeger存储、所有容器健康状态仍需在Docker开发机验证。
- 官方MinIO进程在初始化枚举网络接口时遇到 `netlinkrib: operation not permitted`，尚未监听S3端口。因此真实POST policy上传、Stat和下载的端到端测试尚未通过；进程和临时数据已清理，未修改官方MinIO源码以绕过限制。
- 支付、退款、物流、仓库、开票和短信的正式供应商未指定。当前本机资金/WMS/票据流程是明确标记的演示适配器，不代表真实资金或票据渠道已接通。
- 搜索/推荐为RPC/SQL与规则实现，价差换货、外部账单对账、生产活动策略和容量验收见README的适配边界。

在正常Docker环境执行 `sh scripts/up.sh`、`sh scripts/smoke.sh`，按 `docs/observability.md` 检查Jaeger实际链路；文件实联使用 `services/media/storage_integration_test.go` 的可选测试。所有测试命令和源码已包含在项目中。

## 0.2.1 Docker一键启动补充

新增根目录start.sh和macOS start.command，旧scripts/up.sh转发到统一入口。16项启动脚本隔离回归全部通过，覆盖Docker/Compose/daemon缺失、配置/构建/健康等待失败、首次创建与保留.env、拒绝执行配置文本、空格路径、no-build、等待参数、真实端口输出及兼容入口；不操作真实容器。三个入口均通过sh -n。

真实Compose CLI校验默认端口，以及GATEWAY_PORT=8181、MINIO_API_PORT=9101、MINIO_CONSOLE_PORT=9102的组合；媒体签名端口、MinIO浏览器来源与健康依赖一致。生成器在临时目录输出与提交配置语义相同。

本次更改仅涉及启动、部署配置、文档和脚本测试；没有重新运行无关的应用层回归。Docker守护进程限制仍在，因此镜像构建和整套运行待实际Docker开发机验收。


## 0.2.2 Docker增量启动补充

根目录start.sh改为按服务源码和实际Compose配置检测变化；新增scripts/start-incremental.sh编排、scripts/source-fingerprint.sh指纹工具。按每个容器自己的部署镜像ID判断，不会因共享镜像标签变化而重建其他业务服务。默认保留运行中的全部中间件；明确update-infra才更新中间件。

新增14项源码指纹回归，覆盖领域/网关/平台归属、增删改名、非运行文件排除、空格路径、SHA工具回退和异常路径拒绝，全部通过。启动隔离回归使用持久化fake Docker，验证25种场景：首次及重复启动、订单及前端连续修改、共享代码、demo种子归属、单服务配置、保留中间件、明确中间件更新、缺失/停止容器、初始化任务只执行一次、no-build保留待更新状态、构建/健康失败重试、构建中编辑、引擎及解析后的项目隔离、旧入口/动态端口、dotenv保留/不执行、无效参数及Docker能力预检。

真实Compose v2.35.1再次验证canonical YAML中的项目/镜像解析、38项配置指纹，以及8181/9101/9102端口变更造成对应配置指纹变化。五个启动相关shell文件通过sh -n。部署记录.local已从Docker构建上下文排除。

这些是隔离脚本回归和真实CLI配置检查，未运行真实Docker容器；本次没有修改或重新验收商城业务代码。当前环境的Docker daemon限制仍在，完整容器启动与实际健康检查需在Docker开发机验收。


## 启动地址显示与1045排查补充

启动成功后集中输出可访问地址标题及真实映射端口；同时发布IPv4/IPv6时取首条绑定，将0.0.0.0转换为127.0.0.1、[::]转换为[::1]。两个地址显示定向回归通过，覆盖三个启动入口、自定义端口、双栈通配绑定、指定IP及未映射端口。当前启动测试总数26项，本次仅重跑相关2项，其他25项的增量行为验证沿用上次记录。

新增mysql-auth-recovery.md，说明points等业务账号1045的诊断和保留数据修复步骤，失败提示中提供文档路径。本次未改动数据库账号或业务代码，也未在实际MySQL容器执行恢复SQL；用户环境的修复仍需使用其现存root凭据执行。
