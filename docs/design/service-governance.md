# 服务装配与治理

## 消费者与边界

以 Order 一元服务验证用例。服务装配声明发现名、注入的注册中心及两个独立监听，注册同一个类型安全的 Protobuf 服务实现。客户端声明注册中心和协议，从服务名发现端点。默认协议是 gRPC，显式选择兼容协议才走 go-micro v2。无法识别的协议在构造时返回错误。服务代码沿用 Kronos、标准库和公开 Protobuf。

| 职责 | 执行位置 |
| --- | --- |
| 服务方法、请求、响应和幂等标记 | 服务 Proto 与生成代码 |
| 一元调用注册、服务名解析、连接复用和错误映射 | Kronos 公开应用与客户端装配 |
| 监听、就绪和在途工作收尾 | 复用 `framework/app`，使用现有 Kratos 与旧协议编解码 |
| 存储地址、租约、续约和发现 | `infra/etcd` |

开发者显式提供监听与注册中心。应用接收监听和注册发布的生命周期，注入的注册中心由其创建方关闭。应用提供简洁的默认配置。特殊部署可显式提供对外通告地址。保留现有低层绑定供其他传输使用。[协议地址决定](../adr/0009-discovery-protocols.md) · [现有生命周期](app-lifecycle.md)

## 就绪、故障和恢复

