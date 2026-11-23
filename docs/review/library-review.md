# Kronos 类库与原生 Go 验收审查

2026-11-23 审查 `b0c3419` 上 `go.work` 列出的 22 个独立模块。根模块承载参考服务与集成验收。逐库核对公开契约、调用边界、资源收尾和测试入口，并记录 `examples/` 中可追到实际调用的接线。[工作区](../../go.work) · [模块与依赖边界](../adr/0004-layer-and-module-boundaries.md)

## 初审发现

1. **P1 模型回调的 panic 会退出进程。** `TextModel` 迭代器由 Blades v0.6.0 的 agent 协程执行。临时 Go 用例注入抛出 panic 的模型，子进程以状态 2 退出。请求协程上的 RPC 恢复守卫无法接住这个 panic。需要在执行模型的协程边界将失败转换成内部错误，验证流能结束且进程仍可接收下一次请求。[模型迭代](../../infra/blades/model.go#L34) · [Go panic 语义](https://go.dev/ref/spec#Handling_panics)
2. **P2 Assistant 恢复了请求，指标和链路却漏记这次失败。** 一元与流式处理都在业务函数返回后才执行收尾。注入两次业务 panic 后，外层守卫均返回 `ErrHandlerPanic`，失败指标和已结束的 span 均为零。两次请求也跳过 `request_finished`，恢复诊断仍由守卫写出。请求观测应收到恢复后的错误结果。[一元和流式入口](../../examples/assistant/cmd/assistant/main.go#L201-L220) · [观测收尾](../../examples/assistant/cmd/assistant/logging.go#L26-L59)
3. **P2 MongoDB 的 SRV 查询可以拖过启动期限。** `Open` 承诺在调用者上下文内检查连通性。驱动在 `ApplyURI` 期间同步解析 `mongodb+srv`。使用阻塞 TXT 查询的本地解析器夹具，取消上下文 80ms 后调用仍未返回，释放查询后返回 `ErrInvalidConfig`。启动预算需要覆盖查询阶段，并保留上下文错误。[MongoDB 启动](../../infra/mongodb/database.go#L40-L60) · [MongoDB 驱动 v2.9.2 URI 处理](https://github.com/mongodb/mongo-go-driver/blob/v2.9.2/mongo/options/clientoptions.go)
4. **P2 旧 RPC 服务端可以返回客户端必然拒收的成功响应。** 给处理器返回有效的 1 MiB Protobuf，封帧后超过客户端的 1 MiB 上限。回环用例观察到服务端返回 HTTP 200 且 `Micro-Error` 为空，Kronos 客户端报响应体超限。应按完整帧统一响应上限，并约定超限如何返回旧协议错误。[服务端封帧](../../infra/microv2/handler.go#L158-L162) · [客户端响应上限](../../infra/microv2/client.go#L91-L96)
5. **P2 CI 的导入检查放过业务命令直接依赖厂商 SDK。** ADR 0004 将外部 SDK 限定在负责接入的包。把生产命令 `examples/textbackend/cmd/textbackend` 和 Kafka、MongoDB SDK 的导入路径交给现行规则，两项均被放行。当前这两类接线均使用 Kronos 适配包。按已接入 SDK 的归属补齐规则，才能在新增直接导入时拦截边界回归。[导入规则](../../platform/verify/dependencies.go#L49-L57) · [ADR 0004](../adr/0004-layer-and-module-boundaries.md#依赖方向与注入)

## 逐库示例与审查结果

初审时 18 个模块在 `examples/` 下有真实 Go 接线。补齐 `core/runtime` 和 `core/timezone` 后，20 个运行模块均有对应调用。两个平台工具分别服务协议生成和仓库验收。下表记录当前示例位置。

| 模块 | `examples/` 中的接线 | 包内 Go `Example` | 审查重点与结果 |
| --- | --- | --- | --- |
| `core/locale` | [Assistant 构建语言匹配器与文案目录](../../examples/assistant/cmd/assistant/service.go#L26-L38) | [文案回退](../../core/locale/catalog_test.go#L80) | 匹配、回退和目录快照有测试。 |
| `core/runtime` | [后台文档任务](../../examples/runtimejob/main.go) | [观察 panic 终态](../../core/runtime/example_test.go) | 原生测试覆盖 panic、`Goexit` 和回调隔离。[协程测试](../../core/runtime/coroutine_test.go) |
| `core/timezone` | [计算日报日期](../../examples/reportdate/main.go) | [按 IANA 时区换算日期](../../core/timezone/timezone_test.go#L76) | 夏令时、无效区域及显式时区跨日有测试。 |
| `framework/app` | [Assistant 监听与应用生命周期](../../examples/assistant/cmd/assistant/main.go#L123-L182) | 缺 | 端口回滚、在途流排空与进程重载有原生 Go 测试。 |
| `framework/localization` | [Assistant 协商及传播请求语言](../../examples/assistant/internal/binding/request.go#L21-L22) | 缺 | 权重顺序、回退和元数据快照有测试。 |
| `framework/log` | [Assistant 创建 JSON 日志](../../examples/assistant/cmd/assistant/main.go#L28-L29) | [JSON 日志与请求 ID](../../framework/log/example_test.go#L12) | 日志实例与请求字段有测试。Assistant 的请求收尾见问题 2。 |
| `framework/request` | [Assistant 接收请求 ID](../../examples/assistant/internal/binding/request.go#L16-L22) | [上下文请求 ID](../../framework/request/example_test.go#L10) | ID 校验、无效值替换和上下文读取有测试。 |
| `framework/rpc` | [Assistant 选择 gRPC 或旧协议](../../examples/assistant/assistant.go#L96-L110) | [协议选项](../../framework/rpc/example_test.go#L9) · [一元恢复](../../framework/rpc/recovery_test.go#L85) | 客户端期限、并发关闭及请求恢复有测试。恢复后的观测见问题 2。 |
| `infra/aliyunoss` | [TextBackend 读取 OSS 文本配置](../../examples/textbackend/cmd/textbackend/oss_profile.go#L21-L42) | [预签名 GET](../../infra/aliyunoss/bucket_test.go#L308) | 原生 SDK 对照及 HTTP 夹具覆盖签名、取流和取消。 |
| `infra/blades` | [Assistant 装配模型](../../examples/assistant/cmd/assistant/main.go#L100) · [业务运行与流](../../examples/assistant/cmd/assistant/service.go#L40-L62) | 缺。参考服务另有[公开流式客户端示例](../../examples/assistant/stream_example_test.go#L12) | 文本、流式错误和取消有测试。模型 panic 见问题 1。 |
| `infra/configfile` | [Assistant 读取启动配置](../../examples/assistant/cmd/assistant/config.go#L19) | [加载 JSON](../../infra/configfile/example_test.go#L10) | 严格解码和文件读取错误有测试。 |
| `infra/consul` | [Assistant 装配发现](../../examples/assistant/cmd/assistant/main.go#L83) · [TextBackend 注册与摘除](../../examples/textbackend/cmd/textbackend/registration.go#L20-L38) | [查询健康实例](../../infra/consul/example_test.go#L13) | HTTP 夹具覆盖注册、健康查询、空快照和取消。 |
| `infra/kafka` | [TextBackend 创建发布者](../../examples/textbackend/cmd/textbackend/main.go#L131) · [消费进程处理生成事件](../../examples/textbackend/cmd/textbackend-events/main.go#L47-L61) | [错误配置](../../infra/kafka/config_test.go#L94) | 常规测试覆盖启动取消。确认写入与消费位点另有 Kafka 集成用例。 |
| `infra/microv2` | [Assistant 装配旧协议服务端和客户端](../../examples/assistant/internal/binding/micro.go#L23-L46) | 缺 | 官方两版双向互通实测通过。成功响应尺寸边界见问题 4。 |
| `infra/mongodb` | [TextBackend 打开数据库和类型化集合](../../examples/textbackend/cmd/textbackend/mongo_profile.go#L29-L44) | [错误配置](../../infra/mongodb/database_test.go#L72) · [打开数据库](../../infra/mongodb/integration_test.go#L118)，需要 `mongodb_integration` | 直接地址启动取消有测试。SRV 启动取消见问题 3。 |
| `infra/mysql` | [TextBackend 打开连接池](../../examples/textbackend/cmd/textbackend/mysql_profile.go#L25-L37) | [错误配置](../../infra/mysql/pool_test.go#L75) · [打开 SQL 连接池](../../infra/mysql/integration_test.go#L137)，需要 `mysql_integration` | 池句柄使用 `database/sql`，启动取消有夹具测试。 |
| `infra/oteltrace` | [Assistant 创建追踪器](../../examples/assistant/cmd/assistant/main.go#L160-L168) · [传递链路上下文](../../examples/assistant/internal/binding/request.go#L25-L42) | [创建追踪实例](../../infra/oteltrace/example_test.go#L13) | 本地收集器验证父链路传播。恢复后的 span 收尾见问题 2。 |
| `infra/prometheus` | [Assistant 注册请求指标](../../examples/assistant/cmd/assistant/main.go#L115-L122) | [注册与记录](../../infra/prometheus/example_test.go#L13) | 指标和标签隔离有测试。恢复后的失败计数见问题 2。 |
| `infra/redis` | [TextBackend 装配缓存](../../examples/textbackend/cmd/textbackend/main.go#L115-L125) · [业务读取与回填](../../examples/textbackend/cmd/textbackend/profile_cache.go) | [错误配置](../../infra/redis/client_test.go#L79) · [实例与缓存操作](../../infra/redis/integration_test.go#L172)，需要 `redis_integration` | 等待期限有回环测试。实际缓存操作另有 Redis 集成用例。 |
| `infra/runtimeprofile` | [Assistant 装配独立性能监听](../../examples/assistant/cmd/assistant/main.go#L150-L155) | [创建性能采集服务](../../infra/runtimeprofile/example_test.go#L15) | 回环地址限制、路由隔离和标准 pprof 对照有测试。 |
| `platform/protogen` | 工具。[Assistant 生成模板](../../tools/proto/buf.gen.yaml#L16-L21) · [TextBackend 生成模板](../../tools/proto/buf.textbackend.gen.yaml#L11-L16) | 缺 | 多协议文件和名称冲突有测试。两套参考服务产物重生成一致。 |
| `platform/verify` | 工具。[CI 原生 Go 入口](../../.github/workflows/ci.yml#L23-L39) | 缺 | 工作区测试、分层和生成检查已实跑。厂商 SDK 边界见问题 5。 |

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
