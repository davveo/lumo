# 部署配置核对

日期：2026-10-22。此记录区分静态配置、镜像元数据核对与真实容器运行。

## 已完成

- `docker-compose -f compose.yaml config -q` 通过，包含30个领域服务、gateway、MySQL、Redis、RabbitMQ、MinIO、Collector、Jaeger和一个卷权限初始化任务，共38项。
- `sh -n` 检查 up/down/smoke 和MySQL初始化脚本通过。
- Gateway 权限测试通过：HTTP身份伪造被清除、内部动作不可透传、普通账号不可调用管理员动作、无会话拒绝、DEMO关闭后演示资金动作被拒、非法JSON拒绝。
- 所有宿主机发布端口绑定127.0.0.1，30个领域RPC端口没有发布到宿主机。
- Jaeger2.5 Badger配置参考官方固定版本样例，显式设置OTLP和查询监听地址，使用独立持久卷；共享初始化任务为Jaeger/MinIO设置10001用户权限。
- Collector0.124.1官方构建清单包含本项目使用的otlpreceiver、otlpexporter、memorylimiterprocessor、batchprocessor和healthcheckextension。
- MinIO固定官方源码commit已在本地用Go1.26.10、CGO_ENABLED=0、`-buildvcs=false -trimpath`成功编译；此项验证源码构建命令，不能替代Docker镜像和容器运行验收。

## 固定镜像与架构

| 镜像 | 核对来源 | amd64 / arm64 |
| --- | --- | --- |
| golang:1.26.10-alpine | DockerHub tag元数据 | 均存在 |
| alpine:3.22.1 | DockerHub tag元数据 | 均存在 |
| mysql:8.4.4 | DockerHub tag元数据 | 均存在 |
| redis:7.4.2-alpine | DockerHub tag元数据 | 均存在 |
| rabbitmq:4.0.7-management-alpine | DockerHub tag元数据 | 均存在 |
| jaegertracing/jaeger:2.5.0 | DockerHub tag元数据 | 均存在 |
| ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.124.1 | 官方GHCR匿名manifest | 均存在 |
| lumo-minio:release-20250907 | 本地构建官方固定源码commit | 随构建平台编译；真实镜像运行待验证 |

Collector的DockerHub对应旧tag未能获取，已改用验证可取的官方GHCR镜像。MinIO DockerHub仓库返回404，Quay匿名拉取仍返回401，因此默认使用 `deploy/minio.Dockerfile` 从官方源码构建，不依赖不可取的旧镜像。官方release `RELEASE.2025-09-07T16-13-09Z` 经GitHub API核对commit为 `07c3a429bfed433e49018cb0f78a52145d4bedeb`，构建时再次校验HEAD。

`start.sh`（兼容入口 `scripts/up.sh`）按服务源码、Compose配置和实际容器镜像记录识别更新范围。需要新应用镜像时执行一次 `docker compose build gateway`，随后只对明确选中的业务服务执行 `up --no-deps --no-build --force-recreate --wait`。运行中的中间件不参与默认更新；MinIO只在需要启动且缺少本地镜像，或显式 `--update-infra` 时构建。

## 验证限制

当前交付环境没有可使用的Docker daemon，无法执行完整 `docker compose up`。因此这里不声称已验证所有容器真实启动、镜像构建、MySQL初始化、RabbitMQ路由、MinIO运行和Jaeger写入。Go单元/真实RPC集成测试的结果见 verification.md；这些不能替代容器启动验收。

首次在有Docker的开发机上运行 `sh start.sh` 后，应检查 `docker compose ps`、运行 `sh scripts/smoke.sh`，并在Jaeger按响应trace_id查看到数据库SQL子span。文件服务还应完成一次真实预签名上传、complete校验和download下载。

## 本次一键脚本更新

- 根目录 start.sh 和 macOS start.command 提供统一入口；Docker不可用时在创建.env或启动容器前失败。
- 已有.env不会覆盖，也不会作为shell执行；Compose负责解析配置。
- 默认增量更新业务服务；无变化时跳过构建及容器更新。已停止容器使用no-recreate补启动；首次没有基线时更新业务容器一次，中间件保留。
- 每个业务容器保存自己的源码/配置/镜像部署记录；共享镜像标签变化不会触发无关服务更新。健康等待失败不推进业务部署记录，no-build也不会把未构建源码记为成功。
- 中间件运行但不健康会明确报错；配置变化提示显式update-infra，不会自动重建它们。成功退出的卷初始化任务不重复执行。
- 成功地址通过docker compose port获取，支持自定义网关、Jaeger、MQ及MinIO端口。
- MinIO增加HTTP readiness检查，媒体服务显式等待MinIO健康；生成器与提交的Compose保持一致。
- Compose CLI已校验默认端口和8181/9101/9102自定义组合，MinIO签名端口和浏览器上传来源随配置同步。
- 启动脚本回归使用fake Docker验证进程调用与失败行为，不代表已运行真实容器。当前Docker daemon限制仍然存在。
