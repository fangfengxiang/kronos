# 请求链路追踪

## 消费者与契约

Assistant 部署方通过 `-trace-endpoint` 或同名启动 JSON 项设置完整的 OTLP HTTP 追踪地址。每代进程构造自己的 exporter 和 tracer provider。空配置沿用默认启动。示例 Collector 地址是 `http://127.0.0.1:4318/v1/traces`。

- HTTP、gRPC、SSE 和 go-micro v2 进入共享业务入口时创建服务端 span。有效 W3C `traceparent` 和 `tracestate` 延续上游链路，缺少有效父级时由 SDK 创建根 span。Kronos HTTP、gRPC 和 MicroRPC 客户端把当前 span 的 W3C 上下文交给下游。
- span 以服务与方法标识，记录完成或失败。已恢复的业务 panic 结束为失败状态。失败使用固定描述，原始错误、提示词和业务正文留在其原有边界。日志记录有效的 `trace_id` 与 `span_id`，请求 ID 继续用于应用内关联。[请求恢复](rpc-handler-recovery.md)
- 启动参数校验在取得监听器前完成。应用入口在进程关停完成后为批量 exporter 留最多 2 秒清理与发送，清理错误与应用退出错误一同返回。Collector 由部署方提供。

## 边界与取舍

- 参考应用在业务方法调用 `StartSpanFromContext` 并沿原上下文交接下游工作，装配处持有外部 exporter。Kronos 复用这种调用边界和显式装配，将 exporter 的生命周期交由应用入口。[OTel Go 埋点](https://opentelemetry.io/docs/languages/go/instrumentation/) · [OTLP 导出](https://opentelemetry.io/docs/languages/go/exporters/)
- `infra/oteltrace` 拥有 Kratos v3 扩展和 OpenTelemetry Go SDK。复用 Kratos tracer 的服务端 `Start` 和日志字段，使用标准 W3C propagator。Kronos 在共享入口完成 span，应用只传上下文、方法与结果。SDK 负责生成、采样、批处理和 OTLP HTTP 导出。[Kratos OTel 扩展](https://github.com/go-kratos/kratos/tree/v3.0.0/contrib/otel) · [W3C Trace Context](https://www.w3.org/TR/trace-context/)

## 通过条件

- 用原生 Go 测试对照直接调用 Kratos tracer 与 Kronos adapter 对同一父上下文的 trace ID、parent span ID 和服务端 span 类型。验证固定失败状态和导出后端可接收数据。
- 在 Ubuntu 运行真实 Assistant 与本地 Collector 替身，HTTP、gRPC、SSE 和兼容调用生成 span。显式 W3C 父级、请求日志和关停导出的 OTLP 数据能互相关联。
- Go `Example` 展示装配和 span 生命周期。编译和运行的实际平台分别记录。

## 实测与复盘

- Go 1.25.0、Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C infra/oteltrace test -count=5 -v ./...` 和 `GOWORK=off go test -count=1 ./examples/assistant/...`，均通过。Collector 替身收到 12 条 `Run` 与 5 条 `StreamRun` 服务端 span。HTTP 与 gRPC 流沿用同一个样例父级，导出的 span ID 与请求日志吻合。终止进程后收到批量导出的完整记录。[真实进程追踪](../../examples/assistant/cmd/assistant/tracing_test.go)
- 对照同版本 Kratos tracer 和 Kronos adapter，对同一个 W3C `traceparent` 与 `tracestate` 验证父 span、链路标识和传播关系。Kronos OTLP 导出保留 `request failed` 固定状态。Go 示例启动本地 Collector 替身并接收实际导出。[上游对照](../../infra/oteltrace/provider_test.go) · [可执行示例](../../infra/oteltrace/example_test.go)
- macOS arm64 上独立模块和真实进程竞态测试各重复三轮通过，`go run ./platform/verify test`、`go vet` 与分层检查通过。编译器逃逸诊断识别出初版请求完成闭包，改用按值传递的请求观测数据后该闭包消除。正常退出日志放在 exporter 关闭之后。Windows amd64 通过 `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C infra/oteltrace test -c` 交叉编译，实际追踪运行在 Linux 与 macOS 验证。

将 Kratos OTel 扩展锁定于与 Kratos v3.0.0 相同的提交，OpenTelemetry Go SDK 与 OTLP HTTP exporter 使用 `v1.43.0`。[模块版本](../../infra/oteltrace/go.mod)
