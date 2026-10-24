# Docker启动与增量更新

在macOS打开Docker Desktop，等其启动完成后，双击根目录 `start.command`，或执行：

```sh
sh start.sh
```

Linux使用当前账号可访问的Docker Engine与Compose v2。脚本使用 `/bin/sh`，可从任意工作目录通过完整路径调用，支持工程路径含空格。启动电脑无需安装Go、Node.js、Python、MySQL或消息队列；源码指纹使用系统自带的SHA-256工具。

## 自动识别更新范围

首次启动完整环境包括30个业务服务、HTTP网关，以及Consul、MySQL、Redis、RabbitMQ、MinIO、OpenTelemetry Collector和Jaeger。前端打包进网关，与API同源。所有业务容器共享一份应用镜像，但脚本始终按明确服务列表更新容器。

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

反复执行且代码、配置未变时，不构建镜像、不重建运行容器。缺失的容器会创建，停止的容器在配置和代码未变时补启动。运行中的Consul、MySQL、Redis、RabbitMQ、MinIO、Collector和Jaeger不会被默认重启。已经成功退出的卷权限初始化任务也不会每次重跑。

首次使用新版脚本，没有旧镜像与源码的对应记录，需要更新一次业务容器建立部署基线；中间件仍然保留。记录位于 `.local/start/<Docker引擎>-<Compose项目>/`，仅保存源码指纹、配置指纹和镜像ID。保留这个目录；删除它或换到新的工程目录会重新建立业务基线。

共享镜像标签变化不会让其他业务容器一起更新。脚本对照每个容器自己的已部署镜像ID，成功构建后只替换选中服务；只有健康检查成功才提交部署记录。构建期间源码变化会停止更新，避免错误标记版本。

领域服务扩容后，增量脚本会检查选中服务的全部实例，更新及补启动时显式保留当前副本数；不会把3个副本恢复为1个。扩缩容使用 `docker compose up -d --no-deps --no-build --scale catalog=3 catalog` 等命令，细节见 [consul.md](consul.md)。更新使用Compose重建选中实例，尚不提供逐副本无中断发布；需要此能力时应交由具备滚动发布策略的编排器管理。

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

停止脚本与启动脚本固定使用本工程的 `compose.yaml` 和 `.env`，不受外部 `COMPOSE_FILE` 或 `COMPOSE_ENV_FILES` 干扰。停止前需要保留可读取的 `.env`，确保使用启动时的项目配置。

## 默认地址

| 入口 | 地址 |
| --- | --- |
| 商城前端与API | http://localhost:8080 |
| 管理端 | http://localhost:8080/admin.html |
| API联调台 | http://localhost:8080/debug |
| Consul注册中心 | http://localhost:8500/ui/ |
| Jaeger调用链 | http://localhost:16686 |
| RabbitMQ管理台 | http://localhost:15672 |
| MinIO API | http://localhost:9001 |
| MinIO控制台 | http://localhost:9002 |

默认顾客账号 `demo / Lumo@2026`，管理员账号 `admin / Admin@2026`。支付等外部渠道仍为演示适配器。

`DEMO_MODE=true` 时，catalog和inventory各自增量初始化完整品类样例：除原12件外，新增1,838件商品、3,676个SKU，覆盖20品类的全部586个细分类。重复启动不覆盖已编辑商品、上下架状态和实际库存；关闭演示模式不自动创建这些扩展样例，也不删除已有数据。只更新这部分与前端资源时可执行 `sh start.sh catalog inventory gateway`，保留已有数据库和中间件。修改后端初始分类词条后，可用 `go generate ./internal/democatalog` 重建派生商品计划，用 `python3 scripts/generate-demo-category-assets.py` 重建本地示意图；这些开发工具不是日常启动依赖。

启动成功后，各访问地址下会输出对应登录信息。商城、管理端与联调台在 IAM 启用演示模式时显示初始演示账号；关闭演示模式时提示使用已有账号。RabbitMQ显示运行容器配置的初始化账号密码，MinIO显示运行容器的Access Key和Secret Key，Consul与Jaeger标注默认无需账号密码。已在应用内修改过的密码以实际账号为准。

编辑 `.env` 的 `GATEWAY_PORT`、`CONSUL_UI_PORT`、`JAEGER_PORT`、`RABBITMQ_UI_PORT`、`MINIO_API_PORT`、`MINIO_CONSOLE_PORT` 可调整映射；完成时脚本从Docker查询实际地址。成功提示后集中列出“可访问地址”，包括商城、管理端、联调台、Consul、Jaeger、RabbitMQ和MinIO；同时发布IPv4/IPv6时每个入口只输出一条URL，通配监听地址转换为本机可访问地址。新版示例中MinIO上传来源引用网关端口，签名端点引用MinIO API端口。沿用旧配置时同步其中写死的 `MINIO_CORS_ORIGINS`、`MINIO_PUBLIC_ENDPOINT`；浏览器跨域部署见 `cors.md`。中间件映射调整需显式更新中间件容器。

Consul 持久化数据位于 `consul-data`，沿用同一 Compose 项目名的既有卷。30个领域服务与网关启动后会注册实例和健康检查，RPC查询健康实例，进程退出时注销；Consul重启后存活服务会周期性重新注册。运行中的Consul仍遵循默认保留规则；缺失或停止时会补启动。详细配置与检查命令见 [consul.md](consul.md)。

## 失败处理和验证范围

Docker不可用、Compose能力不足、配置错误、构建失败或健康等待失败均非零退出，保留容器和数据卷。失败的部署不推进业务基线，修复后再次执行即可；未应用成功的服务会再次识别出来。运行但不健康的中间件会报错并提示日志，不自动重启。MySQL报1045时按 [mysql-auth-recovery.md](mysql-auth-recovery.md) 核对账号和当前密码，保留数据卷修复。

每个引擎及Compose项目的启动任务使用独立锁，阻止同一目录并发执行。正常退出和INT/TERM/HUP会清理锁；若被强制杀死，先核对报错中的lock/pid对应进程已结束，再删除该lock目录后重试。

首轮需要下载基础镜像和依赖，MinIO需要从固定官方源码构建；Jaeger固定为2.12.0，Collector固定为0.124.1，并各自加入静态HTTP健康探针。只有业务变化时不会触发这些中间件镜像构建；需要补启动且对应本地镜像标签缺失时，仅构建所需镜像。应用和全部中间件均须通过健康检查；running但无健康检查的旧容器会明确报错。探针检查服务就绪，追踪写入仍需按Trace ID在Jaeger中验证。

增量启动与源码指纹回归使用持久化模拟Docker；这些测试覆盖服务选择、健康状态和部署记录，不替代真实容器及追踪写入验收。开发者可用Python 3执行回归；部署本身不需要Python：

```sh
python3 tests/test_docker_start.py
python3 tests/test_source_fingerprint.py
```
