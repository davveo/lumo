# LUMO 商城完整工程

已将原商城前端接入 Go + GORM + gRPC 多服务项目，包含完整商城、独立管理页面、30 个领域服务与配套依赖。30 个领域服务独立运行，HTTP 网关接入前端，MySQL 按服务隔离 schema / 数据库账号，RabbitMQ 承接领域事件，Redis 用于缓存与秒杀准入。OpenTelemetry + Collector + Jaeger 将调用关联到 SQL。

## 放到桌面并一键启动

将交付的压缩包下载到“下载”目录后，在 macOS 终端执行：

```sh
mkdir -p ~/Desktop/lumo-mall-0.2.2
unzip -n ~/Downloads/lumo-backend.zip -d ~/Desktop/lumo-mall-0.2.2
cd ~/Desktop/lumo-mall-0.2.2/lumo-backend
sh start.sh
```

新版使用单独目录，避免无覆盖解压时继续运行旧版代码。若旧版有自定义 `.env`，先复制到新项目目录并参考新版 `.env.example` 补齐配置；Compose 项目名保持 lumo-mall，因此原命名卷会保留。

macOS 也可以直接双击项目根目录的 `start.command`。命令行可在任意工作目录执行脚本；它会自动定位完整工程。前端随网关镜像打包，启动电脑只需 Docker，不需要另外安装 Go 或 Node.js。

若已经在其他位置解压，可执行 `sh scripts/install-to-desktop.command` 将完整目录复制到桌面；脚本遇到已有同名目录会停止，避免覆盖。

请先启动 Docker Desktop（含 Compose v2）。完整环境默认启动 **30 个业务服务 + HTTP 网关 + MySQL、Redis、RabbitMQ、MinIO、Collector、Jaeger**；建议为 Docker 分配至少 8 GB 内存进行开发联调。镜像和依赖首次下载需要网络。`start.sh` 首次以受限文件权限从 `.env.example` 创建 `.env`，之后保留现有配置。

`start.sh` 首次构建并启动完整环境，以后根据源码与配置指纹，只更新变化的业务容器；运行中的中间件保持原容器。没有变化时不构建、不重建，已停止的服务会补启动。业务共用一次应用镜像构建，但使用明确服务列表更新，避免共享镜像标签改变导致其他容器一起更新。默认等待健康检查600秒，失败返回非零状态，数据库、MQ、Redis和追踪卷保留。`sh scripts/down.sh` 停止环境并保留数据。

首次使用新版脚本时没有历史部署指纹，会更新一次业务容器建立基线；中间件仍保留。以后请在同一工程目录执行，保留 `.local/start` 中的部署记录。

| 入口 | 地址 | 用途 |
| --- | --- | --- |
| 商城 | http://localhost:8080 | 商品、营销、购物车、下单、支付结果及个人中心 |
| 管理端 | http://localhost:8080/admin.html | 演示收款、发货、售后、退款、发票、审核和对账 |
| API 联调台 | http://localhost:8080/debug | 按权限发送业务请求并查看 trace_id |
| Jaeger | http://localhost:16686 | 按服务、操作或 trace_id 查询完整链路 |
| RabbitMQ 管理 | http://localhost:15672 | 领域队列、消费、重试与死信 |
| MinIO API / 控制台 | http://localhost:9001 / http://localhost:9002 | 私有附件上传与授权下载 |

上表为默认端口；脚本完成后查询 Docker 实际映射。`sh start.sh order` 只更新订单服务，`sh start.sh order payment` 更新指定业务服务；前端修改自动只更新gateway，共享平台代码修改更新所有受影响业务服务。`--no-build` 复用镜像，`--wait-timeout 900` 延长等待，只有显式 `--update-infra` 才会重建已运行的中间件。详细规则与验证范围见 [docs/docker-start.md](docs/docker-start.md)。

所有宿主机端口默认绑定 `127.0.0.1`。业务 RPC 与数据库/Redis 仅在 Compose 内部网络开放。默认账号和密钥仅用于本机演示环境，见 `.env.example`。

## 目录和服务分层

