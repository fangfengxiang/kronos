# 框架

承载服务装配、gRPC 与 HTTP 接入、共享调用策略和应用生命周期。按已选的协议与 Kratos 传输契约连接同一个应用实现。

[`framework/rpc`](rpc) 是独立模块，提供一元客户端、协议选项、公开错误和装配消费的服务端接口。[一元 RPC 防腐层](../docs/design/rpc-boundary.md) · [RPC 客户端协议](../docs/design/rpc-client-protocol.md)

[`framework/localization`](localization) 是独立模块，统一 HTTP、gRPC 和 SSE 的语言偏好权重与回退入口。[框架语言协商](../docs/design/framework-localization.md)

[`framework/app`](app) 是独立模块，负责监听器所有权、就绪与有界关停，支持进程间监听器接力。Assistant 命令显式装配四种监听。[应用装配与关停](../docs/design/app-lifecycle.md) · [优雅重载](../docs/design/graceful-reload.md)

通过真实入口验证授权、错误与取消。生命周期验收覆盖部分启动失败、就绪、摘流和有界退出。[架构概览](../docs/architecture/overview.md) · [应用生命周期](../docs/architecture/lifecycle.md)
