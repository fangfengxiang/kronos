# 请求元数据

## 消费者与契约

Assistant 的业务实现用 `request.ID(ctx)` 读取调用关联标识。业务可以把原有 `context.Context` 直接传给下一次标准 RPC。Kronos 的 HTTP 与 gRPC 一元客户端自动发送该标识，主动发起的调用也可以用 `request.WithID` 指定。应用仍用 `framework/localization` 处理语言偏好和语言选择。

Assistant 的 MicroRPC 服务复用入站元数据读取，收到合法请求 ID 时保留，缺少时生成本地 ID。Kronos 自有客户端须发送与 HTTP、gRPC 相同的显式请求 ID、语言偏好和 W3C 上下文。

- HTTP、gRPC 和 SSE 在进入服务方法时读取 `x-request-id`。一个有效值会被保留。允许的值由 1 至 128 字节的 ASCII 字母、数字、点、下划线和连字符组成。缺失、重复或格式非法时生成新的标识，调用继续执行。调用方通过 `WithID` 设置无效值时，当前出站上下文的标识清空。
- 请求 ID 只用于诊断关联。框架通过标准上下文传递这一项，出站语言由客户端显式指定。W3C 追踪字段从当前 span 生成。请求 ID 在一次流式调用内保持不变。[请求链路追踪](request-tracing.md)
- go-micro v2 兼容入口使用可选 HTTP 头。没有发送这些头的旧调用继续沿用本地生成和语言回退。旧协议的 Protobuf 信封、调用序号及编码沿用原有约定。

## Assistant 与统一应用的协议边界

| 入口 | 应用装配和服务契约 | 元数据、终止与观测 |
| --- | --- | --- |
| gRPC 一元 | 统一 `kronos.GRPC` 或 Assistant 私有生成绑定。默认 RPC 协议。 | gRPC metadata。响应或稳定 gRPC 状态结束，调用上下文携带期限和取消。 |
| MicroRPC 一元 | 统一 `kronos.MicroRPC` 或 Assistant 兼容绑定。通过选项显式启用。 | HTTP 精确头和既有 Timeout。响应及稳定错误沿旧 RPC 帧和头部返回。旧客户端省略可选字段时继续工作。 |
| HTTP JSON 一元 | Order 的 `kronos.HTTP` 和 Assistant 私有绑定均使用显式 Proto 映射。 | HTTP 头传请求 ID、语言和 W3C 上下文。返回 JSON 或 HTTP 错误，采用请求上下文取消。 [统一 HTTP 契约](unified-http.md) |
| SSE | `kronos.SSE` 或 Assistant 专用监听。路由由 Proto 提供。 | HTTP 头，断开结束执行。业务完成事件后正常结束；首帧后错误用事件返回。 |
| gRPC 服务端流 | `kronos.GRPC` 或 Assistant 原有 gRPC 绑定。一元和流共享服务身份与监听。 | 流建立时发送 metadata，一条流保持同一个请求 ID。完成事件后需要正常 EOF，取消到达业务执行。 |

统一应用负责 Order 的三路一元监听及 RPC 发现，Assistant 复用同一生命周期模块并显式安装多传输绑定。两种装配的服务端按协议约定建立稳定错误，在共享业务入口设置日志和 Trace。Assistant 指标按方法聚合，Order 额外区分协议与角色。MCP 和 WebRTC 尚无已发布契约。[Assistant 协议](assistant-protocol.md) · [监听与兼容](additional-listeners.md) · [Order 观测](order-diagnostics.md)

