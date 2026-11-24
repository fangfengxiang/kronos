# 请求元数据

## 消费者与契约

Assistant 的业务实现用 `request.ID(ctx)` 读取调用关联标识。业务可以把原有 `context.Context` 直接传给下一次标准 RPC。Kronos 的 HTTP 与 gRPC 一元客户端自动发送该标识，主动发起的调用也可以用 `request.WithID` 指定。应用仍用 `framework/localization` 处理语言偏好和语言选择。

Assistant 的 MicroRPC 服务复用入站元数据读取，实际收到的合法请求 ID 可用于诊断，缺少时生成本地 ID。MicroRPC 客户端发送显式语言偏好。

- HTTP、gRPC 和 SSE 在进入服务方法时读取 `x-request-id`。一个有效值会被保留。允许的值由 1 至 128 字节的 ASCII 字母、数字、点、下划线和连字符组成。缺失、重复或格式非法时生成新的标识，调用继续执行。调用方通过 `WithID` 设置无效值时，当前出站上下文的标识清空。
- 请求 ID 只用于诊断关联。框架通过标准上下文传递这一项，出站语言由客户端显式指定。W3C 追踪字段从当前 span 生成。请求 ID 在一次流式调用内保持不变。[请求链路追踪](request-tracing.md)
- go-micro v2 兼容入口为每次业务调用创建本地标识。旧协议的出站请求沿用已有信封和头部约定。

## 边界与取舍

- `framework/request` 是独立 Go 模块，持有字段名、语义校验、生成和上下文函数。传输绑定读取及设置头部。模块只依赖标准库，生成使用 Go 1.25 已提供的 `crypto/rand.Text`。[Go 生成随机文本](https://pkg.go.dev/crypto/rand@go1.25.0#Text)
- 参考应用把原请求上下文传给下游 RPC，元数据拦截器按明确列表转发一元请求。沿用显式允许的字段和上下文接续。HTTP、gRPC 与 SSE 在服务方法边界一次性装入元数据，保持流上下文与取消来源。Kratos 提供实际的请求头读取和写入。其通用中间件按前缀匹配键，适合约定命名空间的场景。当前契约只取精确字段，避免转发额外字段。[Kratos 元数据中间件](https://github.com/go-kratos/kratos/blob/v3.0.0/middleware/metadata/metadata.go) · [Envoy 请求 ID 约定](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#x-request-id)

## 统一一元入口的请求关联

复审 `91db47a` 时，将调用方设置的请求 ID 发给真实 etcd 发现的 Order，并由测试业务从上下文回显。标准 gRPC 和 MicroRPC 均返回空标识。统一一元入口需要复用 `framework/request` 的既有规则，在服务绑定处接收精确的 `x-request-id`，在客户端传输处发送上下文中的合法值。缺失、重复及不符合规则的入站值由已有校验生成标识。业务方法、日志和下游调用使用同一个标准上下文。

MicroRPC 的底层适配器只负责开放此 HTTP 字段，校验和生成继续留在框架边界。发布时先提供兼容的适配器版本，再更新平台运行时所引用的版本。双协议实测需要确认显式透传、缺省生成、稳定错误日志和已有语言及 Trace 行为。[Go RPC 元数据](https://grpc.io/docs/guides/metadata/) · [请求 ID 字段](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#x-request-id)

## 通过条件

- 模块测试覆盖显式设置、替换、原上下文不被修改、缺失生成、重复和非法值回退。
- 同一标识经标准 HTTP 和 gRPC 到达 Assistant 业务方法。未指定标识时生成可读取的新值。SSE 和 gRPC 流在整次调用中使用一个标识。兼容入口仍能执行原有业务调用，并得到本地标识。
- 对照 Kratos v3.0.0 与 Kronos，用 `x-request-id`、同前缀字段和其他头验证实际选取及传递范围。在 Linux 执行独立模块与 Assistant 集成验收。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 使用 `GOWORK=off go -C framework/request test -count=10 ./...` 和 `GOWORK=off go test -count=1 ./examples/assistant/...`，均通过。实际调用核对 HTTP 与 gRPC 一元透传、兼容入口独立标识、gRPC 流的两条增量及 SSE 模型收到的上下文。[一元调用](../../examples/assistant/assistant_test.go) · [gRPC 流](../../examples/assistant/request_test.go) · [SSE 调用](../../examples/assistant/sse_test.go)
- 同一个 Go 对照测试从相同请求头初始化 Kratos 前缀中间件与 Kronos 精确键绑定。Kratos 传播 `x-request-id` 和 `x-request-id-secondary`。Kronos 只传播 `x-request-id`，出站语言使用调用方显式设置的值。复核后将入站提取放在服务方法入口，流的多次收发共享同一份请求上下文。[对照测试](../../examples/assistant/internal/binding/request_test.go)
- 在 macOS arm64 关闭工作区运行模块与 Assistant 的 `go test -race -count=1`，以及两者的 `go vet`、`go mod tidy -diff`，均通过。模块内的 Go `ExampleWithID` 随测试执行。[可执行示例](../../framework/request/example_test.go)
- MicroRPC 暴露 `CallWithRequestID`，现有 `RequestMetadata` 类型和调用签名保留。Go 1.25.0、Ubuntu 24.04 Linux arm64 的适配模块通过十轮，官方 go-micro v2.0.0 与 v2.9.1 双向用例通过。macOS arm64 竞态回归五轮通过。测试核对显式 ID 作为精确的 HTTP 头发送，默认调用保留现有头部行为。[兼容层测试](../../infra/microv2/client_test.go)
