# 独立部署前端时的 API 跨域配置

Docker 一键启动后，商城页面和 `/api/v1/` 均由 `http://localhost:8080` 的网关提供，同源调用不需要跨域配置。默认 `CORS_ALLOWED_ORIGINS` 为空。

如果前端独立部署，在网关的 `.env` 中设置完整、精确的前端 Origin，例如：

```dotenv
CORS_ALLOWED_ORIGINS=https://shop.example.com,https://preview.example.com
```

重新启动网关后，浏览器才能从上述页面发送 Bearer API 请求。Origin 只包含协议、域名和可选端口，不含路径、末尾斜线、查询参数或片段。不接受 `*`，也不会把任意请求 Origin 反射回去。未允许的跨域请求返回 403，不返回 `Access-Control-Allow-Origin`。

允许 GET、POST 及其 OPTIONS 预检；允许 `Authorization`、`Content-Type`、`Idempotency-Key`、`traceparent`、`tracestate` 请求头，并暴露 `X-Trace-ID`。不启用 Cookie 跨域凭据。认证、管理员权限和业务所有权校验继续由网关及服务执行，CORS 允许并不代表授权。

HTTPS 托管页面应连接浏览器可访问的 HTTPS 网关，不能直接调用用户电脑上的 HTTP 地址。网关前方由反向代理终止 TLS 时，也应把外部页面的完整 HTTPS Origin 显式加入上述列表；网关不盲目信任客户端提交的转发头。前端的 API 地址配置只接受服务地址，不能填入 Token 或密码。

文件上传使用独立的 MinIO POST Policy。其浏览器来源另由 `MINIO_CORS_ORIGINS` 配置；`MINIO_PUBLIC_ENDPOINT` 必须是浏览器实际能访问的 MinIO 地址，不能改写签名后的 Host。对象桶仍保持私有。
