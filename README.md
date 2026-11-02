# LUMO 商城完整工程

已将原商城前端接入 Go + GORM + gRPC 多服务项目，包含完整商城、独立管理页面、30 个领域服务与配套依赖。30 个领域服务独立运行，通过 Consul 注册、健康检查与服务发现连接；独立Nginx前端同源代理到HTTP网关，MySQL 按服务隔离 schema / 数据库账号，RabbitMQ 承接领域事件，Redis 用于缓存与秒杀准入。OpenTelemetry + Collector + Jaeger 将调用关联到 SQL。

新增账号安全中心、游客购物车合并、经营分析、员工岗位权限、CSV/XLSX 商品导入、实时站内消息、采购补货、物流轨迹与异常件；入口、配置和使用限制见 [八项功能说明](docs/feature-expansion.md)，外部渠道协议见 [物流适配](docs/carrier-adapter.md)。

## 放到桌面并一键启动

将交付的压缩包下载到“下载”目录后，在 macOS 终端执行：

```sh
mkdir -p ~/Desktop/lumo-mall-0.2.2
unzip -n ~/Downloads/lumo-backend.zip -d ~/Desktop/lumo-mall-0.2.2
cd ~/Desktop/lumo-mall-0.2.2/lumo-backend
sh start.sh
```

新版使用单独目录，避免无覆盖解压时继续运行旧版代码。若旧版有自定义 `.env`，先复制到新项目目录并参考新版 `.env.example` 补齐配置；Compose 项目名保持 lumo-mall，因此原命名卷会保留。

macOS 也可以直接双击项目根目录的 `start.command`。命令行可在任意工作目录执行脚本；它会自动定位完整工程。前端使用独立 `web` 镜像与容器，启动电脑只需 Docker，不需要另外安装 Go 或 Node.js。

若已经在其他位置解压，可执行 `sh scripts/install-to-desktop.command` 将完整目录复制到桌面；脚本遇到已有同名目录会停止，避免覆盖。

请先启动 Docker Desktop（含 Compose v2）。完整环境默认启动 **30 个业务服务 + HTTP 网关 + 独立前端 + Consul、MySQL、Redis、RabbitMQ、MinIO、Collector、Jaeger**；建议为 Docker 分配至少 8 GB 内存进行开发联调。镜像和依赖首次下载需要网络。`start.sh` 首次以受限文件权限从 `.env.example` 创建 `.env`，之后保留现有配置。

`start.sh` 首次构建并启动完整环境，以后根据源码与配置指纹，只更新变化的业务容器；运行中的中间件保持原容器。没有变化时不构建、不重建，已停止的服务会补启动。30个业务服务和网关共用后端镜像，前端单独构建；使用明确服务列表更新，避免共享镜像标签改变导致其他容器一起更新。默认等待健康检查600秒，失败返回非零状态，数据库、MQ、Redis和追踪卷保留。`sh scripts/down.sh` 停止环境并保留数据。

首次使用新版脚本时没有历史部署指纹，会更新一次业务容器建立基线；中间件仍保留。以后请在同一工程目录执行，保留 `.local/start` 中的部署记录。

旧 `.env` 仅有 `GATEWAY_PORT` 时，启动脚本先私有备份，再将原商城端口保留为 `WEB_PORT` 并拆出API端口；MinIO上传来源的旧变量引用同步迁移，账号密码保留。完整规则见下方部署说明。

| 入口 | 地址 | 用途 |
| --- | --- | --- |
| 商城 | http://localhost:8080 | 商品、营销、购物车、下单、支付结果及个人中心 |
| 管理端 | http://localhost:8080/admin.html | 演示收款、发货、售后、退款、发票、审核和对账 |
| API 网关 | http://localhost:18080/api/v1 | 直接访问业务API，商城也通过8080同源代理访问 |
| API 联调台 | http://localhost:18080/debug | 按权限发送业务请求并查看 x-trace-id 响应头 |
| Consul 注册中心 | http://localhost:8500/ui/ | 查看30个领域服务、网关及其实例健康状态 |
| Jaeger | http://localhost:16686 | 按服务、操作或 trace_id 查询完整链路 |
| RabbitMQ 管理 | http://localhost:15672 | 领域队列、消费、重试与死信 |
| MinIO API / 控制台 | http://localhost:9001 / http://localhost:9002 | 私有附件上传与授权下载 |

