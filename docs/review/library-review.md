# Kronos 类库与原生 Go 验收审查

## 面向使用者的复审基线

`40910b7` 的工作区包含 24 个独立模块和一个参考服务根模块。比上一轮增加了 `platform/kronos` 与 `infra/etcd`。复审沿用下方调用与所有权记录，以实际入口量化重复配置和失败责任。优先检查独立业务从 Proto 到运行的链路。[工作区](../../go.work)

| 模块或链路 | 常用操作与负载基线 | 决策与可执行证据 |
| --- | --- | --- |
| `platform/protogen`、`platform/verify` 的外部开发链路 | 原有验收复制 Kronos 根模块，业务作者无法从验收判断独立模块的 Buf 路径及工作区依赖 | 共用现有生成器和模板。新增仓库外 Go 模块回归，用自己的导入路径生成和构建，并经真实 etcd 调用。[源码接入](../guide/README.md#仓库外开发一元服务) · [外部回归](../../platform/verify/scaffold_test.go) |
| `platform/kronos` 和 `infra/etcd` 的当前调用 | 应用创建并关闭注册中心，向 Kronos 注入服务名、注册中心及监听地址，服务端注册生成绑定，客户端只声明所用协议 | 使用 [Order 命令](../../examples/order/cmd/order/main.go) 和 [类型化客户端命令](../../examples/order/cmd/order-client/main.go) 作为后续装配复审的基线。[所有权和故障用例](../design/service-governance.md) |
| `infra/consul` | TextBackend 手工生成 ID、协调注册失败回滚，并包装传输的 Stop 以注销实例。新一元服务若复制该流程，需要重复承担这些职责 | 新的一元服务用 `consul.NewRPCRegistry` 加统一的 `kronos.Registry`。已有 TextBackend 与 Assistant 接线经实际回归。适配器仍按 Consul 的 passing 健康结果发现标准 gRPC。[责任与验收](../design/consul-service-registration.md) |
| `infra/prometheus`、`platform/kronos` 的服务指标 | Order 的装配重复列出完整 RPC 方法路径。新增 Proto 方法时需要额外更新应用命令 | 选择 `NewServiceRPCMetrics` 后由 Kronos 传入实际绑定的方法。固定清单仍适用于仅调用远端服务的客户端。真实双协议摘流测试观察了首次请求前的零值、在途累加和完成清理。[设计与用法](../design/order-diagnostics.md) |
| `platform/kronos`、`infra/oteltrace` 的观测装配 | Order 分别挂接指标采集、探针和 Trace。RPC 及发现事件使用进程日志，无法为同进程的客户端配置单独输出 | 使用一个 `Observability` 配置挂接同一指标对象及日志、Trace。现有独立选项保留，创建者依旧关闭 exporter。用真实订单、无实例及摘流核对信号和实例日志。[统一接线](../design/order-diagnostics.md#集中装配与所有权) |

2026-11-23 审查 `b0c3419` 上 `go.work` 列出的 22 个独立模块。根模块承载参考服务与集成验收。逐库核对公开契约、调用边界、资源收尾和测试入口，并记录 `examples/` 中可追到实际调用的接线。[工作区](../../go.work) · [模块与依赖边界](../adr/0004-layer-and-module-boundaries.md)

## 初审发现

1. **P1 模型回调的 panic 会退出进程。** `TextModel` 迭代器由 Blades v0.6.0 的 agent 协程执行。临时 Go 用例注入抛出 panic 的模型，子进程以状态 2 退出。请求协程上的 RPC 恢复守卫无法接住这个 panic。需要在执行模型的协程边界将失败转换成内部错误，验证流能结束且进程仍可接收下一次请求。[模型迭代](../../infra/blades/model.go#L34) · [Go panic 语义](https://go.dev/ref/spec#Handling_panics)
2. **P2 Assistant 恢复了请求，指标和链路却漏记这次失败。** 一元与流式处理都在业务函数返回后才执行收尾。注入两次业务 panic 后，外层守卫均返回 `ErrHandlerPanic`，失败指标和已结束的 span 均为零。两次请求也跳过 `request_finished`，恢复诊断仍由守卫写出。请求观测应收到恢复后的错误结果。[一元和流式入口](../../examples/assistant/cmd/assistant/main.go#L201-L220) · [观测收尾](../../examples/assistant/cmd/assistant/logging.go#L26-L59)
3. **P2 MongoDB 的 SRV 查询可以拖过启动期限。** `Open` 承诺在调用者上下文内检查连通性。驱动在 `ApplyURI` 期间同步解析 `mongodb+srv`。使用阻塞 TXT 查询的本地解析器夹具，取消上下文 80ms 后调用仍未返回，释放查询后返回 `ErrInvalidConfig`。启动预算需要覆盖查询阶段，并保留上下文错误。[MongoDB 启动](../../infra/mongodb/database.go#L40-L60) · [MongoDB 驱动 v2.9.2 URI 处理](https://github.com/mongodb/mongo-go-driver/blob/v2.9.2/mongo/options/clientoptions.go)
4. **P2 旧 RPC 服务端可以返回客户端必然拒收的成功响应。** 给处理器返回有效的 1 MiB Protobuf，封帧后超过客户端的 1 MiB 上限。回环用例观察到服务端返回 HTTP 200 且 `Micro-Error` 为空，Kronos 客户端报响应体超限。应按完整帧统一响应上限，并约定超限如何返回旧协议错误。[服务端封帧](../../infra/microv2/handler.go#L158-L162) · [客户端响应上限](../../infra/microv2/client.go#L91-L96)
5. **P2 CI 的导入检查放过业务命令直接依赖厂商 SDK。** ADR 0004 将外部 SDK 限定在负责接入的包。把生产命令 `examples/textbackend/cmd/textbackend` 和 Kafka、MongoDB SDK 的导入路径交给现行规则，两项均被放行。当前这两类接线均使用 Kronos 适配包。按已接入 SDK 的归属补齐规则，才能在新增直接导入时拦截边界回归。[导入规则](../../platform/verify/dependencies.go#L49-L57) · [ADR 0004](../adr/0004-layer-and-module-boundaries.md#依赖方向与注入)

## 逐库示例与审查结果

复审以 `go.work` 中的 24 个独立模块为准。22 个运行模块有可执行的 Go 示例，两个命令模块有真实工具入口和原生 Go 回归。A 为保留结构，B 为按实际消费者作轻量优化，C 为重点降低装配负担。涉及真实外部服务的验证按各组件标签单独执行。

| 模块与决策 | 真实调用入口 | 最小可执行示例 | 常用路径、所有权与边界 |
| --- | --- | --- | --- |
| `core/locale` B | [Assistant 一次装配文案与匹配](../../examples/assistant/cmd/assistant/service.go) | [从资源键构建并匹配](../../core/locale/catalog_test.go) | 原先同一语言维护在支持列表及资源表中。普通资源目录改用资源键定义支持集合。目录持有只读快照，既有独立匹配和查找接口继续可用。[契约与实测](../design/core-localization.md#消息资源) |
| `core/runtime` A | [后台文档任务](../../examples/runtimejob/main.go) | [观察 panic 终态](../../core/runtime/example_test.go) | 通过 Go 启动任务并观察终态。任务持有自己的取消和等待。覆盖 panic、Goexit 和观察回调隔离。 |
| `core/timezone` A | [计算日报日期](../../examples/reportdate/main.go) | [按 IANA 时区换算日期](../../core/timezone/timezone_test.go#L76) | 按 IANA 标识加载时区，由调用方选择业务日期语义。覆盖无效时区、夏令时及跨日。 |
| `framework/app` A | [Assistant 监听与应用生命周期](../../examples/assistant/cmd/assistant/main.go#L123-L182) | [监听就绪与关停](../../framework/app/example_test.go) | 平台和高级调用方提供绑定，App 持有监听。绑定失败回滚，退出在同一预算内释放已发布状态与在途流。 |
| `framework/localization` B | [Assistant 请求绑定](../../examples/assistant/internal/binding/request.go) · [Order 双协议调用](../../test/interop/microv2/localization_test.go) | [按入站偏好协商](../../framework/localization/example_test.go) | 服务提供语言匹配器，框架保留入站与出站偏好。之前统一一元入口缺少偏好映射，现由绑定自动传播显式出站偏好。已有 Assistant 入口使用相同权重和回退逻辑。 |
| `framework/log` A | [Assistant 创建 JSON 日志](../../examples/assistant/cmd/assistant/main.go#L28-L29) | [JSON 日志与请求 ID](../../framework/log/example_test.go#L12) | 用 NewJSON 接收输出流与级别，入口持有流。请求上下文附加结构化字段。 |
| `framework/request` A | [Assistant 接收请求 ID](../../examples/assistant/internal/binding/request.go#L16-L22) | [上下文请求 ID](../../framework/request/example_test.go#L10) | 传输入口接收并验证请求 ID，下游从上下文读取。无效值替换由入口完成。 |
| `framework/rpc` B | [Assistant 选择 gRPC 或旧协议](../../examples/assistant/assistant.go#L96-L110) | [协议选项](../../framework/rpc/example_test.go#L9) · [一元恢复](../../framework/rpc/recovery_test.go#L85) | 应用持有客户端并关闭，调用方提供上下文。新一元业务可用生成客户端，已有多传输调用保留底层入口。 |
| `infra/aliyunoss` B | [TextBackend 读取 OSS 文本配置](../../examples/textbackend/cmd/textbackend/oss_profile.go#L21-L42) | [预签名 GET](../../infra/aliyunoss/bucket_test.go#L308) | 装配入口创建 Bucket 并清理空闲连接，读取方关闭 Open 返回的流。HTTP 夹具覆盖签名、取流和取消。 |
| `infra/blades` B | [Assistant 装配模型](../../examples/assistant/cmd/assistant/main.go#L100) · [业务运行与流](../../examples/assistant/cmd/assistant/service.go#L40-L62) | [注入模型并生成文本](../../infra/blades/example_test.go) | 应用注入模型，Runner 执行 Run 或 Stream。调用方控制上下文，流接收错误触发取消。 |
| `infra/configfile` A | [Assistant 读取启动配置](../../examples/assistant/cmd/assistant/config.go#L19) | [加载 JSON](../../infra/configfile/example_test.go#L10) | 用 LoadJSON 解码指定类型，函数完成文件读取与关闭。覆盖严格解码、未知字段和读取失败。 |
| `infra/consul` C | [Assistant 装配发现](../../examples/assistant/cmd/assistant/main.go#L83) · [TextBackend 注册与摘除](../../examples/textbackend/cmd/textbackend/registration.go#L20-L38) | [查询健康实例](../../infra/consul/example_test.go#L13) | 新服务用 NewRPCRegistry 注入 Kronos，创建方关闭。复用 passing 健康发现，旧 Watch 由调用方取消。只接入 gRPC。 |
| `infra/etcd` B | [Order 启动和两种协议的注册](../../examples/order/cmd/order/main.go) | [独立持有 Registry](../../infra/etcd/example_test.go) | 创建者 Close，应用先注销其发布，两路使用同一租约。地址构造原先无法传入部署的私有 CA 或客户端证书，现使用标准 TLS 配置和共用的证书加载。真实 etcd 的续约、恢复和证书边界已验证。[生产接入与验收](../design/service-governance.md#私有-ca-与客户端证书) |
| `infra/kafka` B | [TextBackend 创建发布者](../../examples/textbackend/cmd/textbackend/main.go#L131) · [消费进程处理生成事件](../../examples/textbackend/cmd/textbackend-events/main.go#L47-L61) | [错误配置](../../infra/kafka/config_test.go#L94) | 应用持有并关闭 Publisher，Consumer.Run 持有消费循环且受上下文取消。确认写入和位点用真实服务验收。 |
| `infra/microv2` B | [Assistant 装配旧协议服务端和客户端](../../examples/assistant/internal/binding/micro.go#L23-L46) | [实际封帧调用并释放空闲连接](../../infra/microv2/example_test.go) | 底层调用方持有 HTTP 客户端并关闭空闲连接。统一框架负责选协议。两版官方协议另有回归。 |
| `infra/mongodb` A | [TextBackend 打开数据库和类型化集合](../../examples/textbackend/cmd/textbackend/mongo_profile.go#L29-L44) | [错误配置](../../infra/mongodb/database_test.go#L72) · [打开数据库](../../infra/mongodb/integration_test.go#L118)，需要 `mongodb_integration` | 入口 Open 并用有期限的上下文 Close。业务经类型化集合访问，直连与 SRV 启动取消有对照。 |
| `infra/mysql` A | [TextBackend 打开连接池](../../examples/textbackend/cmd/textbackend/mysql_profile.go#L25-L37) | [错误配置](../../infra/mysql/pool_test.go#L75) · [打开 SQL 连接池](../../infra/mysql/integration_test.go#L137)，需要 `mysql_integration` | 入口 Open 并持有标准库 sql.DB，启动失败收尾。调用方传上下文，部署配置持有 DSN。 |
| `infra/oteltrace` C | [Assistant 创建追踪器](../../examples/assistant/cmd/assistant/main.go#L160-L168) · [传递链路上下文](../../examples/assistant/internal/binding/request.go#L25-L42) | [创建追踪实例](../../infra/oteltrace/example_test.go#L13) | 创建 OTLP provider 后注入 Kronos，框架持有调用的 span，创建者有界 Close。日志关联与启动接线仍需核对。 |
| `infra/prometheus` C | [Assistant 注册请求指标](../../examples/assistant/cmd/assistant/main.go#L115-L122) | [注册与记录](../../infra/prometheus/example_test.go#L13) | 应用启用服务指标后，由框架从绑定登记方法。观察器持有独立注册表，结果及在途信号复用同一 HTTP 抓取入口。 |
| `infra/redis` A | [TextBackend 装配缓存](../../examples/textbackend/cmd/textbackend/main.go#L115-L125) · [业务读取与回填](../../examples/textbackend/cmd/textbackend/profile_cache.go) | [错误配置](../../infra/redis/client_test.go#L79) · [实例与缓存操作](../../infra/redis/integration_test.go#L172)，需要 `redis_integration` | 入口 New 和 Close，Get 区分空值与未命中。上层决定回源与 TTL。Go 用例区分调用期限与套接字期限。 |
| `infra/runtimeprofile` B | [Assistant 装配独立性能监听](../../examples/assistant/cmd/assistant/main.go#L150-L155) | [创建性能采集服务](../../infra/runtimeprofile/example_test.go#L15) | 应用交付预绑定的回环监听，进程生命周期统一管理采集端点。路由和监听安全约束有测试。 |
| `platform/kronos` C | [Order 命令](../../examples/order/cmd/order/main.go) · [类型化调用](../../examples/order/cmd/order-client/main.go) | [注册生成的绑定](../../examples/order/example_test.go) · [双协议实际调用](../../test/interop/microv2/etcd_test.go) | 应用拥有监听和注销回调，入口拥有注册中心与观测器。客户端拥有连接并关闭。普通接线继续精简。 |
| `platform/protogen` C | 工具。[Assistant 生成模板](../../tools/proto/buf.gen.yaml#L16-L21) · [TextBackend 生成模板](../../tools/proto/buf.textbackend.gen.yaml#L11-L16) | [独立项目生成与构建](../../platform/verify/scaffold_test.go) | 用 Buf 和服务自有 Proto 生成，开发者持有配置和产物。源码工作区和公开模块安装均通过独立项目的真实 etcd 验收。[版本验收](../design/service-scaffold.md#实测) |
| `platform/verify` A | 工具。[CI 原生 Go 入口](../../.github/workflows/ci.yml#L23-L39) | [原生 Go 验收命令](../design/go-verification.md) | 仓库与 CI 通过同一 Go 命令验收，生成检查持有临时目录，既有契约按 Git 基线校验。 |

Go 1.25.0 在 Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C <模块目录> test -run '^Example' -v ./...`，覆盖新补的 App、语言协商、Blades、etcd、MicroRPC 示例。用 `GOWORK=off go test -run '^Example' -v ./examples/order` 验证 Kronos 注册生成服务的示例。macOS arm64 上的 `go run ./platform/verify test` 和 `go run ./platform/verify deps` 通过，涵盖根模块及 24 个独立模块的默认竞态回归与依赖检查。矩阵与 `go.work` 的模块集合一致，24 行链接到本地文件的路径均可解析。[Go 可测试示例](https://go.dev/blog/examples) · [验收入口](../design/go-verification.md)

## 初审的 Shell 测试核查与实测

扫描 Git 跟踪的 Shell 扩展名、可执行入口的 shebang、Go 测试的进程调用及 CI 配置，未发现仍由 Shell 实现的测试。集成用例直接启动 Go 构建的程序或 Docker，断言由 Go `testing` 执行。本轮清理了九份设计文档的失效入口，并在统一验收说明补齐独立测试命令。原先模块、分层与协议检查由 `d5e5fea` 迁移，旧协议互通由 `d0011f7` 迁移。[原生 Go 验收](../design/go-verification.md) · [CI 配置](../../.github/workflows/ci.yml)

| 当前入口 | 本轮实测结果 |
| --- | --- |
| `go run ./platform/verify test` | Go 1.25.0，macOS arm64。根模块和 22 个独立模块的默认 `go test -race -count=1 ./...` 通过。 |
| `go run ./platform/verify deps`、`go run ./platform/verify proto` | Go 1.25.0、Buf 1.70.0，Ubuntu 24.04 Linux arm64。分层规则通过，协议格式、lint 和生成产物比对通过。 |
| `GOWORK=off go test -tags=interop -count=1 -v ./test/interop/microv2` | Go 1.25.0，Ubuntu 24.04 Linux arm64。官方 v2.0.0、v2.9.1 与 Kronos 双向互通通过，覆盖两种请求编码。 |
| `GOWORK=off go test -tags=e2e -count=1 -v ./test/e2e/assistant` | macOS arm64 启动 Linux Compose 容器。Assistant 的四种入口、探针、启动失败和关停均通过。 |

初审用临时原生 Go 用例复现了五个问题。Consul、Kafka、MongoDB、MySQL 和 Redis 的真实服务集成测试需要单独标签，初审未运行这些用例。OSS 验证使用本地 HTTP 夹具。[参考服务集成用例](../../test/integration)

## 修复验收

| 审查项 | 修复效果与实测 |
| --- | --- |
| 1 模型回调 panic | 模型执行协程恢复 panic 并返回 `ErrModelPanic`，SSE 沿既有失败路径结束。Linux arm64 和 macOS arm64 竞态分别通过适配模块及 SSE 三轮回归。[设计与实测](../design/blades-adapter.md#实测与复盘) |
| 2 恢复后请求观测 | 共享业务入口记录恢复后的错误。真实 gRPC 一元及流式调用各产生一次失败计数、一个结束的错误 span 和一条关联日志。Linux arm64 三轮通过，macOS arm64 通过 Assistant 全部竞态回归。[设计与实测](../design/rpc-handler-recovery.md#实测记录) |
| 3 MongoDB SRV 启动等待 | DNS 查询进行时，调用者取消或超时即收到原上下文错误。Linux arm64 与 macOS arm64 竞态的适配测试分别五轮通过，隔离网络中的 MongoDB 8.0.38 直连与 TextBackend 进程回归通过。[设计与实测](../design/mongodb-text-profiles.md#实测记录) |
| 4 旧 RPC 响应边界 | 完整帧恰好 1 MiB 时成功，超限返回可解码的旧 RPC 错误。Linux 两版官方客户端和 Kronos 客户端验证了超限报错、裸编码大响应和双向互通。[设计与实测](../design/go-micro-v2-interop.md#实测与复盘) |
| 5 厂商 SDK 导入边界 | CI 按 SDK 导入前缀检查适配归属。原生工作区夹具拦截 Kafka 生产导入，放行适配和专用测试导入。Linux arm64 与 macOS arm64 的工具测试和全仓导入检查均通过。[规则与实测](../design/go-verification.md#外部-sdk-归属) |
| `core/runtime` 示例 | 后台任务示例从观察回调取得终态。Linux arm64 和 macOS arm64 均实跑命令并验证包内和应用 Go `Example`。[示例与实测](../design/runtime-go.md#后台任务示例) |
| `core/timezone` 示例 | Linux arm64 实跑日报命令，在洛杉矶与上海得到不同日期。macOS arm64 的竞态回归执行包内和应用 Go `Example`。[示例与实测](../design/core-timezone.md#日报日期示例) |

2026-11-24 整体验收使用 Go 1.25.0。macOS arm64 的 `go run ./platform/verify test`、`go run ./platform/verify deps` 和 `GOWORK=off go mod tidy -diff` 通过。Ubuntu 24.04 Linux arm64 的 `GOWORK=off go test -count=1 ./...`、`GOWORK=off go build ./examples/...`、`go run ./platform/verify deps` 与 `go run ./platform/verify proto` 通过。macOS arm64 驱动 `GOWORK=off go test -tags=e2e -count=1 -v ./test/e2e/assistant`，Linux Compose 容器通过四入口、探针、异常启动和关停回归。[Go 验收入口](../design/go-verification.md) · [容器测试](../../test/e2e/assistant/compose_test.go)
