# Order 运行观测

## 使用者与责任

Order 维护者需要区分实例仍在运行、正在接纳请求和已在 etcd 中可发现，并能比较标准 gRPC 与 MicroRPC 的请求结果、耗时和在途数量。系统目标与故障动作见[系统运行反馈](../architecture/rpc-system.md#运行反馈与风险审查)。

应用入口按需创建独立的 Prometheus 适配实例。Kronos 使用 `Metrics` 注入一元调用观察点。使用 `Diagnostics(address, handler)` 明确监听地址以及挂载到 `/metrics` 的标准 HTTP handler。诊断端口复用 `framework/app` 绑定与关停，探针复用 `App.Probes`。`/livez` 和 `/readyz` 可分别观察进程窗口和接纳状态。需要确认可发现时，调用端使用对应协议查询 etcd，并执行实际业务请求。诊断端口由部署配置持有，两个业务协议所有的注册数据继续来自各自监听。

## 信号及含义

| 信号 | 记录范围 |
| --- | --- |
| `kronos_rpc_requests_total` 和 `kronos_rpc_request_duration_seconds` | 一次客户端逻辑调用或一次服务端绑定调用完成时记录。客户端覆盖发现、选择和所有尝试，服务端覆盖解码后进入绑定的请求。结果细分 success 和 error。 |
| `kronos_rpc_in_flight` | 开始观测时增加，完成时减少。按 Proto 方法、协议和 client/server 角色区分，包含进入绑定后被摘流拦截的短暂处理。 |
| `rpc_started`、`rpc_finished`、`rpc_client_finished` | 继续记录协议、方法、稳定状态、错误原因、服务端业务在途和客户端尝试次数。 |
| 续约与发现事件 | etcd 发布者记录租约异常和恢复，客户端记录共享查址失败。对照业务判定和地址变化定位影响。 |

新的一元服务入口使用 `prometheus.NewServiceRPCMetrics()`。应用启动前从已注册的 Protobuf 绑定取得完整方法路径，并通过观察器的可选能力预建指标。指标名称、标签及初始零值沿用现有契约。与应用共享观察器的客户端可以记录这些方法。独立调用方可用 `NewRPCMetrics(operations...)` 声明固定集合。任意未登记的请求值不会创建时间序列，重复登记同一绑定保留已有观测值。Assistant 的现有指标入口保持原样。[Prometheus 指标初始化](https://prometheus.io/docs/practices/instrumentation/#avoid-missing-metrics) · [标签开销](https://prometheus.io/docs/practices/naming/#labels)

## Trace 与调用定位

应用入口创建并持有已有的 OTLP HTTP provider，通过 `kronos.Tracing` 交给应用或客户端。一次逻辑调用记录一个客户端 span。服务端对实际到达的每次调用记录自己的 span。传输只复制 W3C `traceparent` 和 `tracestate`，gRPC 走 metadata，MicroRPC 走 HTTP 请求头。原调用上下文继续持有期限和取消。Order 可以按部署配置开启导出，并在退出时用 5 秒预算关闭自己创建的 provider。[已有 Trace 适配](request-tracing.md) · [OpenTelemetry 上下文传播](https://opentelemetry.io/docs/concepts/context-propagation/)

日志继续记录稳定结果。现有 JSON logger 从请求上下文取 `trace_id` 和 `span_id`。用指标的协议、方法和角色定位时间窗口，再由同维度的结构化日志关联 OTLP span。span 属性记录协议、角色和稳定 RPC 状态。业务消息、目标地址和任意错误文本保留在业务与诊断边界。共享查址独立于任一调用方，查址失败只有客户端逻辑 span，不产生虚构的服务端请求。[日志字段](structured-logging.md#统一一元-rpc-的事件)

## 通过条件

- 用真实 etcd 启动 Order，两个监听均有正确的成功和错误计数。使用两个 Kronos 客户端，分别核对客户端和服务端的观测点。
- 在两个业务请求仍在途时抓取指标，核对协议各有一个服务端请求。启动摘流后，探针分别返回存活和未就绪，etcd 地址由注册中心观察方核对。释放请求后校验在途值归零，诊断端口随应用释放。
- 使用 Order 命令的可选配置取得实际 `/livez`、`/readyz` 和 `/metrics`。默认核心装配行数、依赖隔离和旧客户端互通继续通过。
- Order 装配只启用指标观察器。两种协议的服务端方法序列由生成绑定提供，首个请求之前即可抓取零值。登记错误在启动监听前返回。
- 使用实际 etcd、Order 进程与 HTTP OTLP 接收端。分协议完成业务成功和稳定失败，核对客户端与服务端 span 的父子关系、JSON 日志中的 trace/span ID，以及双方计数。查询无实例时验证客户端 span 和零发送次数，并确认没有服务端 span。

## 指标与探针实测

- Go 1.25.0、Ubuntu 24.04 Linux amd64、官方 etcd v3.6.16 执行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。Order 命令启用实际诊断端口，两协议的成功数、错误数与耗时样本均符合调用记录。摘流测试在两个协议各有一个在途调用时抓取指标。进入摘流时，同一 HTTP 连接观察到 `/livez` 200、`/readyz` 503。调用返回后客户端和服务端的在途数归零，三个监听均可重新绑定。[命令验收](../../test/interop/microv2/etcd_test.go) · [摘流验收](../../test/interop/microv2/drain_test.go)
- macOS arm64 通过 `go run ./platform/verify deps`、`GOWORK=off go -C infra/prometheus test -race ./...`、`GOWORK=off go -C platform/kronos test -race ./...` 和 `GOWORK=off go test -race ./examples/order/...`。Order 核心装配 22 行。[指标实现](../../infra/prometheus/rpc.go) · [行数断言](../../examples/order/cmd/order/main_test.go)
- Go 1.25.0、Ubuntu 24.04 Linux amd64 与真实 etcd v3.6.16 复跑完整互通、恢复与观测套件。仅开启服务指标观察器时，两种协议在首个请求前即暴露已登记方法的零值，调用后计数和在途指标正确。Prometheus 包在 Linux 通过三轮。macOS arm64 的 Prometheus、Kronos 和 Order 竞态回归通过。通过占用指定监听端口验证指标登记失败先于监听绑定，`Run` 保留原错误。[双协议与零值](../../test/interop/microv2/drain_test.go) · [登记失败时序](../../examples/order/cmd/order/main_test.go)

## Trace 实测

- Go 1.25.0、Ubuntu 24.04 Linux amd64 与真实 etcd v3.6.16 运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。gRPC 和 MicroRPC 的成功、校验失败均导出匹配的客户端和服务端 span。OTLP 共收到九个 span。日志中的失败 trace/span ID 与导出一致，两侧指标数匹配实际调用。空地址查询保留 `NO_INSTANCES` 和零发送次数，仅出现客户端 span。[端到端信号验收](../../test/interop/microv2/trace_test.go)
- macOS arm64 关闭工作区，`infra/oteltrace`、`infra/microv2`、`platform/kronos`、Order 及 Assistant 命令包的 Go 竞态测试通过。分层检查通过。复用已接入的日志、Prometheus 和 OTLP 适配，两个业务协议共用一个观察点。[Trace 适配](../../infra/oteltrace/provider.go) · [RPC 观察点](../../platform/kronos/observation.go)
