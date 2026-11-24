# 一元服务脚手架

## 使用者与目标

仓库中新服务的作者需要从 Proto 契约开始生成业务入口和类型化调用方。现有 Order 用例证明双协议接线可复用。脚手架把声明和执行入口固化为一个可以构建的最小示例，新业务从独立契约演进。[Order 统一装配](unary-service-api.md)

从仓库根目录运行 `go run ./platform/verify scaffold warehouse`。名称使用 1 到 32 个小写 ASCII 字母或数字，以字母开头，并遵守 Go 包名规则。工具只建立 `examples/warehouse`，其中包含独立的 Buf 配置、版本化 Proto、公共绑定、私有 Go 绑定、一个共享实现和两个命令。新服务的 Go 代码由根模块维护，沿用平台的 Go 依赖边界。

最小示例提供 `Ping` 方法。服务端通过 `kronos.New`、生成绑定、`Register` 和 `Run` 使用现有引擎。两协议接入同一实现。客户端以生成类型调用，默认 gRPC，显式可选 MicroRPC。启动入口配置 etcd，持有注册中心和客户端的关闭责任。

## 文件、失败与演进

- 暂存文件、Buf lint 和生成完成后，才取得目的目录。目的目录已存在时返回明确错误。创建失败回收本次临时文件和不完整输出。
- 目录内的 `buf.yaml` 和 `buf.gen.yaml` 随服务提交。`generate` 与 `proto` 发现 `examples/<名称>/buf.yaml` 后执行它的 lint、生成和产物检查。原有三个参考服务继续使用各自模板。
- `breaking` 检查已有服务的 Proto 快照。新增服务可以加入基线，后续同名服务按 Buf FILE 规则演进。移除已进入基线的服务需按版本迁移处理。[公开契约兼容](../adr/0010-public-contract-compatibility.md)
- 脚手架使用标准库和现有 Buf、Proto 插件。生成的业务和命令使用公开 Kronos 与协议类型。传输 SDK 限于生成的私有绑定。

## 验收

在 Linux 使用锁定的 Go 和 Buf 从空的服务目录生成示例。核对目标冲突时保留原文件，新服务可通过相同的 lint、重新生成和依赖规则，客户端与服务端均可构建。用实际 etcd 运行生成服务，标准 gRPC 和显式 MicroRPC 都返回样例业务值。变更一个已发布字段的类型应触发兼容门禁。

## 实测

- Go 1.25.0、Buf 1.70.0、Ubuntu 24.04 Linux arm64 与官方 etcd v3.6.16，运行 `GOWORK=off go -C platform/verify test -count=1 -run '^TestScaffoldBuildsFreshService$' -v ./...`。在隔离工作树中生成带数字名称的新服务，重生成的公开和私有绑定与原文件一致。两个实际命令通过 etcd 找到实例，分别返回 `仓库-收到回执`，服务随后正常退出。[生成、构建与调用](../../platform/verify/scaffold_test.go)
- macOS arm64 使用同一 Go 和 Buf 运行 `GOWORK=off go -C platform/verify test -race -count=2 -run '^Test(Scaffold|Breaking)' -v ./...`。两轮通过，验证目标文件保留，独立模块的字段变更及模块丢失能被兼容检查发现。真实工作树的协议生成和 `breaking be671e1` 通过。[独立模块兼容](../../platform/verify/compatibility_test.go)
