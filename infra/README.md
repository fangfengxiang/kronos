# 基础设施

承载外部配置源、注册发现、观测后端、存储、消息与模型供应商等适配。每个适配明确输入、外部语义转换、资源取得和释放。

由实际调用方定义需要的窄接口，再在应用入口接入具体适配。优先复用 Kratos 或上游提供的能力，新增封装须有明确受益的调用方。

[`infra/aliyunoss`](aliyunoss) 提供独立桶客户端、对象下载、上传及 V4 临时下载 URL。[OSS 对象与文本配置](../docs/design/oss-text-profiles.md)

[`infra/blades`](blades) 是独立 Agent 适配模块，提供单次提示词的一元结果与文本增量，管理失败和取消后的收尾。[Blades 文本 Agent 适配](../docs/design/blades-adapter.md)

[`infra/configfile`](configfile) 是独立配置源模块，提供类型化的本地 JSON 快照读取，由消费入口定义配置字段和覆盖规则。[启动配置文件](../docs/design/config-file.md)

[`infra/consul`](consul) 提供 gRPC 实例注册、注销、健康地址查询和可取消的订阅。[Consul 服务注册与订阅](../docs/design/consul-service-registration.md)

[`infra/etcd`](etcd) 提供标准 gRPC 与 go-micro v2 的分协议实例发布、发现、续约和摘除。[服务治理](../docs/design/service-governance.md)

[`infra/kafka`](kafka) 提供同步确认的 Kafka 发布、消费组和处理成功后的位点提交，支持 TLS 与 SASL PLAIN。[Kafka 消息与生成事件](../docs/design/kafka-generation-events.md)

[`infra/microv2`](microv2) 是独立兼容模块，提供 go-micro v2 的入站一元 Protobuf 适配和出站 `proto-rpc` 直连。[兼容监听设计](../docs/design/additional-listeners.md) · [RPC 客户端协议](../docs/design/rpc-client-protocol.md)

[`infra/mongodb`](mongodb) 提供独立连接、类型化文档读取、原子替换和删除。[MongoDB 类型化文档](../docs/design/mongodb-text-profiles.md)

[`infra/mysql`](mysql) 建立并校验独立的标准 MySQL 连接池，提供稳定的建池和重复键错误语义。[MySQL 建池与文本配置](../docs/design/mysql-text-profiles.md)

[`infra/oteltrace`](oteltrace) 复用 Kratos OTel tracer 与 SDK，通过标准上下文记录请求 span 并向 Collector 导出。[请求链路追踪](../docs/design/request-tracing.md)

[`infra/prometheus`](prometheus) 提供独立请求计数、延迟直方图和标准 HTTP 抓取处理器。[请求指标](../docs/design/request-metrics.md)

[`infra/redis`](redis) 提供 Redis 单机与 Cluster 的字符串读取、条件写入、TTL 和删除。[Redis 缓存与回源](../docs/design/redis-text-profile-cache.md)

[`infra/runtimeprofile`](runtimeprofile) 在独立回环监听上提供 Go 堆采样和 goroutine 调用栈快照。[按需运行时快照](../docs/design/runtime-profiles.md)
