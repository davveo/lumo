# MySQL 1045认证失败：保留数据修复

示例：`Access denied for user 'points'@'172.22.0.34' (using password: YES)`。

此错误表示MySQL拒绝认证，需核对账号、Host和密码。日志中的容器IP不是要固定写入DSN的地址；项目仍通过`mysql:3306`连接。Go服务先尝试连接数据库，约60秒仍失败时退出，Docker再拉起，所以可能反复显示`health: starting`。

## 1. 确认实际密码来源

本项目所有领域服务默认使用各自同名MySQL账号和`lumo_<服务>`数据库，密码来自`MALL_DB_PASSWORD`。默认示例值为`lumo_db_local_2026`；若已经修改，以当前实际生效的值为准。

保留的MySQL数据卷不会再次执行初始化脚本，重建MySQL容器也不会修改库中已有密码。`deploy/mysql/01-services.sh`的`CREATE USER IF NOT EXISTS`不会覆盖旧账号密码，不能直接重跑它来同步密码。

Compose的宿主机环境变量也可能覆盖.env。若原密码正确且不准备改密码，可先将项目.env恢复为原业务密码，并排除同名宿主机环境变量覆盖，再更新受影响业务容器。

## 2. 用现存root密码检查账号

在工程目录执行，交互输入数据库当前真实root密码。如果.env中的root密码后来改变过，应使用数据库最初设置且仍然生效的密码；不要为此删除数据卷。

```sh
docker compose exec mysql env MYSQL_HISTFILE=/dev/null \
  mysql --protocol=socket -u root -p
```

检查：

```sql
SELECT User, Host, plugin, account_locked, password_expired
FROM mysql.user WHERE User = 'points';
```

项目首次初始化创建的是`'points'@'%'`。如果还有更具体的Host账号，它可能优先匹配连接来源，需要核对该条账号；不要仅根据报错IP反复新增容器IP账号。

## 3. 将业务账号密码与当前配置同步

以下SQL使用项目默认密码。自定义过`MALL_DB_PASSWORD`时，先把两处默认密码替换成实际生效值，再执行。本项目初始化脚本要求该密码只含英文字母、数字和下划线。

```sql
CREATE DATABASE IF NOT EXISTS lumo_points
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'points'@'%'
  IDENTIFIED BY 'lumo_db_local_2026';
ALTER USER 'points'@'%'
  IDENTIFIED BY 'lumo_db_local_2026';
GRANT ALL PRIVILEGES ON lumo_points.* TO 'points'@'%';
SHOW GRANTS FOR 'points'@'%';
EXIT;
```

这些语句创建缺失账号、更新业务密码及对应库权限，不清空业务表、不修改root密码，不需要`FLUSH PRIVILEGES`。如果检查确认该账号被锁定或存在密码过期状态，应另按实际账号状态处理；不要把所有认证问题直接判定为密码错误。

其他服务若也出现1045，对照同名账号和`lumo_<服务>`库修复。具体名单在`deploy/mysql/01-services.sh`中，各账号仅授权自己的库。

## 4. 重新验证服务

数据库密码修复后，尚在重试的进程或Docker自动重启可能自行恢复。可以检查：

```sh
docker compose logs --tail=50 points
docker compose ps --all points
```

只需再次启动积分服务时：

```sh
sh start.sh points --no-build --wait-timeout 900
```

所有报错账号都修复后，执行完整检查并输出可访问地址：

```sh
sh start.sh --no-build --wait-timeout 900
```

`--no-build`不会把尚未构建的代码标记为已经部署；需要应用代码修改时去掉此参数。启动成功后才显示“可访问地址”，中间件保留规则不变。

参考：

- [MySQL官方镜像：已有数据目录与初始化变量](https://hub.docker.com/_/mysql/)
- [MySQL 8.4：ALTER USER](https://dev.mysql.com/doc/refman/8.4/en/alter-user.html)
- [MySQL 8.4：连接认证排查](https://dev.mysql.com/doc/refman/8.4/en/problems-connecting.html)
