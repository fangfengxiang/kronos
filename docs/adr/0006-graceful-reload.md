# ADR 0006 进程级优雅重载

**状态。** 采纳 Cloudflare `tableflip v1.2.3`，按应用配置启用。

单机运行的服务需要在同一地址完成进程更替。tableflip 提供通用 `net.Listener` 接口，适用于同一应用的 HTTP、gRPC、SSE 和 go-micro v2 监听。Cloudflare 公开了该库在其 Go 项目中的使用方式。[上游项目](https://github.com/cloudflare/tableflip) · [Cloudflare 工程实践](https://blog.cloudflare.com/graceful-upgrades-in-go/)

`framework/app` 直接用 `ListenWithCallback` 继承监听，用 `Upgrade` 启动候选并等候就绪或处理启动失败，用 `Ready` 和 `Exit` 确认交接。Kronos 汇总应用就绪状态和关停预算，命令入口持有信号，Kratos 负责在途连接排空。[上游 API](https://pkg.go.dev/github.com/cloudflare/tableflip@v1.2.3) · [模块依赖](../../framework/app/go.mod) · [接入源码](../../framework/app/reload.go) · [重载契约与验收](../design/graceful-reload.md)

Linux 是重载的首要部署平台。tableflip v1.2.3 在 Windows 上不支持进程级重载，Windows 部署使用普通启动路径。[平台支持](../design/graceful-reload.md#平台支持) · [上游平台说明](https://pkg.go.dev/github.com/cloudflare/tableflip@v1.2.3#hdr-Overview)

先关闭再重绑会制造监听空窗。单独使用 `SO_REUSEPORT` 时，两代都可能在候选完整就绪前接流。继承现有监听器可复用同一个监听队列，并以应用就绪作为摘流条件。[tableflip](https://pkg.go.dev/github.com/cloudflare/tableflip@v1.2.3)