上表为默认端口；脚本完成后查询 Docker 实际映射。`sh start.sh order` 只更新订单服务，`sh start.sh order payment` 更新指定业务服务；`sh start.sh web` 只更新前端；前端修改自动只构建/更新 `web`，共享Go平台代码只更新受影响的后端服务。`--no-build` 复用镜像，`--wait-timeout 900` 延长等待，只有显式 `--update-infra` 才会重建已运行的中间件。详细规则与验证范围见 [docs/docker-start.md](docs/docker-start.md)。

所有宿主机端口默认绑定 `127.0.0.1`。业务 RPC 与数据库/Redis 仅在 Compose 内部网络开放。默认账号和密钥仅用于本机演示环境，见 `.env.example`。

## 目录和服务分层

```text
lumo-backend/
├── api/mall/v1/                  # protobuf 契约与生成代码
├── cmd/
│   ├── server/                   # 单服务启动入口，SERVICE_NAME 决定服务
│   ├── gateway/                  # HTTP → gRPC、鉴权、动作白名单、联调台
│   └── devstack/                 # 不依赖 Docker 的开发联调入口
├── internal/
│   ├── bootstrap/                # 服务装配注册
│   ├── httpgateway/              # HTTP/API 与页面聚合；开发运行器可显式接入静态目录
│   ├── devstack/                 # 独立SQLite + TCP gRPC + 本地持久化事件派发
│   └── platform/                 # RPC、GORM、OTel、Outbox/Inbox、MQ
├── services/
│   ├── catalog/                  # 其余服务采用相同分层
│   │   ├── service.go            # composition root
│   │   └── internal/
│   │       ├── domain/           # 领域模型、状态、规则
│   │       ├── repository/       # GORM 持久化与事务
│   │       ├── usecase/          # 业务流程与跨服务 RPC 编排
│   │       └── transport/        # RPC 请求映射、校验与兼容适配
│   └── ...                      # 共30个服务
├── deploy/                       # MySQL 初始化、Collector、Jaeger
├── scripts/                      # 一键启停与冒烟检查
├── docs/                         # API、追踪、启动和架构资料
├── tests/                        # 跨服务及完整 HTTP 链路集成测试
├── web/
│   ├── Dockerfile                # 独立Nginx前端镜像
│   ├── nginx/                    # 静态缓存与同源API代理
│   ├── dist/                     # 可编辑商城/管理端完整前端与图片
│   └── tests/                    # 前端认证、页面事件与真实HTTP测试
├── compose.yaml
├── Dockerfile
├── start.sh                      # 完整环境一键启动
├── start.command                 # macOS双击入口
└── go.mod / go.sum
```

服务不直接写其他服务的表，也不在实现代码中导入其他服务的 usecase。同步交互通过 gRPC，异步交互通过持久化 Outbox / RabbitMQ。30个领域服务均提供独立RPC方法，当前共162项。29个领域服务以不含动作字段的 `platform.Request` 调用独立公开用例，通过专属protobuf服务注册这些方法；订单保留15个强类型RPC及明确请求/响应。跨服务业务调用使用 `platform.NewClients(...).Inventory.Reserve` 等明确方法。`DomainService.Execute` 仅保留边界兼容，不承载业务分发。契约、扩展和生成方式见 [docs/rpc-methods.md](docs/rpc-methods.md)，HTTP动作及示例见 [docs/api.md](docs/api.md)。

