# 文本后端的动态服务发现

Assistant 可按服务名调用随时变化的 TextBackend 实例。参考部署使用 `-text-backend-consul http://127.0.0.1:8500` 或同名启动 JSON 查询 `textbackend`。固定地址配置 `-text-backend` 与按名称配置各自提供一种文本源，两者同时启用时启动失败。

## 契约与所有权

- `textbackend.NewDiscoveredGRPCClient` 接收逻辑服务名、调用默认期限与 `EndpointDiscovery`。消费侧接口只要求 `Watch(context.Context, string, func([]string)) error`。提供者交付首个完整快照和后续更新，并在取消后及时返回。
- Kratos 继续管理标准 gRPC 客户端。一条连接用官方 `round_robin` 策略在就绪实例间选址。增减端点沿用同一个连接和类型化调用 API。空快照移除已退订的实例，返回 Kronos 的 `UNAVAILABLE`。Assistant 将该失败映射为 `ERROR_REASON_RUN_FAILED`。
- 订阅跟随 gRPC 连接，构造时的上下文约束构造过程，调用上下文约束单次 RPC。应用排空后先关闭客户端并结束订阅，再释放注册查询的 HTTP 连接。查询重试期间保留最近成功的地址集合，订阅意外退出时清空地址并报告错误。

## 复用与取舍

参考入口按逻辑名称使用客户端池，解析器把健康快照更新到 gRPC。保留按名称定位和端点快照更新。官方 gRPC 管理连接和轮询策略，Kronos 私有绑定接入语义明确的快照。Kratos v3.0.0 内置发现解析器跳过零端点，适配通过 `grpc.WithResolvers` 为单个客户端设置解析器，让 `round_robin` 应用空集并删除旧子连接。[Kratos 发现解析器](https://raw.githubusercontent.com/go-kratos/kratos/v3.0.0/transport/grpc/resolver/discovery/resolver.go) · [gRPC 名称解析](https://grpc.io/docs/guides/custom-name-resolution/) · [gRPC 地址集合更新](https://github.com/grpc/grpc-go/blob/v1.81.1/balancer/endpointsharding/endpointsharding.go#L189)

目录公布服务端点，数据面按发布地址直接连接。关闭解析器时可以等待订阅退出，避免地址更新和关闭争用代理解析的内部锁。`WithResolvers` 与 `WithNoProxy` 的使用限定在私有绑定并由双实例回归约束。[gRPC 客户端选项](https://pkg.go.dev/google.golang.org/grpc@v1.81.1#WithResolvers) · [gRPC 解析器关闭实现](https://github.com/grpc/grpc-go/blob/v1.81.1/resolver_wrapper.go)

## 通过条件

- 真实 gRPC 监听结合可控快照，验证首个空集、两实例轮询、全部移除后旧连接停止分配、恢复以及关闭取消观察。原生 Go `Example` 只使用公开 API。
- 在 Ubuntu 搭配官方 Consul，启动真实 Assistant，对同一服务注册两个可区分的 TextBackend 实例。两实例均接到调用。注销仍运行的实例后，调用只到剩余实例。最后一个实例注销后返回稳定错误，重新注册后同一客户端恢复。
- 固定地址联调、仓库竞态测试、Go 分层检查和 Assistant 默认容器验收继续运行。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 上运行 `GOWORK=off go test -count=6 -run '^(TestDiscoveredClientMatchesRoundRobinAndClearsRemovedEndpoints|ExampleNewDiscoveredGRPCClient|TestAssistantRequiresOneTextBackendSource)$' ./examples/textbackend/... ./examples/assistant/cmd/assistant`。Kronos 和官方客户端均完成两实例轮询，空集后连续调用均返回不可用，再发布实例后可调用。构造上下文结束后订阅继续，客户端关闭后订阅退出。[标准 gRPC 对照](../../examples/textbackend/internal/binding/discovery_test.go)
- 在同一环境用官方 Consul 2.1.0 执行 `KRONOS_CONSUL_EXECUTABLE=/path/to/consul GOWORK=off go test -tags=consul_integration -count=3 -v ./test/integration/consul`，注册和发现各通过三轮。Assistant 在已注销的两个实例仍可直连时返回 `ERROR_REASON_RUN_FAILED`，重新注册后同一进程和调用客户端恢复，通过 `SIGTERM` 正常退出。[双实例实测](../../test/integration/consul/discovery_test.go)
- macOS arm64 的全模块竞态、分层检查、`go vet` 及 Linux Compose 回归均通过。两个命令通过 Windows amd64 交叉构建。复核据此保留 gRPC 的连接管理和轮询能力，应用只接入健康地址快照及其生命周期。
