# 统一应用的服务端流

Assistant 的 `StreamRun` 使用单条 Protobuf 请求和有序响应事件。服务端以一个类型化回调交付事件，gRPC 和 SSE 共享就绪、请求 ID、语言、W3C 链路、错误映射、观察点和应用关停。由业务协议决定完成事件，框架交付有序消息和传输终态。MicroRPC 和 HTTP 一元监听只暴露一元方法。

保持 `Bind` 的一元签名，使用 `Service.WithServerStreams(kronos.ServerStream(...))` 补充流方法。注册验证每个方法的消息类型与流形状。SSE 由独立 `kronos.SSE(address)` 显式监听，使用已声明的静态 POST 与整条消息请求体。该入口沿用统一 HTTP 的 ProtoJSON 输入和边界。gRPC 复用 Kratos 的底层服务注册和 grpc-go 的服务端流。SSE 复用 Kratos 的事件编码与终态处理。传输开始前的失败使用 HTTP 状态，发送事件后使用 `event: error`。[gRPC 服务端流](https://grpc.io/docs/what-is-grpc/core-concepts/) · [Kratos SSE 实现](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/http/stream.go)

等待业务回调时沿用原请求上下文。Kratos 的 SSE 对象清除取消，框架以原 HTTP 上下文驱动业务并设置每次事件写入期限。传输发送失败或断开需要结束业务。应用在每个逻辑流结束时记一次观测，省去逐消息重复统计。兼容入口继续只接收一元调用。已有 Assistant 的客户端和文本流接口保留，使用同一文本到事件的转换验证新绑定。

验收使用真实 Assistant 的公开 Protobuf API 与业务契约，联调标准 gRPC 客户端和 HTTP SSE。检查 Unicode 增量和完成顺序、首帧前错误和部分输出后的失败、取消、元数据、关闭。运行既有 Assistant 客户端、旧协议和一元回归。

参考接入使用 `assistant.BindService(unary, streams)`。它将现有文本输出转换为同一份 Protobuf 事件，在成功结束时追加一个完成消息，并沿用 Assistant 的 30 秒一元预算和稳定错误。Assistant 命令继续使用原有启动装配。后续切换入口时需验证 HTTP 探针与指标地址、性能监听及 Tableflip 重载。[参考绑定](../../examples/assistant/kronos.go)

## 实测

Go 1.25.0、Ubuntu 24.04 Linux arm64 执行 `GOWORK=off go test -count=5 -run '^TestUnifiedAssistantStreaming$' ./examples/assistant`，五轮通过。一个应用运行四个业务监听，验证 gRPC 与 SSE 的 Unicode 增量、完成顺序、请求 ID、首帧前及部分输出后的稳定错误、断开取消、每个逻辑流一次成功指标，以及两个已接纳的流在关停后完成。Assistant 旧入口和真实 etcd 的 Order 治理及官方 go-micro v2 联调通过。macOS arm64 的同一流式用例通过三轮竞态，分层、Proto 重生成和相对 `311eff1` 的兼容门禁均通过。[真实监听用例](../../examples/assistant/unified_test.go)