本次最小改动修复 Assistant 的 MicroRPC 出站装配。它已有合法 ID 及 span 上下文，旧入口只向适配器传递语言。复用已用于 Order 的 `CallWithRequestID` 和现有 W3C propagator。在真实五种入口核对同一请求 ID、父 span 和实际业务完成。仅变更客户端所传的可选 HTTP 头，撤回该接线即可恢复早期发送行为。[W3C Trace Context](https://www.w3.org/TR/trace-context/) · [gRPC metadata](https://grpc.io/docs/guides/metadata/)

## 边界与取舍

- `framework/request` 是独立 Go 模块，持有字段名、语义校验、生成和上下文函数。传输绑定读取及设置头部。模块只依赖标准库，生成使用 Go 1.25 已提供的 `crypto/rand.Text`。[Go 生成随机文本](https://pkg.go.dev/crypto/rand@go1.25.0#Text)
- 参考应用把原请求上下文传给下游 RPC，元数据拦截器按明确列表转发一元请求。沿用显式允许的字段和上下文接续。HTTP、gRPC 与 SSE 在服务方法边界一次性装入元数据，保持流上下文与取消来源。Kratos 提供实际的请求头读取和写入。其通用中间件按前缀匹配键，适合约定命名空间的场景。当前契约只取精确字段，避免转发额外字段。[Kratos 元数据中间件](https://github.com/go-kratos/kratos/blob/v3.0.0/middleware/metadata/metadata.go) · [Envoy 请求 ID 约定](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#x-request-id)

## 统一一元入口的请求关联

复审 `91db47a` 时，将调用方设置的请求 ID 发给真实 etcd 发现的 Order，并由测试业务从上下文回显。标准 gRPC 和 MicroRPC 均返回空标识。统一一元入口复用 `framework/request` 的既有规则，在服务绑定处接收精确的 `x-request-id`，在客户端传输处发送上下文中的合法值。缺失、重复及不符合规则的入站值由已有校验生成标识。业务方法、日志和下游调用使用同一个标准上下文。

MicroRPC 的底层适配器负责开放此 HTTP 字段，校验和生成继续留在框架边界。平台运行时引用已公开的 MicroRPC 版本。双协议验收确认显式透传、缺省生成、稳定错误日志和已有语言及 Trace 行为。[Go RPC 元数据](https://grpc.io/docs/guides/metadata/) · [请求 ID 字段](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#x-request-id)

## 通过条件

- 模块测试覆盖显式设置、替换、原上下文不被修改、缺失生成、重复和非法值回退。
- 同一标识经标准 HTTP、gRPC 与 MicroRPC 到达 Assistant 业务方法。未指定标识时生成可读取的新值。SSE 和 gRPC 流在整次调用中使用一个标识。旧客户端省略可选头时，仍能执行原有业务调用并得到本地标识。
- 对照 Kratos v3.0.0 与 Kronos，用 `x-request-id`、同前缀字段和其他头验证实际选取及传递范围。在 Linux 执行独立模块与 Assistant 集成验收。

## 实测与复盘

- Go 1.25.0、Ubuntu 24.04 Linux arm64 的 `GOWORK=off go test -count=1 ./examples/assistant/...` 通过。修复前，通过 Assistant 公开客户端发送的显式 ID 在 MicroRPC 服务端变成了新生成的值。修复后，真实服务进程的 HTTP、SSE、gRPC 一元和服务端流、MicroRPC 一元均返回业务结果。OTLP 共收到 16 个一元、6 个流式 span，其中五个来自显式父级请求，均保留相同的 trace ID、父 span、tracestate 和对应日志中的请求 ID。两版官方客户端的 `TestGoMicroV2UpstreamInterop` 通过。[进程验收](../../examples/assistant/cmd/assistant/tracing_test.go)

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 使用 `GOWORK=off go -C framework/request test -count=10 ./...` 和 `GOWORK=off go test -count=1 ./examples/assistant/...`，均通过。实际调用核对 HTTP 与 gRPC 一元透传、兼容入口独立标识、gRPC 流的两条增量及 SSE 模型收到的上下文。[一元调用](../../examples/assistant/assistant_test.go) · [gRPC 流](../../examples/assistant/request_test.go) · [SSE 调用](../../examples/assistant/sse_test.go)
- 同一个 Go 对照测试从相同请求头初始化 Kratos 前缀中间件与 Kronos 精确键绑定。Kratos 传播 `x-request-id` 和 `x-request-id-secondary`。Kronos 只传播 `x-request-id`，出站语言使用调用方显式设置的值。复核后将入站提取放在服务方法入口，流的多次收发共享同一份请求上下文。[对照测试](../../examples/assistant/internal/binding/request_test.go)
- 在 macOS arm64 关闭工作区运行模块与 Assistant 的 `go test -race -count=1`，以及两者的 `go vet`、`go mod tidy -diff`，均通过。模块内的 Go `ExampleWithID` 随测试执行。[可执行示例](../../framework/request/example_test.go)
- MicroRPC 暴露 `CallWithRequestID`，现有 `RequestMetadata` 类型和调用签名保留。Go 1.25.0、Ubuntu 24.04 Linux arm64 的适配模块通过十轮，官方 go-micro v2.0.0 与 v2.9.1 双向用例通过。macOS arm64 竞态回归五轮通过。测试核对显式 ID 作为精确的 HTTP 头发送，默认调用保留现有头部行为。[兼容层测试](../../infra/microv2/client_test.go)
- Go 1.25.0、Ubuntu 24.04 Linux amd64 和真实 etcd v3.6.16 执行 `GOWORK=off go test -tags=etcd_interop -count=5 -run '^TestOrder(PropagatesRequestIDAcrossBothProtocols|CorrelatesSignalsAcrossBothProtocols)$' -v ./test/interop/microv2`。两种协议均保持显式 ID，缺失或无效的调用生成新的合法标识，两侧可用同一 ID 对照错误日志及 Trace。完整的 `-tags=interop,etcd_interop` 治理、官方旧端和恢复套件，以及 macOS arm64 的全仓竞态与依赖检查均通过。[业务上下文回显](../../test/interop/microv2/request_metadata_test.go) · [日志与 Trace](../../test/interop/microv2/trace_test.go)
