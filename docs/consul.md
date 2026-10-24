# Consul注册与服务发现

完整Compose环境默认运行持久化Consul，UI为 http://localhost:8500/ui/。目录应包含30个领域服务、`gateway`和Consul自身；每个业务实例有独立ID、注册地址及健康检查。旧版本镜像没有注册逻辑，升级时需执行不带服务名的 `sh start.sh` 更新全部业务容器。

服务启动时注册，Consul对领域服务调用标准gRPC Health检查，对网关请求 `/readyz`。RPC客户端仅查询健康实例并持续刷新列表；没有健康实例时返回服务不可用，避免把发现失败误当成注册成功。正常退出时注销实例，长期不健康的实例会由Consul清理；存活进程周期性重新注册，支持Consul重启后的恢复。

## 配置

- `CONSUL_HTTP_ADDR`：Consul HTTP API地址。Compose中固定为 `http://consul:8500`；独立Go进程未设置时关闭Consul集成，继续使用静态RPC地址。
- `CONSUL_HTTP_TOKEN`：可选ACL令牌，通过请求头发送。默认本机Consul未开启ACL。
- `CONSUL_SERVICE_ADDRESS`：可选的实例地址覆盖。Compose不设置此项，由进程使用本容器唯一hostname解析自身IP；不能填写会解析到多个副本的共享服务名，也不应填写 `localhost` 或 `127.0.0.1`。跨主机部署时需提供Consul和调用方可达的本实例地址。
- `CONSUL_SERVICE_ID`：可选实例ID；未设置时为服务名、实例hostname和监听端口的组合。同一服务多副本不能配置相同的固定ID。
- `CONSUL_UI_PORT`：宿主机UI端口，默认8500，仅绑定本机。

显式 `CATALOG_RPC_ADDR` 等领域服务地址，以及网关 `RPC_TARGETS` 的条目，继续优先于Consul发现，便于定向调试。不设置覆盖时，启用Consul的调用方通过注册中心查找健康实例。

`consul-data`保存本机注册中心状态，启停不会删除该卷。若旧工程已使用同一个Compose项目名 `lumo-mall`，现工程继续使用原卷。运行中的Consul默认不会因业务更新而重建；缺失或停止时启动脚本会补启动。

## 多实例

30个领域服务分别运行在独立进程和容器内，使用各自的数据库schema及账号。共享编译镜像不代表运行在同一进程；`SERVICE_NAME`决定单个进程加载哪个领域模块。领域RPC没有固定宿主机端口映射，可以单独扩容：

```sh
# 先启动完整环境，再把商品服务扩为3个实例；其余服务保持原状
docker compose up -d --no-deps --no-build --scale catalog=3 catalog

# 检查3个独立实例及其通过健康检查的地址
docker compose ps catalog
curl --fail --silent 'http://localhost:8500/v1/health/service/catalog?passing=true'

# 后续更新检查全部实例，保留现有副本数
sh start.sh catalog
```

调用方为逻辑服务维持gRPC连接池，持续刷新Consul的passing实例，采用round_robin在可用连接之间分配请求；增加、注销及健康变化会更新端点列表，无需重启调用方。没有可用实例时返回Unavailable，不向固定DNS地址回退；显式配置的调试地址覆盖除外。

同一领域的副本共享该领域数据库与RabbitMQ持久队列。迁移和演示初始化通过MySQL命名锁串行执行；定时扫描也在副本间互斥。消费者竞争同一服务队列，Outbox/Inbox及业务幂等处理重投，不为每个副本复制独立业务队列。

默认Compose的gateway固定映射一个本地HTTP端口，用于单机开发；多网关需在独立网络入口配置负载均衡并移除冲突的固定映射。Consul服务发现负责后端RPC实例选择，不替代浏览器访问入口的负载均衡。

## 检查

在工程目录执行：

```sh
docker compose ps consul
docker compose exec consul consul catalog services
curl --fail --silent http://localhost:8500/v1/health/checks/order
docker compose logs --tail=80 consul order gateway
```

服务列表缺失时，先检查业务容器是否已更新、是否配置 `CONSUL_HTTP_ADDR`、注册地址是否可达，再查看注册失败日志。仅启动Consul容器不能使旧版本业务进程自动注册。
