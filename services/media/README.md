# media-service

负责私有评价图片、售后凭证、客服附件和票据附件的元数据及 MinIO 上传/下载授权。商品公开图应使用另一条明确的发布流程，私有附件桶不会设置公开策略。

支持 `create / complete / get / download`，每一步必须有登录主体；对象查询与下载先校验归属，管理员可按权限查看。`service.go` 组装依赖，调用顺序是 `transport → usecase → repository (GORM)`，对象操作走 `internal/storage`，各层均有 OTel span。

## 对象存储配置

```dotenv
MINIO_ENDPOINT=minio:9000
MINIO_PUBLIC_ENDPOINT=localhost:9001
MINIO_ACCESS_KEY=development-access-key
MINIO_SECRET_KEY=development-secret-key
MINIO_BUCKET=lumo-media
MINIO_USE_SSL=false
```

内部客户端执行建桶/检查私有桶策略和 StatObject；公网客户端在签名时使用 `MINIO_PUBLIC_ENDPOINT`。禁止签名后替换URL中的主机。公网客户端固定 `us-east-1` 区域，签名过程不通过公网地址查询桶区域。

只有 `MINIO_ENDPOINT` 未配置时才启用明确的 metadata-only 模式，`upload_supported=false`，不返回虚假上传地址。配置不完整则启动报错。新建桶默认私有，已存在桶如果含有公开桶策略则拒绝启动。

## 上传协议

1. `create`：输入 `file_name, content_type, size`（兼容 `mime_type, size_bytes`）。允许 JPEG、PNG、WebP、PDF、纯文本，大小 1字节至10MiB。返回 `id/file_id, upload_url, upload_method=POST, upload_fields, expires_in=600`。
2. 客户端将 `upload_fields` 原样作为 multipart 表单字段，最后添加二进制 `file` 字段，POST 到 `upload_url`。不要把它当作普通PUT地址。POST policy 绑定随机对象key、MIME和准确声明长度；文件名不参与key构造。
3. 上传返回成功后调用 `complete`，ID 为 `file_id`。服务用内部 StatObject 复核实际大小与Content-Type；不符则标记 `REJECTED`，不能获取下载授权。成功才变为 `READY`。
4. `get/download`：只有已完成、归属检查通过的文件才返回10分钟预签名GET。每次签名前再次Stat复核，防止未过期上传授权替换对象后绕过大小/MIME约束。

签名URL只在短期响应中返回，不持久化到数据库或写入日志/trace。`StatObject` 验证的是对象存储返回的大小和MIME元数据；正式生产还应接恶意文件扫描和内容嗅探，并配置未完成/拒绝对象清理周期。

## 验证

`go test ./services/media/...` 使用fake对象接口验证归属、状态、大小/MIME、拒绝后的下载限制；真实MinIO SDK的本地签名单测验证公网主机和POST policy精确大小约束。测试不将未连接的外部存储描述为已上传成功。


可选真实MinIO集成测试：

```bash
MINIO_INTEGRATION_BINARY=/absolute/path/to/minio go test ./services/media -run TestMinIORealUploadDownload -count=1 -v
```

该测试启动独立的127.0.0.1:19001/19002进程及临时数据目录，尝试真实POST上传、Stat完成校验、预签名GET字节比对、跨用户拒绝和长度政策拒绝，结束后自动清理。本次执行环境中官方MinIO在启动枚举本机IP时受到 `netlinkrib: operation not permitted` 限制，尚未到S3监听，因此真实POST/Stat/GET尚未验收；已通过fake接口和真实SDK本地签名单测。未修改官方MinIO源码绕过环境限制，默认 `go test` 不启用此可选测试。
