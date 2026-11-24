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

方法由装配入口预先声明。Prometheus 标签只取这些方法、固定协议、两种角色和结果。复用进程内的 Counter、Histogram、Gauge 和独立 registry。原有 Assistant 计数器和标签保持其公开契约。启用诊断时可在同一实例观察成功、错误、在途与就绪，而恢复验收仍通过按服务名调用取得订单。[Prometheus 在线服务的信号](https://prometheus.io/docs/practices/instrumentation/#online-serving-systems) · [Google SRE 监测](https://sre.google/sre-book/monitoring-distributed-systems/)

## 通过条件

- 用真实 etcd 启动 Order，两个监听均有正确的成功和错误计数。使用两个 Kronos 客户端，分别核对客户端和服务端的观测点。
- 在两个业务请求仍在途时抓取指标，核对协议各有一个服务端请求。启动摘流后，探针分别返回存活和未就绪，etcd 地址由注册中心观察方核对。释放请求后校验在途值归零，诊断端口随应用释放。
- 使用 Order 命令的可选配置取得实际 `/livez`、`/readyz` 和 `/metrics`。默认核心装配行数、依赖隔离和旧客户端互通继续通过。

## 指标与探针实测

- Go 1.25.0、Ubuntu 24.04 Linux amd64、官方 etcd v3.6.16 执行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。Order 命令启用实际诊断端口，两协议的成功数、错误数与耗时样本均符合调用记录。摘流测试在两个协议各有一个在途调用时抓取指标。进入摘流时，同一 HTTP 连接观察到 `/livez` 200、`/readyz` 503。调用返回后客户端和服务端的在途数归零，三个监听均可重新绑定。[命令验收](../../test/interop/microv2/etcd_test.go) · [摘流验收](../../test/interop/microv2/drain_test.go)
- macOS arm64 通过 `go run ./platform/verify deps`、`GOWORK=off go -C infra/prometheus test -race ./...`、`GOWORK=off go -C platform/kronos test -race ./...` 和 `GOWORK=off go test -race ./examples/order/...`。Order 核心装配 22 行。[指标实现](../../infra/prometheus/rpc.go) · [行数断言](../../examples/order/cmd/order/main_test.go)
