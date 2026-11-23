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

## etcd 适配契约

`etcd.New(endpoints ...string)` 使用显式的 TCP 端点创建独立客户端，返回初始化错误。调用方拥有客户端并调用 `Close`。平台装配用 `Publish(ctx, serviceName, grpcAddress, microRPCAddress)` 注册一代实例，至少有一个通告地址。返回值是有期限、可重复调用的摘流函数。启动上下文约束首轮发布，成功发布后的续约随注册实例和客户端的生命周期运行。调度使用 15 秒租约、每 5 秒尝试续约和每次 2 秒的 etcd 操作预算。

`Lookup(ctx, serviceName, protocol)` 读取协议自己的前缀，返回去重排序的可用地址字符串。实例不存在时返回空切片，配置错误、后端错误和上下文结束分别表达。通过完整事务绑定两种地址到同一租约。部分失败后尽力撤销新租约，保留启动错误。客户端关闭前，先用发布凭据返回的函数摘除该实例。进程异常退出时租约到期清理记录。

适配器使用 `etcd_integration` 标签验证真实租约。官方 go-micro v2 发现测试使用 `etcd_interop` 标签并独立构建旧 SDK，读取同一个 etcd 上的记录。两种测试通过 `KRONOS_ETCD_TEST_ADDRESS` 指向隔离实例。后续以 Order 进程的双向调用作为整体互通验收。

参考装配沿用了注册能力注入业务进程的做法。当前通过显式实例管理配置和资源，以官方客户端承担连接、读写、事务及租约。选用接受 Go 1.25 基线的 `go.etcd.io/etcd/client/v3 v3.6.14`。实测服务器使用官方 Linux etcd v3.6.16。上游将 Windows amd64 的 etcd 服务端列为 Tier 3。Windows 开发可通过 TCP 指向 Linux etcd。当前 Windows 证据为适配模块交叉编译。[客户端模块](https://github.com/etcd-io/etcd/blob/v3.6.14/client/v3/go.mod) · [服务端发布](https://github.com/etcd-io/etcd/releases/tag/v3.6.16) · [平台支持](https://etcd.io/docs/v3.6/op-guide/supported-platform/)

## 验收目标

| 编号 | 证据 |
| --- | --- |
| T01 | Order 入口核心装配最多 30 行，实际运行 |
| T02 | 官方 go-micro v2 Registry 从真实 etcd 读到新服务兼容端口 |
| T03 | 官方 go-micro v2 Client 经 etcd 发现并完成 Order 调用 |
| T04 | Kronos MicroRPC 客户端经同一 etcd 找到官方 go-micro v2 服务端并完成调用 |
| T05 | 原生 gRPC 客户端成功调用同一服务 |
| T06 | 两个监听进入同一个业务实例，按协议发现正确地址 |
| T07 | 多实例发现和选择，变更后移除失效实例并验证下一次调用 |
| T08 | etcd 失联、续约失败、租约失效和恢复，最终以业务调用验证 |
| T09 | 异常退出进程的节点最终由 etcd 移除 |
| T10 | 提前摘流，允许在途业务完成，关停满足期限 |
| T11 | 双协议保留调用 Deadline 和 Cancel |
| T12 | 关闭实例后连接、租约、监听和后台任务释放 |
| T13 | 生产导入检查保护 Kronos 与底层 SDK 边界 |
| T14 | CI 在 Linux 通过可重复执行的 Go 验收 |

## 实测记录

- Go 1.25.0、Ubuntu 24.04 Linux arm64，使用官方 etcd v3.6.16 镜像摘要 `sha256:b29d816b546a1966fe7034a33b34e231fa98c47ce4d2883f70b0555020b22132` 与临时隔离网络。设置 `KRONOS_ETCD_TEST_ADDRESS` 后运行 `GOWORK=off go -C infra/etcd test -tags=etcd_integration -count=1 -v ./...`。直接检查双协议的同一事务、实例 ID 与租约。验证多实例、上下文取消、续约跨过原 TTL、租约删除后的相同实例重发，以及停止续约后的服务回收。[租约测试](../../infra/etcd/integration_test.go)
- 同一 Linux 环境运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。v2.0.0 和 v2.9.1 的官方 Registry 都只读取兼容端口。原有双向直连回归也通过。本切面验收注册数据。Order 进程的发现调用进入后续端到端验收。v2.0.0 的测试模块显式锁定其旧 etcd 访问链所用的 `github.com/coreos/pkg`。[发现对照](../../test/interop/microv2/etcd_test.go) · [夹具构建](../../test/interop/microv2/fixture_test.go)
- macOS arm64 的 `go run ./platform/verify test` 和 `go run ./platform/verify deps` 通过。Linux arm64 的 `go run ./platform/verify deps` 和 `go run ./platform/verify proto` 通过。运行 `GOWORK=off GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C infra/etcd test -c` 生成 Windows 测试二进制。
- `framework/app.RunWithReadyHook` 已通过 Linux 回环发布和有界关停测试。HTTP 请求证实发布时监听已经可用，撤销与传输共享一个截止时间，部分失败后释放端口。Order 的真实进程摘流进入后续集成验收。[生命周期实测](app-lifecycle.md#实测与复盘)
- Order 进程的 21 行装配已运行。真实 etcd 上的原生 gRPC 与两版官方旧客户端通过服务名路径对照成功，`SIGTERM` 后两个地址注销。[统一服务端与实测](unary-service-api.md#实测)
- Kronos 客户端通过真实 etcd 调用两版官方旧服务端，并在两种协议上验证多实例选择与绕行。生成的 Order 客户端及命令行入口使用同一公开 API。[发现客户端与实测](discovered-rpc-client.md#实测)