Compose 默认启用 Consul。业务服务与网关启动时注册实例，Consul 分别检查 gRPC Health 和网关 `/readyz`；RPC 只从 Consul 获取健康实例并持续刷新地址。进程退出时注销，Consul 重启后实例会定期重新注册。未设置 `CONSUL_HTTP_ADDR` 的独立 Go 开发环境继续使用静态地址；显式的 `*_RPC_ADDR` / `RPC_TARGETS` 保留覆盖能力。配置与排查见 [docs/consul.md](docs/consul.md)。

30个领域服务可分别增加副本：每个容器只装配自身模块，使用所属服务的数据库账号、schema及MQ队列。各副本注册自身地址和唯一实例ID，调用方复用gRPC连接、每2秒刷新Consul健康地址，并通过round_robin分摊到READY实例。共享后端镜像用于统一构建，各服务的进程、配置及副本数量独立；前端有单独的镜像和构建上下文。Trace的 `service.instance.id` 和日志的 `instance_id` 对应Consul实例ID，便于检查请求落到了哪个副本。

## 查看每层调用与 SQL

执行 `sh scripts/smoke.sh`，会创建一笔待支付演示订单并输出 `x-trace-id` 响应头中的链路编号。也可在 http://localhost:18080/debug 的联调台登录后发送请求；页面 API 错误和管理操作结果会显示链路编号。Jaeger 中的典型层级为：

```text
HTTP POST /api/v1/checkout/submit
  gateway.transport.Dispatch
    gateway.usecase.Forward
      gRPC client mall.v1.CheckoutService/Submit
        checkout gRPC server Submit
          checkout.transport.*
            checkout.usecase.*
              checkout.repository.*
                gorm SQL query/update/transaction
              inventory gRPC client mall.v1.InventoryService/Reserve
                inventory server → transport → usecase → repository → SQL
```

RPC、业务层、仓储层、SQL 都沿传入的 `context.Context` 关联。MQ 在 Outbox 中保存 W3C trace 上下文，消费者提取后建立消费 span，能追踪支付事件和后续履约。HTTP、RPC、transport/usecase及MQ调用记录 `lumo.request`、`lumo.response` 与规范错误码，入参和返回值递归脱敏并限制长度；SQL继续记录参数化模板、表、操作、耗时和行数，不展开绑定值。详细查看方式见 [docs/observability.md](docs/observability.md)。

## 验证与当前边界

源码排版以120列为目标，长结构体、参数、条件、字典及调用按层级拆行。首次安装开发工具需要 Go 1.26+、Node.js 22+、Python 3.10+：

```sh
make format-tools  # 安装固定版本开发工具，不修改业务 go.mod
make format        # 统一 Go、JS/CSS、Python 和 Shell 排版
make format-check  # 只检查，不写源码；不符合时返回非零
```

工具版本由 `package.json` 固定，排版规则见 `.editorconfig`、`.prettierrc.json` 和 `pyproject.toml`。Go 使用 golines + gofmt；前端使用 Prettier；Python 使用 Black；Shell 使用 shfmt。依赖只保存在开发目录，不进入应用镜像。生成的 protobuf、前端常量及演示资产由原生成器维护。SQL、URL、模板等原始字符串不为行宽强行切断，HTML 标签属性可以换行，但文本节点空白需保留。

```sh
GOWORK=off go test ./...
GOWORK=off go build -buildvcs=false ./cmd/server ./cmd/gateway
sh scripts/test-frontend.sh
sh scripts/smoke.sh
docker compose ps
docker compose logs --tail=100 checkout inventory payment
```

交付环境的具体测试与验证结果见 `docs/verification.md`（由集成验收生成）。如该文件未包含 Docker 实际启动记录，不能据此认定已经执行过完整 Compose 启动。

