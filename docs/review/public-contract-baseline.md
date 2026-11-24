# 公开 API 与架构基线

基线为主线 `d51d1acae6ac76b6eb7809f4ee29bc21bd9703af`。审查以 `go.work` 为准，覆盖 22 个可导入模块、两个命令模块。根模块提供参考应用、生成产物和集成验收。可导入模块的导出符号包括表中列出的声明及其公开字段、接口方法和方法集。可在基线工作树执行 `GOWORK=off go -C <模块目录> doc -all .` 查阅完整签名。唯一额外列出的子包是 `platform/kronos/internal/transport`，由 Go 的 internal 规则限制导入。真实入口、可运行 Go 示例、逐库细节及已经完成的验收见[逐库复审](library-review.md#逐库示例与审查结果)。

## 推荐入口

新的一元服务在 Proto 定义方法，用生成的绑定把业务实现注册到 `kronos.New` 创建的应用。调用方用生成客户端，默认 gRPC，需要兼容旧服务时显式选 MicroRPC。装配入口需要注册中心、监听、观测时显式注入。业务实现主要依赖服务消息、生成接口和标准库上下文，稳定业务失败可用 `kronos.Error` 表达。数据、缓存、对象、消息和模型可独立接入。[开发指南](../guide/README.md) · [分层和依赖](../adr/0004-layer-and-module-boundaries.md)

## 逐模块公开契约

同一模块可以同时提供普通入口和高级能力。表中的原语供显式装配或组件实现使用。两个命令的稳定面来自被文档化的参数、配置与生成契约。尚待接入统一入口的协议及未兑现的生产目标列在本页末尾。

| 模块 | 公开面与用途 | 依赖、使用者和资源责任 |
| --- | --- | --- |
| [core/locale](../../core/locale) | 原语 `NewCatalog`、`NewCatalogFromBundles`、`NewMatcher`、`Catalog.{Lookup,Match}`、`Matcher.Match`、`Message` | Assistant 启动时组装语言目录。目录持有资源快照，匹配和读取可共享，无关闭动作。 |
| [core/runtime](../../core/runtime) | 原语 `Go`、`Option`、`WithLogger`、`WithObserver`、`WithPanicHandler`、`Outcome`、`Exited`、`Returned`、`Panicked`、`PanicInfo`、`Result` | 后台任务示例依赖标准库。调用方负责取消和等待，各调用的回调可能并发执行。 |
| [core/timezone](../../core/timezone) | 原语 `Load`、`Convert`、`ErrInvalidZone` | 日报示例按 IANA 地区计算日期，包内引入 Go tzdata，调用方选择业务时区。 |
| [framework/app](../../framework/app) | 高级装配 `New`、`App.{Run,RunWithReadyHook,RunWithReload,Ready,Probes}`、`Config`、`Binding`、`Endpoint`、`ReadyHook`、`ReloadConfig`、`Server`、`ErrAlreadyRun` | Assistant 和统一应用使用。以 Kratos 和 Tableflip 执行运行与重载。App 持有监听，一次运行；启动失败回滚，预算覆盖注销和排空。 |
| [framework/localization](../../framework/localization) | 原语 `AcceptLanguageHeader`、`AcceptLanguage`、`WithAcceptLanguage`、`WithIncomingAcceptLanguage`、`Matcher`、`Negotiate` | HTTP/SSE、gRPC 和 MicroRPC 的绑定层处理入出站偏好。应用提供匹配器，匹配输入在请求上下文。 |
| [framework/log](../../framework/log) | 原语 `NewJSON` | Assistant 使用结构化输出，读同请求的相关字段。依赖 request，调用方持有标准库 writer。 |
| [framework/request](../../framework/request) | 原语 `IDHeader`、`ID`、`WithID`、`WithIncomingID` | 调用方提供出站 ID，绑定验证入站 ID。派生标准库上下文，无实例资源。 |
| [framework/rpc](../../framework/rpc) | 高级客户端 `NewUnaryClient`、`UnaryClient.{Call,Do,Close}`、`WithProtocol`、`ResolveProtocol`、`ClientOption`、`Protocol`、`ProtocolGRPC`、`ProtocolMicroV2`、`Server`。原语 `RecoverCall`、`RecoverStream`、`RecoverUnary`、`Code`、`Error.Error`、`InvalidArgument`、`Internal`、`Unavailable`、`ServiceUnavailableReason`、`ErrClientClosed`、`ErrHandlerPanic`、`ErrInvalidStream`、`ErrStreamingUnsupported`、`ErrUnsupportedProtocol` | Assistant 的高级入口和统一绑定复用协议、错误及恢复语义。构造方关闭客户端，运行方管理服务端。 |
| [infra/aliyunoss](../../infra/aliyunoss) | 独立入口 `New`、`Config`、`Bucket.{Open,Put,PresignGet,CloseIdleConnections}`、`ErrInvalidConfig`、`ErrInvalidObject`、`ErrInvalidExpiry`、`ErrNotFound`、`ErrPermissionDenied`、`ErrUnavailable` | TextBackend 组装 OSS 适配。读取者关闭 Open 返回的流，创建者释放空闲连接。 |
| [infra/blades](../../infra/blades) | 独立入口 `New`、`TextModel`、`Runner.{Run,Stream}`、`ErrIncomplete`、`ErrModelPanic` | Assistant 注入模型，适配器持有 Blades 实现。请求方负责取消，流消费提前返回时取消执行。 |
| [infra/configfile](../../infra/configfile) | 独立入口 `LoadJSON` | Assistant 读取启动文件。函数负责本次打开和关闭，标准库严格解码。 |
| [infra/consul](../../infra/consul) | 标准 gRPC 装配 `NewRPCRegistry`、`RPCRegistry.{Publish,Lookup,SupportsProtocol,Close}`。高级入口 `New`、`Instance`、`Registry.{Register,Deregister,Lookup,Watch,Close}` | 统一应用持有注销动作，创建端关闭适配器。原有 TextBackend 和 Assistant 自行装配，Watch 调用者控制取消。 |
| [infra/etcd](../../infra/etcd) | 双协议装配 `New`、`NewWithConfig`、`Config`、`Registry.{Publish,Lookup,Close}`、`ErrInvalidConfig`、`ErrUnavailable` | Order 注入注册中心，创建者 Close，应用释放本次发布。同一实例的协议端点共用租约。 |
| [infra/kafka](../../infra/kafka) | 独立入口 `OpenPublisher`、`NewConsumer`、`Publisher.{Publish,Close}`、`Consumer.Run`、`Config`、`Message`、`PlainAuth`、`ErrInvalidConfig`、`ErrUnavailable`、`ErrClosed` | TextBackend 写入生成事件，消费入口控制 Run 的生命周期。写入方 Close；消费按照回调成功后提交，Run 结束释放消费组。 |
| [infra/microv2](../../infra/microv2) | 兼容原语 `NewHandler`、`NewClient`、`Unary`、`RequestMetadata`、`Error.Error`、`Client.{Call,CallWithMetadata,CallWithRequestID,CallWithTraceContext,CloseIdleConnections}` | 服务框架按旧协议构建并运行 HTTP handler，客户端创建者清理空闲连接，调用上下文取消在途请求。 |
| [infra/mongodb](../../infra/mongodb) | 独立入口 `Open`、`Config`、`Database.Close`、`NewCollection`、`Collection.{FindByID,ReplaceByID,DeleteByID}`、`ErrInvalidConfig`、`ErrUnavailable`、`ErrClosed`、`ErrNotFound`、`ErrDuplicateKey` | TextBackend 复用数据库与类型化集合。数据库创建者关闭连接，集合借用数据库。 |
| [infra/mysql](../../infra/mysql) | 独立入口 `Open`、`Config`、`IsDuplicateKey`、`ErrInvalidConfig`、`ErrUnavailable` | TextBackend 持有并关闭返回的标准库 SQL 连接池。事务和查询属于消费者。 |
| [infra/oteltrace](../../infra/oteltrace) | 装配 `New`、`Config`、`Provider.{StartRPC,StartRequest,Propagate,Close}`、`Propagate` | Order 和 Assistant 注入追踪实现。调用边界结束 span，创建端使用独立关停预算刷新 exporter。 |
| [infra/prometheus](../../infra/prometheus) | 装配 `NewServiceRPCMetrics`、`NewRPCMetrics`、`NewRequestMetrics`、`RPCMetrics.{BeginRPC,RegisterRPCOperations,ServeHTTP}`、`RequestMetrics.{Record,ServeHTTP}` | Order 从绑定登记方法；Assistant 使用已有指标。每个指标适配持有自己的注册表，复用标准 HTTP handler。 |
| [infra/redis](../../infra/redis) | 独立入口 `New`、`Config`、`Client.{Get,Set,SetIfAbsent,Delete,Close}`、`ErrInvalidConfig`、`ErrInvalidTTL`、`ErrClosed`、`ErrUnavailable` | TextBackend 组装并关闭缓存，业务决定回源和 TTL。 |
| [infra/runtimeprofile](../../infra/runtimeprofile) | 高级装配 `NewServer`、`Server.{Start,Stop}` | Assistant 交付预绑定回环监听由生命周期关闭。诊断行为复用标准库 pprof。 |
| [platform/kronos](../../platform/kronos) | 普通入口 `New`、`NewClient`、`App.{Register,Run}`、`Client.Close`、`Name`、`Registry`、`GRPC`、`MicroRPC`、`Protocol`、`ProtocolGRPC`、`ProtocolMicroV2`、`Observability`、`ObservabilityConfig`、`Error.Error`、`InvalidArgument`、`Internal`、`Unavailable`、`ErrUnsupportedProtocol`、`ErrClientClosed`。高级 `Option`、`AdvertiseHost`、`ShutdownTimeout`、`CallTimeout`、`Diagnostics`、`Metrics`、`Tracing`、`RegistryProvider`、`RPCMetrics`、`RPCMetricsHandler`、`RPCTracer`、`WireProtocol`。生成器入口 `Bind`、`Unary`、`Service`、`UnaryMethod`、`Client.Invoke` | Order 及脚手架应用。依赖 app、localization、request、rpc 和 MicroRPC 适配。应用持有监听和注销动作，客户端持有出站连接，两者借用注入资源。 |
| [platform/protogen](../../platform/protogen) | 生成器参数 `api_import`、`unary_bindings`，输出 Go 契约、业务接口和按需生成的绑定及客户端。 | Buf 按 Proto 描述符调用插件。业务工程持有 Proto、Buf 配置和产物。 |
| [platform/verify](../../platform/verify) | 项目命令 `test`、`deps`、`proto`、`generate`、`breaking <baseline>`、`scaffold <name>` | CI 和仓库维护者用同一 Go 入口；工具负责生成产物对比、临时快照和目标目录检查。 |

## 稳定规则

- Proto 的既有 RPC、消息、错误标识和生成消费者依赖由 [ADR 0010](../adr/0010-public-contract-compatibility.md) 的 Buf 检查与双协议验收保护。
- 普通入口、已有模块的合法调用与资源所有权按已文档化契约演进。优先兼容式新增，新增字段使用零值兼容行为，避免给外部实现的接口加必实现方法。兼容检查同时覆盖 Go 签名和实际行为。对冲突组合修正前须定位现有消费者并记录差异。[Go 模块兼容实践](https://go.dev/blog/module-compatibility)
- 底层原语仍是对应模块的公开接口。调用方只在有具体扩展需求时选用。传输实现的具体类型与生成绑定的依赖留在其对应边界。
- 所有规划中的 MCP、WebRTC 和尚未兑现的统一入口能力均未进入公开承诺。现有 Assistant 的 HTTP、SSE 和 gRPC 流有独立契约。

## 统一入口的基线行为

| 入口 | 默认与失败边界 | 并发及所有权 |
| --- | --- | --- |
| `New`、`Register`、`Run` | 缺名字、缺监听、非法公布主机、不支持的注册协议、非正关停预算在构造返回错误；默认关停预算 10 秒。重复注册或绑定不完整返回错误；无服务启动失败。启动后再次 Run 或 Register 失败。 | 注册与首次启动的提交点由应用锁保护；一个应用只运行一次。运行期间监听归应用，RegistryProvider 仍由创建者管理。 |
| `NewClient`、`Invoke`、`Close` | 默认 gRPC、3 秒补充期限；显式调用期限优先。缺注册中心、未知或不支持的协议、非正预算在构造失败。默认以 Proto 全名发现，显式 `Name` 可覆盖。仅 Proto 标明幂等或无副作用时允许另一次传输尝试。 | Client 支持并发调用并复用连接；Close 取消正在进行的操作并释放连接，重复 Close 返回同一结果。注入观测器和注册中心由创建者关闭，需要支持相应并发调用。 |
| `Observability`、`Metrics`、`Tracing`、`Diagnostics` | 默认无诊断监听、指标和追踪，日志走 `slog.Default`。观测组合的空值保留旧设置，独立 Metrics/Tracing 的 nil 清除，Diagnostics 的空地址禁用监听。客户端仅使用观测器、日志等客户端选项。 | 按传入顺序生效，运行期无动态重配置。指标登记失败先于监听，保留原因。App 持有探针监听，调用方持有观察器、抓取 handler、日志输出与 exporter。 |

## 问题、机会与待验证项

| 类型 | 项目与实际消费者 | 后续验收 |
| --- | --- | --- |
| 已确认缺陷 | Order 使用的 `Observability` 同时选择写入和抓取；混合使用 `Metrics` 可以让登记及写入切到新实例，抓取继续暴露旧实例。 | 用同一生成绑定调用并抓取指标，覆盖两类选项顺序、显式抓取和禁用。 |
| 优化机会 | Order 独立项目已有公开模块验收；开发者仍需要协调安装的生成器、运行时版本和 Buf 模板参数。 | 固定既有独立项目路径，比较改造前后必填概念、命令、配置和人工接线。 |
| 优化机会 | Assistant 的 HTTP、SSE 与 gRPC 流使用底层 App。Order 使用统一应用但只接受两种一元监听。 | 先用同一真实业务验证元数据、错误、取消、排空与观测，再提炼重复部分。 |
| 待验证风险 | Order 的既有真实 etcd 分区和恢复验收覆盖业务调用。大量实例的恢复峰值、过载、慢请求、长期资源趋势与观测后端中断仍缺规模证据。 | 用 Linux 记录负载模型、容量、延迟、失败率、资源及恢复后业务结果，再决定是否增加机制。[SRE 监控实践](https://sre.google/sre-book/monitoring-distributed-systems/) |
| 待验证风险 | 公开源码伪版本安装和 CI 已验证。独立消费者按两版运行时和生成器升级的组合证据仍待补齐。 | 用公开 Go 模块做独立安装、重新生成、运行和升级，保留实际版本与回退路径。 |
