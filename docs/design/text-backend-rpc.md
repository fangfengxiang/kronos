# Assistant 调用独立文本后端

Assistant 的装配入口可以在本机或受控网络中运行独立的 TextBackend 进程，以同一份 RPC 契约验证模型端调用。参考后端确定性地产生文本，适合验证请求、错误和生命周期。默认仍由 Assistant 在进程内产生确定性文本。

## 契约与所有权

- `kronos.textbackend.v1.TextBackendService.Generate` 由独立 Proto 定义，使用标准 gRPC。一元请求携带 prompt，响应携带文本。非法输入使用 `ERROR_REASON_INVALID_PROMPT`，内部错误使用 `ERROR_REASON_GENERATION_FAILED`。参考实现返回 `backend: ` 加原始文本。
- `examples/textbackend` 的公开消息和一元服务接口由 `platform/protogen` 生成。公开的 `NewGRPCServer`、`NewGRPCClient`、`Generate` 和 `Close` 使用标准库、协议与 Kronos 类型。Go gRPC 生成服务和 Kratos 客户端及服务端留在私有绑定与生成代码中。
- Assistant 可用 `-text-backend 127.0.0.1:9290` 或同名启动 JSON 将现有 Blades 文本源接到后端。应用入口获取一个可复用的连接，关停时在各业务入口排空后关闭。入站请求标识随下游调用传递，调用沿用上游取消与期限。缺失期限时，客户端默认预算为两秒。连接异步建立，Assistant 就绪由自身监听与接流状态确定。
- 后端生成失败进入模型错误路径，由 Assistant 返回 `ERROR_REASON_RUN_FAILED`。取消与超时沿用按上下文确定的状态。成功文本沿用已有的一元汇总和流增量分段。固定地址路径使用明文 gRPC，适用于本机和受控网络。

## 复用与取舍

参考服务在进程入口按配置创建并复用生成客户端，在业务函数中传输有类型的消息。其装配调用持有 gRPC 连接，部分业务调用从新的背景上下文开始。本切面通过 Kronos 构造器隐藏连接类型，沿请求上下文传递取消、期限和标识，并显式交接资源所有权。保持固定地址，建立后续服务发现的可比较基线。

官方生成器继续拥有 wire 编解码、服务注册和方法调用。复用 `framework/rpc.UnaryClient` 的期限和关闭语义以及 Kratos v3.0.0 的传输。两个服务使用同一生成命令，各自声明 Proto 输入和产物路径。根据两个绑定的实测结果再提炼可共享的生成接线。[gRPC Go 生成代码](https://grpc.io/docs/languages/go/generated-code/) · [gRPC 期限](https://grpc.io/docs/guides/deadlines/) · [Kratos gRPC 客户端 v3.0.0](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/grpc/client.go) · [Buf v2 生成配置](https://buf.build/docs/configuration/v2/buf-gen-yaml/)

固定地址客户端采用 gRPC 的 `pick_first` 策略并显式设置本地服务配置。Kratos v3.0.0 的默认选择器在独立 gRPC 包初始化时可能尚未取得全局 builder。直连采用标准策略所需的地址解析和连接逻辑。[gRPC 负载策略](https://grpc.io/docs/guides/custom-load-balancing/) · [Kratos 默认选择器注册](https://github.com/go-kratos/kratos/blob/v3.0.0/transport/grpc/balancer.go)

启用进程注册时，Consul 通过标准 gRPC 健康检查维护可调用的地址集合。[Consul 服务注册与订阅](consul-service-registration.md)

## 通过条件

- 生成验收核对两个服务的私有 wire 和公开契约，Assistant 现有产物字节一致。依赖检查约束两个应用的业务和装配包直接导入 Kratos 与 gRPC。
- 在真实回环监听，将 Kronos 客户端与同一协议的标准 gRPC 客户端对照。检查响应、可识别的错误、请求标识、调用超时及关闭重用行为。公开用法由原生 Go `Example` 演示。
- 在 Linux 启动两个真实子进程，从 Assistant 调用下游并取得文本。覆盖一元、gRPC 流和后端退出后的稳定错误。两个进程可由 `SIGTERM` 有序关闭。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 上运行 `GOWORK=off go test -count=3 -v -run '^(TestAssistantCallsTextBackend|TestKronosAndStandardGRPCClients|ExampleNewGRPCClient)$' ./examples/assistant/cmd/assistant ./examples/textbackend/...`，双进程和客户端对照各通过三轮，可执行示例也通过。对照标准客户端核对 Unicode 响应及请求标识、`InvalidArgument` 和结构化错误原因。Kronos 客户端补充验证默认期限传到服务端、内部错误处理与可重复关闭。[客户端对照](../../examples/textbackend/internal/binding/interop_test.go)
- 双进程验收从 HTTP 一元和 gRPC 流得到 `backend: 你好`。后端停止后，Assistant 仍就绪，调用返回 `ERROR_REASON_RUN_FAILED`。最终关停后所有端口可重新绑定。实测据此把下游传输错误归入 Assistant 的模型失败路径。[双进程测试](../../examples/assistant/cmd/assistant/text_backend_test.go)
- Linux 的 Buf 1.70.0 运行 `go run ./platform/verify proto` 通过。两份协议均核对格式、lint 和生成产物，Assistant 现有生成内容保持字节一致。macOS arm64 的全模块 Go 竞态、依赖检查和 Linux Compose 回归均通过。Kratos 和 gRPC 沿用仓库已有版本。[Go 验收](go-verification.md) · [可执行示例](../../examples/textbackend/example_test.go)
- 使用 `GOWORK=off GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build` 交叉构建两个命令通过。双进程信号与运行验收在 Linux 和 macOS 执行。
