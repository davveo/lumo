# Docker启动与增量更新

在macOS打开Docker Desktop，等其启动完成后，双击根目录 `start.command`，或执行：

```sh
sh start.sh
```

Linux使用当前账号可访问的Docker Engine与Compose v2。脚本使用 `/bin/sh`，可从任意工作目录通过完整路径调用，支持工程路径含空格。启动电脑无需安装Go、Node.js、Python、MySQL或消息队列；源码指纹使用系统自带的SHA-256工具。

## 自动识别更新范围

首次启动完整环境包括30个业务服务、HTTP网关，以及MySQL、Redis、RabbitMQ、MinIO、OpenTelemetry Collector和Jaeger。前端打包进网关，与API同源。所有业务容器共享一份应用镜像，但脚本始终按明确服务列表更新容器。

| 修改内容 | 默认更新范围 |
| --- | --- |
| `services/order/**`等领域代码 | 对应服务，如order |
| `web/dist/**`、`cmd/gateway/**`、`internal/httpgateway/**` | gateway |
| `cmd/server/**`、`internal/bootstrap/**` | 30个领域服务 |
| `internal/democatalog/**` | catalog、inventory |
| `api/**`、`internal/platform/**`、go.mod/go.sum、Dockerfile | 所有业务服务和gateway |
| 单个业务服务的Compose配置 | 对应业务服务 |
| 文档、测试、开发运行器或启动记录 | 不触发业务更新 |
| 运行中的中间件代码或配置 | 保留原容器，提示显式更新 |

新增且尚未归属分类的 `internal` 包按共享代码处理，以免漏更新。指纹包含文件相对路径及内容，新增、删除和改名也能识别；不依赖Git提交或文件修改时间。

反复执行且代码、配置未变时，不构建镜像、不重建运行容器。缺失的容器会创建，停止的容器在配置和代码未变时补启动。运行中的MySQL、Redis、RabbitMQ、MinIO、Collector和Jaeger不会被默认重启。已经成功退出的卷权限初始化任务也不会每次重跑。

首次使用新版脚本，没有旧镜像与源码的对应记录，需要更新一次业务容器建立部署基线；中间件仍然保留。记录位于 `.local/start/<Docker引擎>-<Compose项目>/`，仅保存源码指纹、配置指纹和镜像ID。保留这个目录；删除它或换到新的工程目录会重新建立业务基线。

共享镜像标签变化不会让其他业务容器一起更新。脚本对照每个容器自己的已部署镜像ID，成功构建后只替换选中服务；只有健康检查成功才提交部署记录。构建期间源码变化会停止更新，避免错误标记版本。

## 常用命令

```sh
# 首次完整启动；以后自动增量更新
sh start.sh

# 明确只更新订单服务，或指定多个业务服务
sh start.sh order
sh start.sh order payment

# 使用现有镜像；未构建源码仍保持待更新状态
sh start.sh --no-build

# 较慢机器延长健康等待，镜像构建时间不计入此等待
sh start.sh --wait-timeout 900

# 明确应用中间件配置或镜像更新，会重建中间件容器
sh start.sh --update-infra

# 旧入口自动使用同一套增量逻辑
sh scripts/up.sh
make up

# 查看状态和日志
docker compose ps --all
docker compose logs --tail=80 gateway order mysql

# 停止并保留数据卷
sh scripts/down.sh
```

指定服务名表示明确要求更新这些业务容器，即使没有源码变化；共享代码或其他服务的修改不在本次指定范围内，下次不带服务名执行会继续检测。新装完整环境应先执行一次不带服务名的 `sh start.sh`。

`--no-build` 不会把当前未构建代码记为已部署。已有旧源码基线时继续保留；无法确认镜像来源时标记待构建，下次正常启动仍会构建并更新。

默认保留已有 `.env`，由Compose解析，不作为shell执行。中间件配置变化不会自动应用到运行中的中间件；需要应用时使用 `--update-infra`。持久化MySQL的账号密码变化仍需按数据库管理流程执行，重建容器不会重新初始化已有数据卷。

## 默认地址

| 入口 | 地址 |
| --- | --- |
| 商城前端与API | http://localhost:8080 |
| 管理端 | http://localhost:8080/admin.html |
| API联调台 | http://localhost:8080/debug |
| Jaeger调用链 | http://localhost:16686 |
| RabbitMQ管理台 | http://localhost:15672 |
| MinIO API | http://localhost:9001 |
| MinIO控制台 | http://localhost:9002 |

默认顾客账号 `demo / Lumo@2026`，管理员账号 `admin / Admin@2026`。支付等外部渠道仍为演示适配器。

编辑 `.env` 的 `GATEWAY_PORT`、`JAEGER_PORT`、`RABBITMQ_UI_PORT`、`MINIO_API_PORT`、`MINIO_CONSOLE_PORT` 可调整映射；完成时脚本从Docker查询实际地址。成功提示后集中列出“可访问地址”，包括商城、管理端、联调台、Jaeger、RabbitMQ和MinIO；同时发布IPv4/IPv6时每个入口只输出一条URL，通配监听地址转换为本机可访问地址。新版示例中MinIO上传来源引用网关端口，签名端点引用MinIO API端口。沿用旧配置时同步其中写死的 `MINIO_CORS_ORIGINS`、`MINIO_PUBLIC_ENDPOINT`；浏览器跨域部署见 `cors.md`。中间件映射调整需显式更新中间件容器。

## 失败处理和验证范围

Docker不可用、Compose能力不足、配置错误、构建失败或健康等待失败均非零退出，保留容器和数据卷。失败的部署不推进业务基线，修复后再次执行即可；未应用成功的服务会再次识别出来。运行但不健康的中间件会报错并提示日志，不自动重启。MySQL报1045时按 [mysql-auth-recovery.md](mysql-auth-recovery.md) 核对账号和当前密码，保留数据卷修复。

每个引擎及Compose项目的启动任务使用独立锁，阻止同一目录并发执行。正常退出和INT/TERM/HUP会清理锁；若被强制杀死，先核对报错中的lock/pid对应进程已结束，再删除该lock目录后重试。

首轮需要下载基础镜像和依赖，MinIO需要从固定官方源码构建；只有业务变化时不会触发MinIO构建。需要补启动MinIO且本地镜像标签缺失时，仍会构建其镜像。应用、MySQL、Redis、RabbitMQ和MinIO有健康检查；Jaeger和Collector目前检查容器运行，追踪写入仍需按Trace ID在Jaeger中验证。

已执行shell语法检查、持久化模拟Docker的增量启动回归、源码指纹回归，以及真实Compose v2.35.1配置/指纹解析核对。执行环境没有可用Docker守护进程，未将这些检查表述为真实容器运行验收。开发者可用Python 3执行回归；部署本身不需要Python：

```sh
python3 tests/test_docker_start.py
python3 tests/test_source_fingerprint.py
```
