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

首次发布故障使用同一个隔离 etcd。阻断发布者连接后，记录装配交给发布钩子的实际端口，确认两路直连可执行 Order。放行发布后，真实租约请求达到期限，应用返回启动失败，两个端口可重新绑定，直连 etcd 的观察方读不到该实例。这样分别验证先就绪、发布失败和启动资源回滚。[系统状态及责任](../architecture/rpc-system.md#生命周期与可发现状态)

在 Linux 上启动真实 Order 进程和隔离 etcd。用测试进程持有的回环 TCP 中继中断 Order 和一组客户端到 etcd 的连接。直连 etcd 的观察端验证两个地址随租约消失，发现受阻的并发调用在各自预算内返回，已知地址的 RPC 仍可完成。恢复连接后核对续约失败、丢失租约及重新发布的事件。服务端与客户端的 etcd 连接各自退避并重连。验收让经历过发现失败的客户端继续按服务名调用，在同一个 22 秒恢复预算内取得两种协议的真实订单响应。[gRPC 连接退避](https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md)

用另一组服务名启动独立 Order 进程。确认发现调用后强制结束该测试进程，观察原记录保留到租约过期，然后核对两个地址均被删除、重新发现返回 `NO_INSTANCES`。测试中继、连接和子进程均由 Go 测试清理。[etcd 租约行为](https://etcd.io/docs/v3.6/dev-guide/interacting_v3/#grant-leases)

## etcd 适配契约

`etcd.New(endpoints ...string)` 使用显式的 TCP 端点创建独立客户端，返回初始化错误。调用方拥有客户端并调用 `Close`。平台装配用 `Publish(ctx, serviceName, grpcAddress, microRPCAddress)` 注册一代实例，至少有一个通告地址。返回值是有期限、可重复调用的摘流函数。启动上下文约束首轮发布，成功发布后的续约随注册实例和客户端的生命周期运行。调度使用 15 秒租约、每 5 秒尝试续约和每次 2 秒的 etcd 操作预算。

`Lookup(ctx, serviceName, protocol)` 读取协议自己的前缀，返回去重排序的可用地址字符串。实例不存在时返回空切片，配置错误、后端错误和上下文结束分别表达。通过完整事务绑定两种地址到同一租约。部分失败后尽力撤销新租约，保留启动错误。客户端关闭前，先用发布凭据返回的函数摘除该实例。进程异常退出时租约到期清理记录。

适配器使用 `etcd_integration` 标签验证真实租约。go-micro v2 的端到端验收使用 `interop,etcd_interop` 标签，构建 Order 进程与官方 SDK 夹具，经同一个 etcd 双向调用。两项测试通过 `KRONOS_ETCD_TEST_ADDRESS` 指向隔离实例。[Linux 验收入口](go-verification.md#order-治理验收)

参考装配沿用了注册能力注入业务进程的做法。当前通过显式实例管理配置和资源，以官方客户端承担连接、读写、事务及租约。选用接受 Go 1.25 基线的 `go.etcd.io/etcd/client/v3 v3.6.14`。实测服务器使用官方 Linux etcd v3.6.16。上游将 Windows amd64 的 etcd 服务端列为 Tier 3。Windows 开发可通过 TCP 指向 Linux etcd。当前 Windows 证据为适配模块交叉编译。[客户端模块](https://github.com/etcd-io/etcd/blob/v3.6.14/client/v3/go.mod) · [服务端发布](https://github.com/etcd-io/etcd/releases/tag/v3.6.16) · [平台支持](https://etcd.io/docs/v3.6/op-guide/supported-platform/)

### 私有 CA 与客户端证书

部署使用私有 CA 或要求客户端证书时，业务入口用 `etcd.NewWithConfig` 传入端点和标准 `*tls.Config`。底层客户端负责 TLS 握手，配置随各自实例使用。带 TLS 的配置接受 HTTPS 与无 scheme 的地址，显式 HTTP 端点会返回配置错误，避免 SDK 按地址规则忽略 TLS。地址校验失败时只报告端点序号。普通 `etcd.New(endpoints...)` 保留已有接入路径。

Order 的服务端和客户端共用证书加载。部署通过 `KRONOS_ETCD_CA_FILE` 提供该实例使用的信任根，`KRONOS_ETCD_CERT_FILE` 和 `KRONOS_ETCD_KEY_FILE` 成对提供客户端证书。使用官方 etcd 要求客户端证书，验证缺认证证书时查询失败，有效证书能完成两种协议的注册、发现与关停注销。TLS 保护注册中心连接，业务传输沿用各自的部署配置。[etcd 传输安全](https://etcd.io/docs/v3.6/op-guide/security/) · [官方客户端配置](https://pkg.go.dev/go.etcd.io/etcd/client/v3@v3.6.14#Config)

## 验收目标

### 需求追踪

| 编号 | 通过条件 | 接口与设计 | 实现责任 | 执行验证 |
| --- | --- | --- | --- | --- |
| T01 | Order 核心装配最多 30 行，实际运行 | [一元装配](unary-service-api.md) | [Order 命令](../../examples/order/cmd/order/main.go) | [装配行数断言](../../examples/order/cmd/order/main_test.go) · [进程调用](../../test/interop/microv2/etcd_test.go) |
| T02 | 官方 Registry 从真实 etcd 读到兼容端口 | [协议前缀和租约](../adr/0009-discovery-protocols.md) | [etcd 发布](../../infra/etcd/publication.go) | [官方 Registry](../../test/interop/microv2/etcd_test.go) |
| T03 | 官方 go-micro v2 Client 按服务名调用 Order | [入站协议](additional-listeners.md#go-micro-v2-兼容范围) | [服务端传输](../../platform/kronos/internal/transport/server.go) | [官方客户端到 Order](../../test/interop/microv2/etcd_test.go) |
| T04 | Kronos MicroRPC 客户端发现并调用官方服务端 | [发现客户端](discovered-rpc-client.md) | [客户端传输](../../platform/kronos/internal/transport/client.go) | [Order 到官方服务端](../../test/interop/microv2/etcd_test.go) |
| T05 | 原生 gRPC 客户端成功调用同一服务 | [方法名与消息类型](unary-service-api.md#协议与发现) | [标准 gRPC 入口](../../platform/kronos/internal/transport/server.go) | [标准 gRPC 进程调用](../../test/interop/microv2/etcd_test.go) |
| T06 | 两个端口按协议发布并共享一个业务实例 | [调用链路](../architecture/rpc-system.md#端到端调用链路) | [业务注册与分协议监听](../../platform/kronos/app.go) | [同一实例的两路业务](../../test/interop/microv2/drain_test.go) |
| T07 | 多实例轮询、故障绕行、摘除和有界重试 | [选择及幂等](discovered-rpc-client.md#实例失败及资源) | [快照与连接](../../platform/kronos/discovery.go) · [调用预算](../../platform/kronos/client.go) | [多实例联调](../../test/interop/microv2/replicas_test.go) |
| T08 | etcd 失联、租约过期，恢复后新请求成功 | [故障场景](#注册中心断连与进程崩溃验收) | [续约与重新发布](../../infra/etcd/publication.go) | [断连和业务恢复](../../test/interop/microv2/resilience_test.go) |
| T09 | 进程崩溃后其两个地址一并移除 | [租约契约](#etcd-适配契约) | [协议地址事务](../../infra/etcd/publication.go) | [强制退出与过期](../../test/interop/microv2/resilience_test.go) |
| T10 | 就绪后发布，失败回滚，先摘流再排空 | [启动及退出](app-lifecycle.md) | [生命周期执行](../../framework/app/app.go) | [首次发布故障](../../test/interop/microv2/startup_test.go) · [双协议关停](../../test/interop/microv2/drain_test.go) |
| T11 | 双协议传播 Deadline 和 Cancel | [预算与取消](discovered-rpc-client.md#调用者契约) | [客户端上下文](../../platform/kronos/client.go) · [旧协议超时](../../infra/microv2/handler.go) | [期限及取消](../../test/interop/microv2/drain_test.go) |
| T12 | 发布、监听、连接及后台查询均可释放 | [资源所有权](../architecture/rpc-system.md#故障如何跨边界传播) | [租约](../../infra/etcd/publication.go) · [连接](../../platform/kronos/discovery.go) | [租约](../../infra/etcd/integration_test.go) · [监听](../../test/interop/microv2/drain_test.go) · [查询与连接](../../platform/kronos/discovery_test.go) |
| T13 | 业务使用 Kronos 与 Protobuf 公开契约 | [依赖方向](../adr/0004-layer-and-module-boundaries.md#依赖方向与注入) | [导入规则](../../platform/verify/dependencies.go) | [生产依赖对照](../../platform/verify/dependencies_test.go) |
| T14 | CI 在独立 Linux 环境复现原生 Go 验收 | [本地与 CI 命令](go-verification.md#order-治理验收) | [Linux 工作流](../../.github/workflows/ci.yml) | [治理基线运行](https://github.com/fangfengxiang/kronos/actions/runs/45306567631) |
| T15 | 指标区分双协议与双方角色，探针反映接纳状态 | [Order 运行观测](order-diagnostics.md) | [观测点](../../platform/kronos/observation.go) · [指标适配](../../infra/prometheus/rpc.go) | [实际探针及摘流指标](../../test/interop/microv2/drain_test.go) · [命令抓取](../../test/interop/microv2/etcd_test.go) |
| T16 | 双协议 Trace 可关联到日志与指标，失败定位到真实阶段 | [Trace 与调用定位](order-diagnostics.md#trace-与调用定位) | [OTLP Trace](../../infra/oteltrace/provider.go) · [传输传播](../../platform/kronos/internal/transport/client.go) | [实际 OTLP、日志和指标](../../test/interop/microv2/trace_test.go) |
| T17 | 保持现有 Proto 和 Go 核心调用契约的兼容 | [版本与迁移](../adr/0010-public-contract-compatibility.md) | [Buf 基线检查](../../platform/verify/compatibility.go) · [CI](../../.github/workflows/ci.yml) | [基线正反例](../../platform/verify/compatibility_test.go) · [外部 Go 消费者编译](../../platform/kronos/public_test.go) |
| T18 | 新服务可从 Proto 生成、构建并经双协议调用 | [一元服务脚手架](service-scaffold.md) | [脚手架命令](../../platform/verify/scaffold.go) · [生成入口](../../platform/verify/protocol.go) | [全新服务经真实 etcd](../../platform/verify/scaffold_test.go) |
| T19 | 实例摘除和客户端释放可从运行日志核对，剩余实例继续成功 | [实例与传输资源状态](discovered-rpc-client.md#实例失败及资源) | [快照计数](../../platform/kronos/discovery.go) · [客户端关闭](../../platform/kronos/client.go) | [真实 etcd 双实例变更](../../test/interop/microv2/replicas_test.go) |
| T20 | 服务端和调用方可经双向 TLS 使用 etcd，证书错误时拒绝查询 | [私有 CA 与客户端证书](#私有-ca-与客户端证书) | [连接配置](../../infra/etcd/registry.go) · [Order 启动配置](../../examples/order/internal/etcdconfig/registry.go) | [真实双向 TLS](../../test/interop/microv2/etcd_tls_test.go) |

## 实测记录

- Go 1.25.0、Ubuntu 24.04 Linux arm64，使用官方 etcd v3.6.16 启用 `client-cert-auth` 与临时私有 CA。设置路径后运行 `GOWORK=off go test -tags=etcd_interop -count=3 -run '^TestOrderWithMutualTLSEtcd$' -v ./test/interop/microv2`，三轮通过。未知 CA、缺客户端证书两种对照均无法查询。服务进程与独立 CLI 使用有效客户端证书，两路均返回 `订单-03 confirmed`，停机后两组记录为空。macOS arm64 的 etcd 与 Order 竞态回归和依赖检查通过。[Go 验收命令](go-verification.md#etcd-双向-tls-验收)
- 同一次改动在 Ubuntu 24.04 Linux amd64 使用普通 etcd v3.6.16，租约测试及 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2` 通过。默认连接、官方旧端、关停和故障恢复均完成真实回归。[默认治理验收](go-verification.md#order-治理验收)

- Go 1.25.0、Ubuntu 24.04 Linux arm64，使用官方 etcd v3.6.16 镜像摘要 `sha256:b29d816b546a1966fe7034a33b34e231fa98c47ce4d2883f70b0555020b22132` 与临时隔离网络。设置 `KRONOS_ETCD_TEST_ADDRESS` 后运行 `GOWORK=off go -C infra/etcd test -tags=etcd_integration -count=1 -v ./...`。直接检查双协议的同一事务、实例 ID 与租约。验证多实例、上下文取消、续约跨过原 TTL、租约删除后的相同实例重发，以及停止续约后的服务回收。[租约测试](../../infra/etcd/integration_test.go)
- 同一 Linux 环境运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。v2.0.0 和 v2.9.1 的官方 Registry 都只读取兼容端口，两版官方旧客户端均能经真实 etcd 调用 Order。原有双向直连回归也通过。v2.0.0 的测试模块显式锁定其旧 etcd 访问链所用的 `github.com/coreos/pkg`。[发现对照](../../test/interop/microv2/etcd_test.go) · [夹具构建](../../test/interop/microv2/fixture_test.go)
- macOS arm64 的 `go run ./platform/verify test` 和 `go run ./platform/verify deps` 通过。Linux arm64 的 `go run ./platform/verify deps` 和 `go run ./platform/verify proto` 通过。运行 `GOWORK=off GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C infra/etcd test -c` 生成 Windows 测试二进制。
- `framework/app.RunWithReadyHook` 已通过 Linux 回环发布和有界关停测试。HTTP 请求证实发布时监听已经可用，撤销与传输共享一个截止时间，部分失败后释放端口。[生命周期实测](app-lifecycle.md#实测与复盘)
- Go 1.25.0、Ubuntu 24.04 Linux amd64 与真实 etcd v3.6.16 运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。阻断首次发布连接后，发布钩子收到两个实际端口，两路直连先返回正确订单。发布到期返回启动错误，两个端口可以立即重绑，etcd 上两条记录均为空。完整互通和恢复用例同时通过。[启动回滚](../../test/interop/microv2/startup_test.go)
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