- 存活指进程仍在运行。就绪指所需监听已能接入并且业务仍接纳新工作。可发现指相应协议的地址已确认发布。三个状态各有观察点。
- 监听就绪后发布。首次发布失败回滚本次启动。收到退出信号，先撤销就绪并摘除注册，再用剩余预算排空传输和清理。缓存地址发来的新请求经应用的接纳状态拦截。已接纳的调用继续在其原有期限内处理。
- 注册中心暂时不可达时，已接纳的业务继续处理。发布者记录续约失败并在有限期限内按周期重试。租约消失时以相同实例 ID 重新发布，恢复以新的真实请求成功为证据。
- 发现失败向调用方报告。客户端在调用期限内取得或更新实例快照，复用连接，并绕开最近失败的地址。确定允许重试的调用在剩余期限内最多换一个实例。Proto 的幂等标记界定可重放的业务请求。
- 运维要能观察发布和续约失败、发现失败、调用结果、耗时以及在途数量。故障验收同时观察注册变化、请求结果与资源收尾。[Acuity 可靠性验收](https://github.com/fangfengxiang/acuity/blob/main/SKILL.md) · [Greatsword 生命周期](https://github.com/fangfengxiang/greatsword/blob/main/skills/greatsword/references/invariant-lifecycle.md)

### 注册中心断连与进程崩溃验收

在 Linux 上启动真实 Order 进程和隔离 etcd。用测试进程持有的回环 TCP 中继中断 Order 和一组客户端到 etcd 的连接。直连 etcd 的观察端验证两个地址随租约消失，发现受阻的并发调用在各自预算内返回，已知地址的 RPC 仍可完成。恢复连接后核对续约失败、丢失租约及重新发布的事件。服务端与客户端的 etcd 连接各自退避并重连。验收让经历过发现失败的客户端继续按服务名调用，在同一个 22 秒恢复预算内取得两种协议的真实订单响应。[gRPC 连接退避](https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md)

用另一组服务名启动独立 Order 进程。确认发现调用后强制结束该测试进程，观察原记录保留到租约过期，然后核对两个地址均被删除、重新发现返回 `NO_INSTANCES`。测试中继、连接和子进程均由 Go 测试清理。[etcd 租约行为](https://etcd.io/docs/v3.6/dev-guide/interacting_v3/#grant-leases)

## etcd 适配契约

`etcd.New(endpoints ...string)` 使用显式的 TCP 端点创建独立客户端，返回初始化错误。调用方拥有客户端并调用 `Close`。平台装配用 `Publish(ctx, serviceName, grpcAddress, microRPCAddress)` 注册一代实例，至少有一个通告地址。返回值是有期限、可重复调用的摘流函数。启动上下文约束首轮发布，成功发布后的续约随注册实例和客户端的生命周期运行。调度使用 15 秒租约、每 5 秒尝试续约和每次 2 秒的 etcd 操作预算。

`Lookup(ctx, serviceName, protocol)` 读取协议自己的前缀，返回去重排序的可用地址字符串。实例不存在时返回空切片，配置错误、后端错误和上下文结束分别表达。通过完整事务绑定两种地址到同一租约。部分失败后尽力撤销新租约，保留启动错误。客户端关闭前，先用发布凭据返回的函数摘除该实例。进程异常退出时租约到期清理记录。

适配器使用 `etcd_integration` 标签验证真实租约。go-micro v2 的端到端验收使用 `interop,etcd_interop` 标签，构建 Order 进程与官方 SDK 夹具，经同一个 etcd 双向调用。两项测试通过 `KRONOS_ETCD_TEST_ADDRESS` 指向隔离实例。[Linux 验收入口](go-verification.md#order-治理验收)

参考装配沿用了注册能力注入业务进程的做法。当前通过显式实例管理配置和资源，以官方客户端承担连接、读写、事务及租约。选用接受 Go 1.25 基线的 `go.etcd.io/etcd/client/v3 v3.6.14`。实测服务器使用官方 Linux etcd v3.6.16。上游将 Windows amd64 的 etcd 服务端列为 Tier 3。Windows 开发可通过 TCP 指向 Linux etcd。当前 Windows 证据为适配模块交叉编译。[客户端模块](https://github.com/etcd-io/etcd/blob/v3.6.14/client/v3/go.mod) · [服务端发布](https://github.com/etcd-io/etcd/releases/tag/v3.6.16) · [平台支持](https://etcd.io/docs/v3.6/op-guide/supported-platform/)

## 验收目标

| 编号 | 通过条件 | 复查入口 |
| --- | --- | --- |
| T01 | Order 核心装配最多 30 行，实际运行 | [装配行数断言](../../examples/order/cmd/order/main_test.go) |
| T02 | 官方 Registry 从真实 etcd 读到兼容端口 | [官方 SDK 与真实 etcd](../../test/interop/microv2/etcd_test.go) |
| T03 | 官方 go-micro v2 Client 按名发现并完成 Order 调用 | [官方客户端到 Order](../../test/interop/microv2/etcd_test.go) |
| T04 | Kronos MicroRPC 客户端发现并调用官方 go-micro v2 服务端 | [Order 到官方服务端](../../test/interop/microv2/etcd_test.go) |
| T05 | 原生 gRPC 客户端成功调用同一服务 | [标准 gRPC 进程调用](../../test/interop/microv2/etcd_test.go) |
| T06 | 两个监听按协议发布地址，并进入同一个业务实例 | [同一实例的两路业务](../../test/interop/microv2/drain_test.go) |
| T07 | 多实例轮询、故障绕行、摘除、下一次调用成功及重试上限 | [多实例联调](../../test/interop/microv2/replicas_test.go) |
| T08 | etcd 失联时续约失败、租约过期，恢复后新请求成功 | [断连与业务恢复](../../test/interop/microv2/resilience_test.go) |
| T09 | 崩溃进程退出后，其两个地址由 etcd 移除 | [强制退出与过期](../../test/interop/microv2/resilience_test.go) |
| T10 | 摘流在排空之前，关停在预算内返回 | [实际双协议关停](../../test/interop/microv2/drain_test.go) |
| T11 | 双协议传播 Deadline 和 Cancel | [调用方期限与取消](../../test/interop/microv2/drain_test.go) |
| T12 | 发布、监听、连接及后台查询各有释放证据 | [租约](../../infra/etcd/integration_test.go) · [监听与处理器](../../test/interop/microv2/drain_test.go) · [客户端](../../platform/kronos/discovery_test.go) |
| T13 | 导入检查保护业务与底层 SDK 的边界 | [生产依赖规则](../../platform/verify/dependencies.go) |
| T14 | CI 在隔离 Linux 环境执行可重复的 Go 验收 | [CI 工作流](../../.github/workflows/ci.yml) · [本地复现](go-verification.md#order-治理验收) |

## 实测记录

- Go 1.25.0、Ubuntu 24.04 Linux arm64，使用官方 etcd v3.6.16 镜像摘要 `sha256:b29d816b546a1966fe7034a33b34e231fa98c47ce4d2883f70b0555020b22132` 与临时隔离网络。设置 `KRONOS_ETCD_TEST_ADDRESS` 后运行 `GOWORK=off go -C infra/etcd test -tags=etcd_integration -count=1 -v ./...`。直接检查双协议的同一事务、实例 ID 与租约。验证多实例、上下文取消、续约跨过原 TTL、租约删除后的相同实例重发，以及停止续约后的服务回收。[租约测试](../../infra/etcd/integration_test.go)
- 同一 Linux 环境运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。v2.0.0 和 v2.9.1 的官方 Registry 都只读取兼容端口，两版官方旧客户端均能经真实 etcd 调用 Order。原有双向直连回归也通过。v2.0.0 的测试模块显式锁定其旧 etcd 访问链所用的 `github.com/coreos/pkg`。[发现对照](../../test/interop/microv2/etcd_test.go) · [夹具构建](../../test/interop/microv2/fixture_test.go)
- macOS arm64 的 `go run ./platform/verify test` 和 `go run ./platform/verify deps` 通过。Linux arm64 的 `go run ./platform/verify deps` 和 `go run ./platform/verify proto` 通过。运行 `GOWORK=off GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C infra/etcd test -c` 生成 Windows 测试二进制。
- `framework/app.RunWithReadyHook` 已通过 Linux 回环发布和有界关停测试。HTTP 请求证实发布时监听已经可用，撤销与传输共享一个截止时间，部分失败后释放端口。[生命周期实测](app-lifecycle.md#实测与复盘)
- Order 进程的 21 行装配已运行。真实 etcd 上的原生 gRPC 与两版官方旧客户端通过服务名路径对照成功，`SIGTERM` 后两个地址注销。[统一服务端与实测](unary-service-api.md#实测)
- Kronos 客户端通过真实 etcd 调用两版官方旧服务端，并在两种协议上验证多实例选择与绕行。生成的 Order 客户端及命令行入口使用同一公开 API。[发现客户端与实测](discovered-rpc-client.md#实测)
- 真实 etcd 的隔离网络验收两个故障场景。Order 进程与 etcd 断连时，12 个并发发现调用在 350 毫秒的各自预算到期时返回，直连业务仍可调用。两个地址随租约过期。恢复连接后，日志记录续约失败、租约丢失、重新发布和恢复，客户端通过原服务名再次取得两种协议的订单响应。另一个 Order 进程被强制结束后，两种地址约 16 秒内消失，两种客户端均返回 `NO_INSTANCES`。[断连和崩溃测试](../../test/interop/microv2/resilience_test.go)
- Linux amd64 首次复跑观测到服务端已重新发布时，客户端还在连接退避。按原预算继续发起真实调用后，经历发现失败的同一个 gRPC 客户端和复用受影响 registry 的 MicroRPC 客户端均取得正确订单。Linux amd64 运行 `GOWORK=off go test -tags=interop,etcd_interop -count=3 -v ./test/interop/microv2` 三轮通过。Linux arm64 运行同一入口并使用 `-count=2 -run '^Test(DiscoveredOrder|OrderRecoversBusinessCalls)'`，两轮通过。[断连恢复实测](../../test/interop/microv2/resilience_test.go)
- 真实 etcd 联调四轮通过。两个端口共用一个 Order 业务实现。摘流期拒绝后续请求，注销后完成在途调用，并在正常与强制退出后释放监听。两种协议分别验证了 Deadline 和 Cancel。[应用生命周期实测](app-lifecycle.md#实测与复盘)
- Order 服务端和客户端二进制在 Linux 编译并实跑。客户端的默认 gRPC 和显式 MicroRPC 都按服务名返回 `订单-03 confirmed`，调用两版旧服务端均得到 `订单-03 legacy-confirmed`。启动与测试命令进入专用 CI 任务。[运行 Order](../guide/README.md#运行-order) · [Linux 验收](go-verification.md#order-治理验收)

## 复盘

- 使用方通过 Proto 生成服务注册和类型化客户端。应用入口保有 registry 的创建错误及资源所有权，调用方用显式协议选项迁移旧端点。
- 实现复用 Kratos 监听和关停、现有旧协议编解码及 etcd 租约。框架在同一入口协调接纳、注销和排空。刷新、重试和连接回收由客户端持有。
- 运维可分别观察进程、接纳状态和发现地址。真正的恢复点是注册中心重新可达后，按原服务名完成两种协议的业务调用。成功和接纳日志可按需启用 DEBUG，摘流拒绝、RPC 错误和发现错误由 WARN 记录。[观测事件](structured-logging.md#统一一元-rpc-的事件)
