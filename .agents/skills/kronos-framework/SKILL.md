---
name: kronos-framework
description: 在 Kronos 仓库新增 Go 服务或消费公开接口时，按 Protobuf 契约完成生成、应用装配、Go 验收和 Linux 参考部署。
---

# Kronos 框架用法

从仓库根目录定位现有服务和 [仓库协作约定](../../../AGENTS.md)。按任务选择 [使用指南](../../../docs/guide/README.md) 和相关设计。

## 协议与装配

- 先在服务的 `api/` 修改 Protobuf。方法、出入参与流事件由协议定义，HTTP 路由通过注解声明。需要手工接入的多传输服务，将其 Proto 路径、生成模板和验收目标登记到 [Buf 工作区](../../../buf.yaml)、[生成模板](../../../tools/proto/) 和 [协议验收入口](../../../platform/verify/protocol.go)。
- 用 Go 1.25.0 及 Buf 1.70.0 从根目录执行 `go run ./platform/verify generate` 和 `go run ./platform/verify proto`。提交协议、公开 Go 契约和私有传输产物。 [公开契约生成](../../../docs/design/protobuf-go-contract.md)
- 业务实现和调用方使用服务公开 Go 契约、Kronos 接口和标准库类型。Kratos 接线留在私有绑定。Assistant 用 `framework/app.Binding` 显式交接多种监听器和运行依赖。独立封装声明自己的 `go.mod` 并登记到 `go.work`。 [模块边界](../../../docs/adr/0004-layer-and-module-boundaries.md) · [应用生命周期](../../../docs/design/app-lifecycle.md)
- 新增一元服务运行 `go run ./platform/verify scaffold warehouse`。在 `examples/warehouse/api/` 修改契约。服务自己的 Buf 配置和启用 `unary_bindings=true` 的生成模板由验收入口自动发现。 [新建一元服务](../../../docs/guide/README.md#新建一元服务)
- Order 展示了完整的运行装配。应用入口用 `kronos.New` 和生成的绑定函数显式注册业务。调用方将 `kronos.NewClient` 交给生成客户端，默认 gRPC，显式 `kronos.Protocol(kronos.ProtocolMicroV2)` 选择旧协议。入口创建和关闭注入的 etcd 实例。 [Order 快速开始](../../../docs/guide/README.md#运行-order)
- 仓库外一元服务从空目录运行已验收版本的 `verify scaffold-project <Go 模块路径> <Kronos 提交>`。使用同一提交的运行时、注册中心与 Buf 生成器。源码联调可使用独立 Go 工作区。 [仓库外接入](../../../docs/guide/README.md#仓库外开发一元服务)
- 以 `kronos.Registry` 注入注册中心。etcd 支持 gRPC 与 MicroRPC 两路发布及私有 CA、客户端证书。Consul 使用 passing 健康结果提供标准 gRPC 发现。 [etcd TLS](../../../docs/guide/README.md#连接启用-tls-的-etcd) · [Consul 接入](../../../docs/guide/README.md#使用-consul-接入一元服务)
- 一元 HTTP 用 `kronos.HTTP(address)` 启用。Proto 注解决定静态 GET 或整条消息的 POST 路由。同一生成绑定服务 HTTP 和 RPC。 [统一 HTTP 监听](../../../docs/design/unified-http.md)
- `kronos.Observability` 接入探针、指标、Trace 与实例日志。指标方法取自 Proto 绑定，创建者在应用排空后关闭 exporter。 [观测装配](../../../docs/design/order-diagnostics.md#集中装配与所有权)
- Assistant 可参考 HTTP、标准 gRPC、SSE 和 go-micro v2 四入口。`rpc.WithProtocol(rpc.ProtocolMicroV2)` 用于显式连接旧一元 RPC。保留 Assistant 的多传输装配与 TextBackend 的底层发现接线作为高级用例。 [客户端选项](../../../docs/design/rpc-client-protocol.md) · [文本后端调用](../../../docs/design/text-backend-rpc.md) · [动态发现](../../../docs/design/text-backend-discovery.md)

## 验收与部署

- 用 Go 的 `testing` 与 `Example` 验证真实入口。运行 `go run ./platform/verify deps` 核对导入边界，再运行 `go run ./platform/verify test` 做独立模块竞态回归。单独测试模块可在该模块目录执行 `GOWORK=off go test ./...`。 [原生 Go 验收](../../../docs/design/go-verification.md)
- 修改既有公开协议时运行 `go run ./platform/verify breaking HEAD` 核对 Git 基线。 [兼容规则](../../../docs/adr/0010-public-contract-compatibility.md)
- Order 的治理验收使用隔离的真实 etcd。从仓库根目录执行租约测试、`-tags=interop,etcd_interop` 双向业务测试和脚手架的真实调用测试。推送后的版本用独立环境执行发布态验收。使用私有 CA 时可运行双向 TLS 专项。 [Order 治理验收](../../../docs/design/go-verification.md#order-治理验收)
- 在 Linux 验证变更使用的后端和进程行为。默认 Assistant Compose 的 Go 验收是 `go test -tags=e2e -count=1 ./test/e2e/assistant`，覆盖健康探针、真实调用、启动异常和退出。组件设计记录外部服务的验收前提与平台结果。 [Compose 交付](../../../docs/design/assistant-compose.md) · [部署和组件用法](../../../docs/guide/README.md)
