# 标准 gRPC 流客户端

## 消费者与契约

使用 Assistant 的 Go 调用方通过 `Client.StreamRun(ctx, input, receive)` 顺序接收公开的 Protobuf 流事件。客户端由标准 `NewRPCClient` 创建，一元和流调用共用连接。调用方传标准上下文，回调返回错误时立即结束本次调用并取消该流。

- 文本增量到达后按序交给回调。客户端观察到唯一完成事件与正常 EOF 后，才向回调交付完成事件并返回成功。缺少完成事件、无法识别的事件或完成后仍收到事件时，返回可用 `errors.Is` 判断的 `rpc.ErrInvalidStream`。已交付的增量可作为部分结果。
- 传输失败沿用公开 RPC 错误转换，取消和超时使用 Go 上下文错误。调用方提供的截止时间优先，否则使用客户端默认值。客户端关闭后拒绝新调用，已经开始的流随底层连接结束。
- 标准 gRPC 流在发起时附带显式请求 ID 和语言偏好。HTTP JSON 和 go-micro v2 一元客户端调用此方法时返回 `rpc.ErrStreamingUnsupported`。SSE 由独立 HTTP 流入口提供。[请求元数据](request-metadata.md) · [框架语言协商](framework-localization.md)

## 边界与取舍

- 参考服务在私有边界持有 gRPC 流，通过自己的接口向业务提供消息和退出信号。当前服务端流由调用方同步消费，复用 gRPC 的 `Recv`、流控和上下文取消，转发每条事件无需额外后台任务或缓冲队列。[gRPC Go 服务端流](https://grpc.io/docs/languages/go/basics/#server-side-streaming-rpc) · [gRPC 客户端流接口](https://pkg.go.dev/google.golang.org/grpc@v1.81.1#ClientStream)
- 连接创建、存根和流收发由现有 Kratos 传输组合承担。Kratos v3.0.0 的流中间件发生在建流后收发时，私有绑定在建流前通过 gRPC 上下文设置声明的字段。框架复用一元调用已有的截止时间和关闭检查。完成事件由 Assistant 协议定义，其顺序校验留在服务绑定中。[Kratos 客户端](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/grpc/client.go) · [Assistant 协议](assistant-protocol.md)

## 通过条件

- 实际 Assistant 流通过公开 Go API 接收增量与完成标记，同时验证上游收到请求 ID、本地化错误以及回调提前结束后服务端收到取消。
- 用同一个 gRPC 服务比较生成客户端的 EOF 与 Kronos 的协议终态判断。覆盖缺少完成、完成后又收到事件以及完成后返回非 OK 状态。
- 在 Linux 运行公开客户端与 RPC 模块测试。Go `Example` 展示只使用 Kronos 调用和接收事件。

## 实测与复盘

- 使用 Go 1.25.0 在 Ubuntu 24.04 Linux arm64 关闭工作区，运行 `go -C framework/rpc test -count=1 ./...` 与 `go test -count=1 ./examples/assistant/...`，均通过。实际 Assistant 子进程用公开客户端读取两条中文增量、完成事件和稳定的中文校验错误。[真实进程测试](../../examples/assistant/cmd/assistant/main_test.go)
- 同一个上游 gRPC 服务只给出增量便正常关闭时，原生生成客户端读到正常 EOF，Kronos 返回 `ErrInvalidStream` 并保留已交付的增量。完成事件后收到额外事件或非 OK 状态时，业务回调没有收到完成通知。复盘将完成事件的交付放在 EOF 检查之后，让业务看到的完成与传输结果一致。[对照测试](../../examples/assistant/stream_protocol_test.go)
- macOS arm64 下流测试与 `ExampleClient_StreamRun` 在竞态检查中重复十轮通过，`go run ./platform/verify test` 与 `go vet ./...` 通过。回调提前结束后服务端收到 `context.Canceled`。已到期请求返回 `context.DeadlineExceeded`，关闭后的客户端拒绝新流。[请求 ID 与取消](../../examples/assistant/request_test.go) · [可执行示例](../../examples/assistant/stream_example_test.go)