真实支付、退款、仓库和开票渠道尚未提供商户凭据，因此使用显式的 **DEMO_MODE** 和管理员演示动作；不会触发真实扣款、开票或物流。搜索采用可运行的商品/库存 RPC 查询路径，未伪装成已接入 Elasticsearch；文件服务接入MinIO，提供真实预签名上传、完成校验和授权下载。原商城的商品、账户、购物车、订单、券积分、活动、售后、票据与工单均已改用后端数据；页面不再伪造支付、成团或退款成功。前端通过同源网关使用 107 个 HTTP 动作所覆盖的业务能力，逐项对应见 [docs/frontend-api-coverage.md](docs/frontend-api-coverage.md)。其中 pricing/quote 与 checkout/quote 是同一报价能力的两种入口，页面走后者，不重复创建无用报价。

默认演示用户 `demo / Lumo@2026`，管理员 `admin / Admin@2026`；RabbitMQ 默认 `mall / lumo_mq_local_2026`。关闭 `DEMO_MODE` 后不能使用演示支付、开票动作。服务内部使用共享 RPC 凭据，面向生产部署时应替换为服务身份 / mTLS 与密钥管理；不能直接将整个内部 RPC 网络发布到公网。

本版 API 使用整数分表示金额，quote_id / order_id / reservation_id 等由服务端生成或受幂等约束。订单、支付、退款、库存和票据状态分开保存，详情和未覆盖政策以各服务测试与 `docs/verification.md` 为准。

## 体验完整链路

1. 在商城使用顾客账号登录，选择规格、加入购物车、保存地址并提交服务端报价后的订单。
2. 在支付页点击“模拟支付”，创建支付单并提交模拟成功回执。已创建的待支付单可继续点击“模拟支付成功”；此流程不会产生真实扣款。
3. 服务端接纳支付后，通过事件确认库存、优惠券和积分，更新购物车并生成待发货记录。顾客页面查询到订单接纳支付后显示结果。模拟提交仅在 `DEMO_MODE` 开启时允许，且只能操作本人的支付单；管理员仍可在 `/admin.html` 的“支付确认”处理演示收款。
4. 管理端“仓库发货”确认出库、演示签收；顾客在订单页确认收货、评价或申请售后。
5. 售后批准、顾客寄回、仓库验收、发起退款、确认演示退款分别进行；原发票保留，退款产生红冲记录。

管理会话与顾客会话分别保存，不能在顾客页越权处理仓库或资金动作。服务器不可达时会显示连接错误并提供接口设置，不回退浏览器模拟数据。

前端单独托管时仍需可达的 HTTPS Go 网关；静态 Site 不承载 Go 服务或本机数据库。推荐直接运行此工程访问 localhost:8080。跨域配置见 [docs/cors.md](docs/cors.md)。

## 已实现与适配边界

| 能力 | 本版状态 |
| --- | --- |
| 普通交易、券积分库存与支付/退款流程 | 已有可运行实现与测试；支付渠道为显式演示适配 |
| RPC到分层SQL追踪 | HTTP、gRPC、业务、仓储、GORM和MQ上下文已接入；具体验证见verification.md |
| 拼团/秒杀/换购/满减/折扣/多件优惠/直降 | 已接入活动入口、服务端报价及配额编排，订单保留规则快照；不等同于生产容量验收 |
| 换货 | 支持同SKU/同价SKU；价差需补差付款的换货明确拒绝，尚未实现补差收款 |
| 对账 | 内部演示账单核对；尚未接入真实支付平台账单文件 |
| 文件与对象存储 | MinIO真实预签名上传/下载，完成时核验对象大小与类型 |
| 搜索与推荐 | 首期RPC/SQL与规则召回，未接入Elasticsearch或训练推荐模型 |
| 外部仓库/物流/开票/短信 | 使用演示/规则适配，需接入选定供应商 |

MinIO公共预构建镜像在本次环境无法访问，因此 `deploy/minio.Dockerfile` 从官方release `RELEASE.2025-09-07T16-13-09Z` 对应固定commit `07c3a429bfed433e49018cb0f78a52145d4bedeb` 构建，首次启动会增加源码和Go依赖下载时间。源码与许可证来自官方仓库 https://github.com/minio/minio 。

## 2026-10-30 工程优化