```text
lumo-backend/
├── api/mall/v1/                  # protobuf 契约与生成代码
├── cmd/
│   ├── server/                   # 单服务启动入口，SERVICE_NAME 决定服务
│   ├── gateway/                  # 同源商城、HTTP → gRPC、鉴权、动作白名单
│   └── devstack/                 # 不依赖 Docker 的开发联调入口
├── internal/
│   ├── bootstrap/                # 服务装配注册
│   ├── httpgateway/              # HTTP/API 与静态前端接入
│   ├── devstack/                 # 独立SQLite + TCP gRPC + 本地持久化事件派发
│   └── platform/                 # RPC、GORM、OTel、Outbox/Inbox、MQ
├── services/
│   ├── catalog/                  # 其余服务采用相同分层
│   │   ├── service.go            # composition root
│   │   └── internal/
│   │       ├── domain/           # 领域模型、状态、规则
│   │       ├── repository/       # GORM 持久化与事务
│   │       ├── usecase/          # 业务流程与跨服务 RPC 编排
│   │       └── transport/        # RPC action 校验和分发
│   └── ...                      # 共30个服务
├── deploy/                       # MySQL 初始化、Collector、Jaeger
├── scripts/                      # 一键启停与冒烟检查
├── docs/                         # API、追踪、启动和架构资料
├── tests/                        # 跨服务及完整 HTTP 链路集成测试
├── web/
│   ├── dist/                     # 可编辑商城/管理端完整前端与图片
│   └── tests/                    # 前端认证、页面事件与真实HTTP测试
├── compose.yaml
├── Dockerfile
├── start.sh                      # 完整环境一键启动
├── start.command                 # macOS双击入口
└── go.mod / go.sum
```

服务不直接写其他服务的表，也不在实现代码中导入其他服务的 usecase。同步交互通过 gRPC，异步交互通过持久化 Outbox / RabbitMQ；接口由 RPC 命令信封承载，业务 payload 在服务 transport 层校验。调用动作和示例见 [docs/api.md](docs/api.md)。

## 查看每层调用与 SQL

执行 `sh scripts/smoke.sh`，会创建一笔待支付演示订单并输出 trace_id。也可在 http://localhost:8080/debug 的联调台登录后发送请求；页面 API 错误和管理操作结果会显示链路编号。Jaeger 中的典型层级为：

```text
HTTP POST /api/v1/checkout/submit
  gateway.transport.Dispatch
    gateway.usecase.Forward
      gRPC client Execute
        checkout gRPC server Execute
          checkout.transport.*
            checkout.usecase.*
              checkout.repository.*
                gorm SQL query/update/transaction
              inventory gRPC client Execute
                inventory server → transport → usecase → repository → SQL
```

RPC、业务层、仓储层、SQL 都沿传入的 `context.Context` 关联。MQ 在 Outbox 中保存 W3C trace 上下文，消费者提取后建立消费 span，能追踪支付事件和后续履约。SQL 记录参数化模板、表、操作、耗时、受影响行数和错误，不将密码、令牌、地址等参数值写入 trace。详细查看方式见 [docs/observability.md](docs/observability.md)。

## 验证与当前边界

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
2. 在支付页创建支付单。支付页会等待服务端状态，不自行跳成功。
3. 另开 `/admin.html`，显式登录管理员，在“支付确认”处理演示收款。顾客页面观察到后端接纳支付后显示结果。
4. 管理端“仓库发货”确认出库、演示签收；顾客在订单页确认收货、评价或申请售后。
5. 售后批准、顾客寄回、仓库验收、发起退款、确认演示退款分别进行；原发票保留，退款产生红冲记录。

管理会话与顾客会话分别保存，不能在顾客页越权处理仓库或资金动作。服务器不可达时会显示连接错误并提供接口设置，不回退浏览器模拟数据。

前端单独托管时仍需可达的 HTTPS Go 网关；静态 Site 不承载 Go 服务或本机数据库。推荐直接运行此工程访问 localhost:8080。跨域配置见 [docs/cors.md](docs/cors.md)。

## 已实现与适配边界

| 能力 | 本版状态 |
| --- | --- |
| 普通交易、券积分库存与支付/退款流程 | 已有可运行实现与测试；支付渠道为显式演示适配 |
| RPC到分层SQL追踪 | HTTP、gRPC、业务、仓储、GORM和MQ上下文已接入；具体验证见verification.md |
| 拼团/秒杀/换购 | 提供基础状态与资源编排，正式活动政策需按业务确认；不等同于生产容量验收 |
| 换货 | 支持同SKU/同价SKU；价差需补差付款的换货明确拒绝，尚未实现补差收款 |
| 对账 | 内部演示账单核对；尚未接入真实支付平台账单文件 |
| 文件与对象存储 | MinIO真实预签名上传/下载，完成时核验对象大小与类型 |
| 搜索与推荐 | 首期RPC/SQL与规则召回，未接入Elasticsearch或训练推荐模型 |
| 外部仓库/物流/开票/短信 | 使用演示/规则适配，需接入选定供应商 |

MinIO公共预构建镜像在本次环境无法访问，因此 `deploy/minio.Dockerfile` 从官方release `RELEASE.2025-09-07T16-13-09Z` 对应固定commit `07c3a429bfed433e49018cb0f78a52145d4bedeb` 构建，首次启动会增加源码和Go依赖下载时间。源码与许可证来自官方仓库 https://github.com/minio/minio 。
