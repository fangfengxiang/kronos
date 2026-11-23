# 基础设施

承载外部配置源、注册发现、观测后端、存储、消息与模型供应商等适配。每个适配明确输入、外部语义转换、资源取得和释放。

由实际调用方定义需要的窄接口，再在应用入口接入具体适配。优先复用 Kratos 或上游提供的能力，新增封装须有明确受益的调用方。

[`infra/blades`](blades) 是独立 Agent 适配模块，提供单次提示词的一元结果与文本增量，管理失败和取消后的收尾。[Blades 文本 Agent 适配](../docs/design/blades-adapter.md)

[`infra/configfile`](configfile) 是独立配置源模块，提供类型化的本地 JSON 快照读取，由消费入口定义配置字段和覆盖规则。[启动配置文件](../docs/design/config-file.md)

[`infra/microv2`](microv2) 是独立兼容模块，提供 go-micro v2 的入站一元 Protobuf 适配和出站 `proto-rpc` 直连。[兼容监听设计](../docs/design/additional-listeners.md) · [RPC 客户端协议](../docs/design/rpc-client-protocol.md)

[`infra/prometheus`](prometheus) 提供独立请求计数、延迟直方图和标准 HTTP 抓取处理器。[请求指标](../docs/design/request-metrics.md)
