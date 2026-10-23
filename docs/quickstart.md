# 启动与日常开发

1. 下载新版压缩包，在桌面创建空目录 `lumo-mall-0.2.2`，解压到其中。
2. 启动 Docker Desktop，执行 `cd ~/Desktop/lumo-mall-0.2.2/lumo-backend && sh start.sh`。
   macOS 也可双击项目根目录的 `start.command`；旧命令 `sh scripts/up.sh` 和 `make up` 继续可用。
3. 访问 http://localhost:8080 使用商城；管理端为 `/admin.html`，接口联调台为 `/debug`，Jaeger 位于 http://localhost:16686。
4. 执行 `sh scripts/smoke.sh` 检查登录、商品读取、报价、下单和 trace_id。

启动无需在电脑上安装 Go、protoc、MySQL、Redis 、RabbitMQ 或 MinIO；这些由镜像构建和 Compose 提供。`smoke.sh` 额外需要 curl 和 Python 3，只用于解析和打印冒烟请求，不是服务运行依赖。

## 常用命令

```sh
sh start.sh --no-build
sh start.sh --wait-timeout 900
sh start.sh order
sh start.sh order payment
docker compose ps
docker compose logs -f gateway checkout inventory
sh scripts/down.sh
```

重复执行 `sh start.sh` 自动检测变化，保留已运行中间件。新版第一次运行会为业务容器建立部署基线，后续仅更新变化的业务服务。保留当前工程的 `.local/start`；删除记录会再次触发业务基线初始化。中间件配置修改默认不应用到运行中的容器，需要更新时显式使用 `sh start.sh --update-infra`。

`.env.example` 中的变量都有本地开发默认值。若修改 `GATEWAY_PORT` 等端口，需同时在浏览器和 `BASE_URL` 中使用新端口；启动脚本会按实际 Docker 端口映射输出访问地址。新版 `.env.example` 的 MinIO 上传来源自动引用 `GATEWAY_PORT`，签名地址引用 `MINIO_API_PORT`；如果沿用旧 `.env` 中写死的值，需要一起更新。DSN 用 UTC 并启用 `parseTime`。

MySQL 初始化在数据卷为空时创建30个 schema 和独立用户。以后修改数据库密码不会自动修改已存在卷中的用户密码，应通过数据库管理流程更新，不能只改 `.env` 并期待初始化脚本再次执行。项目启停脚本不会隐式删除卷。

## 本机 Go 调试

使用 Go 1.26 或更新版本。`make test`、`make build` 和前端测试脚本默认独立运行本模块，避免误用上级目录的 `go.work`；直接执行 Go 命令时，可加 `GOWORK=off`，或将本项目加入自己选择的工作区。

```sh
GOWORK=off go test ./...
GOWORK=off go build -buildvcs=false -o bin/server ./cmd/server
GOWORK=off go build -buildvcs=false -o bin/gateway ./cmd/gateway
```

单服务主要配置：`SERVICE_NAME`、`RPC_ADDR`、`DB_DRIVER`、`DB_DSN`、`REDIS_ADDR`、`AMQP_URL`、`OTEL_EXPORTER_OTLP_ENDPOINT`、`INTERNAL_RPC_TOKEN`、`DEMO_MODE`。Gateway 的 `RPC_TARGETS` 可使用 `catalog=127.0.0.1:9101,inventory=127.0.0.1:9102` 覆盖本机调试地址；业务服务使用 `CATALOG_RPC_ADDR`、`INVENTORY_RPC_ADDR` 等对应服务名环境变量。

Go 服务统一镜像通过 `SERVICE_NAME` 装配指定领域模块；每个 Compose 业务容器只启动一个服务。平台 worker 和领域定时任务跟随所属服务运行，不共享业务写库权限。

## 排查启动失败

- `docker compose config`：先检查 Compose 变量和配置。
- MySQL unhealthy：查看 `docker compose logs mysql`，确认磁盘、密码格式及初始化过程；DB 密码限制字母数字下划线，以确保初始化与 DSN 可解释。
- RPC unavailable：检查对应服务日志和健康状态，确认内部服务凭据一致。
- MQ 积压：查看 RabbitMQ 界面与服务错误；成功回放不能重复扣库存或重复生成订单。
- 无 trace：检查 Collector 和 Jaeger 日志，确认 OTLP endpoint；见 observability.md。

完整 Compose 环境使用多个独立进程，首次构建时应给 Docker 足够内存。不要用未验证的机器规格推断生产承载能力。

MinIO API为9001端口、控制台为9002端口，默认访问凭据在.env.example。媒体服务先登记文件并生成上传授权，完成上传后调用complete校验，随后调用download得到短时下载地址。MinIO镜像由固定官方源码构建，首次构建比拉取应用镜像更慢。

## 不启动 Docker 的前端开发联调

```sh
CGO_ENABLED=1 go run -buildvcs=false ./cmd/devstack
# 另一个终端运行真实前端/HTTP自动测试
sh scripts/test-frontend.sh
```

此开发入口需要 C 编译器，因为 SQLite 驱动使用 CGO；不能用 CGO_ENABLED=0 构建的 devstack 运行 SQLite。它为30个服务创建独立的本地数据库和真实 TCP gRPC 服务，并用持久化本地事件派发替代 RabbitMQ。不用于证明 Docker、MySQL、Redis、RabbitMQ、MinIO 或 Jaeger 已启动。默认数据写入 `.local/devstack`；可用 DEVSTACK_DATA_DIR 指定开发数据目录。完整依赖验证仍使用 Compose。

若之前已有同名项目，请将新版解压到一个新目录后比较合并，保留旧项目 `.env` 与数据库卷；不要把解压的无覆盖提示当作升级已成功。
