# 架构决策

| 记录 | 决策范围 |
| --- | --- |
| [0001 Protobuf 与传输](0001-protobuf-and-transport.md) | 协议权威、Kratos 接入与 HTTP 映射 |
| [0002 Agent 流协议](0002-agent-stream.md) | Blades 的适配边界、流事件与取消 |
| [0003 全球化与多语言](0003-localization.md) | 语言协商、资源归属和回退 |
| [0004 职责层与 Go 模块边界](0004-layer-and-module-boundaries.md) | 四层目录、独立封装模块和依赖方向 |
| [0005 go-micro v2 兼容监听](0005-legacy-rpc-listener.md) | 专用监听、协议适配与旧客户端验收 |
| [0006 进程级优雅重载](0006-graceful-reload.md) | TCP 监听继承、候选就绪与旧进程排空 |
| [0007 Consul 实例注册与健康地址订阅](0007-consul-service-registration.md) | 注册生命周期、健康视图和空快照 |

决定优先写清使用者、备选方案、适用前提和验收。尚待确认的格式或默认值在各记录中标明，实施时以可复查的证据更新状态。