- 网关仅接受可信代理的转发 IP。Compose 默认用 `TRUSTED_PROXY_HOSTS=web` 解析前端容器的当前地址，每 5 秒刷新；直连 API 不信任转发头。其他部署可显式设置 `TRUSTED_PROXY_CIDRS`，默认不信任任何网段。多级代理从右向左取首个不可信跳点，客户端不能用伪造的头部前缀改变限流身份。
- 密码登录在 bcrypt 前使用 IAM 数据库中的频控：每账号每分钟最多 10 次，每 IP 每分钟最多 60 次；连续失败 5 次后按 1、2、4 秒递增退避，最多 64 秒。成功登录清除失败退避。不存在或停用的账号也执行密码散列比较。登录输入在后台页面重绘时保留在内存中，不进入公开状态、存储或 HTML，离开登录页及登录结束时清除密码。
- 搜索使用商品服务的稳定游标继续扫描，不再将前 100 个候选当成全部匹配项。请求可带 `cursor`，响应带 `has_more`、`next_cursor`、`total_exact`；仅扫描完成时返回准确 `total`。每次最多扫描 20 批，稀疏库存筛选可能返回空的部分页，此时仍提供继续游标，页面显示“继续加载”。搜索条件或策略版本变化后需重新开始。
- 订单、支付、售后和发票列表支持 `limit`（1–100）、`offset`（0–10000）和 `cursor`，响应包含 `total`、`has_more` 与 `next_cursor`。深页使用游标；游标与服务、账号、角色绑定，查询始终应用服务端归属条件。订单的强类型 protobuf 契约保留并扩展分页字段。顾客历史页面按游标读取后续记录。
- RabbitMQ 按集中订阅清单建立所有服务队列，在精确绑定就绪后移除旧的 `#` 绑定，不删除队列消息。业务事件仍要求可路由和发布确认；目前无订阅方的四类信息事件（商品变更、行为接纳、评价发布、退货验收完成）保留生产者持久记录，允许经 broker 确认后无接收方。未知事件仍按路由错误处理。
- `npm run build:web` 从可编辑的 `web/dist` 生成 `web/build`：每个入口各一个启动 JS/CSS 包，文件名包含内容哈希，并生成 gzip 文件。装修与分类编辑器进入管理页面时才加载；公开首页配置独立加载。Nginx 对哈希资源使用长期不可变缓存，HTML 重新验证。Docker 前端镜像自动执行构建，运行镜像不包含 Node。
- `.github/workflows/ci.yml` 包含 Go/前端/生成一致性/排版、临时 MySQL 与 RabbitMQ、实际 Nginx 代理/压缩/缓存，以及 Chromium 下的下单、演示支付、售后退款、编辑器懒加载检查。Playwright 浏览器检查使用构建后的前端和独立数据库。
- Go 下载缓存不再由 Git 跟踪。`make`、前端和浏览器测试、工具安装通过 `scripts/go.sh` 保留显式的外部缓存配置，并纠正项目内或相对缓存路径。IDE 的 GOPATH 也应设为项目外的绝对路径（例如 `$HOME/go`）。旧缓存本次归档到忽略的 `.local/cache-backup`，没有删除磁盘内容；Docker 上下文和源码指纹排除嵌套模块缓存。

```sh
make test
make test-web
make build-web
npx --no-install playwright install chromium
make test-browser
make format-check
```

基础设施测试显式启用后只创建独立 scratch schema/vhost：MySQL 使用 `LUMO_MYSQL_ADMIN_TEST`、`LUMO_MYSQL_SEARCH_TEST`、`LUMO_MYSQL_PROJECTION_TEST`，地址可用 `LUMO_MYSQL_TEST_ADDR` 覆盖；RabbitMQ 使用 `LUMO_AMQP_TEST=1`、`LUMO_TEST_AMQP_URL` 和 `LUMO_AMQP_MANAGEMENT_URL`。真实 Nginx 测试使用 `LUMO_TEST_WEB_IMAGE` 指定独立测试镜像。这些是测试环境参数，不需要写入商城 `.env`。
