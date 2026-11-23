# 结构化日志

## 消费者与契约

Assistant 的部署方从进程标准错误流收集逐行 JSON。命令入口创建自己的日志实例，并记录监听启动、候选重载失败、进程失败和正常退出。业务调用入口记录方法、状态、稳定原因与耗时。日志随请求上下文获得 `request_id`，同一调用的多个记录可以关联。

- `framework/log.NewJSON(io.Writer, *slog.HandlerOptions)` 返回标准 `*slog.Logger`。调用方管理输出位置和等级。关联标识由 `framework/request` 提供，未设置标识时省略 `request_id`。调用方通过 `InfoContext` 等方法传入原请求上下文，日志实例与级别互相独立。
- Assistant 在业务方法结束时记录 `request_finished`。字段包含 `operation`、`status`、`duration_us`，有业务原因时记录 `reason`。正常结果使用 `INFO`，失败结果使用 `WARN`。已知业务错误的状态与原因沿用公开错误。取消和超时保留标准语义。未知错误记为 `INTERNAL`，日志仅记录状态字段。业务日志选取固定字段，提示词、正文、请求头和内部错误文本留在业务处理边界。
- 启动与重载日志记录运维需要的监听地址和启动错误。进程正常退出会记录 `app_stopped`，启动或运行失败会记录 `app_failed`。独立命令进程将同一实例设为标准默认日志，接收 Kratos 的传输启动与关停消息。命令帮助和参数诊断由 `flag` 输出。日志由运行环境采集和轮转。[应用装配与关停](app-lifecycle.md) · [请求元数据](request-metadata.md)

## 边界与取舍

- 参考服务为下游调用日志保留原请求上下文，日志类库用结构化字段和关联标识。沿用上下文关联与结构化字段，日志实例由应用入口构造和注入，避免配置影响同进程的另一个实例。
- Kratos v3.0.0 的 `log.NewLogger` 已提供 `slog.Handler` 上下文提取和实例化装饰。直接复用该能力与 Go 的 JSON handler。Kratos 请求日志中间件会默认提取请求参数和错误内容。Assistant 在业务入口一次写入固定安全字段，同时覆盖一元和流式调用。[Kratos 日志构造](https://github.com/go-kratos/kratos/blob/v3.0.0/log/builder.go) · [Kratos 请求日志](https://github.com/go-kratos/kratos/blob/v3.0.0/middleware/logging/logging.go) · [Go slog](https://pkg.go.dev/log/slog@go1.25.0#JSONHandler)

## 通过条件

- 原生 Go 测试确认两套日志输出和级别独立。对照标准 `slog` 与 Kronos，只有设置合法请求 ID 的上下文会追加 `request_id`。Go `Example` 展示调用。
- 给默认 Kratos 中间件和 Assistant 日志入口传同一个包含私有提示词的请求，以实际输出核对参数、内部错误和固定状态字段。失败分类与上一切面的错误映射一致。
- 在 Linux 构建并启动真正的 Assistant。四个入口可调用，输出能逐行解码。覆盖请求 ID、失败字段、重载错误和 `SIGTERM` 后的正常退出事件。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 关闭工作区运行 `go -C framework/log test -count=10 ./...` 和 `go test -count=1 ./examples/assistant/...`，均通过。Assistant 的真实进程输出逐行可解码，标准调用的标识贯穿成功及校验失败，重载错误保留候选退出原因。`SIGTERM` 正常退出后出现 `app_stopped`。退出码 23 的重载候选由 Go 测试程序构建。[真实进程](../../examples/assistant/cmd/assistant/main_test.go) · [候选测试](../../examples/assistant/cmd/assistant/reload_test.go)
- 对照测试在相同上下文中运行 Go 的 JSON handler 与 Kronos logger，两者保持标准消息和级别，Kronos 自动追加显式请求标识。用同一个样例提示词和失败错误执行默认 Kratos 日志中间件与 Assistant 实际入口。默认中间件包含提示词和内部错误，Assistant 输出包含 `request_id`、`operation`、`status` 和 `duration_us`。[实例与标准库对照](../../framework/log/json_test.go) · [请求日志对照](../../examples/assistant/cmd/assistant/logging_test.go)
- 在 macOS arm64 关闭工作区，日志模块和 Assistant 的 `go test -race -count=1`、根模块与日志模块的 `go vet` 和 `go mod tidy -diff` 均通过。复核关注业务实例与进程输出的所有权，库构造独立 logger，独立命令负责让 Kratos 传输日志使用同一个 JSON writer。[可执行示例](../../framework/log/example_test.go)
