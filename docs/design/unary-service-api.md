# 统一一元服务装配

## 应用入口

Order 命令通过 `platform/kronos` 组合发现名、显式注入的 registry 及独立的 `GRPC`、`MicroRPC` 监听。部署也可启用 `HTTP`，由 Proto 提供路由。用 `app.Register(order.BindOrderService(implementation))` 注册一份类型化业务实现。生成函数从 Proto 取得服务、方法及消息类型。业务实现接收标准上下文及业务消息。命令将信号上下文交给 `app.Run`，并在运行结束后关闭它创建的 registry。[统一 HTTP 监听](unified-http.md)

`kronos.New` 验证装配选项，`Register` 核对 Protobuf 描述符和方法签名。应用只能运行一次，只能在运行前注册。空服务、无监听、重复服务及不兼容方法应在接入连接前报告。直接使用同一个 TCP 地址时，现有生命周期负责检测绑定冲突并释放资源。配置 `:0` 可独立申请端口。

## 协议与发现

- gRPC 服务名和全方法名来自原始 Protobuf 描述符。使用 Kratos 的 gRPC 传输和 grpc-go 的服务注册、编解码、拦截器。go-micro v2 通过现有 `infra/microv2` 处理信封，兼容端点为 Proto 短服务名与方法名拼接，例如 `OrderService.GetOrder`。兼容协议的 `Micro-Service` 使用显式发现名。
- 公开调用类型为 Kronos 与 Protobuf，SDK 类型留在传输实现。服务端用同一个有类型的回调处理三路一元入口。沿用 `rpc.Error`、取消和期限语义，并用现有请求恢复守卫捕获业务 panic。
- 绑定就绪后，应用将实际端口和通告主机交给发布钩子。显式绑定可达的地址可以直接通告。通配监听默认通告本地回环，容器或远端部署用 `AdvertiseHost` 指定调用方可达的主机。应用先撤销就绪、撤销注册，再排空。缓存节点新请求在应用入口被拒绝。[生命周期](app-lifecycle.md) · [分协议地址](../adr/0009-discovery-protocols.md)

生成器在开启选项的服务上增加类型化装配函数。既有 Assistant 和 TextBackend 生成策略继续沿用各自模板。当前统一入口接收一元服务。流式传输使用 Assistant 的既有装配路径，依据实际消费扩展。[gRPC Go 服务注册](https://pkg.go.dev/google.golang.org/grpc@v1.82.1#ServiceRegistrar) · [Protobuf 公开契约](protobuf-go-contract.md)

## 通过条件

- 一个 Order 实现经两个不同的实际端口返回业务数据。使用标准 gRPC 生成客户端与兼容 Protobuf 客户端分别调用。
- 使用真实 etcd 观察 Order 就绪后发布的两个实际端口。用官方 go-micro v2 客户端通过服务名发现并调用，发送停止信号后验证注销。
- 命令核心装配不超过 30 行，完整业务与命令编译通过依赖边界检查。原生测试覆盖非法装配、错误映射及正常关停。Proto 重新生成与已提交文件一致。

## 实测

- Order 的 `run` 为 22 行。原生 Go 测试校验行数上限，并核对绑定缺失、重复服务和 Protobuf 消息不匹配的失败。[Order 命令](../../examples/order/cmd/order/main.go) · [装配测试](../../examples/order/cmd/order/main_test.go) · [注册测试](../../examples/order/service_test.go)
- Go 1.25.0、Ubuntu 24.04 Linux arm64，运行 `GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2`。以真实 etcd v3.6.16 启动 Order 子进程。原生 gRPC 和 v2.0.0、v2.9.1 官方客户端通过各自地址收到同一订单及预期的无效 ID 错误。官方旧客户端通过服务名、以两种编码调用，Registry 只返回兼容端口。发送 `SIGTERM` 后进程以状态零退出，两个地址均消失。[Order 进程验收](../../test/interop/microv2/etcd_test.go)
- Linux 的 `go run ./platform/verify proto` 和 `go run ./platform/verify deps` 通过。macOS arm64 的全工作区 Go 竞态测试通过。Assistant 的 Linux Compose 四入口和关停回归通过。[原生 Go 验收](go-verification.md)
