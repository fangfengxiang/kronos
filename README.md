# Kronos

Kronos 是一个面向全球应用的、协议优先、云原生、AI 原生、多语言的 Go RPC 框架。

以 Kratos 为运行底座，由 Protobuf 定义服务、方法、出入参及流事件。gRPC 是主入口，按协议声明提供 HTTP 映射。Kronos 负责应用装配、生命周期和可跨传输验证的调用约定。参考应用首版覆盖 Agent 调用与流式输出，以及中英文语言协商和回退。

## 仓库结构

当前以 [核心原语](core/README.md)、[基础设施](infra/README.md)、[框架](framework/README.md)、[平台](platform/README.md) 四层起步。每个独立封装包拥有自己的 `go.mod`。[开发工作区](go.work) 串联各组件与参考应用。[层与模块的决定](docs/adr/0004-layer-and-module-boundaries.md)

## 文档入口

| 文档 | 负责的内容 |
| --- | --- |
| [架构概览](docs/architecture/overview.md) | 说明框架、传输、平台和业务的职责与依赖方向 |
| [首版落地设计](docs/design/first-vertical-slice.md) | 界定参考应用范围、公开行为、实施顺序和验收 |
| [Assistant v1 协议](docs/design/assistant-protocol.md) | 约定公开入口、流终态、错误标识与生成验收 |
| [Go 公开契约生成](docs/design/protobuf-go-contract.md) | 说明公开 Protobuf 类型、一元服务接口和生成验收 |
| [原生 Go 验收](docs/design/go-verification.md) | 说明跨模块测试、业务依赖边界与协议生成检查 |
| [一元 RPC 防腐层](docs/design/rpc-boundary.md) | 说明业务接口、Kratos 适配、期限与错误转换 |
| [RPC 请求处理恢复](docs/design/rpc-handler-recovery.md) | 说明一元和流处理异常、稳定错误及安全诊断 |
| [调用独立文本后端](docs/design/text-backend-rpc.md) | 说明双进程 gRPC 直连与调用验收 |
| [Consul 服务注册与订阅](docs/design/consul-service-registration.md) | 说明健康实例发布、地址变化与注销 |
| [动态发现文本后端](docs/design/text-backend-discovery.md) | 说明 gRPC 按服务名调用、实例轮询和撤销路由 |
| [MySQL 建池与文本配置](docs/design/mysql-text-profiles.md) | 说明建池、事务、上下文及 TextBackend 的配置读取 |
| [Redis 缓存与回源](docs/design/redis-text-profile-cache.md) | 说明单机和 Cluster 的 TTL、原子回填及故障回源 |
| [MongoDB 类型化文档](docs/design/mongodb-text-profiles.md) | 说明按 ID 读取、原子替换及 TextBackend 的文档配置 |
| [OSS 对象与文本配置](docs/design/oss-text-profiles.md) | 说明对象读写、临时下载授权及 TextBackend 的对象配置 |
| [Kafka 消息与生成事件](docs/design/kafka-generation-events.md) | 说明 broker 确认、消费位点、TLS/SASL 及事件接线 |
| [RPC 客户端协议选择](docs/design/rpc-client-protocol.md) | 说明标准 gRPC 与 go-micro v2 的调用选项和互通验收 |
| [标准 gRPC 流客户端](docs/design/rpc-stream-client.md) | 说明公开流事件、终态确认和取消传播 |
| [请求元数据](docs/design/request-metadata.md) | 说明请求标识、上下文传播和兼容监听行为 |
| [结构化日志](docs/design/structured-logging.md) | 说明请求关联、日志安全字段和真实进程验收 |
| [请求指标](docs/design/request-metrics.md) | 说明 Prometheus 抓取、跨传输统计和标签范围 |
| [请求链路追踪](docs/design/request-tracing.md) | 说明 W3C 传播、OTLP HTTP 导出和停机清理 |
| [按需运行时快照](docs/design/runtime-profiles.md) | 说明从独立 loopback 监听获取 Go heap 与 goroutine 快照 |
| [框架语言协商](docs/design/framework-localization.md) | 说明 HTTP、gRPC 和 SSE 的语言偏好与文案回退 |
| [Blades 文本 Agent 适配](docs/design/blades-adapter.md) | 说明模型装配、文本增量、失败与取消 |
| [应用装配与关停](docs/design/app-lifecycle.md) | 说明监听器所有权、探针与关停验收 |
| [Assistant 容器交付](docs/design/assistant-compose.md) | 说明 Compose 配置、四入口、健康探针与进程退出 |
| [启动配置文件](docs/design/config-file.md) | 说明启动参数、文件快照和候选进程重新读取行为 |
| [应用优雅重载](docs/design/graceful-reload.md) | 说明进程接力、候选就绪和旧进程排空 |
| [SSE 与 go-micro v2 监听](docs/design/additional-listeners.md) | 说明独立监听、流事件和旧协议互通范围 |
| [go-micro v2 原生 Go 互通验收](docs/design/go-micro-v2-interop.md) | 说明两版官方客户端和服务端的双向对照测试 |
| [架构决策](docs/adr/README.md) | 记录已对齐的方向、取舍及验证要求 |
| [框架使用文档](docs/guide/README.md) | 汇总服务定义、生成、测试和部署流程 |
| [仓库协作约定](AGENTS.md) | 说明文档先行、改动边界和交付要求 |

## 交付状态

Core 包提供 goroutine 执行、语言匹配、消息查找和 IANA 时区转换。Assistant 通过 Blades 执行确定性文本模型，使用 Docker Compose 挂载配置并运行 HTTP、gRPC、SSE 与 go-micro v2 四个监听。Linux 容器已验证探针、实际调用、启动失败和进程关停。[Compose 使用指南](docs/guide/README.md#通过-docker-compose-运行)

框架内置 Skill 将随参考应用交付，指导使用者定义协议、生成代码、接线、测试和部署。经验证的参考应用可进一步提炼为可复制的服务模板。

[Core 首版落地设计与验收](docs/design/core-v1.md)
