# 结构化日志

## 消费者与契约

Assistant 的部署方从进程标准错误流收集逐行 JSON。命令入口创建自己的日志实例，并记录监听启动、候选重载失败、进程失败和正常退出。业务调用入口记录方法、状态、稳定原因与耗时。日志随请求上下文获得 `request_id`，同一调用的多个记录可以关联。

- `framework/log.NewJSON(io.Writer, *slog.HandlerOptions)` 返回标准 `*slog.Logger`。调用方管理输出位置和等级。关联标识由 `framework/request` 提供，未设置标识时省略 `request_id`。调用方通过 `InfoContext` 等方法传入原请求上下文，日志实例与级别互相独立。
- Assistant 在业务方法结束时记录 `request_finished`。字段包含 `operation`、`status`、`duration_us`，有业务原因时记录 `reason`。正常结果使用 `INFO`，失败结果使用 `WARN`。已知业务错误的状态与原因沿用公开错误。取消和超时保留标准语义。未知错误和已恢复的业务 panic 记为 `INTERNAL`。业务日志选取固定字段，提示词、正文、请求头和内部错误文本留在业务处理边界。[请求恢复](rpc-handler-recovery.md)
- 启动与重载日志记录运维需要的监听地址和启动错误。进程正常退出会记录 `app_stopped`，启动或运行失败会记录 `app_failed`。独立命令进程将同一实例设为标准默认日志，接收 Kratos 的传输启动与关停消息。命令帮助和参数诊断由 `flag` 输出。日志由运行环境采集和轮转。[应用装配与关停](app-lifecycle.md) · [请求元数据](request-metadata.md)

### 统一一元 RPC 的事件

应用间的 RPC 以 `service`、`operation` 和 `protocol` 关联注册、发现与调用。服务端在接纳业务时用 `rpc_started` 记录当前 `in_flight`，在完成或拒绝时以 `rpc_finished` 记录耗时、稳定状态和当前在途数量。摘流拒绝记录为 `UNAVAILABLE` 与 `SERVICE_UNAVAILABLE`。应用实例独立计数，两种协议共用该实例的在途数量。

客户端的 `rpc_client_finished` 记录状态、耗时和传输尝试次数。注册中心查询在共享预算内失败时记录 `rpc_discovery_failed`，包含协议、服务名和查询耗时。沿用标准 `slog`，成功和接纳事件使用 DEBUG，失败使用 WARN。使用者可用标准日志级别启用详细记录。业务调用日志仅写入固定字段与公开错误原因。[Go slog](https://pkg.go.dev/log/slog@go1.25.0) · [治理故障契约](service-governance.md#就绪故障和恢复)

按需刷新成功时用 DEBUG 记录 `rpc_discovery_snapshot`。`discovered_instances` 是快照中的去重地址数，`pooled_transports` 是当前可复用的传输对象数。`rpc_client_closed` 的 `released_transports` 是 `Close` 成功清理的对象数，范围为当前整个客户端。[连接与快照](discovered-rpc-client.md#实例失败及资源)

## 边界与取舍

- 参考服务为下游调用日志保留原请求上下文，日志类库用结构化字段和关联标识。沿用上下文关联与结构化字段，日志实例由应用入口构造和注入，避免配置影响同进程的另一个实例。
- Kratos v3.0.0 的 `log.NewLogger` 已提供 `slog.Handler` 上下文提取和实例化装饰。直接复用该能力与 Go 的 JSON handler。Kratos 请求日志中间件会默认提取请求参数和错误内容。Assistant 在业务入口一次写入固定安全字段，同时覆盖一元和流式调用。[Kratos 日志构造](https://github.com/go-kratos/kratos/blob/v3.0.0/log/builder.go) · [Kratos 请求日志](https://github.com/go-kratos/kratos/blob/v3.0.0/middleware/logging/logging.go) · [Go slog](https://pkg.go.dev/log/slog@go1.25.0#JSONHandler)

## 通过条件

- 原生 Go 测试确认两套日志输出和级别独立。对照标准 `slog` 与 Kronos，只有设置合法请求 ID 的上下文会追加 `request_id`。Go `Example` 展示调用。
- 给默认 Kratos 中间件和 Assistant 日志入口传同一个包含私有提示词的请求，以实际输出核对参数、内部错误和固定状态字段。失败分类与上一切面的错误映射一致。
- 在 Linux 构建并启动真正的 Assistant。四个入口可调用，输出能逐行解码。覆盖请求 ID、失败字段、重载错误和 `SIGTERM` 后的正常退出事件。
- Order 的实际摘流测试捕获双协议接纳、拒绝和完成事件，核对在途数量从 1 增至 2，最终回到 0，并检查相应状态、稳定原因和耗时。真实断连测试覆盖以下结果。短期限调用独立结束，共享查询达到期限后返回明确发现错误。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 关闭工作区运行 `go -C framework/log test -count=10 ./...` 和 `go test -count=1 ./examples/assistant/...`，均通过。Assistant 的真实进程输出逐行可解码，标准调用的标识贯穿成功及校验失败，重载错误保留候选退出原因。`SIGTERM` 正常退出后出现 `app_stopped`。退出码 23 的重载候选由 Go 测试程序构建。[真实进程](../../examples/assistant/cmd/assistant/main_test.go) · [候选测试](../../examples/assistant/cmd/assistant/reload_test.go)
- 对照测试在相同上下文中运行 Go 的 JSON handler 与 Kronos logger，两者保持标准消息和级别，Kronos 自动追加显式请求标识。用同一个样例提示词和失败错误执行默认 Kratos 日志中间件与 Assistant 实际入口。默认中间件包含提示词和内部错误，Assistant 输出包含 `request_id`、`operation`、`status` 和 `duration_us`。[实例与标准库对照](../../framework/log/json_test.go) · [请求日志对照](../../examples/assistant/cmd/assistant/logging_test.go)
- 在 macOS arm64 关闭工作区，日志模块和 Assistant 的 `go test -race -count=1`、根模块与日志模块的 `go vet` 和 `go mod tidy -diff` 均通过。复核关注业务实例与进程输出的所有权，库构造独立 logger，独立命令负责让 Kratos 传输日志使用同一个 JSON writer。[可执行示例](../../framework/log/example_test.go)
- Ubuntu 24.04 Linux arm64 使用真实 etcd 运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。摘流时两路日志记录接纳、拒绝与完成，验证在途从 1 增至 2，完成后为 0，并核对稳定失败原因、耗时和客户端尝试次数。断连测试同时验证短期限调用和共享发现超时。macOS arm64 的全仓竞态和依赖边界校验通过。[实际 RPC 日志断言](../../test/interop/microv2/observation_test.go)
