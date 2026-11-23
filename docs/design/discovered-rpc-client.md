# 统一发现客户端

## 调用者契约

`kronos.NewClient` 接收显式注入的 registry。默认使用标准 gRPC。调用 `kronos.Protocol(kronos.ProtocolMicroV2)` 才启用 go-micro v2。配置未知协议时返回 `ErrUnsupportedProtocol`。生成的 `order.NewOrderServiceClient` 提供类型化方法。默认发现名来自 Proto 服务全名。迁移旧服务时使用 `kronos.Name` 显式覆盖发现名。

一个客户端可以持有多个服务的连接，由创建者调用 `Close`。连接与查询生命周期归客户端，注入的 registry 仍归其创建者。默认调用期限为 3 秒，只在调用方没有期限时生效。同一次调用的发现、发送和重试共享原有上下文预算。取消和期限错误保留 Go 语义。

go-micro v2 通过纳秒级 `Timeout` 传递剩余预算。Kronos 兼容监听对单次业务处理保留 30 秒上限，客户端更短的期限和显式取消继续生效。[旧协议请求规则](additional-listeners.md#go-micro-v2-兼容范围)

## 实例、失败及资源

- 按服务名与明确的协议查询注册中心。连续调用复用最近的地址快照和连接。同一服务的并发调用共用一次最多 2 秒的查询和一个完成信号。每个调用方可以独立取消等待。快照按需刷新，查询失败时报告错误。关闭客户端取消查询和在途调用。
- 一个服务内轮询可用实例。端点不可达或返回摘流状态时短暂避开该地址。地址从快照移除后，已有调用可以使用其连接完成，最后一个调用释放连接。注册中心变化由下一轮按需刷新观测。
- Proto 标明 `NO_SIDE_EFFECTS` 或 `IDEMPOTENT` 时，首个端点不可用后在原预算内至多换一个实例。业务错误沿稳定错误返回。复用 grpc-go 与旧协议适配，保持传输各自的取消语义和地址类型。[gRPC 重试](https://grpc.io/docs/guides/retry/) · [Proto 方法幂等含义](https://protobuf.dev/reference/protobuf/google.protobuf/#methodoptions)

查询合并沿用已有服务快照的锁和生命周期。原先的 `singleflight.DoChan` 为每个等待方保留通道直到查询结束。注册中心失联且短期限调用持续到达时，这些通道跟随请求量增长。服务快照持有一个完成通道，共享查询更新快照后关闭通道。等待方指向同一个值，取消后立即离开。保留有界查询与错误缓存。[singleflight 实现](https://github.com/golang/sync/blob/v0.21.0/singleflight/singleflight.go) · [Go 通道广播取消示例](https://go.dev/blog/pipelines#explicit-cancellation)

## 通过条件

- 默认 gRPC 和显式 MicroRPC 都经 etcd 找到 Order。调用代码通过生成类型使用 Kronos。
- 在真实 etcd 启动两版官方 go-micro v2 服务端，Kronos 仅按服务名发现并读取业务响应。
- 验证同一服务的轮询、实例消失、失败绕过、可重试边界、连接关闭及取消。首个发现方被取消时，其他有预算的调用仍可使用同一查询结果。
- 阻塞一次查询，连续结束多个等待方，核对期间只发起一次查询、共享信号随后释放。地址消失时关闭空闲连接，仍占用的连接由最后一个在途调用释放。客户端重复关闭保持每条连接只关闭一次。

## 实测

- Go 1.25.0、Ubuntu 24.04 Linux arm64 和真实 etcd v3.6.16，运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。默认与显式旧协议客户端都通过服务名调用 Order。Kronos 发现并调用 v2.0.0、v2.9.1 的实际官方进程，得到旧服务的业务数据和稳定参数错误。默认 gRPC 对只有旧 RPC 节点的服务返回 `NO_INSTANCES`。[双向业务互通](../../test/interop/microv2/etcd_test.go)
- 同一 Linux 实测中，两种协议均在两个真实发布的实例之间轮询。一次实例不可用后，带幂等标记的 Order 查询切到另一个节点，随后在冷却期及实例摘除后继续成功。[多实例与绕行](../../test/interop/microv2/replicas_test.go)
- macOS arm64 的 Order 竞态测试五轮通过，确认共享查询保留独立等待期限、关闭取消查询和未知协议错误。`go run ./platform/verify test` 与 `go run ./platform/verify deps` 通过，Linux 的 `go run ./platform/verify proto` 通过。[客户端边界测试](../../examples/order/client_test.go)
- macOS arm64 运行 `GOWORK=off go -C platform/kronos test -race -count=10 ./...`，64 个短期限调用依次结束时仍只发起一次查询，查询结束后完成信号释放。验证未变地址复用连接，已移除地址的连接按使用数量关闭。`GOWORK=off go test -race -count=5 ./examples/order/...` 与真实 etcd Linux 互通套件同样通过。[发现状态与连接测试](../../platform/kronos/discovery_test.go)
