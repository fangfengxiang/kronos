# 原生 Go 验收入口

## 消费者与契约

开发者和 GitHub CI 从仓库根目录使用 `go run ./platform/verify`。独立 Go 模块 `platform/verify` 统一调用仓库验收工具，功能断言留在各包的 Go 测试和 `Example`。

| 参数 | 行为 |
| --- | --- |
| `test` | 读取 `go.work` 中的模块，在各模块关闭工作区并执行 `go test -race -count=1 ./...`。 |
| `deps` | 逐模块读取 `go list` 的直接依赖，执行层级矩阵、私有传输边界和已接入 SDK 的归属约束。 |
| `proto` | 校验锁定的 Buf 版本、协议格式、lint 和生成产物。生成产物先写入临时目录，按相对路径和内容比对。 |
| `generate` | 使用锁定的 Buf 版本和各服务的生成模板更新协议产物。 |
| `breaking <基线提交>` | 将当前 Proto 与指定 Git 快照对照。Buf FILE 规则检查线协议、JSON 和生成代码的兼容性。 |
| `scaffold <名称>` | 在 `examples/<名称>` 创建可编译的一元服务、标准与兼容入口、类型化客户端和独立 Buf 配置。 |

任一检查失败时返回非零退出码，输出定位失败的模块、导入或生成文件。参考服务的私有绑定和生成件使用 Kratos 与 gRPC，业务和命令代码通过 Kronos 与服务协议接入。

go-micro v2 的双向互通单独运行 `GOWORK=off go test -tags=interop -count=1 ./test/interop/microv2`。Assistant 的容器端到端验收运行 `GOWORK=off go test -tags=e2e -count=1 ./test/e2e/assistant`。两项测试均有自己的 Go 夹具和断言。[旧协议互通](go-micro-v2-interop.md) · [Compose 验收](assistant-compose.md)

协议变更前运行 `go run ./platform/verify breaking HEAD` 检查工作树。CI 用推送前或 PR 目标提交校验实际变更。临时 Git 仓库覆盖新增字段和破坏原有字段的用例。Go 消费者测试编译公开装配、绑定、调用及关闭签名。[公开契约兼容规则](../adr/0010-public-contract-compatibility.md)

## Order 治理验收

本地与 CI 使用隔离的真实 etcd v3。GitHub Actions 在 Ubuntu 24.04 启动固定摘要的官方镜像，将容器端口映射到随机宿主端口。通过 Go 集成测试的线性一致读确认 etcd 已能处理请求。三条测试命令使用同一个 `KRONOS_ETCD_TEST_ADDRESS`，动态服务名隔离各个场景。[GitHub Actions 服务容器](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idservices) · [治理契约](service-governance.md)

```sh
GOWORK=off go -C infra/etcd test -tags=etcd_integration -count=1 -v ./...
GOWORK=off go test -tags=interop,etcd_interop -count=1 -v ./test/interop/microv2
GOWORK=off go -C platform/verify test -count=1 -run '^Test(Scaffold|IndependentProject)BuildsFreshService$' -v ./...
```

三条命令依次验证真实租约、Order 及官方 go-micro v2 双向互通、仓库内脚手架与独立业务模块生成的全新服务。上游 SDK 在隔离的临时 Go 模块内构建。第三条使用锁定的 Buf 版本，基于同一个真实 etcd 运行生成的客户端和服务端。首次运行需要访问 Go 模块代理，或准备可用的缓存。[官方互通夹具](../../test/interop/microv2/testdata/etcdregistry/main.go) · [脚手架实测](service-scaffold.md#实测)

### 仓库外版本验收

`main` 推送的治理任务把该次提交传给版本验收。测试在临时业务模块关闭工作区，通过 Go 模块代理取得运行时、注册中心和生成器，随后用同一个真实 etcd 完成两种调用和停机。本地已准备 etcd 时，可用以下命令复跑指定版本。

```sh
KRONOS_PUBLISHED_REVISION="$kronos_revision" GOWORK=off \
  go -C platform/verify test -count=1 -run '^TestPublishedProjectBuildsFreshService$' -v ./...
```

与其他治理用例一样，用 `KRONOS_ETCD_TEST_ADDRESS` 指定隔离实例。[调用测试](../../platform/verify/scaffold_test.go) · [设计与取舍](service-scaffold.md#仓库外业务项目)

### etcd 双向 TLS 验收

使用隔离的官方 etcd 并开启 `client-cert-auth`。设置 `KRONOS_ETCD_TEST_TLS_ENDPOINT` 和 Order 的 CA、证书、密钥路径后，运行 `GOWORK=off go test -tags=etcd_interop -count=1 -run '^TestOrderWithMutualTLSEtcd$' -v ./test/interop/microv2`。该用例核对可信客户端能注册和发现，CA 不匹配或缺少客户端证书时查询失败，两种 RPC 协议均能完成业务调用和关停注销。[证书输入](../guide/README.md#连接启用-tls-的-etcd) · [Go 进程测试](../../test/interop/microv2/etcd_tls_test.go)

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
- Go 1.25.0、Ubuntu 24.04 Linux arm64 执行 Order 两条治理命令、`go run ./platform/verify deps`、`go run ./platform/verify test` 和 `go run ./platform/verify proto`，全部通过。Assistant 的原生 Go Compose 验收通过，运行实际 Linux 容器。macOS arm64 经宿主随机回环端口执行两条真实 etcd 测试并加 `-race`，均通过。用 actionlint v1.7.12 校验工作流。使用 OCI 清单确认固定的 etcd 镜像摘要含 Linux amd64 和 arm64。[治理测试](service-governance.md#实测记录) · [工作流](../../.github/workflows/ci.yml)
- 运行 `GOWORK=off GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$output_directory" ./examples/order/cmd/order ./examples/order/cmd/order-client` 构建两个发布命令。在 Docker 的 Linux amd64 仿真环境启动这两个二进制与官方 etcd v3.6.16，使用默认配置调用 `-id 订单-amd64` 与 `-protocol micro-v2 -id 订单-amd64`。两次均返回 `订单-amd64 confirmed`，服务正常退出且进程状态为 0。[Order 命令](../guide/README.md#运行-order)
- Go 1.25.0、Ubuntu 24.04 Linux amd64 仿真环境从空的 `GOMODCACHE` 和 `GOCACHE` 运行租约与双向互通测试。Go 从公开模块代理下载并验证依赖，两版官方 go-micro v2 均完成构建和双向业务调用，断连与崩溃用例通过。随后完整套件三轮通过，追加验证三实例下的重试上限和同一客户端的恢复。[重试实测](discovered-rpc-client.md#实测) · [恢复实测](service-governance.md#实测记录)
- Go 1.25.0、Buf 1.70.0 在 Ubuntu 24.04 Linux arm64 运行 `GOWORK=off go -C platform/verify test -run '^TestBreakingChecksThePublishedFieldContract$' -v ./...`、`go run ./platform/verify breaking be671e1` 和 `go run ./platform/verify proto`。新增字段被接受，改变已有字段类型被拒绝。实际协议与已上线基线兼容，生成检查通过。外部 Go 签名夹具通过竞态编译，工作流通过 actionlint。[兼容用例](../../platform/verify/compatibility_test.go) · [外部 Go 签名](../../platform/kronos/public_test.go)
