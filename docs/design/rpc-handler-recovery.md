# RPC 请求处理恢复

Assistant 和 TextBackend 的业务实现通过同一层守卫交给各传输。请求处理协程中的 panic 被转换为 Kronos 内部错误，由服务现有错误映射产生公开响应。调用方沿用现有 Protobuf 和错误标识。

## 契约与边界

- `framework/rpc.RecoverUnary` 和 `RecoverStream` 包装类型化处理函数，使用调用方给出的上下文和参数。正常结果、业务错误和取消继续由原函数决定。守卫从执行请求的同一协程恢复 panic，返回 `ErrHandlerPanic`。传输绑定在已有请求元数据建立后调用它。[Go panic 语义](https://go.dev/ref/spec#Handling_panics)
- Assistant 的 HTTP、gRPC 一元和 go-micro v2 一元返回已有的 `ERROR_REASON_RUN_FAILED`。gRPC 流通过状态结束。SSE 在写入首帧前返回结构化 HTTP 错误，已发送增量后返回 `event: error`，已有文本仍可读取。流完成事件由绑定在处理函数正常返回后产生。
- TextBackend 的 gRPC 保留 `ERROR_REASON_GENERATION_FAILED`。两个参考服务均通过私有绑定装配守卫，业务实现和调用方继续使用 Kronos 接口。
- 恢复时用既有 `slog` 记录 panic 类型和请求协程栈。诊断字段限定为类型、栈和由标准日志接线从上下文取得的请求标识。日志处理器中的 panic 与本次 RPC 错误隔离。后台任务沿各自的运行入口收尾。
- Assistant 的共享入口先建立请求观测，再用同一套守卫执行接流检查和业务方法。返回的 `ErrHandlerPanic` 参与一次失败计数、span 结束和 `request_finished`。任意应用实现仍能通过传输外层守卫获得公开错误映射。

## 复用与取舍

参考一元和流拦截器在请求入口恢复异常并记录诊断，适用于从同一入口复用处理逻辑。Kratos 的标准恢复中间件可替换错误与 logger，默认诊断记录完整请求及 panic 值。Go HTTP server 的内置恢复负责终止连接或流。当前使用标准库恢复并在类型化的业务调用层归一化错误，沿用 Kratos 的传输、错误编码和 SSE 终止，保留兼容 RPC 的信封语义。[Kratos 恢复中间件](https://github.com/go-kratos/kratos/blob/v3.0.0/middleware/recovery/recovery.go) · [Go HTTP handler](https://pkg.go.dev/net/http@go1.25.0#Handler)

将恢复放在共享观测之内，使用同一个错误值驱动指标、链路与日志。对任意已装配服务的保护仍由私有传输绑定负责。业务 API、观测适配模块和 Protobuf 沿用现有契约。[OpenTelemetry 错误记录](https://opentelemetry.io/docs/specs/semconv/general/recording-errors/)

## 通过条件

- 原生 Go 测试验证字符串及 nil panic、正常业务错误、日志处理器异常和安全诊断。真实 Assistant 的四监听与两个流入口返回一致失败语义。验证首帧前与已发一帧后的 SSE 行为、gRPC 流中止以及同一进程后续成功请求。
- 真实 TextBackend gRPC 监听返回既有生成错误，随后仍可完成调用。Linux 和本机竞态测试覆盖框架层与调用边界，公开入口继续由 Kronos 类型和标准库构成。
- 从真实 Assistant gRPC 一元和流式入口注入业务 panic，两次均返回稳定失败。一元无结果，流式保留已发送的增量。每个入口各产生一条失败计数、一个结束的错误 span 和一条 `request_finished`。

## 实测记录

- Go 1.25.0、Ubuntu 24.04 Linux arm64，运行 `GOWORK=off go -C framework/rpc test -count=3 -run 'TestRecovery|ExampleRecoverUnary' -v ./...`。恢复、nil panic、日志诊断、日志处理器 panic 及原生 `Example` 通过。[守卫测试](../../framework/rpc/recovery_test.go)
- 同环境运行 `GOWORK=off go test -count=3 -run 'TestAssistantThroughKronosPublicAPI|TestSSEFailureAndDisconnect|TestGRPCStreamRecoversAfterPartialResponse|TestKronosAndStandardGRPCClients' ./examples/assistant ./examples/textbackend/internal/binding`，三轮通过。真实 HTTP、gRPC、SSE、go-micro v2 及原生 TextBackend gRPC 返回稳定错误，后续请求成功。gRPC 流日志含传播的请求标识，SSE 在两种发送阶段分别完成 HTTP 错误和终止事件。[Assistant 一元](../../examples/assistant/assistant_test.go) · [gRPC 流](../../examples/assistant/request_test.go) · [SSE](../../examples/assistant/sse_test.go) · [原生 gRPC 对照](../../examples/textbackend/internal/binding/interop_test.go)
- 临时 Go `-overlay` 使用原 HTTP 和 go-micro v2 业务接线时，原生响应断言均检测到 EOF。仓库接线通过相同用例，分别返回 HTTP 500 JSON 和带错误字段的旧协议 RPC 帧。
- macOS arm64 对 `framework/rpc` 与上述调用路径运行 `-race -count=3` 通过。`go run ./platform/verify deps`、`go vet ./...` 及根目录 `go mod tidy -diff` 通过。`CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build` 验证模块、Assistant 和 TextBackend 可构建，运行记录来自 Linux 和 macOS。
- `go run ./platform/verify test` 通过所有模块竞态回归。`GOWORK=off go test -race -tags=e2e -count=1 -v ./test/e2e/assistant` 通过默认 Assistant Linux Compose 部署回归。
- 新增原生 gRPC 一元与流式注入用例，修复前两次失败均缺计数、span 和完成日志。Go 1.25.0、Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go test -count=3 -run '^TestAssistantRecordsRecoveredRequests$' ./examples/assistant/cmd/assistant`，各方法每次得到一条失败计数、一个错误 span 和一条关联日志。macOS arm64 运行 `GOWORK=off go test -race -count=1 ./examples/assistant/...` 通过。[请求结果观测回归](../../examples/assistant/cmd/assistant/recovery_test.go)
