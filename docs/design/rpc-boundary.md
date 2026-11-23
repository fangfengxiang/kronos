# 一元 RPC 防腐层

## 使用者与契约

首个消费者为 Assistant `Run`。业务实现只接收 `context.Context` 和服务 Proto 消息。应用入口选择 HTTP 和 gRPC 的监听器，调用方选择地址和缺省调用期限。公开接口使用 Kronos 和标准库类型，业务方法、消息形状、错误标识与 HTTP 路由沿用 [Assistant v1 契约](assistant-protocol.md)。

`framework/rpc` 提供一元调用对象、客户端协议选项、稳定的调用错误以及生命周期所用的 `Server` 接口。类型化调用对象持有调用与关闭函数，允许并发调用。所有者负责关闭客户端，关闭函数最多执行一次，关闭后发起的新调用返回 `ErrClientClosed`。请求已有截止时间时保留原值，缺少时才设置配置的缺省期限。缺省期限为零时沿用请求上下文。[RPC 客户端协议](rpc-client-protocol.md)

参考用例的两种客户端内部将 Kratos 响应还原为 Kronos 错误。业务校验使用 `INVALID_ARGUMENT` 类别及服务定义的 `reason`。畸形 HTTP 请求体返回 `INVALID_REQUEST` 与固定文案。未知业务错误使用固定公开文案，底层诊断由服务所有者处理。客户端取消和超时使用标准 `context` 错误，调用方可用 `errors.Is` 判断。

## 复用与取舍

- 已有实际消费者通过私有生成客户端组装请求、沿用原上下文并转换结果。Kronos 沿用这段接入方式和缺省期限规则。Kratos 的客户端超时选项会限制已有调用期限，因此在私有客户端关闭该选项，由 `rpc.UnaryClient` 补缺省值。[Kratos gRPC 客户端实现](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/grpc/client.go)
- 直接复用已有 Protobuf、官方 Go/gRPC 生成器和 Kratos HTTP 绑定。三种生成产物留在 Assistant 的同一个 `internal/wire` Go 包，公开入口只重导出需要的消息并持有私有适配。协议包名、消息编号、RPC 名称及 HTTP 路由保持一致。首个服务的接线由少量手写代码验证，再评估生成这些接线的收益。[Kratos 生成绑定](https://go-kratos.dev/docs/component/transport/overview/)
- 参考应用接收已打开的监听器。调用者负责在启动前获取与回收未交接的监听器，Kratos 负责监听后的传输。首版使用环回地址验证一元调用。进程装配、TLS 接线、流调用与探针由各自的后续切面完成。

## 验收

1. 同一个业务实现通过真实 HTTP 和 gRPC 监听返回同一份结果。验证业务错误、未知错误与取消经封装返回预期值。保留协议描述符、消息往返和生成一致性检查。
2. 在两种调用路径上验证调用期限的优先级，独立验证客户端关闭行为。在公共用法中检查应用、业务和调用方的导入与公开签名。
3. 各封装模块用 `GOWORK=off` 构建和测试。参考应用在版本发布前用工作树依赖验收。

## 实测与复盘

- Go 1.25.0 下运行 `bash scripts/test-modules.sh`，根模块和三个封装模块分别通过竞态测试。`bash scripts/check-layer-deps.sh`、`bash tools/proto/generate.sh --check`、`GOWORK=off go vet ./...` 与 `GOWORK=off go mod verify` 通过。
- 在临时独立模块中使用工作树替换，并关闭工作区，运行 `go test -race -count=1 ./...`。调用样例仅导入标准库、Assistant 公开包和 `framework/rpc`，真实双入口通过。验证了相同结果、稳定错误标识、未知错误和 HTTP 解码失败的安全文案，以及取消传播。
- 客户端期限由包级测试精确核对，连接关闭由并发测试验证。保留现有生成绑定后，手写适配只承担公开接口与传输之间的转换。后续先用进程装配和流式调用扩展验收，再决定生成接线的形态。
