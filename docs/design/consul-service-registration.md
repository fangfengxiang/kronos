# Consul 服务注册与健康地址订阅

TextBackend 的启动入口可以向部署方指定的 Consul agent 注册自身。调用方或运维工具可以通过 `infra/consul` 查找健康实例，并观察新增、移除与全空快照。

Assistant 可以把订阅接到 gRPC 连接，按服务名调用当前健康实例。[动态服务发现](text-backend-discovery.md)

## 契约与所有权

- `infra/consul` 是独立 Go 模块。`New` 接受 HTTP(S) agent 地址，沿用官方 SDK 的 ACL 和 TLS 环境配置。`Register` 使用 `Instance{ID, Name, Address}` 注册明文 gRPC 实例，按 ID 替换检查。`Deregister` 按 ID 注销，实例已经不存在时可重复调用。调用方提供请求上下文，负责取消在途订阅后关闭空闲 HTTP 连接。
- Consul 对实例做标准 gRPC 健康检查。初始状态为 critical，每两秒检查一次，单次检查一秒。`Lookup` 和 `Watch` 查询 `grpc` 标签及 passing 状态。地址去重并排序。`Watch` 同步交付首份成功快照及其后的地址变化，包括空集合。调用方负责及时完成回调。SDK 对查询失败重试，调用方取消订阅后 `Watch` 返回对应的上下文错误。
- TextBackend 设 `-consul http://127.0.0.1:8500` 时，以 `textbackend` 名称和本进程唯一 ID 注册。监听绑定后注册，标准 gRPC 健康检查通过后进入发现结果。`-advertise-grpc` 可指定 Consul agent 与调用方可达的 `host:port`。未指定时公布实际监听地址。注册失败回滚启动，停止时在关停预算内尝试注销，再结束 gRPC 服务。

## 复用与取舍

### Kronos 应用接线

使用统一一元入口的业务可用 `consul.NewRPCRegistry(endpoint)` 构建适配器，并将同一实例传给服务端和客户端的 `kronos.Registry`。调用入口持有并关闭适配器。Consul 路径使用标准 gRPC，通过健康检查后进入发现结果。应用或客户端声明 MicroRPC 时在构造阶段返回 `kronos.ErrUnsupportedProtocol`。原有 `New`、`Watch` 和手工实例接线继续提供给 TextBackend 和 Assistant。

适配器复用现有注册、passing 查询与注销。每次发布生成独有实例 ID，在监听就绪后开始注册，应用关停调用返回的注销函数。注册响应失败可能发生在 agent 已接收后，按同一 ID 做有界回滚并保留两项错误。使用官方 agent 启动 Order，验证生成客户端按名取得预期订单，停机后健康结果及 agent 记录均消失。[Consul 注册接口](https://developer.hashicorp.com/consul/api-docs/agent/service#register-service) · [健康查询](https://developer.hashicorp.com/consul/api-docs/health#list-nodes-for-service)

参考服务在进程入口注入注册器，由官方 Consul API 注册实例、查询 passing 节点和订阅服务更新。本模块复用同一类上游接口，订阅由调用上下文结束。保留任意时刻健康实例集合，实例 ID 与进程生命周期对齐。具体版本和上游选择见 [ADR 0007](../adr/0007-consul-service-registration.md)。

注册地址来自实际监听，支持配置 `:0` 时公布真实端口。通配地址的部署方通过 `-advertise-grpc` 提供可达地址。注销请求有一秒上限，传输仍使用应用剩余的关停预算。Consul health checks 在进程非正常退出时剔除不健康实例，持续异常实例由 Consul 的自动注销配置清理。[Consul 注册接口](https://developer.hashicorp.com/consul/api-docs/agent/service#register-service) · [Consul gRPC 健康检查](https://developer.hashicorp.com/consul/api-docs/agent/check)

## 通过条件

- 用原生 Go 测试校验注册载荷、上下文取消、地址规整、空快照和实际资源释放，提供不需要 Consul 服务的可执行 `Example`。
- 在 Ubuntu 运行官方 Consul agent 和真实 TextBackend 子进程。对照官方健康查询和 Kronos 订阅，核对服务出现、gRPC 可调用、SIGTERM 后下线及空快照。
- 核对 Go 1.25.0 各模块构建、Linux 实测和跨层导入。将平台边界和操作方式同步到使用文档。

## 实测与复盘

- 在 Go 1.25.0、Ubuntu 24.04 Linux arm64 上运行 `GOWORK=off go -C infra/consul test -count=4 ./...`。测试覆盖 IPv6 注册、地址去重及节点地址回退、可重复注销、订阅首个空快照、地址未变时抑制通知，以及取消阻塞 HTTP 请求。[适配测试](../../infra/consul/registry_test.go)
- 使用官方 Consul 2.1.0 执行 `KRONOS_CONSUL_EXECUTABLE=/path/to/consul GOWORK=off go test -tags=consul_integration -count=3 -v ./test/integration/consul`，三轮通过。TextBackend 监听 `127.0.0.1:0`，两种查询都观察到实际分配的地址并可以调用。`SIGTERM` 后 agent 记录被删除，订阅交付空快照，取消订阅正常返回。[官方发行包](https://releases.hashicorp.com/consul/2.1.0/) · [真实 agent 测试](../../test/integration/consul/registration_test.go)
- 在同一 Linux 环境运行 `GOWORK=off go test -count=5 -run '^TestRegistrationFailureWithdrawsInstanceAndClosesPort$' ./examples/textbackend/cmd/textbackend`。复核请求失败边界后加入同 ID 的注销回滚。模拟注册响应异常时，入口发出回滚注销并释放端口。[启动回滚测试](../../examples/textbackend/cmd/textbackend/registration_test.go)
- macOS arm64 的全模块 Go 竞态测试、跨层依赖检查与 Linux Compose 回归通过。Consul SDK 适配和 TextBackend 命令通过 Windows amd64 交叉构建，真实 agent 与进程验收运行于 Linux。
- Go 1.25.0、Ubuntu 24.04 Linux arm64 与官方 Consul 2.1.0 执行 `GOWORK=off go test -tags=consul_integration -count=1 -v ./test/integration/consul`。Order 经健康发现取得订单，关停后 passing 查询为空且 agent 注册记录消失。应用和客户端在构造时拒绝 Consul 的 MicroRPC 配置。同一套件的 TextBackend 注册及 Assistant 发现通过。Consul 包在 Linux 通过三轮，macOS arm64 的 Consul、Kronos 包均通过三轮竞态回归。回环模拟注册响应失败，确认使用同一实例 ID 注销。[实际 Order 接线](../../test/integration/consul/order_test.go) · [注册失败夹具](../../infra/consul/rpc_test.go)
- Ubuntu 24.04 Linux amd64 与 etcd v3.6.16 复跑真实租约及 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`，双协议、两版官方旧端、摘流和恢复用例通过。Linux 的 `go run ./platform/verify deps` 通过。[etcd 回归入口](go-verification.md#order-治理验收)
