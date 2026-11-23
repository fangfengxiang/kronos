# 原生 Go 验收入口

## 消费者与契约

开发者和 GitHub CI 从仓库根目录使用 `go run ./platform/verify`。独立 Go 模块 `platform/verify` 统一调用仓库验收工具，功能断言留在各包的 Go 测试和 `Example`。

| 参数 | 行为 |
| --- | --- |
| `test` | 读取 `go.work` 中的模块，在各模块关闭工作区并执行 `go test -race -count=1 ./...`。 |
| `deps` | 逐模块读取 `go list` 的直接依赖，执行层级矩阵、私有传输边界和已接入 SDK 的归属约束。 |
| `proto` | 校验锁定的 Buf 版本、协议格式、lint 和生成产物。生成产物先写入临时目录，按相对路径和内容比对。 |
| `generate` | 使用锁定的 Buf 版本和各服务的生成模板更新协议产物。 |

任一检查失败时返回非零退出码，输出定位失败的模块、导入或生成文件。参考服务的私有绑定和生成件使用 Kratos 与 gRPC，业务和命令代码通过 Kronos 与服务协议接入。

go-micro v2 的双向互通单独运行 `GOWORK=off go test -tags=interop -count=1 ./test/interop/microv2`。Assistant 的容器端到端验收运行 `GOWORK=off go test -tags=e2e -count=1 ./test/e2e/assistant`。两项测试均有自己的 Go 夹具和断言。[旧协议互通](go-micro-v2-interop.md) · [Compose 验收](assistant-compose.md)

## 外部 SDK 归属

已接入 SDK 的导入前缀由对应适配模块持有。业务包可以使用 Kronos 模块、标准库以及公开 Protobuf 契约。参考服务的私有 `internal/binding` 与 `internal/wire` 负责 Kratos 和 gRPC 接线。Kronos 内部的 app、log、追踪和性能监听也有明确的 Kratos 导入归属。旧 go-micro SDK 由独立的互通夹具持有。[导入规则](../../platform/verify/dependencies.go) · [模块与依赖边界](../adr/0004-layer-and-module-boundaries.md)

沿用 `go list` 的生产包 `Imports`，检查当前构建平台参与编译的直接导入。前缀按完整路径段匹配，覆盖同一 SDK 的子包和主版本路径。通用依赖继续正常使用，接入新的外部 SDK 时在验证入口登记适配归属。测试可通过 `TestImports` 与 `XTestImports` 直接使用官方 SDK 做原生对照，生产导入检查仍单独执行。[Go 包列表命令](https://pkg.go.dev/cmd/go@go1.25.0#hdr-List_packages_or_modules)

## 边界与取舍

- 参考服务将协议生成集中在固定入口，真实调用另用 Go `testing` 验证。沿用固定接线与原生测试。模块枚举和导入分析直接使用 `go work edit -json` 和 `go list -json`，Go 测试使用 `go test`。[Go 工作区命令](https://go.dev/ref/mod#go-work-edit) · [Go 包列表命令](https://pkg.go.dev/cmd/go#hdr-List_packages_or_modules) · [可执行示例](https://go.dev/blog/examples)
- Buf 与 Go 生成器仍按原定版本和模板执行。Go 工具负责命令调度与生成文件比对。模块本身使用标准库，已有 Buf 安装和 CI 运行条件见使用指南。[Buf 生成命令](https://buf.build/docs/reference/cli/buf/generate/)

## 通过条件

- 使用实际工作区核对逐模块测试通过、依赖校验通过、协议产物与重新生成一致。Linux 上执行 Go 验收入口并保留实测结果。
- Go 测试注入禁止的跨层导入、业务层 Kratos 和厂商 SDK 导入，以及修改、缺失和多出的生成文件，验证能识别差异。适配包和测试专用导入按各自规则通过。
- CI 与使用指南调用原生 Go 入口。文档示例能通过 `go test` 执行。

## 实测与复盘

- 使用 Go 1.25.0、Buf 1.70.0 在 Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C platform/verify test -count=1 -v ./...`、`go run ./platform/verify deps` 和 `go run ./platform/verify proto`。分层、生成文件路径与内容检查通过。Linux 环境使用 Go 本地模块代理缓存完成协议生成。[导入边界测试](../../platform/verify/dependencies_test.go) · [生成文件测试](../../platform/verify/protocol_test.go)
- macOS arm64 上运行 `go run ./platform/verify test`，根模块与十二个独立模块分别通过竞态测试。`go run ./platform/verify generate` 重建的协议文件与已提交的内容一致。`go run ./platform/verify deps`、`go run ./platform/verify proto` 以及验证模块 `go vet` 也通过。
- 第一轮复盘提高了依赖检查的粒度。原有矩阵继续约束四层，Assistant 生产包的直接导入单独核对。相同前缀的普通目录也受检查，只有实际适配和生成目录允许传输库。[可执行配置示例](../../infra/configfile/example_test.go)
- 修复前，隔离工作区的 `go list` 检查漏掉 TextBackend 命令对 Kafka SDK 的直接导入。Go 1.25.0、Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C platform/verify test -count=3 ./...` 和 `go run ./platform/verify deps` 通过。macOS arm64 的验证模块竞态三轮及实际仓库的依赖检查通过。原生夹具验证允许适配包的 SDK 导入和测试对照，拒绝命令层直接导入。[导入边界回归](../../platform/verify/dependencies_test.go)
